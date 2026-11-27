// Copyright (c) 2026 Neomantra Corp.

// Package nt3dgeo places things on the Earth: WGS84 latitude, longitude, and
// height, the Earth-centered frame (ECEF) that 3D Tiles and satellite
// positioning use, and the local east-north-up frame a scene is drawn in.
//
// The three frames:
//
//   - [Geodetic]: degrees of latitude and longitude and meters above the
//     WGS84 ellipsoid. Heights are never above sea level: a height from a
//     survey or an elevation model is usually above the geoid, and must be
//     converted by whoever reads it, because this package has no geoid.
//   - ECEF: meters in float64, X through latitude 0 and longitude 0, Z through
//     the north pole. [Geodetic.ECEF] and [FromECEF] convert.
//   - [ENU]: meters east, north, and up of an origin on the Earth. X east,
//     Y north, Z up is nt3d's Z-up world, so a scene is drawn in an ENU frame
//     as it is.
//
// ECEF coordinates are millions of meters, past what float32 holds to better
// than half a meter. [ENU.Place] turns a transform into ECEF into what a
// scene takes: a float64 node origin and a float32 matrix with no translation.
//
// [Region] is a 3D Tiles bounding region, and [Region.Box] the oriented box
// around it.
//
// The package imports only the standard library and nt3d/math, so a server or
// a tool can share these conversions with a renderer without linking one. It
// is tested against reference values computed by PROJ, in
// testdata/geo-vectors.json.
package nt3dgeo

import (
	"errors"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// The WGS84 ellipsoid (EPSG:4979 geodetic, EPSG:4978 Earth-centered).
const (
	// SemiMajorAxis is the ellipsoid's equatorial radius, in meters.
	SemiMajorAxis = 6378137.0
	// Flattening is how much shorter the polar radius is, as a fraction.
	Flattening = 1 / 298.257223563
	// SemiMinorAxis is the ellipsoid's polar radius, in meters.
	SemiMinorAxis = SemiMajorAxis * (1 - Flattening)

	eccentricity2       = Flattening * (2 - Flattening)
	secondEccentricity2 = (SemiMajorAxis*SemiMajorAxis - SemiMinorAxis*SemiMinorAxis) / (SemiMinorAxis * SemiMinorAxis)
)

// Geodetic is a position on the Earth: latitude and longitude in degrees,
// north and east positive, and height in meters above the WGS84 ellipsoid.
// The zero value is where the equator meets the prime meridian.
type Geodetic struct {
	Lat    float64 `json:"lat"`
	Lon    float64 `json:"lon"`
	Height float64 `json:"height"`
}

// Validate reports a position that is not on the Earth: a number that is not
// finite, a latitude outside -90 to 90, or a longitude outside -180 to 180.
func (g Geodetic) Validate() error {
	if !nt3dmath.IsFinite(g.Lat) || !nt3dmath.IsFinite(g.Lon) || !nt3dmath.IsFinite(g.Height) {
		return errors.New("nt3dgeo: position is not finite")
	}
	if math.Abs(g.Lat) > 90 {
		return errors.New("nt3dgeo: latitude must be within -90 to 90 degrees")
	}
	if math.Abs(g.Lon) > 180 {
		return errors.New("nt3dgeo: longitude must be within -180 to 180 degrees")
	}
	return nil
}

// ECEF returns g in Earth-centered meters. g must be valid.
func (g Geodetic) ECEF() nt3dmath.Vec3d {
	sinLat, cosLat := sincosDegrees(g.Lat)
	sinLon, cosLon := sincosDegrees(g.Lon)
	n := primeVertical(sinLat)
	r := (n + g.Height) * cosLat
	return nt3dmath.Vec3d{X: r * cosLon, Y: r * sinLon, Z: (n*(1-eccentricity2) + g.Height) * sinLat}
}

// FromECEF returns the position of the Earth-centered point p. It agrees with
// Geodetic.ECEF to well under a millimeter from 5000 km below the surface up
// past geostationary orbit, at the poles included, where the longitude is 0.
// A point that is not finite is an error; the Earth's center, which has no
// direction, comes back as latitude 0, longitude 0.
func FromECEF(p nt3dmath.Vec3d) (Geodetic, error) {
	if !p.IsFinite() {
		return Geodetic{}, errors.New("nt3dgeo: point is not finite")
	}
	r := math.Hypot(p.X, p.Y)
	if r < 1e-9 {
		// On the axis the latitude is a pole's and the iteration below has
		// nothing to divide by.
		switch {
		case p.Z > 0:
			return Geodetic{Lat: 90, Height: p.Z - SemiMinorAxis}, nil
		case p.Z < 0:
			return Geodetic{Lat: -90, Height: -p.Z - SemiMinorAxis}, nil
		}
		return Geodetic{Height: -SemiMajorAxis}, nil
	}
	// Bowring's iteration on the reduced latitude: a few rounds reach the
	// last bit at any height a scene uses.
	beta := math.Atan2(SemiMajorAxis*p.Z, SemiMinorAxis*r)
	lat := 0.0
	for range 8 {
		sinB, cosB := math.Sincos(beta)
		next := math.Atan2(p.Z+secondEccentricity2*SemiMinorAxis*sinB*sinB*sinB, r-eccentricity2*SemiMajorAxis*cosB*cosB*cosB)
		done := math.Abs(next-lat) < 1e-16
		lat = next
		if done {
			break
		}
		sinLat, cosLat := math.Sincos(lat)
		beta = math.Atan2((1-Flattening)*sinLat, cosLat)
	}
	sinLat, cosLat := math.Sincos(lat)
	n := primeVertical(sinLat)
	return Geodetic{
		Lat:    lat * 180 / math.Pi,
		Lon:    math.Atan2(p.Y, p.X) * 180 / math.Pi,
		Height: r*cosLat + (p.Z+eccentricity2*n*sinLat)*sinLat - n,
	}, nil
}

// primeVertical is the ellipsoid's radius of curvature across the meridian
// at the latitude whose sine is sinLat.
func primeVertical(sinLat float64) float64 {
	return SemiMajorAxis / math.Sqrt(1-eccentricity2*sinLat*sinLat)
}

// sincosDegrees is math.Sincos of an angle in degrees, exact at the quarter
// turns, so a pole is on the axis and the antimeridian has no Y.
func sincosDegrees(degrees float64) (sin, cos float64) {
	switch degrees {
	case 0:
		return 0, 1
	case 90:
		return 1, 0
	case -90:
		return -1, 0
	case 180, -180:
		return 0, -1
	}
	return math.Sincos(degrees * math.Pi / 180)
}
