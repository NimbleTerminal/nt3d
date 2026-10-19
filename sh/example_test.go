package nt3dsh_test

import (
	"fmt"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
)

func ExampleDiffuse() {
	radiance, err := nt3dsh.Project(2, func(nt3dmath.Vec3) nt3dsh.RGB { return nt3dsh.RGB{1, .5, .25} })
	if err != nil {
		panic(err)
	}
	diffuse := nt3dsh.Diffuse(radiance)
	rgb := nt3dsh.Evaluate(diffuse[:], nt3dmath.Vec3{Z: 1})
	fmt.Printf("%.2f %.2f %.2f\n", rgb[0], rgb[1], rgb[2])
	// Output: 1.00 0.50 0.25
}
