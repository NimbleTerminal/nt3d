// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

// farTriangle is a glTF of one triangle whose vertices are millimeters from
// their node, in glTF's Y-up: (0.001, 0.002, 0.003), a meter along X from
// that, and a meter along Y. root is the JSON of the document's other
// members: the node's placement, and extensions.
func farTriangle(node, root string) (doc []byte, files map[string][]byte) {
	var buffer []byte
	for _, v := range []float32{.001, .002, .003, 1.001, .002, .003, .001, 1.002, .003} {
		buffer = binary.LittleEndian.AppendUint32(buffer, math.Float32bits(v))
	}
	text := fmt.Sprintf(`{"asset":{"version":"2.0"},%s"buffers":[{"uri":"side.bin","byteLength":36}],"bufferViews":[{"buffer":0,"byteLength":36}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],"nodes":[{"mesh":0%s}],"scenes":[{"nodes":[0]}],"scene":0}`, root, node)
	return []byte(text), map[string][]byte{"side.bin": buffer}
}

// Stamford station in Earth-centered meters, with fractions float32 cannot
// hold beside the millions: at this size its steps are half a meter.
var farPlace = nt3dmath.Vec3d{X: 1364789.133, Y: -4619824.799, Z: 4166319.166}

// first is the first vertex, turned Z-up: (0.001, -0.003, 0.002).
var first = nt3dmath.Vec3{X: .001, Y: -.003, Z: .002}

func exactly(t *testing.T, what string, got, want nt3dmath.Vec3) {
	t.Helper()
	if d := got.Sub(want).D().Len(); d > 1e-6 {
		t.Fatalf("%s is %v, %g from %v: millimeters were lost to the millions of meters", what, got, d, want)
	}
}

// A model whose node sits millions of meters out, as 3D Tiles content placed
// on the Earth does, keeps its vertices exact: the far placement comes back
// as Model.Origin in float64, and the geometry stays near zero, in the flat
// objects and in the asset alike.
func TestGLTFFarNodeIsRecenteredBeforeFloat32(t *testing.T) {
	// The node's translation in glTF's Y-up is (x, z, -y) of the Z-up place.
	node := fmt.Sprintf(`,"translation":[%v,%v,%v]`, farPlace.X, farPlace.Z, -farPlace.Y)
	doc, files := farTriangle(node, "")
	m, err := Decode(doc, &nt3dmesh.Options{Open: opener(files)})
	check(t, err)
	if d := m.Origin.Dist(farPlace); d > 1e-9 {
		t.Fatalf("Origin %+v is %g m from the node's place", m.Origin, d)
	}
	exactly(t, "the flat object's first vertex", m.Objects[0].Geometry.Vertices[0].Position, first)

	scene := nt3d.NewScene()
	root, err := m.AddTo(scene, nt3d.Root, "tile")
	check(t, err)
	if got, _ := scene.NodeOrigin(root); got != [3]float64{farPlace.X, farPlace.Y, farPlace.Z} {
		t.Fatalf("AddTo left the node's origin at %v, want the model's Origin", got)
	}
	// Seen by a camera at the same place, the vertex is millimeters away.
	camera := nt3d.DefaultCamera()
	camera.Origin = [3]float64{farPlace.X, farPlace.Y, farPlace.Z}
	frame, err := scene.StillFrame(camera, 32, 32)
	check(t, err)
	drawn := false
	for _, d := range frame.Draws {
		if len(d.Geometry.Vertices) == 0 {
			continue
		}
		drawn = true
		exactly(t, "the asset's first vertex, as drawn", d.World.Point(d.Geometry.Vertices[0].Position), first)
	}
	if !drawn {
		t.Fatal("the asset draws no triangle")
	}
}

// CESIUM_RTC names a center the vertices are relative to, in the Z-up space
// the model ends in and not in glTF's Y-up. It may be required, the vertices
// are left as they are, and the center comes back as Model.Origin.
func TestGLTFCesiumRTCCenterIsTheOrigin(t *testing.T) {
	root := fmt.Sprintf(`"extensionsUsed":["CESIUM_RTC"],"extensionsRequired":["CESIUM_RTC"],"extensions":{"CESIUM_RTC":{"center":[%v,%v,%v]}},`, farPlace.X, farPlace.Y, farPlace.Z)
	doc, files := farTriangle("", root)
	m, err := Decode(doc, &nt3dmesh.Options{Open: opener(files)})
	check(t, err)
	if m.Origin != farPlace {
		t.Fatalf("Origin %+v, want the center as written", m.Origin)
	}
	exactly(t, "the first vertex", m.Objects[0].Geometry.Vertices[0].Position, first)
	for name, center := range map[string]string{"two numbers": `[1,2]`, "not a number": `[1,2,"x"]`, "too large": `[1e300,1e300,1e308]`} {
		bad, files := farTriangle("", `"extensions":{"CESIUM_RTC":{"center":`+center+`}},`)
		if _, err := Decode(bad, &nt3dmesh.Options{Open: opener(files)}); err == nil && name != "too large" {
			t.Errorf("a center of %s was accepted", name)
		}
	}
}

// An ordinary model has no origin, and is read as before.
func TestGLTFNearModelHasNoOrigin(t *testing.T) {
	doc, files := farTriangle(`,"translation":[10,20,30]`, "")
	m, err := Decode(doc, &nt3dmesh.Options{Open: opener(files)})
	check(t, err)
	if m.Origin != (nt3dmath.Vec3d{}) {
		t.Fatalf("Origin %+v for a model ten meters from zero", m.Origin)
	}
	exactly(t, "the first vertex", m.Objects[0].Geometry.Vertices[0].Position, nt3dmath.Vec3{X: 10.001, Y: -30.003, Z: 20.002})
	scene := nt3d.NewScene()
	id, err := m.AddTo(scene, nt3d.Root, "near")
	check(t, err)
	if got, _ := scene.NodeOrigin(id); got != ([3]float64{}) {
		t.Fatalf("AddTo gave a near model the origin %v", got)
	}
}
