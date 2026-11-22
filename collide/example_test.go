// Copyright (c) 2026 Neomantra Corp.

package nt3dcollide_test

import (
	"fmt"

	nt3dcollide "nimbleterminal.dev/nt3d/collide"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// A capsule dropped above a floor falls under the caller's gravity, lands,
// and stays grounded. The caller keeps the pose between calls; the package
// keeps nothing.
func ExampleMoveCapsule() {
	v := func(x, y, z float64) nt3dmath.Vec3d { return nt3dmath.Vec3d{X: x, Y: y, Z: z} }
	floor := nt3dcollide.NewMeshFromTriangles([]nt3dcollide.Triangle{
		{A: v(-10, -10, 0), B: v(10, -10, 0), C: v(10, 10, 0)},
		{A: v(-10, -10, 0), B: v(10, 10, 0), C: v(-10, 10, 0)},
	})
	body := nt3dcollide.Capsule{Radius: 0.4, Height: 1.8}
	cfg := nt3dcollide.MoveConfig{StepHeight: 0.3, GroundSnap: 0.2}

	pos, vel, grounded := v(0, 0, 2), v(1, 0, 0), false
	const dt = 1.0 / 60
	for range 120 {
		vel.Z -= 9.81 * dt
		res, err := nt3dcollide.MoveCapsule(floor, body, pos, vel, dt, grounded, cfg)
		if err != nil {
			fmt.Println(err)
			return
		}
		pos, vel, grounded = res.Position, res.Velocity, res.Grounded
	}
	fmt.Printf("grounded %v at x=%.1f z=%.2f\n", grounded, pos.X, pos.Z)

	hit, ok := floor.RayCast(v(3, 3, 5), v(0, 0, -1), 10)
	fmt.Printf("ray hit %v at distance %.0f\n", ok, hit.Distance)
	// Output:
	// grounded true at x=2.0 z=0.01
	// ray hit true at distance 5
}
