// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3d

import (
	"errors"
	"math"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d/geo"
	"nimbleterminal.dev/nt3d/math"
)

const explicitTileset = `{
  "asset": {"version": "1.1", "tilesetVersion": "7", "copyright": "© Example\u0007 Survey\n2026",
            "extras": {"attribution": ["OpenStreetMap contributors (ODbL)", "© Example Survey 2026", ""]}},
  "geometricError": 500,
  "schema": {"id": "city", "classes": {"building": {"properties": {
      "height": {"type": "SCALAR", "componentType": "FLOAT32", "semantic": "HEIGHT"},
      "use": {"type": "ENUM", "enumType": "use"}}}},
      "enums": {"use": {"values": [{"name": "home", "value": 0}, {"name": "shop", "value": 1}]}}},
  "metadata": {"class": "building", "properties": {"height": 12.5}},
  "groups": [{"class": "building", "properties": {"use": "shop"}}],
  "root": {
    "transform": [1,0,0,0, 0,1,0,0, 0,0,1,0, 1364789.25,-4619824.5,4166319.125,1],
    "boundingVolume": {"box": [0,0,10, 100,0,0, 0,50,0, 0,0,10]},
    "geometricError": 100,
    "refine": "ADD",
    "content": {"uri": "root.glb", "group": 0, "boundingVolume": {"sphere": [0,0,10,60]}},
    "children": [
      {"boundingVolume": {"region": [-1.2836,0.7163,-1.2834,0.7165,-30,120]}, "geometricError": 10,
       "contents": [{"uri": "a.glb"}, {"uri": "b.b3dm"}]},
      {"boundingVolume": {"sphere": [50,0,10,30]}, "geometricError": 0, "refine": "replace",
       "content": {"url": "legacy.b3dm"}, "metadata": {"class": "building"}}
    ]
  }
}`

// A tileset comes back as plain data that says what the document said: the
// tree, each tile's transform, bounds, error, refinement and contents, the
// schema and metadata, and who to credit, as text safe to show.
func TestParseTilesetReadsTheWholeDocument(t *testing.T) {
	ts, err := ParseTileset([]byte(explicitTileset), nil)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Asset.Version != "1.1" || ts.Asset.TilesetVersion != "7" || ts.GeometricError != 500 {
		t.Fatalf("asset %+v, error %g", ts.Asset, ts.GeometricError)
	}
	if got := ts.Asset.Credits(); len(got) != 2 || got[0] != "© Example Survey 2026" || got[1] != "OpenStreetMap contributors (ODbL)" {
		t.Fatalf("credits %q: want the copyright, cleaned to one printable line, then the attribution that does not repeat it", got)
	}
	root := ts.Root
	if root.Transform[12] != 1364789.25 || root.Refine != RefineAdd || root.GeometricError != 100 || len(root.Children) != 2 {
		t.Fatalf("root %+v", root)
	}
	if root.Bounds.Kind != VolumeBox || root.Bounds.Box.Axes[1].Y != 50 || root.Bounds.Box.Center.Z != 10 {
		t.Fatalf("root bounds %+v", root.Bounds)
	}
	if c := root.Contents; len(c) != 1 || c[0].URI != "root.glb" || c[0].Group != 0 || !c[0].HasBounds || c[0].Bounds.Radius != 60 {
		t.Fatalf("root contents %+v", c)
	}
	a, b := root.Children[0], root.Children[1]
	if a.Refine != RefineInherit || a.Transform != nt3dmath.IdentityD() || a.Bounds.Kind != VolumeRegion || len(a.Contents) != 2 || a.Contents[1].URI != "b.b3dm" || a.Contents[0].Group != -1 {
		t.Fatalf("first child %+v", a)
	}
	if math.Abs(a.Bounds.Region.North-0.7165*180/math.Pi) > 1e-12 || a.Bounds.Region.MaxHeight != 120 {
		t.Fatalf("region %+v: want the file's radians as degrees", a.Bounds.Region)
	}
	if b.Refine != RefineReplace || b.Contents[0].URI != "legacy.b3dm" || b.Metadata == nil || b.Metadata.Class != "building" {
		t.Fatalf("second child %+v: want lower-case refine and the 1.0 url read", b)
	}
	if p := ts.Schema.Classes["building"].Properties["height"]; p.Type != "SCALAR" || p.ComponentType != "FLOAT32" || p.Semantic != "HEIGHT" {
		t.Fatalf("schema property %+v", p)
	}
	if e := ts.Schema.Enums["use"]; e.ValueType != "UINT16" || len(e.Values) != 2 || e.Values[1].Name != "shop" {
		t.Fatalf("schema enum %+v", e)
	}
	if ts.Metadata.Properties["height"] != 12.5 || ts.Groups[0].Properties["use"] != "shop" {
		t.Fatalf("metadata %+v, groups %+v", ts.Metadata, ts.Groups)
	}
}

