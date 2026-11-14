// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"encoding/binary"
	"slices"
	"unsafe"

	"github.com/gogpu/wgpu"

	"nimbleterminal.dev/nt3d/math"
)

// Manual frames retain their frame-wide revision/index contract. Scene frames
// use stable node identities and independent geometry revisions, and shared
// geometry (Frame.Shared) its own identity, whatever nodes show it.
type geometryKey struct {
	node     NodeID
	revision uint64
	index    int
	manual   bool
	shared   uint64
}

type manualSignature struct {
	vertices *Vertex
	nVerts   int
	indices  *uint32
	nIndices int
	points   *Point
	nPoints  int
	boxes    *Box
	nBoxes   int
	lines    *Vertex
	nLines   int
	arrows   *Arrow
	nArrows  int
	material Material
	hasMat   bool
	style    PointStyle
	drop     DropShadow
}

func manualSig(geom Geometry) manualSignature {
	sig := manualSignature{
		vertices: unsafe.SliceData(geom.Vertices),
		nVerts:   len(geom.Vertices),
		indices:  unsafe.SliceData(geom.Indices),
		nIndices: len(geom.Indices),
		points:   unsafe.SliceData(geom.Points),
		nPoints:  len(geom.Points),
		boxes:    unsafe.SliceData(geom.Boxes),
		nBoxes:   len(geom.Boxes),
		lines:    unsafe.SliceData(geom.Lines),
		nLines:   len(geom.Lines),
		arrows:   unsafe.SliceData(geom.Arrows),
		nArrows:  len(geom.Arrows),
		style:    geom.PointStyle,
		drop:     geom.DropShadow,
	}
	if geom.Material != nil {
		sig.material = *geom.Material
		sig.hasMat = true
	}
	return sig
}

type sharedGeometry struct {
	key           geometryKey
	manual        manualSignature
	refs          int
	sh            gpuBatch
	sprite        gpuBatch
	axes, shBytes gpuBatch
	order         gpuBatch
	pointRevision uint64
	meshRevision  uint64
	skin          *gpuSkin
	batches       [9]gpuBatch
}

// gpuGeometry is a geometry's uploaded primitives and the instance sets that
// draw them: one set for each draw of the frame, reused across frames.
type gpuGeometry struct {
	sh            gpuBatch // Gaussians' SH header, and their float coefficients when Precise.
	sprite        gpuBatch // Sprite rects, or Precise Gaussians' float axes.
	axes, shBytes gpuBatch // Compact Gaussians' half axes and SH bytes.
	order         gpuBatch // Clustered Gaussians' Clusters.Order, which the sort keys ranges of.
	pointRevision uint64
	meshRevision  uint64
	creasesRev    uint64
	skin          *gpuSkin
	batches       [9]gpuBatch // Same primitive slots as gpuRenderer; slot 7 is textured triangles, slot 8 drop discs.
	sets          []*instanceSet
	used          int // Sets drawn from this frame.
	shared        *sharedGeometry
	// creases are the mesh's crease edges, by the bits of each crease angle
	// its instances have asked for: nodes that share the mesh may differ.
	// They live as long as the uploaded mesh; SetOutline does not replace it.
	creases map[uint32]*gpuCrease
}

// gpuCrease is a mesh's crease edges at one angle: two line records a segment.
type gpuCrease struct {
	batch gpuBatch
	used  uint64 // The crease frame a draw last used it in.
}

func (n *gpuGeometry) clearCreases() {
	for _, c := range n.creases {
		releaseBatch(c.batch)
	}
	n.creases = nil
	for _, s := range n.sets {
		for _, b := range s.creaseBinds {
			b.Release()
		}
		s.creaseBinds = nil
	}
}

func (n *gpuGeometry) release(shared *gpuShared) {
	for _, s := range n.sets {
		s.release()
	}
	n.sets = nil
	for _, c := range n.creases {
		releaseBatch(c.batch)
	}
	n.creases = nil
	if n.shared != nil && shared != nil {
		shared.mu.Lock()
		n.shared.refs--
		if n.shared.refs > 0 {
			shared.mu.Unlock()
			return
		}
		if shared.geometries[n.shared.key] == n.shared {
			delete(shared.geometries, n.shared.key)
		}
		sg := n.shared
		shared.mu.Unlock()
		sg.skin.release()
		releaseBatch(sg.sprite)
		releaseBatch(sg.sh)
		releaseBatch(sg.axes)
		releaseBatch(sg.shBytes)
		releaseBatch(sg.order)
		for _, b := range sg.batches {
			releaseBatch(b)
		}
		return
	}
	n.skin.release()
	releaseBatch(n.sprite)
	releaseBatch(n.sh)
	releaseBatch(n.axes)
	releaseBatch(n.shBytes)
	releaseBatch(n.order)
	for _, b := range n.batches {
		releaseBatch(b)
	}
}

