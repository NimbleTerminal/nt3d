// Copyright (c) 2026 Neomantra Corp.

package nt3dgeo

import (
	"errors"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// Region is a block of the Earth between two longitudes, two latitudes, and
// two heights: a 3D Tiles bounding region. Angles are degrees and heights are
// meters above the WGS84 ellipsoid. A region whose West is greater than its
// East crosses the antimeridian, running east from West through 180 to East.
type Region struct {
	West, South, East, North float64
	MinHeight, MaxHeight     float64
}

// RegionFromRadians returns the region a 3D Tiles file writes as
// [west, south, east, north, minimum height, maximum height], its angles in
// radians.
func RegionFromRadians(v [6]float64) Region {
	const degrees = 180 / math.Pi
	return Region{West: v[0] * degrees, South: v[1] * degrees, East: v[2] * degrees, North: v[3] * degrees, MinHeight: v[4], MaxHeight: v[5]}
}

// Radians returns r as a 3D Tiles file writes it.
func (r Region) Radians() [6]float64 {
	const radians = math.Pi / 180
	return [6]float64{r.West * radians, r.South * radians, r.East * radians, r.North * radians, r.MinHeight, r.MaxHeight}
}

// Validate reports a region that bounds nothing: a number that is not finite,
// a longitude outside -180 to 180 or a latitude outside -90 to 90 (with the
// rounding of a file's radians allowed for), a South above its North, or a
// MinHeight above its MaxHeight.
func (r Region) Validate() error {
	for _, v := range []float64{r.West, r.South, r.East, r.North, r.MinHeight, r.MaxHeight} {
		if !nt3dmath.IsFinite(v) {
			return errors.New("nt3dgeo: region is not finite")
		}
	}
	const slack = 1e-9
	if math.Abs(r.West) > 180+slack || math.Abs(r.East) > 180+slack {
		return errors.New("nt3dgeo: region longitudes must be within -180 to 180 degrees")
	}
	if math.Abs(r.South) > 90+slack || math.Abs(r.North) > 90+slack || r.South > r.North {
		return errors.New("nt3dgeo: region latitudes must be within -90 to 90 degrees, south no greater than north")
	}
	if r.MinHeight > r.MaxHeight {
		return errors.New("nt3dgeo: region minimum height is above its maximum")
	}
	return nil
}

// regionSteps is how many spans a region's surface is sampled across, each
// way, to bound it.
const regionSteps = 16

// Box returns an oriented box, in Earth-centered meters, that holds the whole
// of r, with its axes east, north, and up at r's middle.
//
// The box bounds the curved block and not only its eight corners: the surface
// between the corners bulges past them, by about 2 m across a 10 km region
// and by hundreds of kilometers across a continent. It is found by sampling
// both height surfaces and adding the most the surface can rise between
// samples, so it is never too small, and is loose by centimeters for a city
// tile and by about 2% of the Earth's radius for a region that spans it.
func (r Region) Box() (nt3dmath.OBBd, error) {
	if err := r.Validate(); err != nil {
		return nt3dmath.OBBd{}, err
	}
	south, north := max(-90, r.South), min(90, r.North)
	west, span := r.West, r.East-r.West
	if span < 0 {
		span += 360
	}
	frame := ENU{Origin: Geodetic{Lat: (south + north) / 2, Lon: wrapLongitude(west + span/2), Height: (r.MinHeight + r.MaxHeight) / 2}}
	lo := nt3dmath.Vec3d{X: math.Inf(1), Y: math.Inf(1), Z: math.Inf(1)}
	hi := lo.Scale(-1)
	include := func(lat, lon float64) {
		for _, height := range [2]float64{r.MinHeight, r.MaxHeight} {
			p := frame.Point(Geodetic{Lat: lat, Lon: wrapLongitude(lon), Height: height})
			lo = nt3dmath.Vec3d{X: min(lo.X, p.X), Y: min(lo.Y, p.Y), Z: min(lo.Z, p.Z)}
			hi = nt3dmath.Vec3d{X: max(hi.X, p.X), Y: max(hi.Y, p.Y), Z: max(hi.Z, p.Z)}
		}
	}
	for i := range regionSteps + 1 {
		lat := south + (north-south)*float64(i)/regionSteps
		for j := range regionSteps + 1 {
			include(lat, west+span*float64(j)/regionSteps)
		}
	}
	// Between two samples the surface is an arc and the samples its chord.
	// The arc rises above the chord by no more than its sagitta on the most
	// gently curved circle there is, of the meridian's greatest radius; one
	// sagitta for each way the samples step bounds the surface between four.
	radius := SemiMajorAxis*SemiMajorAxis/SemiMinorAxis + max(0, r.MaxHeight)
	sagitta := func(degrees float64) float64 {
		s := math.Sin(degrees / regionSteps * math.Pi / 180 / 4)
		return radius * 2 * s * s
	}
	pad := sagitta(north-south) + sagitta(span)
	half := hi.Sub(lo).Scale(.5).Add(nt3dmath.Vec3d{X: pad, Y: pad, Z: pad})
	box := nt3dmath.OBBd{
		Center: lo.Add(hi).Scale(.5),
		Axes:   [3]nt3dmath.Vec3d{{X: half.X}, {Y: half.Y}, {Z: half.Z}},
	}.Transformed(frame.ToECEF())
	if !box.IsFinite() {
		return nt3dmath.OBBd{}, errors.New("nt3dgeo: region is too large to bound")
	}
	return box, nil
}

// wrapLongitude brings a longitude in degrees into -180 to 180.
func wrapLongitude(lon float64) float64 {
	switch {
	case lon > 180:
		return lon - 360
	case lon < -180:
		return lon + 360
	}
	return lon
}