// What the specification requires of a tileset is required, and a document
// that asks for more than its limits is refused with ErrTooLarge.
func TestParseTilesetRefusesWhatIsWrong(t *testing.T) {
	tile := `{"boundingVolume": {"sphere": [0,0,0,1]}, "geometricError": 1}`
	doc := func(root string) string {
		return `{"asset": {"version": "1.1"}, "geometricError": 1, "root": ` + root + `}`
	}
	for name, text := range map[string]string{
		"not JSON":              `{`,
		"no asset":              `{"geometricError": 1, "root": ` + tile + `}`,
		"version 2.0":           `{"asset": {"version": "2.0"}, "geometricError": 1, "root": ` + tile + `}`,
		"no root":               `{"asset": {"version": "1.1"}, "geometricError": 1}`,
		"no tileset error":      `{"asset": {"version": "1.1"}, "root": ` + tile + `}`,
		"no bounding volume":    doc(`{"geometricError": 1}`),
		"no tile error":         doc(`{"boundingVolume": {"sphere": [0,0,0,1]}}`),
		"negative error":        doc(`{"boundingVolume": {"sphere": [0,0,0,1]}, "geometricError": -1}`),
		"empty volume":          doc(`{"boundingVolume": {}, "geometricError": 1}`),
		"short box":             doc(`{"boundingVolume": {"box": [0,0,0]}, "geometricError": 1}`),
		"negative radius":       doc(`{"boundingVolume": {"sphere": [0,0,0,-1]}, "geometricError": 1}`),
		"region past the pole":  doc(`{"boundingVolume": {"region": [0,0,1,2,0,1]}, "geometricError": 1}`),
		"short transform":       doc(`{"transform": [1,0,0], "boundingVolume": {"sphere": [0,0,0,1]}, "geometricError": 1}`),
		"projective transform":  doc(`{"transform": [1,0,0,1, 0,1,0,0, 0,0,1,0, 0,0,0,1], "boundingVolume": {"sphere": [0,0,0,1]}, "geometricError": 1}`),
		"flat transform":        doc(`{"transform": [1,0,0,0, 0,1,0,0, 0,0,0,0, 0,0,0,1], "boundingVolume": {"sphere": [0,0,0,1]}, "geometricError": 1}`),
		"unknown refine":        doc(`{"refine": "BLEND", "boundingVolume": {"sphere": [0,0,0,1]}, "geometricError": 1}`),
		"content without a uri": doc(`{"content": {}, "boundingVolume": {"sphere": [0,0,0,1]}, "geometricError": 1}`),
		"content and contents":  doc(`{"content": {"uri": "a"}, "contents": [{"uri": "b"}], "boundingVolume": {"sphere": [0,0,0,1]}, "geometricError": 1}`),
		"group out of range":    doc(`{"content": {"uri": "a", "group": 3}, "boundingVolume": {"sphere": [0,0,0,1]}, "geometricError": 1}`),
		"null child":            doc(`{"children": [null], "boundingVolume": {"sphere": [0,0,0,1]}, "geometricError": 1}`),
		"required extension":    `{"asset": {"version": "1.1"}, "extensionsRequired": ["3DTILES_bounding_volume_S2"], "geometricError": 1, "root": ` + tile + `}`,
	} {
		if _, err := ParseTileset([]byte(text), nil); err == nil {
			t.Errorf("%s: accepted", name)
		} else if errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: called too large: %v", name, err)
		}
	}
	nested := strings.Repeat(`{"boundingVolume": {"sphere": [0,0,0,1]}, "geometricError": 1, "children": [`, 5) + tile + strings.Repeat(`]}`, 5)
	for name, o := range map[string]*Options{"bytes": {MaxBytes: 64}, "tiles": {MaxTiles: 5}, "depth": {MaxDepth: 5}} {
		if _, err := ParseTileset([]byte(doc(nested)), o); !errors.Is(err, ErrTooLarge) {
			t.Errorf("over the %s limit: %v, want ErrTooLarge", name, err)
		}
	}
	if _, err := ParseTileset([]byte(doc(nested)), &Options{MaxTiles: 6, MaxDepth: 6}); err != nil {
		t.Errorf("at the limits: %v", err)
	}
}