// instanceSet is one draw's instances of a geometry: their transforms, an
// array the mesh shaders index by instance, and bind groups pairing it with
// each of the geometry's batches. Draws cannot start mid-array (a nonzero
// first instance needs an optional feature), so each draw has its own.
type instanceSet struct {
	transform *wgpu.Buffer
	capacity  int
	binds     [9]*wgpu.BindGroup
	// creaseBinds pair the transforms with the geometry's crease edges.
	creaseBinds map[*gpuCrease]*wgpu.BindGroup
	last        []transformState // What the buffer holds.
	// The order and run table binds[0] holds: the sort's, of the
	// generation it was made in, or the stand-ins.
	order, runs *wgpu.Buffer
	generation  uint64
}

func (s *instanceSet) release() {
	for _, b := range s.binds {
		if b != nil {
			b.Release()
		}
	}
	for _, b := range s.creaseBinds {
		b.Release()
	}
	s.creaseBinds = nil
	if s.transform != nil {
		s.transform.Release()
	}
}

// Two matrices, outline color/width, two optical parameter vectors, two toon
// vectors (shade tint + rim, bands + softness), PBR parameters, and emissive.
const instanceRecordSize = 240

// transformState is what an instance's transform record holds.
type transformState struct {
	optics                            [8]float32
	pbr                               [4]float32
	emissive                          [3]float32
	outline                           OutlineStyle
	toon                              ToonStyle // Zero unless a lit mesh's material is toon.
	unlit                             bool
	envReflect                        bool
	world                             nt3dmath.Mat4
	specular, shininess, reflectivity float32
	procedural                        bool   // Its texture is drawn each frame: wrapped by hand.
	extension                         uint32 // Where its mesh's extension records start: its index count, or 0.
}

// gpuDraw draws a run of a geometry's instances.
type gpuDraw struct {
	geom    *gpuGeometry
	set     *instanceSet
	count   uint32
	index   int  // The frame geometry of its first instance: material and texture.
	blended bool // One instance with transparent triangles or blended points, drawn in blending order.
}

func frameGeometryKey(f Frame, i int) geometryKey {
	if p := f.Geometry[i].posed; p != nil {
		return geometryKey{shared: p.identity}
	}
	if len(f.Shared) != 0 && f.Shared[i] != 0 {
		return geometryKey{shared: f.Shared[i]}
	}
	if len(f.GeometryRevisions) != 0 {
		return geometryKey{node: f.Instances[i].Node, revision: f.GeometryRevisions[i]}
	}
	return geometryKey{revision: f.Revision, index: i, manual: true}
}

