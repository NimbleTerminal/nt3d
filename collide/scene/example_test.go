// Copyright (c) 2026 Neomantra Corp.

package nt3dcollidescene_test

import (
	"fmt"

	"nimbleterminal.dev/nt3d"
	nt3dcollidescene "nimbleterminal.dev/nt3d/collide/scene"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// The geometry a scene draws becomes the mesh collision queries run
// against, placed by the same world transform. Here a ray cast straight
// down finds the top of a box that was raised two units.
func ExampleNewMesh() {
	box, _ := nt3d.BuildCuboid(nt3d.Cuboid{Size: nt3dmath.Vec3{X: 4, Y: 4, Z: 1}})
	lo, hi := box.Bounds.Min.Z, box.Bounds.Max.Z
	world := nt3dmath.Translation(nt3dmath.Vec3{Z: 2}).D()
	mesh := nt3dcollidescene.NewMesh(box, world)

	hit, ok, err := mesh.RayCast(nt3dmath.Vec3d{Z: 10}, nt3dmath.Vec3d{Z: -1}, 20)
	fmt.Println(ok, err)
	fmt.Printf("the box spans z=%.1f..%.1f; the ray lands at z=%.1f\n", float64(lo)+2, float64(hi)+2, hit.Position.Z)
	// Output:
	// true <nil>
	// the box spans z=1.5..2.5; the ray lands at z=2.5
}
