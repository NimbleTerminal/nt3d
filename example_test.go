// Copyright (c) 2026 Neomantra Corp.

package nt3d_test

import (
	"fmt"
	"image"
	"image/color"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// rgba is a pixel of a rendered image.
func rgba(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

// ball is a sphere painted one flat, unlit color.
func ball(c color.RGBA) nt3d.Geometry {
	g, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .7, Segments: 24, Rings: 12, Color: c, Material: &nt3d.Material{Unlit: true}})
	if err != nil {
		panic(err)
	}
	return g
}

// The whole pipeline: a Scene holds nodes, a Frame is a snapshot of it
// through a camera, and a Renderer turns the frame into an image. The scene
// may be edited again as soon as the frame is taken.
func Example() {
	scene := nt3d.NewScene()
	if _, err := scene.SetGeometry("ball", ball(color.RGBA{R: 230, G: 120, B: 40, A: 255})); err != nil {
		panic(err)
	}

	frame, err := scene.StillFrame(nt3d.DefaultCamera(), 64, 48)
	if err != nil {
		panic(err)
	}
	renderer := nt3d.NewSoftwareRenderer()
	defer renderer.Close()
	img, err := renderer.Render(frame)
	if err != nil {
		panic(err)
	}

	fmt.Println("image:", img.Bounds().Dx(), "by", img.Bounds().Dy())
	fmt.Println("center:", rgba(img, 32, 24))
	fmt.Println("corner is the background:", rgba(img, 0, 0) == frame.Background)
	// Output:
	// image: 64 by 48
	// center: {230 120 40 255}
	// corner is the background: true
}

// A frame is a list of draws, in the order they are drawn. Nodes are drawn in
// the order they were added unless they are given a DrawOrder; a group's
// order places everything under it.
func ExampleScene_SetNodeDrawOrder() {
	scene := nt3d.NewScene()
	water, _ := scene.Add(nt3d.Root, "water")
	for _, name := range []string{"pond", "hill", "river", "tree"} {
		parent := nt3d.Root
		if name == "pond" || name == "river" {
			parent = water
		}
		id, _ := scene.Add(parent, name)
		_ = scene.SetNodeGeometry(id, ball(color.RGBA{A: 255}))
	}
	// Water is blended, so it must be drawn after the scenery behind it.
	_ = scene.SetNodeDrawOrder(water, nt3d.DrawOrder{Layer: 1})

	frame, _ := scene.Frame(nt3d.DefaultCamera(), 32, 32)
	for _, draw := range frame.Draws {
		fmt.Println(draw.Name)
	}
	// Output:
	// hill
	// tree
	// pond
	// river
}

// SetNodeData attaches an application's own value to a node. It comes back
// from a pick and in every frame's draws, so nothing needs to be encoded in
// node names.
func ExampleScene_SetNodeData() {
	type building struct {
		Address string
		Floors  int
	}
	scene := nt3d.NewScene()
	id, _ := scene.SetGeometry("b-17", ball(color.RGBA{R: 200, G: 200, B: 200, A: 255}))
	_ = scene.SetNodeData(id, building{Address: "17 Canal Street", Floors: 6})

	camera := nt3d.DefaultCamera()
	frame, _ := scene.Frame(camera, 64, 64)
	if hit := scene.Pick(camera, frame, 32, 32, 0); hit != nil {
		b := hit.Data.(building)
		fmt.Println(b.Address, "has", b.Floors, "floors")
	}
	// Output:
	// 17 Canal Street has 6 floors
}

// A scene holds its look: background, lighting, fog, environment, and
// post-processing. Every frame starts out with it, so a scene is lit where
// it is built rather than on each frame.
func ExampleScene_SetLook() {
	scene := nt3d.NewScene()
	look := scene.Look() // DefaultLook, for a new scene
	look.Background = color.RGBA{R: 16, G: 14, B: 20, A: 255}
	look.Fog = nt3d.Fog{Near: 4, Far: 20}
	scene.SetLook(look)

	frame, _ := scene.Frame(nt3d.DefaultCamera(), 32, 32)
	fmt.Println("background:", frame.Background)
	fmt.Println("fog from", frame.Fog.Near, "to", frame.Fog.Far)
	// Output:
	// background: {16 14 20 255}
	// fog from 4 to 20
}