// prepareGeometry uploads geometry the cache lacks and groups the frame's
// drawn instances into draws: each geometry's opaque instances together,
// split only by texture, and each transparent instance alone, as blending
// order needs.
func (g *gpuRenderer) prepareGeometry(dev *wgpu.Device, f Frame, drawn []bool) error {
	if g.geometries == nil {
		g.geometries = make(map[geometryKey]*gpuGeometry)
	}
	keys := make([]geometryKey, len(f.Geometry))
	used := make(map[geometryKey]bool, len(f.Geometry))
	retainLOD := len(f.GeometryRevisions) != 0 || len(f.Shared) != 0
	for i := range f.Geometry {
		keys[i] = frameGeometryKey(f, i)
		used[keys[i]] = true
		if retainLOD {
			for _, k := range f.Geometry[i].retainKeys {
				used[k] = true
			}
		}
	}
	for key, node := range g.geometries {
		if !used[key] {
			node.release(g.shared)
			delete(g.geometries, key)
		}
		node.used = 0
	}
	type groupKey struct {
		outline      OutlineStyle
		geometry     geometryKey
		base         textureKey
		normal       textureKey
		orm          textureKey
		emissive     textureKey
		transmitting bool
	}
	type group struct {
		key     geometryKey
		members []int
		blended bool
	}
	var groups []*group
	byKey := map[groupKey]*group{}
	g.nodes = make([]*gpuGeometry, len(f.Geometry))
	for i, geom := range f.Geometry {
		node := g.geometries[keys[i]]
		if node == nil && g.shared != nil {
			g.shared.mu.Lock()
			if sg := g.shared.geometries[keys[i]]; sg != nil && (!keys[i].manual || sg.manual == manualSig(geom)) {
				sg.refs++
				node = &gpuGeometry{
					sh:            sg.sh,
					sprite:        sg.sprite,
					axes:          sg.axes,
					shBytes:       sg.shBytes,
					order:         sg.order,
					pointRevision: sg.pointRevision,
					meshRevision:  sg.meshRevision,
					creasesRev:    sg.meshRevision,
					skin:          sg.skin,
					batches:       sg.batches,
					shared:        sg,
				}
				g.geometries[keys[i]] = node
			}
			g.shared.mu.Unlock()
		}
		if node == nil {
			node = &gpuGeometry{}
			for kind, data := range geometryData(geom) {
				record := 48
				if kind == 0 {
					record = pointRecord
				}
				if err := g.uploadData(dev, &node.batches[kind], data, record); err != nil {
					node.release(nil)
					return err
				}
			}
			if extended(geom) {
				node.batches[7].count = uint32(len(geom.Indices))
			}
			if geom.pointUpdate != nil {
				node.pointRevision = geom.pointUpdate.revision
			}
			if geom.meshUpdate != nil {
				node.meshRevision = geom.meshUpdate.revision
				node.creasesRev = geom.meshUpdate.revision
			}
			if err := g.uploadData(dev, &node.sh, gaussianSHData(&geom), 4); err != nil {
				node.release(nil)
				return err
			}
			if err := g.uploadData(dev, &node.sprite, spriteData(geom.PointStyle, geom.Points), 48); err != nil {
				node.release(nil)
				return err
			}
			if compactGaussians(geom.PointStyle) {
				if err := g.uploadData(dev, &node.axes, compactAxisData(geom.Points), 4); err != nil {
					node.release(nil)
					return err
				}
			}
			if compactGaussians(geom.PointStyle) || geom.GaussianSHPalette != nil {
				if err := g.uploadData(dev, &node.shBytes, compactSHData(&geom), 4); err != nil {
					node.release(nil)
					return err
				}
			}
			if geom.Clusters != nil && geom.PointStyle.Blend == AlphaBlend {
				if err := g.uploadData(dev, &node.order, orderData(geom.Clusters.Order), 4); err != nil {
					node.release(nil)
					return err
				}
			}
			if g.shared != nil {
				sg := &sharedGeometry{
					key:           keys[i],
					refs:          1,
					sh:            node.sh,
					sprite:        node.sprite,
					axes:          node.axes,
					shBytes:       node.shBytes,
					order:         node.order,
					pointRevision: node.pointRevision,
					meshRevision:  node.meshRevision,
					batches:       node.batches,
				}
				if keys[i].manual {
					sg.manual = manualSig(geom)
				}
				node.shared = sg
				g.shared.mu.Lock()
				g.shared.geometries[keys[i]] = sg
				g.shared.mu.Unlock()
			}
			g.geometries[keys[i]] = node
			g.uploads++
		}
		if node.shared != nil {
			node.order = node.shared.order
			node.pointRevision = node.shared.pointRevision
			node.meshRevision = node.shared.meshRevision
			node.skin = node.shared.skin
		}
		if geom.Clusters == nil && node.order.buffer != nil {
			// A point update dropped the clusters the order was uploaded for.
			releaseBatch(node.order)
			node.order = gpuBatch{}
		} else if geom.Clusters != nil && geom.PointStyle.Blend == AlphaBlend && node.order.buffer == nil {
			// An older snapshot, from before that update, still has them.
			if err := g.uploadData(dev, &node.order, orderData(geom.Clusters.Order), 4); err != nil {
				return err
			}
		}
		u := geom.pointUpdate
		revision := uint64(0)
		if u != nil {
			revision = u.revision
		}
		if node.pointRevision != revision {
			first, end := 0, len(geom.Points)
			if u != nil && node.pointRevision == u.previous {
				first, end = u.first, u.end
			}
			data := geometryData(Geometry{Points: geom.Points[first:end], PointStyle: geom.PointStyle})[0]
			if err := dev.Queue().WriteBuffer(node.batches[0].buffer, uint64(first*pointRecord), data); err != nil {
				return err
			}
			if geom.DropShadow.Strength > 0 {
				data := geometryData(Geometry{Bounds: geom.Bounds, DropShadow: geom.DropShadow})[8]
				if err := dev.Queue().WriteBuffer(node.batches[8].buffer, 0, data); err != nil {
					return err
				}
			}
			if node.sprite.buffer != nil {
				if err := dev.Queue().WriteBuffer(node.sprite.buffer, uint64(first*48), spriteData(geom.PointStyle, geom.Points[first:end])); err != nil {
					return err
				}
			}
			if node.axes.buffer != nil {
				if err := dev.Queue().WriteBuffer(node.axes.buffer, uint64(first*24), compactAxisData(geom.Points[first:end])); err != nil {
					return err
				}
			}
			node.pointRevision = revision
		}
		mu := geom.meshUpdate
		meshRev := uint64(0)
		if mu != nil {
			meshRev = mu.revision
		}
		if node.creasesRev != meshRev {
			node.clearCreases()
			node.creasesRev = meshRev
		}
		if node.meshRevision != meshRev && len(geom.Indices) > 0 {
			slot := 1
			if mapped(geom) {
				slot = 7
			}
			g.meshScratch, g.flowScratch = appendMeshRecords(g.meshScratch[:0], g.flowScratch, geom)
			if err := dev.Queue().WriteBuffer(node.batches[slot].buffer, 0, g.meshScratch); err != nil {
				return err
			}
			if geom.DropShadow.Strength > 0 && geom.Bounds.IsValid() && node.batches[8].buffer != nil {
				if err := dev.Queue().WriteBuffer(node.batches[8].buffer, 0, dropShadowData(&geom)); err != nil {
					return err
				}
			}
			node.meshRevision = meshRev
		}
		if node.shared != nil {
			node.shared.order = node.order
			node.shared.pointRevision = node.pointRevision
			node.shared.meshRevision = node.meshRevision
		}
		g.nodes[i] = node
		if !drawn[i] {
			continue
		}
		if transparent(&f.Geometry[i]) || blendedPoints(&f.Geometry[i]) {
			groups = append(groups, &group{keys[i], []int{i}, true})
			continue
		}
		k := groupKey{geometry: keys[i], outline: geom.Outline, transmitting: transmitting(&geom)}
		if mapped(geom) {
			aniso := f.anisotropy()
			k.base = baseKey(geom.Material, aniso)
			k.normal = textureKey{src: normalMap(geom.Material), repeat: geom.Material.NormalRepeat, aniso: aniso}
			k.orm = textureKey{src: ormMap(geom.Material), repeat: geom.Material.ORMRepeat, aniso: aniso}
			k.emissive = textureKey{src: emissiveMap(geom.Material), repeat: geom.Material.EmissiveRepeat, aniso: aniso}
		}
		if grp := byKey[k]; grp != nil {
			grp.members = append(grp.members, i)
		} else {
			byKey[k] = &group{key: keys[i], members: []int{i}}
			groups = append(groups, byKey[k])
		}
	}
	g.draws, g.blendDraw = g.draws[:0], map[int]int{}
	for _, grp := range groups {
		node := g.geometries[grp.key]
		set, err := g.instanceSet(dev, node, len(grp.members))
		if err != nil {
			return err
		}
		// Alpha points draw from the frame's sort when it runs.
		geom := &f.Geometry[grp.members[0]]
		sorted := grp.blended && g.plan.points > 0 && blendedPoints(geom) && geom.PointStyle.Blend == AlphaBlend
		if err := g.bindPoints(dev, node, set, sorted); err != nil {
			return err
		}
		if err := writeTransforms(dev, set, f, grp.members); err != nil {
			return err
		}
		if grp.blended {
			g.blendDraw[grp.members[0]] = len(g.draws)
		}
		g.draws = append(g.draws, gpuDraw{node, set, uint32(len(grp.members)), grp.members[0], grp.blended})
	}
	// Sets no draw used this frame are freed.
	for _, node := range g.geometries {
		for _, s := range node.sets[node.used:] {
			s.release()
		}
		node.sets = node.sets[:node.used]
	}
	return nil
}

