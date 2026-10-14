// Copyright (c) 2026 Neomantra Corp.
package nt3d

import "github.com/gogpu/wgpu"

// Manual frames retain their frame-wide revision/index contract. Scene frames
// use stable node identities and independent geometry revisions.
type geometryKey struct {
	node     NodeID
	revision uint64
	index    int
	manual   bool
}
type gpuGeometry struct {
	batches     [8]gpuBatch // Same primitive slots as gpuRenderer; slot 7 is textured triangles.
	transform   *wgpu.Buffer
	last        Instance
	transformed bool
}

func (n *gpuGeometry) release() {
	for _, b := range n.batches {
		releaseBatch(b)
	}
	if n.transform != nil {
		n.transform.Release()
	}
}
func frameGeometryKey(f Frame, i int) geometryKey {
	if len(f.GeometryRevisions) != 0 {
		return geometryKey{node: f.Instances[i].Node, revision: f.GeometryRevisions[i]}
	}
	return geometryKey{revision: f.Revision, index: i, manual: true}
}

func (g *gpuRenderer) prepareGeometry(dev *wgpu.Device, f Frame) error {
	if g.geometries == nil {
		g.geometries = make(map[geometryKey]*gpuGeometry)
	}
	used := make(map[geometryKey]bool, len(f.Geometry))
	for i := range f.Geometry {
		used[frameGeometryKey(f, i)] = true
	}
	for key, node := range g.geometries {
		if !used[key] {
			node.release()
			delete(g.geometries, key)
		}
	}
	g.nodes = make([]*gpuGeometry, len(f.Geometry))
	for i, geom := range f.Geometry {
		key := frameGeometryKey(f, i)
		node := g.geometries[key]
		if node == nil {
			node = &gpuGeometry{}
			var err error
			node.transform, err = dev.CreateBuffer(&wgpu.BufferDescriptor{Size: 128, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
			if err != nil {
				return err
			}
			for kind, data := range geometryData(geom) {
				if err = g.uploadBatch(dev, &node.batches[kind], data, node.transform); err != nil {
					node.release()
					return err
				}
			}
			g.geometries[key] = node
			g.uploads++
		}
		g.nodes[i] = node
		v := instance(f, i)
		if !node.transformed || node.last.World != v.World || node.last.Visible != v.Visible {
			if err := writeTransform(dev, node.transform, v, geom.Material); err != nil {
				return err
			}
			node.last, node.transformed = v, true
		}
	}
	return nil
}

// writeTransform writes an instance's world and normal matrices. The normal
// matrix only turns directions, so its last column is free: it carries the
// material's specular strength and exponent, and whether the node shows.
func writeTransform(dev *wgpu.Device, buffer *wgpu.Buffer, v Instance, m *Material) error {
	inv, _ := v.World.InverseAffine()
	normal := inv.Transpose()
	normal[12], normal[13], normal[14], normal[15] = 0, 0, 0, 0
	if m != nil && m.Specular > 0 && !m.Unlit {
		normal[12], normal[13] = m.Specular, m.shininess()
	}
	if v.Visible {
		normal[15] = 1
	}
	return dev.Queue().WriteBuffer(buffer, 0, append(floats(v.World[:]...), floats(normal[:]...)...))
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
	b.count = uint32(len(data) / 48)
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
