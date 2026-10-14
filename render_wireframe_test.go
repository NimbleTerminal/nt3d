package nt3d

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"nimbleterminal.dev/nt3d/math"
)

func TestWireframeLabelsUseDisplayColumns(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cols   int
		labels []CellText
		want   string
	}{
		{"clusters", 8, []CellText{{Text: "e\u0301界👩‍💻"}}, "e\u0301界👩‍💻   "},
		{"right clipping", 6, []CellText{{X: 4, Text: "A界"}}, "    A "},
		{"left clipping", 6, []CellText{{X: -1, Text: "界Z"}}, " Z    "},
		{"overwrite continuation", 6, []CellText{{Text: "界"}, {X: 1, Text: "x"}}, " x    "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := frame(t, NewScene())
			f.CellText = tc.labels
			wire, err := RenderWireframe(f, tc.cols, 2)
			check(t, err)
			rows := strings.Split(ansi.Strip(wire), "\n")
			if len(rows) != 2 || rows[0] != tc.want || rows[1] != strings.Repeat(" ", tc.cols) {
				t.Fatalf("unexpected rows: %q", rows)
			}
			for _, row := range rows {
				if width := ansi.StringWidth(row); width != tc.cols {
					t.Fatalf("width %d, want %d: %q", width, tc.cols, row)
				}
			}
		})
	}
}

func TestWireframeCoalescesColors(t *testing.T) {
	f := frame(t, NewScene())
	f.Matrix = nt3dmath.Identity()
	ink := color.RGBA{R: 12, G: 34, B: 56, A: 255}
	f.DynamicLines = []Vertex{{Position: nt3dmath.Vec3{X: -.5, Z: .5}, Color: ink}, {Position: nt3dmath.Vec3{X: .5, Z: .5}, Color: ink}}
	f.CellText = []CellText{{Text: "label", Color: ink}}
	wire, err := RenderWireframe(f, 8, 4)
	check(t, err)
	// One foreground sequence for the label and one for the contiguous stroke,
	// with a reset at the end of each colored run rather than per drawn cell.
	if count := strings.Count(wire, "\x1b[38;2;12;34;56m"); count != 2 {
		t.Fatalf("got %d foreground sequences: %q", count, wire)
	}
	if !strings.Contains(wire, "label") || !strings.Contains(wire, "···") {
		t.Fatalf("text or stroke split by style escapes: %q", wire)
	}
}

func TestWireGridPreservesCurvesAndBorders(t *testing.T) {
	g, err := BuildHeightfield(Heightfield{NX: 72, NY: 56, MinX: -.7, MaxX: .7, MinY: -.7, MaxY: .7, Sampler: func(x, y float32) float32 { return .2 * x * y }})
	check(t, err)
	f := frame(t, NewScene())
	f.Matrix = nt3dmath.Identity()
	counts := []int{}
	for _, size := range [][2]int{{40, 16}, {120, 48}} {
		edges := make(map[[2]nt3dmath.Vec3]bool)
		wireGrid(f, g, size[0], size[1], func(a, b nt3dmath.Vec3) { edges[[2]nt3dmath.Vec3{a, b}] = true })
		nx, ny := g.WireGrid[0], g.WireGrid[1]
		// Every chosen row/column contains all original segments, including the
		// outer border. No diagonal chords or disconnected triangle samples.
		count := 0
		for y := 0; y < ny; y++ {
			n := 0
			for x := 0; x < nx-1; x++ {
				if edges[[2]nt3dmath.Vec3{g.Vertices[y*nx+x].Position, g.Vertices[y*nx+x+1].Position}] {
					n++
				}
			}
			if n != 0 && n != nx-1 {
				t.Fatal("disconnected row")
			}
			if (y == 0 || y == ny-1) && n != nx-1 {
				t.Fatal("missing border row")
			}
			count += n
		}
		for x := 0; x < nx; x++ {
			n := 0
			for y := 0; y < ny-1; y++ {
				if edges[[2]nt3dmath.Vec3{g.Vertices[y*nx+x].Position, g.Vertices[(y+1)*nx+x].Position}] {
					n++
				}
			}
			if n != 0 && n != ny-1 {
				t.Fatal("disconnected column")
			}
			if (x == 0 || x == nx-1) && n != ny-1 {
				t.Fatal("missing border column")
			}
			count += n
		}
		if count != len(edges) {
			t.Fatal("unexpected lattice edges")
		}
		counts = append(counts, count)
	}
	if counts[1] <= counts[0] {
		t.Fatal("larger viewport did not increase detail")
	}
	var bounds nt3dmath.AABB
	bounds.Include(nt3dmath.Vec3{X: -.3, Y: -.3, Z: -1})
	bounds.Include(nt3dmath.Vec3{X: .3, Y: .3, Z: 1})
	clipped, err := Clip(g, bounds)
	check(t, err)
	if clipped.WireGrid != ([2]int{}) {
		t.Fatal("clipped topology retained lattice metadata")
	}
	g.WireGrid = [2]int{72, 55}
	if g.Validate() == nil {
		t.Fatal("invalid lattice accepted")
	}
}