// instanceSet takes a geometry's next set for this frame, sized for n
// instances.
func (g *gpuRenderer) instanceSet(dev *wgpu.Device, node *gpuGeometry, n int) (*instanceSet, error) {
	if node.used == len(node.sets) {
		node.sets = append(node.sets, &instanceSet{})
	}
	s := node.sets[node.used]
	node.used++
	if s.capacity >= n {
		return s, nil
	}
	// The set is empty until its buffer and every bind group exist: one
	// that fails is tried again next frame, not drawn from half made.
	capacity := max(n, 2*s.capacity)
	s.release()
	*s = instanceSet{}
	next := instanceSet{}
	size := uint64(capacity) * instanceRecordSize
	var err error
	if next.transform, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: size, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst}); err != nil {
		return nil, err
	}
	for kind, b := range node.batches {
		if b.buffer == nil {
			continue
		}
		if kind == 0 {
			continue // Points bind by the frame's sort: bindPoints.
		}
		entries := []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: uniformSize}, {Binding: 1, Buffer: b.buffer, Size: b.size}, {Binding: 2, Buffer: next.transform, Size: size}}
		if next.binds[kind], err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.bindLayout, Entries: entries}); err != nil {
			next.release()
			return nil, err
		}
	}
	next.capacity = capacity
	*s = next
	return s, nil
}

// bindPoints makes a set's point bind group, unless the one it has binds
// the order and run table it should: the sort's for a sorted draw, the
// stand-ins otherwise. The sort's buffers are made again when they grow,
// so a group of an older generation, or over another run table, is too.
func (g *gpuRenderer) bindPoints(dev *wgpu.Device, node *gpuGeometry, s *instanceSet, sorted bool) error {
	b := node.batches[0]
	if b.buffer == nil {
		return nil
	}
	order, orderSize, runs := g.zeros, uint64(4), g.zeroRun
	if sorted {
		order, runs = g.sort.payload[g.sort.final], g.sort.runs
		orderSize = order.Size()
	}
	if s.binds[0] != nil && s.order == order && s.runs == runs && s.generation == g.sort.generation {
		return nil
	}
	if s.binds[0] != nil {
		s.binds[0].Release()
		s.binds[0] = nil
	}
	entries := []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: uniformSize}, {Binding: 1, Buffer: g.zeros, Size: 48}, {Binding: 2, Buffer: s.transform, Size: uint64(s.capacity) * instanceRecordSize}, {Binding: 3, Buffer: b.buffer, Size: b.size}, {Binding: 4, Buffer: order, Size: orderSize}}
	sh, shSize := g.zeros, uint64(4*shHeaderFloats)
	if node.sh.buffer != nil {
		sh, shSize = node.sh.buffer, node.sh.size
	}
	entries = append(entries, wgpu.BindGroupEntry{Binding: 5, Buffer: sh, Size: shSize}, wgpu.BindGroupEntry{Binding: 6, Buffer: runs, Size: 16})
	// Compact Gaussians' half axes and SH bytes; zeros for other points.
	for i, b := range []gpuBatch{node.axes, node.shBytes} {
		entry := wgpu.BindGroupEntry{Binding: uint32(7 + i), Buffer: g.zeros, Size: 48}
		if b.buffer != nil {
			entry.Buffer, entry.Size = b.buffer, b.size
		}
		entries = append(entries, entry)
	}
	if node.sprite.buffer != nil {
		entries[1] = wgpu.BindGroupEntry{Binding: 1, Buffer: node.sprite.buffer, Size: node.sprite.size}
	}
	bind, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.pointBindLayout, Entries: entries})
	if err != nil {
		return err
	}
	s.binds[0], s.order, s.runs, s.generation = bind, order, runs, g.sort.generation
	return nil
}

