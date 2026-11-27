// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3d

import (
	"errors"
	"fmt"
	"math"

	"nimbleterminal.dev/nt3d/geo"
	"nimbleterminal.dev/nt3d/math"
)

// VolumeKind is which of the three bounding volumes a Volume is.
type VolumeKind uint8

const (
	// VolumeBox is an oriented box in the tile's space.
	VolumeBox VolumeKind = iota
	// VolumeRegion is a block of the Earth between longitudes, latitudes,
	// and heights. It is in no tile's space: transforms do not move it.
	VolumeRegion
	// VolumeSphere is a sphere in the tile's space.
	VolumeSphere
)

// Volume is a tile's bounding volume: the one of Box, Region, and Sphere
// that Kind names.
type Volume struct {
	Kind   VolumeKind
	Box    nt3dmath.OBBd
	Region nt3dgeo.Region
	// Center and Radius are the sphere's.
	Center nt3dmath.Vec3d
	Radius float64
}

type rawVolume struct {
	Box    []float64 `json:"box"`
	Region []float64 `json:"region"`
	Sphere []float64 `json:"sphere"`
}

func (r *rawVolume) volume() (Volume, error) {
	finite := func(v []float64) bool {
		for _, x := range v {
			if !nt3dmath.IsFinite(x) {
				return false
			}
		}
		return true
	}
	switch {
	case r.Box != nil:
		if len(r.Box) != 12 || !finite(r.Box) {
			return Volume{}, errors.New("nt3dtiles3d: a bounding box is 12 finite numbers")
		}
		b := r.Box
		return Volume{Kind: VolumeBox, Box: nt3dmath.OBBd{
			Center: nt3dmath.Vec3d{X: b[0], Y: b[1], Z: b[2]},
			Axes:   [3]nt3dmath.Vec3d{{X: b[3], Y: b[4], Z: b[5]}, {X: b[6], Y: b[7], Z: b[8]}, {X: b[9], Y: b[10], Z: b[11]}},
		}}, nil
	case r.Region != nil:
		if len(r.Region) != 6 {
			return Volume{}, errors.New("nt3dtiles3d: a bounding region is 6 numbers")
		}
		region := nt3dgeo.RegionFromRadians([6]float64(r.Region))
		if err := region.Validate(); err != nil {
			return Volume{}, fmt.Errorf("nt3dtiles3d: bounding region: %w", err)
		}
		return Volume{Kind: VolumeRegion, Region: region}, nil
	case r.Sphere != nil:
		if len(r.Sphere) != 4 || !finite(r.Sphere) || r.Sphere[3] < 0 {
			return Volume{}, errors.New("nt3dtiles3d: a bounding sphere is 4 finite numbers, its radius zero or more")
		}
		s := r.Sphere
		return Volume{Kind: VolumeSphere, Center: nt3dmath.Vec3d{X: s[0], Y: s[1], Z: s[2]}, Radius: s[3]}, nil
	}
	return Volume{}, errors.New("nt3dtiles3d: a bounding volume must be a box, a region, or a sphere")
}

// World returns v as an oriented box in Earth-centered meters, given toECEF,
// the product of the tile's transform and its ancestors'. It is what a
// selection measures distance to and tests against the view.
//
// A box is carried through the transform. A region is already on the Earth
// and ignores it; its box holds its curved surface (nt3dgeo.Region.Box). A
// sphere becomes the cube around it, so a distance to it is at most the
// sphere's, never more.
func (v Volume) World(toECEF nt3dmath.Mat4d) (nt3dmath.OBBd, error) {
	var box nt3dmath.OBBd
	switch v.Kind {
	case VolumeBox:
		box = v.Box.Transformed(toECEF)
	case VolumeRegion:
		return v.Region.Box()
	case VolumeSphere:
		// The greatest stretch of the transform is at most the root of the
		// sum of its columns' squared lengths.
		x := toECEF.Direction(nt3dmath.Vec3d{X: 1})
		y := toECEF.Direction(nt3dmath.Vec3d{Y: 1})
		z := toECEF.Direction(nt3dmath.Vec3d{Z: 1})
		r := v.Radius * math.Max(x.Len(), math.Max(y.Len(), z.Len()))
		if uniform := math.Abs(x.Len()-y.Len()) < 1e-9*x.Len() && math.Abs(x.Len()-z.Len()) < 1e-9*x.Len(); !uniform {
			r = v.Radius * math.Sqrt(x.LenSq()+y.LenSq()+z.LenSq())
		}
		box = nt3dmath.OBBd{Center: toECEF.Point(v.Center), Axes: [3]nt3dmath.Vec3d{{X: r}, {Y: r}, {Z: r}}}
	}
	if !box.IsFinite() {
		return nt3dmath.OBBd{}, errors.New("nt3dtiles3d: bounding volume is too large to place")
	}
	return box, nil
}
