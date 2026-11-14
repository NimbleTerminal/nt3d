// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"image"
	"image/color"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func TestSetMaterialSubtreeSharedLODAndAtomicErrors(t *testing.T) {
	s := NewScene()
	group, err := s.Add(Root, "group")
	check(t, err)

	// Non-mesh point node in subtree is ignored by SetMaterial.
	ptsID, err := s.SetGeometry("pts", Geometry{Points: []Point{{Radius: 2, Color: white}}})
	check(t, err)
	check(t, s.Reparent(ptsID, group))

	// Shared mesh instances: one inside group, one outside group.
	cube, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, Color: white})
	check(t, err)
	sg, err := NewSharedGeometry(cube)
	check(t, err)
	inID, err := s.SetShared("in-shared", sg)
	check(t, err)
	check(t, s.Reparent(inID, group))
	outID, err := s.SetShared("out-shared", sg)
	check(t, err)
	_ = outID

	// LOD mesh inside group.
	lodID, err := s.Add(group, "lod")
	check(t, err)
	check(t, s.SetLOD(lodID, []LODLevel{
		{MaxDistance: 5, Shared: sg},
		{MaxDistance: 0, Geometry: cube},
	}))

	before := frame(t, s)

	// Instance-uniform-only material update preserves SharedGeometry and revisions!
	pbrMat := &Material{PBR: true, Metallic: .8, Roughness: .25, EnvironmentReflect: true}
	check(t, s.SetMaterial(group, pbrMat))
	pbrMat.Metallic = .1 // Caller mutation must not leak.

	afterUniform := frame(t, s)
	for idx, inst := range afterUniform.Instances {
		switch inst.Name {
		case "pts":
			if afterUniform.Geometry[idx].Material != nil {
				t.Fatal("point node acquired material")
			}
		case "in-shared":
			m := afterUniform.Geometry[idx].Material
			if m == nil || m.Metallic != .8 || !m.EnvironmentReflect {
				t.Fatalf("in-shared material not updated: %+v", m)
			}
			if afterUniform.Shared[idx] == 0 || afterUniform.GeometryRevisions[idx] != before.GeometryRevisions[idx] {
				t.Fatal("instance-only SetMaterial broke SharedGeometry")
			}
		case "out-shared":
			if afterUniform.Geometry[idx].Material != nil {
				t.Fatal("node outside subtree was modified")
			}
		case "lod":
			m := afterUniform.Geometry[idx].Material
			if m == nil || m.Metallic != .8 {
				t.Fatalf("LOD tier material not updated: %+v", m)
			}
		}
	}
	if before.Geometry[1].Material != nil {
		t.Fatal("previous Frame snapshot was mutated")
	}

	// Vertex-baked change (Transparent: true) detaches modified SharedGeometry
	// cleanly while leaving out-shared on sg!
	check(t, s.SetMaterial(group, &Material{Transparent: true, Specular: .4}))
	afterTrans := frame(t, s)
	for idx, inst := range afterTrans.Instances {
		switch inst.Name {
		case "in-shared":
			if !afterTrans.Geometry[idx].Material.Transparent || afterTrans.Shared[idx] != 0 {
				t.Fatal("transparent transition did not detach shared geometry")
			}
		case "out-shared":
			if afterTrans.Shared[idx] == 0 || afterTrans.Geometry[idx].Material != nil {
				t.Fatal("out-shared was disturbed")
			}
		}
	}

	// Atomic UV validation failure: BuildCuboid has UVs, so add an untextured
	// mesh without UVs deeper in the subtree and verify textured SetMaterial
	// fails without mutating any sibling.
	noUV := quad(.5, .5, white, nil, false)
	noUV.UV = nil
	noUVID, err := s.SetGeometry("no-uv", noUV)
	check(t, err)
	check(t, s.Reparent(noUVID, group))

	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	tex, err := NewTexture(img)
	check(t, err)
	if err := s.SetMaterial(group, &Material{Texture: tex}); err == nil {
		t.Fatal("expected error applying textured material to UV-less mesh")
	}
	afterErr := frame(t, s)
	for idx, inst := range afterErr.Instances {
		if inst.Name == "in-shared" && !afterErr.Geometry[idx].Material.Transparent {
			t.Fatal("failed SetMaterial partially mutated sibling")
		}
	}

	// Clearing material with nil restores default shading across the subtree.
	check(t, s.SetMaterial(group, nil))
	afterNil := frame(t, s)
	for idx, inst := range afterNil.Instances {
		if inst.Name == "in-shared" && afterNil.Geometry[idx].Material != nil {
			t.Fatal("nil SetMaterial did not clear material")
		}
	}

	// Invalid material arguments fail even on empty subtrees.
	empty, err := s.Add(Root, "empty")
	check(t, err)
	if s.SetMaterial(empty, &Material{Metallic: 2}) == nil {
		t.Fatal("accepted out-of-range Metallic")
	}
	if s.SetMaterial(NodeID(99999), nil) == nil {
		t.Fatal("accepted unknown root node")
	}
}

func TestSetMaterialOnDeformedMesh(t *testing.T) {
	s := NewScene()
	g := quad(.6, .6, white, nil, true)
	g.UV = []UV{{0, 0}, {1, 0}, {1, 1}, {0, 1}}
	id, err := s.SetGeometry("cloth", g)
	check(t, err)
	morph := MorphTarget{Positions: make([]nt3dmath.Vec3, len(g.Vertices))}
	morph.Positions[0] = nt3dmath.Vec3{Z: .3}
	check(t, s.SetDeformation(id, Deformation{Morphs: []MorphTarget{morph}, Weights: []float32{1}}))

	nmap := testNormalMap(t, color.NRGBA{128, 128, 255, 255})
	check(t, s.SetMaterial(id, &Material{NormalMap: nmap, NormalStrength: 1, Specular: .5}))
	f := frame(t, s)
	check(t, validateFrame(f))
	if f.Geometry[0].Material == nil || f.Geometry[0].Material.NormalMap != nmap || len(f.Geometry[0].Tangents) != len(g.Vertices) {
		t.Fatal("deformed mesh did not receive normal map and generated tangents")
	}
}