// A volume becomes a box in Earth-centered meters: a box goes through the
// tile's transform, a region is already on the Earth and ignores it, and a
// sphere becomes the cube around it, scaled as the transform scales.
func TestVolumeWorld(t *testing.T) {
	move := nt3dmath.TRSd(nt3dmath.Vec3d{X: 1000, Y: 2000, Z: 3000}, nt3dmath.Quatd{W: 1}, nt3dmath.Vec3d{X: 2, Y: 2, Z: 2})
	box, err := Volume{Kind: VolumeBox, Box: nt3dmath.OBBd{Center: nt3dmath.Vec3d{X: 1}, Axes: [3]nt3dmath.Vec3d{{X: 5}, {Y: 4}, {Z: 3}}}}.World(move)
	if err != nil || box.Center != (nt3dmath.Vec3d{X: 1002, Y: 2000, Z: 3000}) || box.Axes[1].Y != 8 {
		t.Fatalf("box %+v, %v", box, err)
	}
	sphere, err := Volume{Kind: VolumeSphere, Center: nt3dmath.Vec3d{Y: 1}, Radius: 10}.World(move)
	if err != nil || sphere.Center != (nt3dmath.Vec3d{X: 1000, Y: 2002, Z: 3000}) || math.Abs(sphere.Axes[0].X-20) > 1e-9 {
		t.Fatalf("sphere %+v, %v: want a cube of half side 20 around the moved center", sphere, err)
	}
	region := nt3dgeo.Region{West: -73.60, South: 41.00, East: -73.48, North: 41.09, MaxHeight: 100}
	want, _ := region.Box()
	got, err := Volume{Kind: VolumeRegion, Region: region}.World(move)
	if err != nil || got != want {
		t.Fatalf("a region moved with its tile's transform: %+v, %v", got, err)
	}
}

// A tileset's center is the middle of its root's bounds, as a position on
// the Earth, whatever kind of volume bounds it.
func TestTilesetCenter(t *testing.T) {
	ts, err := ParseTileset([]byte(explicitTileset), nil)
	if err != nil {
		t.Fatal(err)
	}
	// The root's box is centered 10 m along Z of a transform that only moves.
	want := nt3dmath.Vec3d{X: 1364789.25, Y: -4619824.5, Z: 4166319.125 + 10}
	center, err := ts.Center()
	if err != nil || center.ECEF().Dist(want) > 1e-6 {
		t.Fatalf("center %+v (%v), %g m from the box's middle", center, err, center.ECEF().Dist(want))
	}
	region, err := ParseTileset([]byte(`{"asset":{"version":"1.1"},"geometricError":1,"root":{"boundingVolume":{"region":[-1.2836,0.7163,-1.2834,0.7165,0,100]},"geometricError":0}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	center, err = region.Center()
	if err != nil || math.Abs(center.Lat-0.7164*180/math.Pi) > 1e-6 || math.Abs(center.Lon+1.2835*180/math.Pi) > 1e-6 {
		t.Fatalf("a region's center is %+v (%v), want its middle latitude and longitude", center, err)
	}
}

// A sphere carried through a transform that stretches it askew is an
// ellipsoid, and its box holds all of it and no more than it must: each
// half side is how far the ellipsoid reaches along that axis. Columns of
// equal length do not make a transform an even scale.
func TestASkewedSpheresBoxHoldsItsSurface(t *testing.T) {
	// A turn of an eighth, then a stretch of 2, 1 and 1.58 along x, y, z:
	// all three columns come out 1.58 long, and the sphere reaches x = 2.
	skew := nt3dmath.TRSd(nt3dmath.Vec3d{}, nt3dmath.Quatd{W: 1}, nt3dmath.Vec3d{X: 2, Y: 1, Z: math.Sqrt(2.5)}).
		Mul(nt3dmath.TRSd(nt3dmath.Vec3d{}, nt3dmath.QuatdAxisAngle(nt3dmath.Vec3d{Z: 1}, math.Pi/4), nt3dmath.Vec3d{X: 1, Y: 1, Z: 1}))
	box, err := Volume{Kind: VolumeSphere, Radius: 1}.World(skew)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 64; i++ {
		for j := 0; j <= 32; j++ {
			a, b := float64(i)*math.Pi/32, float64(j)*math.Pi/32
			p := skew.Point(nt3dmath.Vec3d{X: math.Sin(b) * math.Cos(a), Y: math.Sin(b) * math.Sin(a), Z: math.Cos(b)})
			if d := box.Distance(p); d > 1e-9 {
				t.Fatalf("the sphere's surface at %v is %g outside its box %+v", p, d, box)
			}
		}
	}
	for axis, want := range []float64{2, 1, math.Sqrt(2.5)} {
		got := []float64{box.Axes[0].X, box.Axes[1].Y, box.Axes[2].Z}[axis]
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("the box reaches %g along axis %d, and the sphere %g", got, axis, want)
		}
	}
}