// A frame made by a scene is the caller's to edit before it is rendered:
// hiding a draw in one frame leaves the scene as it was.
func ExampleDraw() {
	scene := nt3d.NewScene()
	_, _ = scene.SetGeometry("red", ball(color.RGBA{R: 255, A: 255}))
	frame, _ := scene.StillFrame(nt3d.DefaultCamera(), 32, 32)
	renderer := nt3d.NewSoftwareRenderer()
	defer renderer.Close()

	shown, _ := renderer.Render(frame)
	frame.Draws[0].Hidden = true
	hidden, _ := renderer.Render(frame)
	again, _ := scene.StillFrame(nt3d.DefaultCamera(), 32, 32)

	fmt.Println("shown:", rgba(shown, 16, 16))
	fmt.Println("hidden is the background:", rgba(hidden, 16, 16) == frame.Background)
	fmt.Println("the scene still shows it:", !again.Draws[0].Hidden)
	// Output:
	// shown: {255 0 0 255}
	// hidden is the background: true
	// the scene still shows it: true
}

// A frame can be built by hand, without a scene: a draw needs only its
// geometry, and is drawn where the geometry lies unless World moves it.
func ExampleFrame() {
	camera := nt3d.DefaultCamera()
	frame := nt3d.Frame{
		Width: 64, Height: 64,
		Matrix:     camera.Matrix(1),
		Background: color.RGBA{A: 255},
		Draws: []nt3d.Draw{
			{Geometry: ball(color.RGBA{G: 255, A: 255})},
			{Geometry: ball(color.RGBA{B: 255, A: 255}), World: nt3dmath.Translation(nt3dmath.Vec3{X: 100})}, // far out of view
		},
	}
	renderer := nt3d.NewSoftwareRenderer()
	defer renderer.Close()
	img, err := renderer.Render(frame)
	if err != nil {
		panic(err)
	}
	fmt.Println("center:", rgba(img, 32, 32))
	// Output:
	// center: {0 255 0 255}
}

// Methods named Apply reach a whole subtree: every mesh under the node they
// are given, as it is when they are called. A node added afterwards is not
// changed.
func ExampleScene_ApplyMaterial() {
	scene := nt3d.NewScene()
	fleet, _ := scene.Add(nt3d.Root, "fleet")
	add := func(name string) {
		id, _ := scene.Add(fleet, name)
		_ = scene.SetNodeGeometry(id, ball(color.RGBA{R: 200, G: 200, B: 200, A: 255}))
	}
	add("tug")
	add("barge")
	_ = scene.ApplyMaterial(fleet, &nt3d.Material{Metallic: 1, Roughness: .3})
	add("ferry")

	frame, _ := scene.StillFrame(nt3d.DefaultCamera(), 32, 32)
	for _, draw := range frame.Draws {
		fmt.Println(draw.Name, "is metal:", draw.Geometry.Material.Metallic == 1)
	}
	// Output:
	// tug is metal: true
	// barge is metal: true
	// ferry is metal: false
}

// A camera that cannot mean what it says is refused where it is used, with
// the field named, instead of drawing some other view. Zeros are not
// mistakes: they mean the defaults.
func ExampleCamera_Validate() {
	scene := nt3d.NewScene()
	_, _ = scene.SetGeometry("ball", ball(color.RGBA{A: 255}))

	camera := nt3d.DefaultCamera()
	camera.Projection = nt3d.Perspective
	camera.Near, camera.Far = 10, 5
	_, err := scene.Frame(camera, 32, 32)
	fmt.Println(err)

	fmt.Println("the zero camera:", nt3d.Camera{}.Validate())
	// Output:
	// nt3d: camera Far (5) must be beyond Near (10)
	// the zero camera: <nil>
}

// A zero Intensity means the default, so it cannot say "none": Off does. A
// light that is off keeps its place in the list and its settings.
func ExampleLight() {
	scene := nt3d.NewScene()
	_, _ = scene.SetGeometry("ball", ball(color.RGBA{R: 200, G: 200, B: 200, A: 255}))
	_ = scene.ApplyMaterial(nt3d.Root, nil) // Lit, unlike ball's own material.

	look := scene.Look()
	look.Lighting = nt3d.Lighting{
		Ambient: nt3d.Ambient{Off: true},
		Lights:  []nt3d.Light{{Kind: nt3d.DirectionalLight, Direction: nt3dmath.Vec3{X: 1, Y: -1, Z: 2}}},
	}
	renderer := nt3d.NewSoftwareRenderer()
	defer renderer.Close()
	center := func() color.RGBA {
		scene.SetLook(look)
		frame, _ := scene.StillFrame(nt3d.DefaultCamera(), 32, 32)
		img, _ := renderer.Render(frame)
		return rgba(img, 16, 16)
	}

	fmt.Println("lit:", center().R > 100)
	look.Lighting.Lights[0].Intensity = 0 // Zero means 1: still lit.
	fmt.Println("intensity zero:", center().R > 100)
	look.Lighting.Lights[0].Off = true
	fmt.Println("off:", center())
	// Output:
	// lit: true
	// intensity zero: true
	// off: {0 0 0 255}
}
