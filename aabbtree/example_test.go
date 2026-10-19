// Copyright (c) 2026 Neomantra Corp.
package nt3daabbtree_test

import (
	"fmt"

	"nimbleterminal.dev/nt3d/aabbtree"
	"nimbleterminal.dev/nt3d/math"
)

type object struct {
	Name   string
	Bounds nt3dmath.AABB
}

func cube(name string, x, y, z float32) *object {
	var b nt3dmath.AABB
	b.Include(nt3dmath.Vec3{X: x, Y: y, Z: z})
	b.Include(nt3dmath.Vec3{X: x + 1, Y: y + 1, Z: z + 1})
	return &object{Name: name, Bounds: b}
}

func Example() {
	tree := nt3daabbtree.New[*object]()
	crate := cube("crate", 0, 0, 0)
	crateID := tree.Insert(crate.Bounds, crate)
	for _, o := range []*object{cube("barrel", 3, 0, 0), cube("lamp", 10, 10, 10)} {
		tree.Insert(o.Bounds, o)
	}

	// Everything within the region [-1, 5]³.
	var region nt3dmath.AABB
	region.Include(nt3dmath.Vec3{X: -1, Y: -1, Z: -1})
	region.Include(nt3dmath.Vec3{X: 5, Y: 5, Z: 5})
	tree.QueryOverlap(region, func(_ nt3daabbtree.ID, o *object) bool {
		fmt.Println("in region:", o.Name)
		return true
	})

	// The first thing a ray from x = -5 along +X meets.
	ray := nt3dmath.Ray{Origin: nt3dmath.Vec3{X: -5, Y: .5, Z: .5}, Direction: nt3dmath.Vec3{X: 1}}
	tree.RayCast(ray, func(_ nt3daabbtree.ID, o *object, t float32) bool {
		fmt.Printf("ray hits %s at t=%g\n", o.Name, t)
		return false
	})

	// The crate moves far away; its ID is unchanged.
	tree.Update(crateID, cube("crate", 20, 0, 0).Bounds)
	n := 0
	tree.QueryOverlap(region, func(nt3daabbtree.ID, *object) bool { n++; return true })
	fmt.Println("in region after the move:", n)
	// Output:
	// in region: crate
	// in region: barrel
	// ray hits crate at t=5
	// in region after the move: 1
}