func pbrParams(m *Material) [4]float32 {
	if !pbrActive(m) {
		return [4]float32{}
	}
	return [4]float32{m.Metallic, m.Roughness, dielectricF0(m) + 1, occlusionStrength(m)}
}

// writeTransforms writes a draw's instance transforms, when they changed.
func writeTransforms(dev *wgpu.Device, s *instanceSet, f Frame, members []int) error {
	states := make([]transformState, len(members))
	for k, i := range members {
		m := f.Geometry[i].Material
		states[k].world = instance(f, i).World
		states[k].optics = opticalParams(m, f.Geometry[i].PointStyle)
		states[k].pbr = pbrParams(m)
		states[k].emissive = emissiveFactor(m, f.Linear)
		states[k].outline = f.Geometry[i].Outline
		states[k].unlit = m != nil && m.Unlit
		states[k].toon = toonParams(m)
		if m != nil && m.Specular > 0 && !m.Unlit {
			states[k].specular, states[k].shininess = m.Specular, m.shininess()
		}
		if m != nil && !m.Unlit {
			states[k].reflectivity = m.Reflectivity
			states[k].envReflect = m.EnvironmentReflect
		}
		if mapped(f.Geometry[i]) {
			states[k].procedural = baseMap(m).shader != ""
		}
		if extended(f.Geometry[i]) {
			states[k].extension = uint32(len(f.Geometry[i].Indices))
		}
	}
	if slices.Equal(states, s.last) {
		return nil
	}
	data := make([]byte, 0, instanceRecordSize*len(states))
	for _, st := range states {
		data = append(data, transformRecord(st.world, st.specular, st.shininess, st.reflectivity, st.procedural, true, st.outline, st.unlit, st.envReflect, st.extension, st.optics, st.toon, st.pbr, st.emissive)...)
	}
	s.last = states
	return dev.Queue().WriteBuffer(s.transform, 0, data)
}

// transformRecord is an instance's world and normal matrices. The normal
// matrix only turns directions, so its last column is free: it carries the
// material's specular strength and exponent, whether its texture is
// procedural, and whether the node shows. Its last row is free too: the
// unlit flag, reflection F0, and where the mesh's extension records start (its index
// count; the shader cannot trust arrayLength, which Metal reports from
// the allocation, not the binding).
func transformRecord(world nt3dmath.Mat4, specular, shininess, reflectivity float32, procedural, visible bool, outline OutlineStyle, unlit, envReflect bool, extension uint32, optics [8]float32, toon ToonStyle, pbr [4]float32, emissive [3]float32) []byte {
	inv, _ := world.InverseAffine()
	normal := inv.Transpose()
	normal[12], normal[13], normal[14], normal[15] = specular, shininess, 0, 0
	if procedural {
		normal[14] = 1
	}
	if visible {
		normal[15] = 1
	}
	// The normal matrix's unused last row carries the unlit flag.
	normal[3], normal[7], normal[11] = 0, float32(extension), reflectivity
	if unlit {
		normal[3] = 1
	}
	data := append(floats(world[:]...), floats(normal[:]...)...)
	c := outline.Color
	outlineW := outline.Width
	if outline.Units == WorldUnits {
		outlineW = -outlineW
	}
	data = appendFloats(data, float32(c.R)/255, float32(c.G)/255, float32(c.B)/255, outlineW)
	data = appendFloats(data, optics[:]...)
	shade := toon.shade()
	envRefl := float32(0)
	if envReflect {
		envRefl = 1
	}
	data = appendFloats(data, shade[0], shade[1], shade[2], toon.Rim, float32(toon.Bands), toon.Softness, envRefl, 0)
	return appendFloats(data, pbr[0], pbr[1], pbr[2], pbr[3], emissive[0], emissive[1], emissive[2], 0)
}

func writeTransform(dev *wgpu.Device, buffer *wgpu.Buffer, v Instance, m *Material) error {
	var specular, shininess, reflectivity float32
	if m != nil && m.Specular > 0 && !m.Unlit {
		specular, shininess = m.Specular, m.shininess()
	}
	if m != nil && !m.Unlit {
		reflectivity = m.Reflectivity
	}
	return dev.Queue().WriteBuffer(buffer, 0, transformRecord(v.World, specular, shininess, reflectivity, false, v.Visible, OutlineStyle{}, m != nil && m.Unlit, m != nil && !m.Unlit && m.EnvironmentReflect, 0, [8]float32{}, toonParams(m), pbrParams(m), emissiveFactor(m, false)))
}

