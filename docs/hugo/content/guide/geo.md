---
title: "Geographic scenes"
weight: 83
---

# Geographic scenes

`nt3d/geo` (package `nt3dgeo`) places things on the Earth. It converts between latitude, longitude and height, the Earth-centered frame that 3D Tiles and satellite positioning use, and the flat local frame a scene is drawn in, and it hands the result to a scene without losing precision on the way.

The package imports only `nt3d/math` and the standard library, so a server that writes tiles and a tool that checks them can share these conversions with a renderer without linking one. A test holds that. Everything is `float64`.

## Three frames

| Frame | Type | Units | Axes |
| --- | --- | --- | --- |
| Geodetic | `nt3dgeo.Geodetic` | degrees of latitude and longitude, meters of height | north and east positive |
| ECEF | `nt3dmath.Vec3d` | meters | X through latitude 0 and longitude 0, Z through the north pole |
| Local | `nt3dgeo.ENU` | meters | X east, Y north, Z up of an origin |

**Heights are above the WGS84 ellipsoid, never above sea level.** Surveys, lidar and elevation models usually give heights above the geoid, which near Stamford, Connecticut is about 29 m below the ellipsoid: a height read as the wrong kind puts everything 29 m too high. `nt3dgeo` has no geoid and does not guess; whoever reads such data converts it first.

```go
station := nt3dgeo.Geodetic{Lat: 41.0465, Lon: -73.5418, Height: 12.5}
if err := station.Validate(); err != nil { … } // off the Earth, or not a number

p := station.ECEF()               // Earth-centered meters
back, err := nt3dgeo.FromECEF(p)  // and back, to well under a millimeter
```

`FromECEF` holds that accuracy from 5000 km below the surface to past geostationary orbit, at the poles included.

## Drawing in a local frame

An `ENU` is meters east, north and up of its `Origin`. X east, Y north, Z up is nt3d's Z-up world, so a scene is drawn in an ENU frame as it is: pick an origin in the middle of what you draw, and every position becomes a few thousand meters instead of six million.

```go
city := nt3dgeo.ENU{Origin: nt3dgeo.Geodetic{Lat: 41.0465, Lon: -73.5418}}

harbor := city.Point(nt3dgeo.Geodetic{Lat: 41.0352, Lon: -73.5387, Height: 2})
// 260.7 m east, 1254.9 m south, 1.9 m up

where, err := city.Geodetic(harbor) // back to latitude and longitude
```

The frame is exact in three dimensions; it is not a map projection. The plane is flat and the Earth is not, so a point on the ground 14 km from the origin sits 15.7 m below the plane, and `Point` says so. Two frames a few kilometers apart are also turned slightly against each other. Go between frames through ECEF, with `ToECEF()` and `FromECEF()`, which are `nt3dmath.Mat4d` transforms, and neither effect is ignored:

```go
tileToCity := city.FromECEF().Mul(tile.ToECEF())
```

## Placing a model without losing precision

ECEF coordinates are about 6.4 million meters, and `float32` holds those to half a meter. A scene keeps what is large in `float64`: a node has an origin ([`SetNodeOrigin`](../scenes/#transforms)) beside its `float32` transform, a camera has `Camera.Origin`, and the scene subtracts one from the other before it rounds.

`ENU.Place` produces the two halves from a transform into ECEF, such as the product of a 3D Tiles tile's `transform` and its ancestors':

```go
origin, local, err := city.Place(tileToECEF)
if err != nil { … } // not finite, or not affine
id, err := scene.Add(parent, "tile")
err = scene.SetTransform(id, local)
err = scene.SetNodeOrigin(id, origin)
```

`origin` is where the model's own origin lies in the city frame, in `float64`. `local` turns and scales the model and carries no translation. A model 14 km from the city's origin lands within a tenth of a millimeter of where `float64` puts it.

This works when the model's own vertices are small numbers. A file that stores Earth-centered coordinates in its vertices has already lost the precision by the time it is `float32`; subtract a `float64` center from them while reading, and add it to the transform.

## Bounding regions

A 3D Tiles bounding region is a block between two longitudes, two latitudes and two heights. `Region` holds one in degrees; `RegionFromRadians` reads the six numbers a tileset writes, in radians, and `Radians()` writes them back. A region whose `West` is greater than its `East` crosses the antimeridian.

```go
region := nt3dgeo.RegionFromRadians(volume) // [west, south, east, north, min, max]
box, err := region.Box()                    // nt3dmath.OBBd, in ECEF meters
far := box.Distance(eyeECEF)                // 0 inside the box
```

`Box` returns an oriented box, with its axes east, north and up at the region's middle, that holds the whole curved block. The eight corners are not enough: the surface between them bulges outward, by about 2 m across a 10 km region and by hundreds of kilometers across a continent. The box is found by sampling both height surfaces and adding the most the surface can rise between samples, so it is never too small. It is loose by centimeters for a city tile.

`nt3dmath.OBBd` is the oriented box: `Distance` and `Nearest` measure to it, `Transformed` carries it through a `Mat4d`, and `Corners` and `Radius` describe it.

## Reference values

The conversions are tested against values computed by PROJ 9.9.0, in `geo/testdata/geo-vectors.json`: geodetic to ECEF at the equator, the poles, the antimeridian and Stamford; ENU at 1 m, 1 km and 10 km; and points on the surfaces of four regions. Another implementation of these frames can read the same file.
