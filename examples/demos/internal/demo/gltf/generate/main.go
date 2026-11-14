// Copyright (c) 2026 Neomantra Corp.

// Generates the original CC0 Assembly and Spotlights glTF samples.
// Run go generate in the parent package.
package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func main() {
	if err := generateAssembly(); err != nil {
		log.Fatal(err)
	}
	if err := generateSpotlights(); err != nil {
		log.Fatal(err)
	}
}

func lookPose(eye, target, up nt3dmath.Vec3) [16]float64 {
	back := eye.Sub(target).Normalize()
	right := up.Cross(back).Normalize()
	u := back.Cross(right)
	return [16]float64{
		float64(right.X), float64(right.Y), float64(right.Z), 0,
		float64(u.X), float64(u.Y), float64(u.Z), 0,
		float64(back.X), float64(back.Y), float64(back.Z), 0,
		float64(eye.X), float64(eye.Y), float64(eye.Z), 1,
	}
}

func axisQuat(axis int, angle float64) [4]float64 {
	q := [4]float64{0, 0, 0, math.Cos(angle / 2)}
	q[axis] = math.Sin(angle / 2)
	return q
}

func axisQuat32(axis int, angle float64) [4]float32 {
	q := [4]float32{0, 0, 0, float32(math.Cos(angle / 2))}
	q[axis] = float32(math.Sin(angle / 2))
	return q
}

