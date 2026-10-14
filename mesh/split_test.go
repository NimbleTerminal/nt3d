// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"testing"

	"nimbleterminal.dev/nt3d"
)

// pieces lowers the triangles an Object may hold.
func pieces(t *testing.T, n int) {
	t.Helper()
	real := pieceTriangles
	pieceTriangles = n
	t.Cleanup(func() { pieceTriangles = real })
}

// sizes lists each object's triangles, checking it is valid geometry.
func sizes(t *testing.T, m *Model) (out []int) {
	t.Helper()
	for _, o := range m.Objects {
		check(t, o.Geometry.Validate())
		if len(o.Plain) != len(o.Geometry.Vertices) {
			t.Fatalf("%d plain marks for %d vertices", len(o.Plain), len(o.Geometry.Vertices))
		}
		out = append(out, len(o.Geometry.Indices)/3)
	}
	return out
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLargeObjectsAreSplit(t *testing.T) {
	pieces(t, 5)
	m, err := DecodeSTL(binarySTLData("", make([]uint16, 12)...), nil)
	check(t, err)
	if got := sizes(t, m); !equal(got, []int{5, 5, 2}) || m.Triangles() != 12 {
		t.Fatalf("STL pieces %v", got)
	}

	pieces(t, 3)
	files := cubeFiles(t)
	m, err = DecodeOBJ([]byte(cubeOBJ), &Options{Open: opener(files)})
	check(t, err)
	// Wood (4 triangles, shared vertices) and paint (8) split into pieces of
	// three, each with vertices of its own and its material.
	if got := sizes(t, m); !equal(got, []int{3, 1, 3, 3, 2, 0}) || m.Triangles() != 12 {
		t.Fatalf("OBJ pieces %v", got)
	}
	if m.Objects[1].Material != "wood" || m.Objects[1].Geometry.Material == nil || m.Objects[1].Name != "box" || len(m.Objects[1].Geometry.Vertices) != 3 {
		t.Fatalf("second wood piece %+v", m.Objects[1].Name)
	}

	m, err = Decode3MF(assembly3MF(t), nil)
	check(t, err)
	if got := sizes(t, m); !equal(got, []int{3, 1, 3, 1, 3, 1}) || m.Objects[3].Part != 1 || m.Objects[3].Name != "Lid" || len(m.Parts) != 3 {
		t.Fatalf("3MF pieces %v", got)
	}

	s := nt3d.NewScene()
	group, err := m.AddTo(s, nt3d.Root, "assembly")
	check(t, err)
	if node, _ := s.Node(group); len(node.Children) != 6 {
		t.Fatal(node.Children)
	}
}

func TestTriangleBudget(t *testing.T) {
	if (*Options)(nil).limit() != DefaultMaxTriangles || (&Options{MaxTriangles: 10_000_000}).limit() != 10_000_000 {
		t.Fatal("the budget is not the caller's to raise")
	}
}
