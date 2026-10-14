// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
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
	batches [8]gpuBatch // Same primitive slots as gpuRenderer; slot 7 is textured triangles.
	sets    []*instanceSet
	used    int // Sets drawn from this frame.
}

func (n *gpuGeometry) release() {
	for _, b := range n.batches {
		releaseBatch(b)
	}
	for _, s := range n.sets {
		s.release()
	}
}

// instanceSet is one draw's instances of a geometry: their transforms, an
// array the mesh shaders index by instance, and bind groups pairing it with
// each of the geometry's batches. Draws cannot start mid-array (a nonzero
// first instance needs an optional feature), so each draw has its own.
type instanceSet struct {
	transform *wgpu.Buffer
	capacity  int
	binds     [8]*wgpu.BindGroup
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

// transformState is what an instance's transform record holds.
type transformState struct {
	world               nt3dmath.Mat4
	specular, shininess float32
}

// gpuDraw draws a run of a geometry's instances.
type gpuDraw struct {
	geom    *gpuGeometry
	set     *instanceSet
	count   uint32
	index   int  // The frame geometry of its first instance: material and texture.
	blended bool // One transparent instance, drawn in blending order.
}

func frameGeometryKey(f Frame, i int) geometryKey {
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
		geometry geometryKey
		texture  *Texture
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
				if err := g.uploadData(dev, &node.batches[kind], data); err != nil {
					node.release()
					return err
				}
			}
			g.geometries[keys[i]] = node
			g.uploads++
		}
		g.nodes[i] = node
		if !drawn[i] {
			continue
		}
		if transparent(&f.Geometry[i]) {
			groups = append(groups, &group{keys[i], []int{i}, true})
			continue
		}
		k := groupKey{geometry: keys[i]}
		if textured(geom) {
			k.texture = geom.Material.Texture
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
	s.release()
	*s = instanceSet{capacity: max(n, 2*s.capacity)}
	size := uint64(s.capacity) * 128
	var err error
	if s.transform, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: size, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst}); err != nil {
		return nil, err
	}
	for kind, b := range node.batches {
		if b.buffer == nil {
			continue
		}
		if s.binds[kind], err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.bindLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: uniformSize}, {Binding: 1, Buffer: b.buffer, Size: b.size}, {Binding: 2, Buffer: s.transform, Size: size}}}); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// writeTransforms writes a draw's instance transforms, when they changed.
func writeTransforms(dev *wgpu.Device, s *instanceSet, f Frame, members []int) error {
	states := make([]transformState, len(members))
	for k, i := range members {
		states[k].world = instance(f, i).World
		if m := f.Geometry[i].Material; m != nil && m.Specular > 0 && !m.Unlit {
			states[k].specular, states[k].shininess = m.Specular, m.shininess()
		}
	}
	if slices.Equal(states, s.last) {
		return nil
	}
	data := make([]byte, 0, 128*len(states))
	for _, st := range states {
		data = append(data, transformRecord(st.world, st.specular, st.shininess, true)...)
	}
	s.last = states
	return dev.Queue().WriteBuffer(s.transform, 0, data)
}

// transformRecord is an instance's world and normal matrices. The normal
// matrix only turns directions, so its last column is free: it carries the
// material's specular strength and exponent, and whether the node shows.
func transformRecord(world nt3dmath.Mat4, specular, shininess float32, visible bool) []byte {
	inv, _ := world.InverseAffine()
	normal := inv.Transpose()
	normal[12], normal[13], normal[14], normal[15] = specular, shininess, 0, 0
	if visible {
		normal[15] = 1
	}
	return append(floats(world[:]...), floats(normal[:]...)...)
}

func writeTransform(dev *wgpu.Device, buffer *wgpu.Buffer, v Instance, m *Material) error {
	var specular, shininess float32
	if m != nil && m.Specular > 0 && !m.Unlit {
		specular, shininess = m.Specular, m.shininess()
	}
	return dev.Queue().WriteBuffer(buffer, 0, transformRecord(v.World, specular, shininess, v.Visible))
}

// uploadData uploads a geometry batch's records, to be bound per instance set.
func (g *gpuRenderer) uploadData(dev *wgpu.Device, b *gpuBatch, data []byte) error {
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
	b.count, b.size = uint32(len(data)/48), uint64(len(data))
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
	b.bind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: g.bindLayout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: g.uniform, Size: uniformSize}, {Binding: 1, Buffer: b.buffer, Size: uint64(len(data))}, {Binding: 2, Buffer: transform, Size: 128}}})
	if err != nil {
		return err
	}
	b.count, b.size = uint32(len(data)/48), uint64(len(data))
	return nil
}

func geometryData(geom Geometry) (data [8][]byte) {
	// Each node binds one transform; records always reference its slot zero.
	vertex := func(kind int, v Vertex, radius float32) {
		data[kind] = append(data[kind], floats(v.Position.X, v.Position.Y, v.Position.Z, radius, v.Normal.X, v.Normal.Y, v.Normal.Z, 0, float32(v.Color.R)/255, float32(v.Color.G)/255, float32(v.Color.B)/255, 0)...)
	}
	for _, p := range geom.Points {
		vertex(0, Vertex{Position: p.Position, Color: p.Color}, p.Radius)
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
		if geom.Material != nil && geom.Material.Unlit {
			v.Normal.X = 0
			v.Normal.Y = 0
			v.Normal.Z = 0
		}
		if textured(geom) {
			// The texture gives color, so the color slot carries the cutout
			// threshold (or a transparent vertex's opacity), flags (1 repeat,
			// 2 edge-on, 4 tint), and, for a tint, the vertex color as a
			// 24-bit integer, which a float holds exactly.
			m, flags, cut := geom.Material, 0, geom.Material.threshold()
			if blended {
				cut = opacity
			}
			if m.Repeat {
				flags |= 1
			}
			if m.EdgeOn {
				flags |= 2
			}
			if m.Tint {
				flags |= 4
			}
			rgb := float32(int(v.Color.R)<<16 | int(v.Color.G)<<8 | int(v.Color.B))
			uv := geom.UV[index]
			data[7] = append(data[7], floats(v.Position.X, v.Position.Y, v.Position.Z, uv.U, v.Normal.X, v.Normal.Y, v.Normal.Z, uv.V, cut, float32(flags), rgb, 0)...)
		} else {
			vertex(1, v, opacity)
		}
	}
	for _, b := range geom.Boxes {
		vertex(2, Vertex{b.Min, b.Size, b.Color}, 1)
	}
	for _, v := range geom.Lines {
		vertex(3, v, lineWidth(geom.LineWidth))
	}
	for _, a := range geom.Arrows {
		data[6] = append(data[6], floats(a.Start.X, a.Start.Y, a.Start.Z, lineWidth(a.Width), a.End.X, a.End.Y, a.End.Z, a.HeadSize, float32(a.Color.R)/255, float32(a.Color.G)/255, float32(a.Color.B)/255, 0)...)
	}
	return
}
