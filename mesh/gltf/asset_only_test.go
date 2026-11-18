// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"errors"
	"fmt"
	"testing"

	"github.com/qmuntal/gltf"
	"nimbleterminal.dev/nt3d"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

// twoPlacements is one mesh of two triangles shown by two nodes, the second
// moved and under a parent: enough hierarchy and sharing to tell an asset by.
func twoPlacements() *gltf.Document {
	doc := gltfDoc(prim{positions: [][3]float32{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}}, indices: []uint16{0, 1, 2, 2, 1, 3}})
	doc.Nodes = []*gltf.Node{
		{Name: "first", Mesh: gltf.Index(0)},
		{Name: "group", Children: []int{2}, Translation: [3]float64{0, 2, 0}},
		{Name: "second", Mesh: gltf.Index(0), Translation: [3]float64{3, 0, 0}},
	}
	doc.Scenes = []*gltf.Scene{{Nodes: []int{0, 1}}}
	return doc
}

// assetSummary describes an asset's nodes: names, parents, placements, and
// how many triangles each shows.
func assetSummary(a *nt3d.SceneAsset) []string {
	var out []string
	for _, n := range a.Nodes {
		tris := 0
		switch {
		case n.Shared != nil:
			tris = len(n.Shared.Geometry().Indices) / 3
		case n.Geometry != nil:
			tris = len(n.Geometry.Indices) / 3
		}
		out = append(out, fmt.Sprintf("%s parent=%d local=%v triangles=%d shared=%t", n.Name, n.Parent, n.Local, tris, n.Shared != nil))
	}
	return out
}

// AssetOnly reads each primitive once, for the asset, and skips the flat
// view. The asset is the one a full decode builds.
func TestAssetOnlyBuildsTheSameAssetWithoutObjects(t *testing.T) {
	data := glb(t, twoPlacements())
	full, err := Decode(data, &nt3dmesh.Options{})
	check(t, err)
	only, err := Decode(data, &nt3dmesh.Options{AssetOnly: true})
	check(t, err)

	if len(full.Objects) != 2 || full.Triangles() != 4 {
		t.Fatalf("a full decode has %d objects and %d triangles, want 2 and 4", len(full.Objects), full.Triangles())
	}
	if len(only.Objects) != 0 {
		t.Fatalf("an asset-only decode built %d flat objects", len(only.Objects))
	}
	if only.Asset == nil {
		t.Fatal("an asset-only decode built no asset")
	}
	want, got := assetSummary(full.Asset), assetSummary(only.Asset)
	if len(got) != len(want) {
		t.Fatalf("asset-only has %d nodes, a full decode %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("node %d: asset-only %s, full %s", i, got[i], want[i])
		}
	}
	// The asset is usable as it is.
	s := nt3d.NewScene()
	if _, err := only.Asset.Instantiate(s, nt3d.Root, "model"); err != nil {
		t.Fatal(err)
	}
	f, err := s.Frame(nt3d.DefaultCamera(), 32, 32)
	check(t, err)
	if len(f.Draws) != 2 {
		t.Fatalf("the instantiated asset draws %d nodes, want 2", len(f.Draws))
	}
}

// The triangle limit holds without the flat pass that used to enforce it. A
// mesh several nodes share counts once, as the asset holds it once.
func TestAssetOnlyHonorsTheTriangleLimit(t *testing.T) {
	data := glb(t, twoPlacements())
	if _, err := Decode(data, &nt3dmesh.Options{AssetOnly: true, MaxTriangles: 1}); !errors.Is(err, nt3dmesh.ErrTooManyTriangles) {
		t.Fatalf("two triangles under a limit of one: error %v, want ErrTooManyTriangles", err)
	}
	if _, err := Decode(data, &nt3dmesh.Options{AssetOnly: true, MaxTriangles: 2}); err != nil {
		t.Fatalf("one shared mesh of two triangles under a limit of two: %v", err)
	}
	// The flat view places the mesh twice, and counts it twice.
	if _, err := Decode(data, &nt3dmesh.Options{MaxTriangles: 2}); !errors.Is(err, nt3dmesh.ErrTooManyTriangles) {
		t.Fatalf("a full decode of four placed triangles under a limit of two: error %v, want ErrTooManyTriangles", err)
	}
}

// A scene with nothing in it is refused either way.
func TestAssetOnlyRefusesAnEmptyScene(t *testing.T) {
	doc := gltf.NewDocument()
	doc.Nodes = []*gltf.Node{{Name: "empty"}}
	doc.Scenes = []*gltf.Scene{{Nodes: []int{0}}}
	data := glb(t, doc)
	_, fullErr := Decode(data, &nt3dmesh.Options{})
	_, onlyErr := Decode(data, &nt3dmesh.Options{AssetOnly: true})
	if fullErr == nil || onlyErr == nil || fullErr.Error() != onlyErr.Error() {
		t.Fatalf("an empty scene: full decode says %v, asset-only says %v", fullErr, onlyErr)
	}
}
