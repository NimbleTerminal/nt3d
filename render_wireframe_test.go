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
