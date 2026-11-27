// Copyright (c) 2026 Neomantra Corp.

package nt3dgeo_test

import (
	"fmt"

	"nimbleterminal.dev/nt3d/geo"
)

// A scene of a city is drawn in meters east, north, and up of a point in it.
// Positions on the Earth go into that frame, and come back out of it.
func ExampleENU() {
	station := nt3dgeo.Geodetic{Lat: 41.0465, Lon: -73.5418}
	city := nt3dgeo.ENU{Origin: station}

	harbor := city.Point(nt3dgeo.Geodetic{Lat: 41.0352, Lon: -73.5387, Height: 2})
	fmt.Printf("harbor: %.1f m east, %.1f m north, %.1f m up\n", harbor.X, harbor.Y, harbor.Z)

	back, _ := city.Geodetic(harbor)
	fmt.Printf("and back: %.4f, %.4f at %.1f m\n", back.Lat, back.Lon, back.Height)
	// Output:
	// harbor: 260.7 m east, -1254.9 m north, 1.9 m up
	// and back: 41.0352, -73.5387 at 2.0 m
}

// A 3D Tiles tile carries a transform into Earth-centered meters, millions of
// meters long. Place splits it for a scene: a float64 origin for
// Scene.SetNodeOrigin, and a float32 matrix that only turns the tile.
func ExampleENU_Place() {
	city := nt3dgeo.ENU{Origin: nt3dgeo.Geodetic{Lat: 41.0465, Lon: -73.5418}}
	// A tile modeled east-north-up around a point 10 km away.
	tile := nt3dgeo.ENU{Origin: nt3dgeo.Geodetic{Lat: 41.1365, Lon: -73.4228, Height: 100}}

	origin, local, err := city.Place(tile.ToECEF())
	fmt.Printf("origin: %.3f %.3f %.3f\n", origin[0], origin[1], origin[2])
	fmt.Printf("translation left in float32: %v %v %v, err: %v\n", local[12], local[13], local[14], err)
	// Output:
	// origin: 9991.552 10001.977 84.325
	// translation left in float32: 0 0 0, err: <nil>
}

// A tile's bounding region becomes a box in Earth-centered meters that holds
// its curved surface, which a selection measures its distance to.
func ExampleRegion_Box() {
	region := nt3dgeo.Region{West: -73.60, South: 41.00, East: -73.48, North: 41.09, MaxHeight: 120}
	box, err := region.Box()
	fmt.Printf("%.0f m east to west, %.0f m north to south, %.0f m tall, err: %v\n",
		2*box.Axes[0].Len(), 2*box.Axes[1].Len(), 2*box.Axes[2].Len(), err)

	inside := nt3dgeo.Geodetic{Lat: 41.0465, Lon: -73.5418, Height: 12.5}
	above := nt3dgeo.Geodetic{Lat: 41.0465, Lon: -73.5418, Height: 1000}
	fmt.Printf("the station is %.0f m from the box, a point 1 km up %.0f m\n", box.Distance(inside.ECEF()), box.Distance(above.ECEF()))
	// Output:
	// 10096 m east to west, 9997 m north to south, 124 m tall, err: <nil>
	// the station is 0 m from the box, a point 1 km up 880 m
}