func generateAssembly() error {
	d := gltf.NewDocument()
	d.Asset.Generator = "nt3d CC0 assembly generator"
	// A face-split cube with two UV sets; all geometry remains local to its node.
	var positions, normals [][3]float32
	var uv, uv1 [][2]float32
	var indices []uint16
	faces := [][3]nt3dmath.Vec3{
		{{Z: 1}, {X: 1}, {Y: 1}}, {{Z: -1}, {X: -1}, {Y: 1}},
		{{X: 1}, {Z: -1}, {Y: 1}}, {{X: -1}, {Z: 1}, {Y: 1}},
		{{Y: 1}, {X: 1}, {Z: -1}}, {{Y: -1}, {X: 1}, {Z: 1}},
	}
	for _, face := range faces {
		start := uint16(len(positions))
		for _, xy := range [][2]float32{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}} {
			p := face[0].Add(face[1].Scale(xy[0])).Add(face[2].Scale(xy[1])).Scale(.5)
			positions = append(positions, [3]float32{p.X, p.Y, p.Z})
			normals = append(normals, [3]float32{face[0].X, face[0].Y, face[0].Z})
			u, v := (xy[0]+1)*.5, (1-xy[1])*.5
			uv = append(uv, [2]float32{u, v})
			uv1 = append(uv1, [2]float32{v, 1 - u})
		}
		indices = append(indices, start, start+1, start+2, start, start+2, start+3)
	}
	attrs := gltf.PrimitiveAttributes{gltf.POSITION: modeler.WritePosition(d, positions), gltf.NORMAL: modeler.WriteNormal(d, normals), gltf.TEXCOORD_0: modeler.WriteTextureCoord(d, uv), "TEXCOORD_1": modeler.WriteTextureCoord(d, uv1)}
	index := modeler.WriteIndices(d, indices)
	for kind := range 2 {
		img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
		for y := range 64 {
			for x := range 64 {
				c := color.NRGBA{30, 160, 180, 255}
				if (x/16+y/16)%2 == 0 {
					c = color.NRGBA{245, 178, 45, 255}
				}
				if x%16 < 2 || y%16 < 2 {
					c = color.NRGBA{25, 35, 50, 255}
				}
				if kind == 1 {
					nx := .6 * math.Sin(float64(x)*math.Pi/8)
					ny := .6 * math.Cos(float64(y)*math.Pi/8)
					nz := math.Sqrt(1 - nx*nx - ny*ny)
					c = color.NRGBA{uint8((nx + 1) * 127.5), uint8((ny + 1) * 127.5), uint8((nz + 1) * 127.5), 255}
				}
				img.SetNRGBA(x, y, c)
			}
		}
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			return err
		}
		im, err := modeler.WriteImage(d, "pattern", "image/png", &b)
		if err != nil {
			return err
		}
		d.Textures = append(d.Textures, &gltf.Texture{Source: gltf.Index(im)})
	}
	transform := func(offset, scale []float64, rotation float64, coord int) gltf.Extensions {
		return gltf.Extensions{"KHR_texture_transform": map[string]any{"offset": offset, "scale": scale, "rotation": rotation, "texCoord": coord}}
	}
	d.Materials = []*gltf.Material{
		{Name: "gunmetal", PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorFactor: &[4]float64{.14, .20, .26, 1}, MetallicFactor: gltf.Float(.3), RoughnessFactor: gltf.Float(.45)}},
		{Name: "reference UV grid", PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorTexture: &gltf.TextureInfo{Index: 0}, MetallicFactor: gltf.Float(0), RoughnessFactor: gltf.Float(.7)}},
		{Name: "transformed grid and independent relief", PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorTexture: &gltf.TextureInfo{Index: 0, Extensions: transform([]float64{.2, .1}, []float64{2, 1}, math.Pi/4, 0)}, MetallicFactor: gltf.Float(.1), RoughnessFactor: gltf.Float(.3)}, NormalTexture: &gltf.NormalTexture{Index: gltf.Index(1), Extensions: transform([]float64{.1, .2}, []float64{2, 2}, -.3, 1)}},
	}
	for i := range d.Materials {
		d.Meshes = append(d.Meshes, &gltf.Mesh{Name: d.Materials[i].Name, Primitives: []*gltf.Primitive{{Attributes: attrs, Indices: gltf.Index(index), Material: gltf.Index(i)}}})
	}
	cube := func(name string, mesh int, t, s [3]float64) *gltf.Node {
		return &gltf.Node{Name: name, Mesh: gltf.Index(mesh), Translation: t, Scale: s}
	}
	d.Nodes = []*gltf.Node{
		{Name: "assembly", Children: []int{1, 2, 8, 9, 10, 11}},
		cube("plinth", 0, [3]float64{0, -.15, 0}, [3]float64{3.6, .3, 2.6}),
		{Name: "turntable pivot", Children: []int{3, 4}},
		cube("column", 0, [3]float64{0, .65, 0}, [3]float64{.4, 1.3, .4}),
		{Name: "shoulder pivot", Translation: [3]float64{0, 1.3, 0}, Children: []int{5, 6}},
		cube("upper arm", 1, [3]float64{.7, 0, 0}, [3]float64{1.6, .35, .45}),
		{Name: "tool pivot", Translation: [3]float64{1.4, 0, 0}, Children: []int{7}},
		cube("relief tool", 2, [3]float64{0, -.25, 0}, [3]float64{.6, .7, .65}),
		cube("reference tile", 1, [3]float64{-.85, .08, .85}, [3]float64{.75, .12, .75}),
		cube("transformed relief tile", 2, [3]float64{.15, .08, .85}, [3]float64{.75, .12, .75}),
		{Name: "Perspective inspection", Camera: gltf.Index(0)},
		{Name: "Orthographic plan", Camera: gltf.Index(1)},
	}
	d.Nodes[10].Matrix = lookPose(nt3dmath.Vec3{X: 4, Y: 3, Z: 6}, nt3dmath.Vec3{Y: .7}, nt3dmath.Vec3{Y: 1})
	d.Nodes[11].Matrix = lookPose(nt3dmath.Vec3{Y: 6}, nt3dmath.Vec3{}, nt3dmath.Vec3{Z: -1})
	d.Cameras = []*gltf.Camera{{Name: "Perspective inspection", Type: "perspective", Perspective: &gltf.Perspective{Yfov: math.Pi / 4, Znear: .1, Zfar: gltf.Float(30)}}, {Name: "Orthographic plan", Type: "orthographic", Orthographic: &gltf.Orthographic{Xmag: 2.4, Ymag: 2.4, Znear: .1, Zfar: 20}}}
	times := modeler.WriteAccessor(d, gltf.TargetNone, []float32{0, 1, 2, 3, 4})
	animation := &gltf.Animation{Name: "Articulated assembly"}
	for _, track := range []struct {
		node, axis int
		angles     []float64
	}{{2, 1, []float64{-.6, 0, .6, 0, -.6}}, {4, 2, []float64{-.3, .35, .6, .35, -.3}}, {6, 2, []float64{.5, 0, -.5, 0, .5}}} {
		var values [][4]float32
		for _, a := range track.angles {
			values = append(values, axisQuat32(track.axis, a))
		}
		output := modeler.WriteAccessor(d, gltf.TargetNone, values)
		animation.Channels = append(animation.Channels, &gltf.AnimationChannel{Sampler: len(animation.Samplers), Target: gltf.AnimationChannelTarget{Node: gltf.Index(track.node), Path: gltf.TRSRotation}})
		animation.Samplers = append(animation.Samplers, &gltf.AnimationSampler{Input: times, Output: output})
	}
	d.Animations = []*gltf.Animation{animation}
	d.Scenes = []*gltf.Scene{{Name: "Assembly lab", Nodes: []int{0}}}
	d.Scene = gltf.Index(0)
	d.ExtensionsUsed = []string{"KHR_texture_transform"}
	d.ExtensionsRequired = d.ExtensionsUsed
	if err := os.MkdirAll("Assembly", 0o755); err != nil {
		return err
	}
	f, err := os.Create("Assembly/assembly.glb")
	if err != nil {
		return err
	}
	defer f.Close()
	enc := gltf.NewEncoder(f)
	enc.AsBinary = true
	return enc.Encode(d)
}

