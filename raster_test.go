// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// flatFrame shows geometry with clip space as the world: x and y from -1
// to 1 across the frame, at the depth z gives, evenly lit.
func flatFrame(w, h int, geometry ...Geometry) Frame {
	f := Frame{Width: w, Height: h, Matrix: nt3dmath.Identity(), Linear: true, Background: color.RGBA{0, 0, 0, 255}, Lighting: even}
	for _, g := range geometry {
		g.Bounds = GeometryBounds(g)
		f.Geometry = append(f.Geometry, g)
	}
	return f
}

func coloredPixels(img image.Image, want color.RGBA) int {
	n := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			if c == want {
				n++
			}
		}
	}
	return n
}

// discPixels counts the pixels whose centers lie within r of (x, y).
func discPixels(w, h int, x, y, r float32) int {
	n := 0
	for py := range h {
		for px := range w {
			if dx, dy := float32(px)+.5-x, float32(py)+.5-y; dx*dx+dy*dy <= r*r {
				n++
			}
		}
	}
	return n
}

// A point is a disc of its full radius, fractions and all, drawn where it
// hangs into view though its center does not.
func TestSoftwarePointRadius(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	for _, c := range []struct{ x, radius float32 }{{0, 4.9}, {0, 32}, {1 + 3./64, 8}} {
		f := flatFrame(128, 128, Geometry{Points: []Point{{Position: nt3dmath.Vec3{X: c.x, Z: .5}, Radius: c.radius, Color: red}}})
		got := coloredPixels(softwareRender(f), red)
		if want := discPixels(128, 128, (c.x+1)*64, 64, c.radius); got != want || want == 0 {
			t.Errorf("point at x=%v, radius %v: %d pixels, want %d", c.x, c.radius, got, want)
		}
	}
}

// A point whose center is off screen is picked where its disc shows.
func TestPickPointHangingIntoView(t *testing.T) {
	s := NewScene()
	camera := DefaultCamera()
	const w, h = 200, 120
	ray := camera.RayFromPixel(w+3, h/2, w, h)
	center := ray.Origin.Add(ray.Direction.Scale(float32(math.Sqrt(float64(ray.Origin.Dot(ray.Origin))))))
	_, err := s.SetGeometry("dot", Geometry{Points: []Point{{Position: center, Radius: 8, Color: color.RGBA{255, 0, 0, 255}, Datum: 7}}})
	check(t, err)
	f, err := s.Frame(camera, w, h)
	check(t, err)
	if at(softwareRender(f), w-1, h/2) != (color.RGBA{255, 0, 0, 255}) {
		t.Fatal("the disc is not drawn at the edge")
	}
	if hit := s.Pick(camera, f, w-.5, h/2+.5, 0); hit == nil || hit.Primitive != PointPrimitive || hit.Datum != 7 {
		t.Fatalf("pick at the edge: %+v", hit)
	}
}

// A gradient stroke mixes in linear light, as the GPU's rasterizer does:
// halfway from red to black is half red's light.
func TestStrokeGradientIsLinear(t *testing.T) {
	red, black := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 0, 255}
	f := flatFrame(64, 8, Geometry{Lines: []Vertex{{Position: nt3dmath.Vec3{X: -1, Z: .5}, Color: red}, {Position: nt3dmath.Vec3{X: 1, Z: .5}, Color: black}}, LineWidth: 2})
	f.Background = color.RGBA{0, 0, 255, 255}
	img := softwareRender(f)
	for x := range 64 {
		want := encode8(1 - (float32(x)+.5)/64)
		if got := at(img, x, 4); int(got.R)-int(want) > 1 || int(want)-int(got.R) > 1 {
			t.Fatalf("pixel %d: %v, want red %d", x, got, want)
		}
	}
}

// A transparent quad's shared diagonal is blended once: the rasterizer
// gives each pixel center on an edge to one triangle, as the GPU does.
func TestTransparentSharedEdgeBlendsOnce(t *testing.T) {
	// The diagonal runs through pixel centers.
	f := flatFrame(8, 8, quad(1, .5, color.RGBA{255, 255, 255, 128}, glass, false))
	img := softwareRender(f)
	want := at(img, 1, 6)
	for y := range 8 {
		for x := range 8 {
			if got := at(img, x, y); got != want {
				t.Fatalf("pixel (%d, %d) is %v, the rest %v", x, y, got, want)
			}
		}
	}
	// Every pixel of a fan of triangles about a center is drawn once too.
	fan := Geometry{Material: glass}
	fan.Vertices = append(fan.Vertices, Vertex{Position: nt3dmath.Vec3{Z: .5}, Normal: nt3dmath.Vec3{Z: 1}, Color: color.RGBA{255, 255, 255, 128}})
	for i := range 8 {
		a := float64(i) * math.Pi / 4
		fan.Vertices = append(fan.Vertices, Vertex{Position: nt3dmath.Vec3{X: float32(2 * math.Cos(a)), Y: float32(2 * math.Sin(a)), Z: .5}, Normal: nt3dmath.Vec3{Z: 1}, Color: color.RGBA{255, 255, 255, 128}})
		fan.Indices = append(fan.Indices, 0, uint32(1+i), uint32(1+(i+1)%8))
	}
	img = softwareRender(flatFrame(8, 8, fan))
	for y := range 8 {
		for x := range 8 {
			if got := at(img, x, y); got != want {
				t.Fatalf("fan pixel (%d, %d) is %v, want %v", x, y, got, want)
			}
		}
	}
}

// A line drawn on a surface is what Pick finds there, and only between its
// ends: strokes end square.
func TestPickLineOnSurface(t *testing.T) {
	floor := quad(1, 0, blue, nil, false)
	line := Geometry{Lines: []Vertex{{Position: nt3dmath.Vec3{X: -.5, Y: .1}}, {Position: nt3dmath.Vec3{X: .5, Y: .1}}}, LineWidth: 8}
	for _, order := range [][]Geometry{{floor, line}, {line, floor}} {
		s := NewScene()
		for i, g := range order {
			_, err := s.SetGeometry(fmt.Sprint(i), g)
			check(t, err)
		}
		camera := DefaultCamera()
		camera.Projection, camera.Alpha = Perspective, 89
		f, err := s.Frame(camera, 200, 200)
		check(t, err)
		for _, x := range []float32{-.4, -.1, .2, .45} {
			px, py, _, _ := f.Matrix.Project(nt3dmath.Vec3{X: x, Y: .1}, f.Width, f.Height)
			if hit := s.Pick(camera, f, px, py, 0); hit == nil || hit.Primitive != LinePrimitive {
				t.Fatalf("on the line at x=%v: %+v", x, hit)
			}
		}
		// Two pixels past the end, along the line on screen, is within half
		// its width but past its square end.
		x0, y0, _, _ := f.Matrix.Project(nt3dmath.Vec3{X: -.5, Y: .1}, f.Width, f.Height)
		x1, y1, _, _ := f.Matrix.Project(nt3dmath.Vec3{X: .5, Y: .1}, f.Width, f.Height)
		length := float32(math.Hypot(float64(x1-x0), float64(y1-y0)))
		if hit := s.Pick(camera, f, x1+2*(x1-x0)/length, y1+2*(y1-y0)/length, 0); hit == nil || hit.Primitive != TrianglePrimitive {
			t.Fatalf("past the line's end: %+v", hit)
		}
	}
}
