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