func buildCylinderY(d *gltf.Document, radius, height float32, segs int, alongZ bool) (gltf.PrimitiveAttributes, int) {
	var pos, norm [][3]float32
	var idx []uint16
	half := height * 0.5
	mapVec := func(x, y, z float32) [3]float32 {
		if alongZ {
			return [3]float32{x, z, -y}
		}
		return [3]float32{x, y, z}
	}
	// Barrel
	for i := range segs {
		a0 := float64(i) * 2 * math.Pi / float64(segs)
		a1 := float64(i+1) * 2 * math.Pi / float64(segs)
		s0, c0 := float32(math.Sin(a0)), float32(math.Cos(a0))
		s1, c1 := float32(math.Sin(a1)), float32(math.Cos(a1))
		base := uint16(len(pos))
		pos = append(pos,
			mapVec(radius*s0, -half, radius*c0),
			mapVec(radius*s1, -half, radius*c1),
			mapVec(radius*s1, half, radius*c1),
			mapVec(radius*s0, half, radius*c0),
		)
		norm = append(norm,
			mapVec(s0, 0, c0),
			mapVec(s1, 0, c1),
			mapVec(s1, 0, c1),
			mapVec(s0, 0, c0),
		)
		idx = append(idx, base, base+1, base+2, base, base+2, base+3)
	}
	// Top (+Y, or -Z when alongZ) and bottom caps
	topCenter := uint16(len(pos))
	pos = append(pos, mapVec(0, half, 0))
	norm = append(norm, mapVec(0, 1, 0))
	for i := range segs {
		a := float64(i) * 2 * math.Pi / float64(segs)
		pos = append(pos, mapVec(radius*float32(math.Sin(a)), half, radius*float32(math.Cos(a))))
		norm = append(norm, mapVec(0, 1, 0))
	}
	for i := range segs {
		r0 := topCenter + 1 + uint16(i)
		r1 := topCenter + 1 + uint16((i+1)%segs)
		idx = append(idx, topCenter, r0, r1)
	}
	botCenter := uint16(len(pos))
	pos = append(pos, mapVec(0, -half, 0))
	norm = append(norm, mapVec(0, -1, 0))
	for i := range segs {
		a := float64(i) * 2 * math.Pi / float64(segs)
		pos = append(pos, mapVec(radius*float32(math.Sin(a)), -half, radius*float32(math.Cos(a))))
		norm = append(norm, mapVec(0, -1, 0))
	}
	for i := range segs {
		r0 := botCenter + 1 + uint16(i)
		r1 := botCenter + 1 + uint16((i+1)%segs)
		idx = append(idx, botCenter, r1, r0)
	}
	attrs := gltf.PrimitiveAttributes{
		gltf.POSITION: modeler.WritePosition(d, pos),
		gltf.NORMAL:   modeler.WriteNormal(d, norm),
	}
	return attrs, modeler.WriteIndices(d, idx)
}