// uploadData uploads a geometry batch's records, to be bound per instance set.
func (g *gpuRenderer) uploadData(dev *wgpu.Device, b *gpuBatch, data []byte, record int) error {
	if len(data) == 0 {
		return nil
	}
	if err := g.checkUpload(len(data)); err != nil {
		return err
	}
	var err error
	b.buffer, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: uint64(len(data)), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
	if err != nil {
		return err
	}
	if err = dev.Queue().WriteBuffer(b.buffer, 0, data); err != nil {
		return err
	}
	b.count, b.size = uint32(len(data)/record), uint64(len(data))
	return nil
}

func (g *gpuRenderer) uploadBatch(dev *wgpu.Device, b *gpuBatch, data []byte, transform *wgpu.Buffer) error {
	if len(data) == 0 {
		return nil
	}
	if err := g.checkUpload(len(data)); err != nil {
		return err
	}
	var err error
	b.buffer, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: uint64(len(data)), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
	if err != nil {
		return err
	}
	if err = dev.Queue().WriteBuffer(b.buffer, 0, data); err != nil {
		return err
	}
	b.bind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.bindLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: uniformSize}, {Binding: 1, Buffer: b.buffer, Size: uint64(len(data))}, {Binding: 2, Buffer: transform, Size: instanceRecordSize}}})
	if err != nil {
		return err
	}
	b.count, b.size = uint32(len(data)/48), uint64(len(data))
	return nil
}

func appendMeshRecords(dst []byte, flowScratch []UV, geom Geometry) ([]byte, []UV) {
	if len(geom.Indices) == 0 {
		return dst, flowScratch
	}
	needed := len(geom.Indices) * 48
	if extended(geom) {
		needed *= 2
	}
	if cap(dst)-len(dst) < needed {
		next := make([]byte, len(dst), len(dst)+needed)
		copy(next, dst)
		dst = next
	}
	blended := transparent(&geom)
	var outlineCodes []uint32
	if geom.posed == nil && geom.splitOutlines() {
		outlineCodes = outlineNormals(geom.Vertices)
	}
	isMapped := mapped(geom)
	isExt := isMapped && extended(geom)
	for _, index := range geom.Indices {
		v := geom.Vertices[index]
		var outlineCode uint32
		if outlineCodes != nil {
			outlineCode = outlineCodes[index]
		}
		// Opaque triangles are wholly opaque; transparent ones carry their
		// vertices' alpha, where a mesh's radius or a texture's cutout
		// threshold would be.
		opacity := float32(1)
		if blended {
			opacity = float32(v.Color.A) / 255
		}
		if isMapped {
			// The texture gives color, so the color slot carries the cutout
			// threshold (or a transparent vertex's opacity), flags (1 repeat,
			// 2 edge-on, 4 tint, 8 extension records follow, 16 they carry
			// tangent frames, and bits 6..23 carry any smooth outline normal),
			// and, for a tint, the vertex color as a 24-bit integer, which a
			// float holds exactly.
			m, flags, cut := geom.Material, int(outlineCode)<<6, geom.Material.threshold()
			if m.NormalRepeat {
				flags |= 32
			}
			if isExt {
				flags |= 8
				if len(geom.Tangents) > 0 {
					flags |= 16
				}
			}
			if blended {
				cut = opacity
			}
			if m.Repeat {
				flags |= 1
			}
			if m.EdgeOn && m.Texture != nil {
				flags |= 2
			}
			if m.Tint || m.Texture == nil {
				flags |= 4
			}
			rgb := float32(int(v.Color.R)<<16 | int(v.Color.G)<<8 | int(v.Color.B))
			strength := float32(0)
			if m.NormalMap != nil {
				strength = m.NormalStrength
			}
			uv := geom.UV[index]
			dst = appendFloats(dst, v.Position.X, v.Position.Y, v.Position.Z, uv.U, v.Normal.X, v.Normal.Y, v.Normal.Z, uv.V, cut, float32(flags), rgb, strength)
		} else {
			dst = appendFloats(dst, v.Position.X, v.Position.Y, v.Position.Z, opacity, v.Normal.X, v.Normal.Y, v.Normal.Z, 0, float32(v.Color.R)/255, float32(v.Color.G)/255, float32(v.Color.B)/255, float32(outlineCode))
		}
	}
	// A normal-mapped mesh with tangents or independent normal UVs has an
	// extension record for each index after the primary ones: the normal
	// map's coordinates, and the tangent frame with its handedness in w
	// (zero without tangents).
	if isExt {
		// Flow is fixed in the rest UV field. Resolve it once per vertex, not once
		// per repeated triangle index, when streaming a large animated grid.
		var flows []UV
		if geom.Material.FlowMap != nil {
			if cap(flowScratch) < len(geom.Vertices) {
				flowScratch = make([]UV, len(geom.Vertices))
			}
			flows = flowScratch[:len(geom.Vertices)]
			for i := range flows {
				flows[i] = materialFlow(geom.Material, geom.UV[i])
			}
		}
		for _, i := range geom.Indices {
			uv := geom.UV[i]
			if len(geom.NormalUV) > 0 {
				uv = geom.NormalUV[i]
			}
			var t Tangent
			if len(geom.Tangents) > 0 {
				t = geom.Tangents[i]
			}
			flow := UV{}
			if flows != nil {
				flow = flows[i]
			}
			dst = appendFloats(dst, uv.U, uv.V, flow.U, flow.V, t.Direction.X, t.Direction.Y, t.Direction.Z, t.W, 0, 0, 0, 0)
		}
	}
	return dst, flowScratch
}

