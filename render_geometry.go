// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"encoding/binary"
	"slices"

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

// gpuGeometry is a geometry's uploaded primitives and the instance sets that
// draw them: one set for each draw of the frame, reused across frames.
type gpuGeometry struct {
	sh            gpuBatch
	sprite        gpuBatch
	pointRevision uint64
	skin          *gpuSkin
	batches       [9]gpuBatch // Same primitive slots as gpuRenderer; slot 7 is textured triangles, slot 8 drop discs.
	sets          []*instanceSet
	used          int           // Sets drawn from this frame.
	order         *wgpu.Buffer  // Alpha points' order, farthest first, as last sorted.
	view          nt3dmath.Mat4 // The matrix it was sorted under.
	sorted        bool
}

func (n *gpuGeometry) release() {
	n.skin.release()
	releaseBatch(n.sprite)
	releaseBatch(n.sh)
	for _, b := range n.batches {
		releaseBatch(b)
	}
	for _, s := range n.sets {
		s.release()
	}
	if n.order != nil {
		n.order.Release()
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
	last      []transformState // What the buffer holds.
}

func (s *instanceSet) release() {
	for _, b := range s.binds {
		if b != nil {
			b.Release()
		}
	}
	if s.transform != nil {
		s.transform.Release()
	}
}

// Two matrices, outline color/width, and two optical parameter vectors.
const instanceRecordSize = 176

// transformState is what an instance's transform record holds.
type transformState struct {
	optics                            [8]float32
	outline                           OutlineStyle
	unlit                             bool
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
	for i := range f.Geometry {
		keys[i] = frameGeometryKey(f, i)
		used[keys[i]] = true
	}
	for key, node := range g.geometries {
		if !used[key] {
			node.release()
			delete(g.geometries, key)
		}
		node.used = 0
	}
	type groupKey struct {
		outline  OutlineStyle
		geometry geometryKey
		texture  *Texture
		normal   *Texture
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
		if node == nil {
			node = &gpuGeometry{}
			for kind, data := range geometryData(geom) {
				record := 48
				if kind == 0 {
					record = pointRecord
				}
				if err := g.uploadData(dev, &node.batches[kind], data, record); err != nil {
					node.release()
					return err
				}
			}
			if extended(geom) {
				node.batches[7].count = uint32(len(geom.Indices))
			}
			if geom.PointStyle.Blend == AlphaBlend && len(geom.Points) > 0 {
				var err error
				if node.order, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: 4 * uint64(len(geom.Points)), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst}); err != nil {
					node.release()
					return err
				}
			}
			if geom.pointUpdate != nil {
				node.pointRevision = geom.pointUpdate.revision
			}
			if err := g.uploadData(dev, &node.sh, gaussianSHData(&geom), 4); err != nil {
				node.release()
				return err
			}
			if err := g.uploadData(dev, &node.sprite, spriteData(geom.PointStyle, geom.Points), 48); err != nil {
				node.release()
				return err
			}
			g.geometries[keys[i]] = node
			g.uploads++
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
			node.pointRevision, node.sorted = revision, false
		}
		g.nodes[i] = node
		if !drawn[i] {
			continue
		}
		if transparent(&f.Geometry[i]) || blendedPoints(&f.Geometry[i]) {
			groups = append(groups, &group{keys[i], []int{i}, true})
			continue
		}
		k := groupKey{geometry: keys[i], outline: geom.Outline}
		if mapped(geom) {
			k.texture = baseMap(geom.Material)
			k.normal = normalMap(geom.Material)
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
		if err := writeTransforms(dev, set, f, grp.members); err != nil {
			return err
		}
		if grp.blended {
			g.blendDraw[grp.members[0]] = len(g.draws)
			if err := g.sortPoints(dev, node, f, grp.members[0]); err != nil {
				return err
			}
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
		layout, entries := g.bindLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: uniformSize}, {Binding: 1, Buffer: b.buffer, Size: b.size}, {Binding: 2, Buffer: next.transform, Size: size}}
		if kind == 0 {
			order, orderSize := g.zeros, uint64(4)
			if node.order != nil {
				order, orderSize = node.order, 4*uint64(b.count)
			}
			layout, entries = g.pointBindLayout, []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: uniformSize}, {Binding: 1, Buffer: g.zeros, Size: 48}, {Binding: 2, Buffer: next.transform, Size: size}, {Binding: 3, Buffer: b.buffer, Size: b.size}, {Binding: 4, Buffer: order, Size: orderSize}}
			sh, shSize := g.zeros, uint64(48)
			if node.sh.buffer != nil {
				sh, shSize = node.sh.buffer, node.sh.size
			}
			entries = append(entries, wgpu.BindGroupEntry{Binding: 5, Buffer: sh, Size: shSize})
			if node.sprite.buffer != nil {
				entries[1] = wgpu.BindGroupEntry{Binding: 1, Buffer: node.sprite.buffer, Size: node.sprite.size}
			}
		}
		if next.binds[kind], err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: layout, Entries: entries}); err != nil {
			next.release()
			return nil, err
		}
	}
	next.capacity = capacity
	*s = next
	return s, nil
}