func buildTorusY(d *gltf.Document, majorR, minorR float32, uSegs, vSegs int) (gltf.PrimitiveAttributes, int) {
	var pos, norm [][3]float32
	var idx []uint16
	for iu := range uSegs {
		u := float64(iu) * 2 * math.Pi / float64(uSegs)
		cu, su := float32(math.Cos(u)), float32(-math.Sin(u))
		for iv := range vSegs {
			v := float64(iv) * 2 * math.Pi / float64(vSegs)
			cv, sv := float32(math.Cos(v)), float32(math.Sin(v))
			nx, ny, nz := cv*cu, sv, cv*su
			r := majorR + minorR*cv
			pos = append(pos, [3]float32{r * cu, minorR * sv, r * su})
			norm = append(norm, [3]float32{nx, ny, nz})
		}
	}
	for iu := range uSegs {
		nu := (iu + 1) % uSegs
		for iv := range vSegs {
			nv := (iv + 1) % vSegs
			i00 := uint16(iu*vSegs + iv)
			i10 := uint16(nu*vSegs + iv)
			i11 := uint16(nu*vSegs + nv)
			i01 := uint16(iu*vSegs + nv)
			idx = append(idx, i00, i10, i11, i00, i11, i01)
		}
	}
	attrs := gltf.PrimitiveAttributes{
		gltf.POSITION: modeler.WritePosition(d, pos),
		gltf.NORMAL:   modeler.WriteNormal(d, norm),
	}
	return attrs, modeler.WriteIndices(d, idx)
}

