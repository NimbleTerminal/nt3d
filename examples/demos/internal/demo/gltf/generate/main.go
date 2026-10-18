// Copyright (c) 2026 Neomantra Corp.
// Generates the original CC0 assembly sample. Run go generate in the parent package.
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
	if err := generate(); err != nil {
		log.Fatal(err)
	}
}
func generate() error {
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
	pose := func(eye, target, up nt3dmath.Vec3) [16]float64 {
		back := eye.Sub(target).Normalize()
		right := up.Cross(back).Normalize()
		u := back.Cross(right)
		return [16]float64{float64(right.X), float64(right.Y), float64(right.Z), 0, float64(u.X), float64(u.Y), float64(u.Z), 0, float64(back.X), float64(back.Y), float64(back.Z), 0, float64(eye.X), float64(eye.Y), float64(eye.Z), 1}
	}
	d.Nodes[10].Matrix = pose(nt3dmath.Vec3{X: 4, Y: 3, Z: 6}, nt3dmath.Vec3{Y: .7}, nt3dmath.Vec3{Y: 1})
	d.Nodes[11].Matrix = pose(nt3dmath.Vec3{Y: 6}, nt3dmath.Vec3{}, nt3dmath.Vec3{Z: -1})
	d.Cameras = []*gltf.Camera{{Name: "Perspective inspection", Type: "perspective", Perspective: &gltf.Perspective{Yfov: math.Pi / 4, Znear: .1, Zfar: gltf.Float(30)}}, {Name: "Orthographic plan", Type: "orthographic", Orthographic: &gltf.Orthographic{Xmag: 2.4, Ymag: 2.4, Znear: .1, Zfar: 20}}}
	times := modeler.WriteAccessor(d, gltf.TargetNone, []float32{0, 1, 2, 3, 4})
	animation := &gltf.Animation{Name: "Articulated assembly"}
	for _, track := range []struct {
		node, axis int
		angles     []float64
	}{{2, 1, []float64{-.6, 0, .6, 0, -.6}}, {4, 2, []float64{-.3, .35, .6, .35, -.3}}, {6, 2, []float64{.5, 0, -.5, 0, .5}}} {
		var values [][4]float32
		for _, a := range track.angles {
			q := [4]float32{0, 0, 0, float32(math.Cos(a / 2))}
			q[track.axis] = float32(math.Sin(a / 2))
			values = append(values, q)
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
	f, err := os.Create("Assembly/assembly.glb")
	if err != nil {
		return err
	}
	defer f.Close()
	enc := gltf.NewEncoder(f)
	enc.AsBinary = true
	return enc.Encode(d)
}