func TestWireframeClipsLongProjectedLines(t *testing.T) {
	f := frame(t, NewScene())
	f.Matrix = nt3dmath.Identity()
	ink := color.RGBA{R: 12, G: 34, B: 56, A: 255}
	// Both endpoints are far outside the viewport; the whole middle row should
	// still be connected. The old capped sampler missed almost all of it.
	f.DynamicLines = []Vertex{
		{Position: nt3dmath.Vec3{X: -1e6, Z: .5}, Color: ink},
		{Position: nt3dmath.Vec3{X: 1e6, Z: .5}, Color: ink},
	}
	wire, err := RenderWireframe(f, 80, 20)
	check(t, err)
	rows := strings.Split(ansi.Strip(wire), "\n")
	for y, row := range rows {
		want := strings.Repeat(" ", 80)
		if y == 10 {
			want = strings.Repeat("·", 80)
		}
		if row != want {
			t.Fatalf("row %d: %q", y, row)
		}
	}
}

func TestWireframeDoesNotTruncateOffscreenLineIntoGrid(t *testing.T) {
	f := frame(t, NewScene())
	f.Matrix = nt3dmath.Identity()
	// x=-1.01 projects just left of the first cell center. Truncation toward
	// zero used to leak this entirely offscreen segment into column zero.
	f.DynamicLines = []Vertex{
		{Position: nt3dmath.Vec3{X: -1.01, Y: -.5, Z: .5}},
		{Position: nt3dmath.Vec3{X: -1.01, Y: .5, Z: .5}},
	}
	wire, err := RenderWireframe(f, 80, 20)
	check(t, err)
	if strings.Contains(ansi.Strip(wire), "·") {
		t.Fatal("offscreen segment leaked into grid")
	}
}

func wireText(t *testing.T, s *Scene, cols, rows int) []string {
	t.Helper()
	f, err := s.Frame(DefaultCamera(), cols*8, rows*16)
	check(t, err)
	wire, err := RenderWireframe(f, cols, rows)
	check(t, err)
	return strings.Split(ansi.Strip(wire), "\n")
}

func TestWireframeHidesOccludedEdges(t *testing.T) {
	front, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}})
	check(t, err)
	// A small box straight behind the large one, as the camera looks.
	_, _, _, forward := DefaultCamera().basis()
	// It is large enough on screen that its edges, not its outline, are drawn.
	behind, err := BuildCuboid(Cuboid{Size: nt3dmath.Vec3{X: .6, Y: .6, Z: .6}})
	check(t, err)
	behind, err = TransformGeometry(behind, nt3dmath.Translation(forward.Scale(1.4)))
	check(t, err)
	alone, both := NewScene(), NewScene()
	for _, s := range []*Scene{alone, both} {
		_, err = s.SetGeometry("front", front)
		check(t, err)
	}
	_, err = both.SetGeometry("behind", behind)
	check(t, err)
	a, b := wireText(t, alone, 60, 24), wireText(t, both, 60, 24)
	if f, _ := both.Frame(DefaultCamera(), 480, 384); newWireDepth(f, 60, 24).dense(1) {
		t.Fatal("the hidden box is dense, so this tests nothing")
	}
	if strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Fatalf("hidden box drawn:\n%s\n---\n%s", strings.Join(a, "\n"), strings.Join(b, "\n"))
	}
	// Box instances are hidden alike, and hide what is behind them.
	boxes := NewScene()
	_, err = boxes.SetGeometry("front", Geometry{Boxes: []Box{{Min: nt3dmath.Vec3{X: -.5, Y: -.5, Z: -.5}, Size: nt3dmath.Vec3{X: 1, Y: 1, Z: 1}}}})
	check(t, err)
	before := strings.Join(wireText(t, boxes, 60, 24), "\n")
	_, err = boxes.SetGeometry("behind", behind)
	check(t, err)
	if after := strings.Join(wireText(t, boxes, 60, 24), "\n"); after != before {
		t.Fatal("box instances did not hide the mesh behind them")
	}
}

func TestWireframeOutlinesDenseMeshes(t *testing.T) {
	sphere, err := BuildSphere(Sphere{Radius: .8, Segments: 128, Rings: 64})
	check(t, err)
	s := NewScene()
	_, err = s.SetGeometry("sphere", sphere)
	check(t, err)
	rows := wireText(t, s, 60, 24)
	// Each row crossing the sphere shows its two silhouette edges, with the
	// many small triangles between them left out.
	top, bottom := -1, -1
	for i, row := range rows {
		if strings.Contains(row, "·") {
			if top < 0 {
				top = i
			}
			bottom = i
		}
	}
	crossed := 0
	for _, row := range rows[top+(bottom-top)/3 : bottom-(bottom-top)/3+1] {
		first, last := strings.Index(row, "·"), strings.LastIndex(row, "·")
		if first < 0 || last-first < 12 {
			t.Fatalf("silhouette missing: %q", row)
		}
		crossed++
		middle := []rune(row)[len([]rune(row[:first]))+4 : len([]rune(row[:last]))-4]
		if strings.Contains(string(middle), "·") {
			t.Fatalf("interior drawn: %q", row)
		}
	}
	if crossed < 2 {
		t.Fatalf("no outline:\n%s", strings.Join(rows, "\n"))
	}
}