func geometryData(geom Geometry) (data [9][]byte) {
	// Each record is twelve floats; each kind is sized once, so encoding
	// allocates per kind rather than per record.
	const record = 12 * 4
	var records [9]int
	records[0], records[2], records[3], records[6] = len(geom.Points), len(geom.Boxes), len(geom.Lines), len(geom.Arrows)
	for kind, n := range records {
		if n > 0 {
			size := record
			if kind == 0 {
				size = pointRecord
			}
			data[kind] = make([]byte, 0, n*size)
		}
	}
	if geom.DropShadow.Strength > 0 && geom.Bounds.IsValid() {
		data[8] = dropShadowData(&geom)
	}
	// Each node binds one transform; records always reference its slot zero.
	vertex := func(kind int, v Vertex, radius float32) {
		data[kind] = appendFloats(data[kind], v.Position.X, v.Position.Y, v.Position.Z, radius, v.Normal.X, v.Normal.Y, v.Normal.Z, 0, float32(v.Color.R)/255, float32(v.Color.G)/255, float32(v.Color.B)/255, 0)
	}
	// Points' radii are negative in world units.
	sign := float32(1)
	if geom.PointStyle.Units == WorldUnits {
		sign = -1
	}
	for _, p := range geom.Points {
		data[0] = appendFloats(data[0], p.Position.X, p.Position.Y, p.Position.Z, sign*p.Radius)
		data[0] = binary.LittleEndian.AppendUint32(data[0], uint32(p.Color.R)|uint32(p.Color.G)<<8|uint32(p.Color.B)<<16|uint32(p.Color.A)<<24)
	}
	if len(geom.Indices) > 0 {
		slot := 1
		if mapped(geom) {
			slot = 7
		}
		data[slot], _ = appendMeshRecords(nil, nil, geom)
	}
	for _, b := range geom.Boxes {
		vertex(2, Vertex{b.Min, b.Size, b.Color}, 1)
	}
	for _, v := range geom.Lines {
		vertex(3, v, lineWidth(geom.LineWidth))
	}
	for _, a := range geom.Arrows {
		data[6] = appendFloats(data[6], a.Start.X, a.Start.Y, a.Start.Z, lineWidth(a.Width), a.End.X, a.End.Y, a.End.Z, a.HeadSize, float32(a.Color.R)/255, float32(a.Color.G)/255, float32(a.Color.B)/255, 0)
	}
	return
}

// pointRecord is the bytes of a point's record: position and radius, four
// floats, and its color as RGBA8. Other records are twelve floats.
const pointRecord = 20

// spriteData is a sprite's atlas rect, or a Precise Gaussian's float axes,
// 48 bytes a point; compact Gaussians have none (compactAxisData).
func spriteData(style PointStyle, points []Point) []byte {
	if style.Shape == GaussianPoint && !style.Precise || style.Texture == nil && style.Shape != GaussianPoint {
		return nil
	}
	data := make([]byte, 0, len(points)*48)
	for _, p := range points {
		if style.Shape == GaussianPoint {
			for _, a := range p.GaussianAxes {
				data = appendFloats(data, a.X, a.Y, a.Z, 0)
			}
			continue
		}
		r := style.spriteRect(p)
		data = appendFloats(data, r[0], r[1], r[2], r[3], 0, 0, 0, 0, 0, 0, 0, 0)
	}
	return data
}

// compactGaussians reports whether a style's Gaussians upload compact
// records: half axes and SH bytes in place of floats.
func compactGaussians(style PointStyle) bool {
	return style.Shape == GaussianPoint && !style.Precise
}

// compactAxisData is the compact Gaussians' axes, 24 bytes a splat: the
// twelve halves of packGaussianAxes, little-endian, which vs_gaussian reads
// as six words, two halves each, the low half first.
func compactAxisData(points []Point) []byte {
	data := make([]byte, 0, len(points)*24)
	for _, p := range points {
		for _, h := range packGaussianAxes(p.GaussianAxes) {
			data = binary.LittleEndian.AppendUint16(data, h)
		}
	}
	return data
}

// orderData is a cluster set's point order as little-endian words, which
// keysClustered reads ranges of.
func orderData(order []uint32) []byte {
	data := make([]byte, 0, len(order)*4)
	for _, i := range order {
		data = binary.LittleEndian.AppendUint32(data, i)
	}
	return data
}