func generateSpotlights() error {
	d := gltf.NewDocument()
	d.Asset.Generator = "nt3d CC0 spotlight stage generator"

	stageAttrs, stageIdx := buildCylinderY(d, 1.05, 0.08, 24, false)
	torusAttrs, torusIdx := buildTorusY(d, 0.30, 0.085, 20, 12)
	pillarAttrs, pillarIdx := buildCylinderY(d, 0.085, 0.44, 12, false)
	lampAttrs, lampIdx := buildCylinderY(d, 0.052, 0.12, 10, true)

	emissiveStrength := func(s float64) gltf.Extensions {
		return gltf.Extensions{"KHR_materials_emissive_strength": map[string]any{"emissiveStrength": s}}
	}
	d.Materials = []*gltf.Material{
		{
			Name: "stage floor",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorFactor: &[4]float64{0.62, 0.65, 0.72, 1},
				MetallicFactor:  gltf.Float(0.08),
				RoughnessFactor: gltf.Float(0.42),
			},
		},
		{
			Name: "brushed gold",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorFactor: &[4]float64{0.96, 0.74, 0.34, 1},
				MetallicFactor:  gltf.Float(0.92),
				RoughnessFactor: gltf.Float(0.22),
			},
		},
		{
			Name: "satin ceramic",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
				BaseColorFactor: &[4]float64{0.86, 0.88, 0.92, 1},
				MetallicFactor:  gltf.Float(0.05),
				RoughnessFactor: gltf.Float(0.32),
			},
		},
		{
			Name:                 "warm lamp",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorFactor: &[4]float64{0.25, 0.22, 0.20, 1}, MetallicFactor: gltf.Float(0.6), RoughnessFactor: gltf.Float(0.35)},
			EmissiveFactor:       [3]float64{1.0, 0.82, 0.55},
			Extensions:           emissiveStrength(2.4),
		},
		{
			Name:                 "cyan lamp",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorFactor: &[4]float64{0.18, 0.24, 0.28, 1}, MetallicFactor: gltf.Float(0.6), RoughnessFactor: gltf.Float(0.35)},
			EmissiveFactor:       [3]float64{0.22, 0.82, 1.0},
			Extensions:           emissiveStrength(2.4),
		},
		{
			Name:                 "magenta lamp",
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorFactor: &[4]float64{0.26, 0.18, 0.24, 1}, MetallicFactor: gltf.Float(0.6), RoughnessFactor: gltf.Float(0.35)},
			EmissiveFactor:       [3]float64{1.0, 0.28, 0.68},
			Extensions:           emissiveStrength(2.4),
		},
	}

	d.Meshes = []*gltf.Mesh{
		{Name: "stage floor", Primitives: []*gltf.Primitive{{Attributes: stageAttrs, Indices: gltf.Index(stageIdx), Material: gltf.Index(0)}}},
		{Name: "gold ring", Primitives: []*gltf.Primitive{{Attributes: torusAttrs, Indices: gltf.Index(torusIdx), Material: gltf.Index(1)}}},
		{Name: "ceramic pillar", Primitives: []*gltf.Primitive{{Attributes: pillarAttrs, Indices: gltf.Index(pillarIdx), Material: gltf.Index(2)}}},
		{Name: "warm lamp", Primitives: []*gltf.Primitive{{Attributes: lampAttrs, Indices: gltf.Index(lampIdx), Material: gltf.Index(3)}}},
		{Name: "cyan lamp", Primitives: []*gltf.Primitive{{Attributes: lampAttrs, Indices: gltf.Index(lampIdx), Material: gltf.Index(4)}}},
		{Name: "magenta lamp", Primitives: []*gltf.Primitive{{Attributes: lampAttrs, Indices: gltf.Index(lampIdx), Material: gltf.Index(5)}}},
	}

	d.Extensions = gltf.Extensions{
		"KHR_lights_punctual": map[string]any{
			"lights": []map[string]any{
				{
					"name":      "Key Warm Spot",
					"type":      "spot",
					"color":     []float64{1.0, 0.84, 0.60},
					"intensity": 3.4,
					"range":     6.5,
					"spot":      map[string]any{"innerConeAngle": 0.18, "outerConeAngle": 0.44},
				},
				{
					"name":      "Cyan Sweep Spot",
					"type":      "spot",
					"color":     []float64{0.24, 0.80, 1.0},
					"intensity": 2.9,
					"range":     6.5,
					"spot":      map[string]any{"innerConeAngle": 0.16, "outerConeAngle": 0.42},
				},
				{
					"name":      "Magenta Accent Spot",
					"type":      "spot",
					"color":     []float64{1.0, 0.30, 0.68},
					"intensity": 2.6,
					"range":     6.5,
					"spot":      map[string]any{"innerConeAngle": 0.16, "outerConeAngle": 0.40},
				},
				{
					"name":      "Overhead Fill",
					"type":      "point",
					"color":     []float64{0.72, 0.80, 1.0},
					"intensity": 0.45,
					"range":     6.0,
				},
			},
		},
	}

	lightExt := func(idx int) gltf.Extensions {
		return gltf.Extensions{"KHR_lights_punctual": map[string]any{"light": idx}}
	}

	// Nodes:
	// 0: stage root
	// 1: stage floor
	// 2: sculpture spindle (animated 360 Y spin)
	// 3: tilted gold torus
	// 4..6: 3 satin ceramic pillars
	// 7: overhead rig carousel (animated 360 Y spin)
	// 8,9: boom 0 yaw mount + gimbal (Key Warm Spot + warm lamp mesh)
	// 10,11: boom 1 yaw mount + gimbal (Cyan Sweep Spot + cyan lamp mesh)
	// 12,13: boom 2 yaw mount + gimbal (Magenta Accent Spot + magenta lamp mesh)
	// 14: Overhead Fill point light
	// 15: Stage front authored perspective camera
	d.Nodes = []*gltf.Node{
		{Name: "spotlight stage", Children: []int{1, 2, 4, 5, 6, 7, 14, 15}},
		{Name: "stage floor", Mesh: gltf.Index(0), Translation: [3]float64{0, -0.04, 0}},
		{Name: "sculpture spindle", Translation: [3]float64{0, 0.40, 0}, Children: []int{3}},
		{Name: "gold ring", Mesh: gltf.Index(1), Rotation: axisQuat(0, 0.62)},
	}
	for i := range 3 {
		a := float64(i) * 2 * math.Pi / 3
		d.Nodes = append(d.Nodes, &gltf.Node{
			Name:        []string{"pillar A", "pillar B", "pillar C"}[i],
			Mesh:        gltf.Index(2),
			Translation: [3]float64{0.58 * math.Sin(a), 0.22, 0.58 * math.Cos(a)},
		})
	}
	d.Nodes = append(d.Nodes, &gltf.Node{
		Name:        "overhead rig",
		Translation: [3]float64{0, 1.06, 0},
		Children:    []int{8, 10, 12},
	})
	spotNames := [3]string{"Key Warm Spot", "Cyan Sweep Spot", "Magenta Accent Spot"}
	for i := range 3 {
		yaw := float64(i) * 2 * math.Pi / 3
		gimbalIdx := 9 + 2*i
		d.Nodes = append(d.Nodes,
			&gltf.Node{
				Name:        spotNames[i] + " mount",
				Translation: [3]float64{0.76 * math.Sin(yaw), 0, 0.76 * math.Cos(yaw)},
				Rotation:    axisQuat(1, yaw),
				Children:    []int{gimbalIdx},
			},
			&gltf.Node{
				Name:       spotNames[i],
				Mesh:       gltf.Index(3 + i),
				Rotation:   axisQuat(0, -0.88),
				Extensions: lightExt(i),
			},
		)
	}
	d.Nodes = append(d.Nodes,
		&gltf.Node{
			Name:        "Overhead Fill",
			Translation: [3]float64{0, 1.32, 0},
			Extensions:  lightExt(3),
		},
		&gltf.Node{
			Name:   "Stage front",
			Camera: gltf.Index(0),
			Matrix: lookPose(nt3dmath.Vec3{X: 0.35, Y: 1.35, Z: 2.45}, nt3dmath.Vec3{Y: 0.24}, nt3dmath.Vec3{Y: 1}),
		},
	)

	d.Cameras = []*gltf.Camera{{
		Name:        "Stage front",
		Type:        "perspective",
		Perspective: &gltf.Perspective{Yfov: math.Pi / 4, Znear: 0.1, Zfar: gltf.Float(25)},
	}}

	times := modeler.WriteAccessor(d, gltf.TargetNone, []float32{0, 1, 2, 3, 4})
	anim := &gltf.Animation{Name: "Spotlight sweep"}
	addRotTrack := func(node, axis int, angles []float64) {
		vals := make([][4]float32, len(angles))
		for i, a := range angles {
			vals[i] = axisQuat32(axis, a)
		}
		out := modeler.WriteAccessor(d, gltf.TargetNone, vals)
		anim.Channels = append(anim.Channels, &gltf.AnimationChannel{
			Sampler: len(anim.Samplers),
			Target:  gltf.AnimationChannelTarget{Node: gltf.Index(node), Path: gltf.TRSRotation},
		})
		anim.Samplers = append(anim.Samplers, &gltf.AnimationSampler{Input: times, Output: out})
	}
	// Overhead rig spins +360 deg around Y; central spindle counter-rotates -360 deg.
	addRotTrack(7, 1, []float64{0, math.Pi / 2, math.Pi, 3 * math.Pi / 2, 2 * math.Pi})
	addRotTrack(2, 1, []float64{0, -math.Pi / 2, -math.Pi, -3 * math.Pi / 2, -2 * math.Pi})
	// Spotlight gimbals nod their downward pitch (-X) in staggered phases to sweep cones across the stage.
	addRotTrack(9, 0, []float64{-0.68, -1.04, -0.72, -1.00, -0.68})
	addRotTrack(11, 0, []float64{-1.02, -0.66, -0.98, -0.70, -1.02})
	addRotTrack(13, 0, []float64{-0.84, -0.68, -1.04, -0.80, -0.84})

	d.Animations = []*gltf.Animation{anim}
	d.Scenes = []*gltf.Scene{{Name: "Spotlight stage", Nodes: []int{0}}}
	d.Scene = gltf.Index(0)
	d.ExtensionsUsed = []string{"KHR_lights_punctual", "KHR_materials_emissive_strength"}

	if err := os.MkdirAll("Spotlights", 0o755); err != nil {
		return err
	}
	f, err := os.Create("Spotlights/spotlights.glb")
	if err != nil {
		return err
	}
	defer f.Close()
	enc := gltf.NewEncoder(f)
	enc.AsBinary = true
	return enc.Encode(d)
}