// writeTransforms writes a draw's instance transforms, when they changed.
func writeTransforms(dev *wgpu.Device, s *instanceSet, f Frame, members []int) error {
	states := make([]transformState, len(members))
	for k, i := range members {
		states[k].world = instance(f, i).World
		states[k].optics = opticalParams(f.Geometry[i].Material, f.Geometry[i].PointStyle)
		states[k].outline = f.Geometry[i].Outline
		states[k].unlit = f.Geometry[i].Material != nil && f.Geometry[i].Material.Unlit
		if m := f.Geometry[i].Material; m != nil && m.Specular > 0 && !m.Unlit {
			states[k].specular, states[k].shininess = m.Specular, m.shininess()
		}
		if m := f.Geometry[i].Material; m != nil && !m.Unlit {
			states[k].reflectivity = m.Reflectivity
		}
		if mapped(f.Geometry[i]) {
			states[k].procedural = baseMap(f.Geometry[i].Material).shader != ""
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
		data = append(data, transformRecord(st.world, st.specular, st.shininess, st.reflectivity, st.procedural, true, st.outline, st.unlit, st.extension, st.optics)...)
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
func transformRecord(world nt3dmath.Mat4, specular, shininess, reflectivity float32, procedural, visible bool, outline OutlineStyle, unlit bool, extension uint32, optics ...[8]float32) []byte {
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
	data = appendFloats(data, float32(c.R)/255, float32(c.G)/255, float32(c.B)/255, outline.Width)
	var o [8]float32
	if len(optics) > 0 {
		o = optics[0]
	}
	return appendFloats(data, o[:]...)
}

func writeTransform(dev *wgpu.Device, buffer *wgpu.Buffer, v Instance, m *Material) error {
	var specular, shininess, reflectivity float32
	if m != nil && m.Specular > 0 && !m.Unlit {
		specular, shininess = m.Specular, m.shininess()
	}
	if m != nil && !m.Unlit {
		reflectivity = m.Reflectivity
	}
	return dev.Queue().WriteBuffer(buffer, 0, transformRecord(v.World, specular, shininess, reflectivity, false, v.Visible, OutlineStyle{}, m != nil && m.Unlit, 0))
}

// sortPoints orders a geometry's alpha points farthest first for the view
// they are drawn in, when that changed since they were last sorted.
func (g *gpuRenderer) sortPoints(dev *wgpu.Device, node *gpuGeometry, f Frame, i int) error {
	if node.order == nil {
		return nil
	}
	m := f.Matrix.Mul(instance(f, i).World)
	if node.sorted && node.view == m {
		return nil
	}
	g.orderBytes = g.orderBytes[:0]
	for _, k := range g.sorter.sort(f.Geometry[i].Points, &m) {
		g.orderBytes = binary.LittleEndian.AppendUint32(g.orderBytes, k)
	}
	if err := dev.Queue().WriteBuffer(node.order, 0, g.orderBytes); err != nil {
		return err
	}
	node.view, node.sorted = m, true
	return nil
}

// uploadData uploads a geometry batch's records, to be bound per instance set.
func (g *gpuRenderer) uploadData(dev *wgpu.Device, b *gpuBatch, data []byte, record int) error {
	if len(data) == 0 {
		return nil
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

func geometryData(geom Geometry) (data [9][]byte) {
	// Each record is twelve floats; each kind is sized once, so encoding
	// allocates per kind rather than per record.
	const record = 12 * 4
	var records [9]int
	records[0], records[2], records[3], records[6] = len(geom.Points), len(geom.Boxes), len(geom.Lines), len(geom.Arrows)
	if mapped(geom) {
		records[7] = len(geom.Indices)
	} else {
		records[1] = len(geom.Indices)
	}
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
	blended := transparent(&geom)
	for _, index := range geom.Indices {
		v := geom.Vertices[index]
		// Opaque triangles are wholly opaque; transparent ones carry their
		// vertices' alpha, where a mesh's radius or a texture's cutout
		// threshold would be.
		opacity := float32(1)
		if blended {
			opacity = float32(v.Color.A) / 255
		}
		if mapped(geom) {
			// The texture gives color, so the color slot carries the cutout
			// threshold (or a transparent vertex's opacity), flags (1 repeat,
			// 2 edge-on, 4 tint, 8 extension records follow, 16 they carry
			// tangent frames), and, for a
			// tint, the vertex color as a 24-bit integer, which a float
			// holds exactly.
			m, flags, cut := geom.Material, 0, geom.Material.threshold()
			if m.NormalRepeat {
				flags |= 32
			}
			if extended(geom) {
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
			data[7] = appendFloats(data[7], v.Position.X, v.Position.Y, v.Position.Z, uv.U, v.Normal.X, v.Normal.Y, v.Normal.Z, uv.V, cut, float32(flags), rgb, strength)
		} else {
			vertex(1, v, opacity)
		}
	}
	// A normal-mapped mesh with tangents or independent normal UVs has an
	// extension record for each index after the primary ones: the normal
	// map's coordinates, and the tangent frame with its handedness in w
	// (zero without tangents).
	if extended(geom) {
		// Flow is fixed in the rest UV field. Resolve it once per vertex, not once
		// per repeated triangle index, when streaming a large animated grid.
		var flows []UV
		if geom.Material.FlowMap != nil {
			flows = make([]UV, len(geom.Vertices))
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
			data[7] = appendFloats(data[7], uv.U, uv.V, flow.U, flow.V, t.Direction.X, t.Direction.Y, t.Direction.Z, t.W, 0, 0, 0, 0)
		}
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

func spriteData(style PointStyle, points []Point) []byte {
	if style.Texture == nil && style.Shape != GaussianPoint {
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

func dropShadowData(g *Geometry) []byte {
	a, b := g.Bounds.Min, g.Bounds.Max
	fixed := float32(0)
	if g.DropShadow.FixedFloor {
		fixed = 1
	}
	return appendFloats(nil, a.X, a.Y, a.Z, g.DropShadow.Strength, b.X, b.Y, b.Z, g.DropShadow.Radius, g.DropShadow.FloorZ, fixed, 0, 0)
}

// SH storage is separate from point/axis records so ordinary points pay no
// extra GPU memory and 500k degree-three splats stay below 128 MiB per buffer.
func gaussianSHData(g *Geometry) []byte {
	if len(g.GaussianSH) == 0 {
		return nil
	}
	data := make([]byte, 0, 48+len(g.GaussianSH)*12)
	m := gaussianSHMatrix(g)
	data = appendFloats(data, m[0], m[1], m[2], m[4], m[5], m[6], m[8], m[9], m[10])
	count := gaussianSHCount(g)
	active := count
	if g.PointStyle.SHDCOnly {
		active = 1
	}
	data = appendFloats(data, float32(count), float32(active), 0)
	for _, c := range g.GaussianSH {
		data = appendFloats(data, c[:]...)
	}
	return data
}