// compactSHData is binding 8's SH bytes. Compact Gaussians store a byte a
// channel as SPZ does (packSHDC for each splat's first coefficient,
// packSHBand for the rest), in the float coefficients' order and padded to
// whole words. A palette adds each splat's 16-bit label, little-endian and
// padded to whole words, at shLayout's labelsAt; compact palettes follow
// them as packSHBand bytes, centroid-major, at paletteAt. A Precise palette
// keeps only the labels here, at byte 0, and its floats in gaussianSHData.
func compactSHData(g *Geometry) []byte {
	compact, p := compactGaussians(g.PointStyle), g.GaussianSHPalette
	if len(g.GaussianSH) == 0 || !compact && p == nil {
		return nil
	}
	labelsAt, paletteAt := shLayout(g)
	size := labelsAt
	if p != nil {
		size = (2*len(g.Points)+3)&^3 + labelsAt
		if compact {
			size = (paletteAt + 3*len(p.Coefficients) + 3) &^ 3
		}
	}
	data := make([]byte, 0, size)
	if compact {
		count := gaussianSHCount(g)
		for i, c := range g.GaussianSH {
			for _, v := range c {
				if i%count == 0 {
					data = append(data, packSHDC(v))
				} else {
					data = append(data, packSHBand(v))
				}
			}
		}
		data = padWords(data)
	}
	if p == nil {
		return data
	}
	for _, l := range g.GaussianSHLabels {
		data = binary.LittleEndian.AppendUint16(data, l)
	}
	data = padWords(data)
	if compact {
		for _, c := range p.Coefficients {
			for _, v := range c {
				data = append(data, packSHBand(v))
			}
		}
		data = padWords(data)
	}
	return data
}

// padWords pads data with zeros to whole 4-byte words.
func padWords(data []byte) []byte {
	for len(data)%4 != 0 {
		data = append(data, 0)
	}
	return data
}

// shLayout is where a palette's labels and centroids sit: labelsAt is a
// byte offset in compactSHData; paletteAt is a byte offset there too for
// compact Gaussians, or a float index in gaussianSHData with Precise. Both
// are 0 without a palette.
func shLayout(g *Geometry) (labelsAt, paletteAt int) {
	if g.GaussianSHPalette == nil {
		return 0, 0
	}
	if !compactGaussians(g.PointStyle) {
		return 0, shHeaderFloats + 3*len(g.GaussianSH)
	}
	labelsAt = (3*len(g.GaussianSH) + 3) &^ 3
	return labelsAt, labelsAt + (2*len(g.Points)+3)&^3
}

func dropShadowData(g *Geometry) []byte {
	a, b := g.Bounds.Min, g.Bounds.Max
	fixed := float32(0)
	if g.DropShadow.FixedFloor {
		fixed = 1
	}
	return appendFloats(nil, a.X, a.Y, a.Z, g.DropShadow.Strength, b.X, b.Y, b.Z, g.DropShadow.Radius, g.DropShadow.FloorZ, fixed, 0, 0)
}

// shHeaderFloats is the SH header's length: 16 floats, 64 bytes.
const shHeaderFloats = 16

// SH storage is separate from point/axis records so ordinary points pay no
// extra GPU memory and 500k degree-three splats stay below 128 MiB per buffer.
// Every Gaussian geometry has the 64-byte header, with or without SH:
// [0..8] the SH-from-local 3×3; [9] RGB entries a point in GaussianSH (1
// with a palette); [10] coefficients evaluated (gaussianSHTotal, or 1 under
// SHDCOnly); [11] 1 for the compact records, 0 for the float ones; [12] the
// palette's centroid count (0 without one); [13] and [14] shLayout's
// labelsAt and paletteAt (exact as floats: compact offsets are whole words
// under 2²⁶ bytes, and a Precise float index stays under 2²⁴); [15] 0. Only
// Precise geometries' float coefficients follow it, and then a Precise
// palette's.
func gaussianSHData(g *Geometry) []byte {
	if g.PointStyle.Shape != GaussianPoint || len(g.Points) == 0 {
		return nil
	}
	compact, p := compactGaussians(g.PointStyle), g.GaussianSHPalette
	size := 4 * shHeaderFloats
	if !compact {
		size += len(g.GaussianSH) * 12
		if p != nil {
			size += len(p.Coefficients) * 12
		}
	}
	data := make([]byte, 0, size)
	m := gaussianSHMatrix(g)
	data = appendFloats(data, m[0], m[1], m[2], m[4], m[5], m[6], m[8], m[9], m[10])
	active := gaussianSHTotal(g)
	if g.PointStyle.SHDCOnly {
		active = 1
	}
	flag := float32(0)
	if compact {
		flag = 1
	}
	labelsAt, paletteAt := shLayout(g)
	data = appendFloats(data, float32(gaussianSHCount(g)), float32(active), flag, float32(p.Count()), float32(labelsAt), float32(paletteAt), 0)
	if compact {
		return data
	}
	for _, c := range g.GaussianSH {
		data = appendFloats(data, c[:]...)
	}
	if p != nil {
		for _, c := range p.Coefficients {
			data = appendFloats(data, c[:]...)
		}
	}
	return data
}
