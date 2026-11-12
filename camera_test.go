package nt3d

import (
	"image/color"
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func TestCameraProjectionAndRay(t *testing.T) {
	for _, proj := range []Projection{Orthographic, Perspective} {
		c := DefaultCamera()
		c.Projection = proj
		m := c.Matrix(2)
		x, y, z, ok := m.Project(nt3dmath.Vec3{}, 800, 400)
		if !ok || abs(x-400) > .001 || abs(y-200) > .001 || z <= 0 || z >= 1 {
			t.Fatalf("center: %v %v %v %v", x, y, z, ok)
		}
		r := c.ray(50, 25, 100, 50, 8, 8)
		if r.Origin.Scale(-1).Normalize().Dot(r.Direction) < .999 {
			t.Fatal("central ray misses target")
		}
		rr := c.RayFromCell(75, 25, 100, 50, 8, 16)
		if !nt3dmath.IsFinite(float64(rr.Direction.X)) {
			t.Fatal("invalid ray")
		}
	}
}

func TestOrthoDistanceAndCellAspect(t *testing.T) {
	c := DefaultCamera()
	c.Alpha = 0
	c.Beta = 0
	a := c.RayFromCell(4, 2, 5, 5, 8, 16)
	b := c.RayFromCell(4, 2, 5, 5, 16, 16)
	if math.Abs(float64(b.Origin.Sub(c.Target).Y)) < math.Abs(float64(a.Origin.Sub(c.Target).Y))*1.9 {
		t.Fatal("cell aspect ignored")
	}
	c.Distance *= 2
	d := c.RayFromCell(4, 2, 5, 5, 8, 16)
	if abs(d.Origin.Y-2*a.Origin.Y) > .001 {
		t.Fatal("ortho scale ignored")
	}
}

func TestSoftwareTriangle(t *testing.T) {
	c := DefaultCamera()
	g := Geometry{Vertices: []Vertex{{nt3dmath.Vec3{X: -1, Y: -1, Z: 0}, nt3dmath.Vec3{Z: 1}, color.RGBA{255, 0, 0, 255}}, {nt3dmath.Vec3{X: 1, Y: -1, Z: 0}, nt3dmath.Vec3{Z: 1}, color.RGBA{255, 0, 0, 255}}, {nt3dmath.Vec3{X: 0, Y: 1, Z: 0}, nt3dmath.Vec3{Z: 1}, color.RGBA{255, 0, 0, 255}}}, Indices: []uint32{0, 1, 2}}
	img := softwareRender(Frame{Width: 100, Height: 100, Matrix: c.Matrix(1), Geometry: []Geometry{g}, Lighting: oldLight(nt3dmath.Vec3{Z: 1}, .3, false)})
	n := 0
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			if r > 0 {
				n++
			}
		}
	}
	if n < 30 {
		t.Fatalf("triangle invisible: %d", n)
	}
}

func TestCameraFit(t *testing.T) {
	for _, proj := range []Projection{Orthographic, Perspective} {
		for _, scale := range []float32{.01, 1, 500, 1e4} {
			for _, aspect := range []float64{.5, 2} {
				var b nt3dmath.AABB
				offset := nt3dmath.Vec3{X: 3 * scale, Y: -2 * scale, Z: scale}
				b.Include(offset.Add(nt3dmath.Vec3{X: -scale, Y: -.5 * scale, Z: -.2 * scale}))
				b.Include(offset.Add(nt3dmath.Vec3{X: scale, Y: .5 * scale, Z: .2 * scale}))
				c := DefaultCamera()
				c.Projection = proj
				c = c.Fit(b, aspect)
				if c.Target != b.Min.Add(b.Max).Scale(.5) {
					t.Fatal("not centered")
				}
				m := c.Matrix(float32(aspect))
				width, height := int(400*aspect), 400
				reach := float32(0)
				for i := range 8 {
					p := b.Min
					if i&1 != 0 {
						p.X = b.Max.X
					}
					if i&2 != 0 {
						p.Y = b.Max.Y
					}
					if i&4 != 0 {
						p.Z = b.Max.Z
					}
					x, y, _, ok := m.Project(p, width, height)
					if !ok {
						t.Fatalf("%v scale %v aspect %v: corner %v outside the view or depth range", proj, scale, aspect, p)
					}
					reach = max(reach, abs(x/float32(width)*2-1), abs(y/float32(height)*2-1))
				}
				// The bounding sphere fits the narrower side; the box reaches well out.
				if reach < .45 {
					t.Fatalf("%v scale %v aspect %v: loose fit, corners reach %v", proj, scale, aspect, reach)
				}
			}
		}
	}
	c := DefaultCamera()
	if c.Fit(nt3dmath.AABB{}, 1) != c.Valid() || c.Fit(nt3dmath.AABB{}, math.NaN()) != c.Valid() {
		t.Fatal("invalid fit changed the camera")
	}
}

func TestCameraLookAt(t *testing.T) {
	eye, target := nt3dmath.Vec3{X: 10, Y: -4, Z: 6}, nt3dmath.Vec3{X: 1, Y: 2, Z: 0}
	c := DefaultCamera().LookAt(eye, target)
	if got := c.Eye(); got.Sub(eye).Dot(got.Sub(eye)) > 1e-8 || c.Target != target {
		t.Fatalf("eye %v, target %v", got, c.Target)
	}
	if c.LookAt(target, target) != c {
		t.Fatal("degenerate LookAt changed the camera")
	}
}

func TestPerspectiveFarOnlyClipping(t *testing.T) {
	for _, far := range []float64{950, .01} {
		c := DefaultCamera()
		c.Projection, c.Far, c.Pose = Perspective, far, nt3dmath.Identity()
		near := .05
		if near >= far {
			near = far / 4000
		}
		m := c.Matrix(1)
		for _, d := range []float64{near, 2 * near, far} {
			_, _, z, w := m.Transform(nt3dmath.Vec3{Z: -float32(d)})
			want := far / (far - near) * (1 - near/d)
			if math.Abs(float64(z/w)-want) > 1e-5 {
				t.Fatalf("far %g depth at %g: got %g want %g", far, d, z/w, want)
			}
		}
	}
}

func TestCameraLookDirection(t *testing.T) {
	c := DefaultCamera()
	c.Distance, c.Projection = 600, Perspective
	eye := nt3dmath.Vec3{X: 10, Y: -4, Z: 6}
	for _, f := range []nt3dmath.Vec3{{X: 1, Y: 2, Z: .1}, {Z: 1}} {
		got := c.LookDirection(eye, f)
		if got.Distance != c.Distance || got.Projection != c.Projection || got.Eye().Sub(eye).Dot(got.Eye().Sub(eye)) > 1e-8 {
			t.Fatalf("pose or distance changed: %+v, eye %v", got, got.Eye())
		}
		if got.RayFromPixel(50, 50, 100, 100).Direction.Dot(f.Normalize()) < .999 {
			t.Fatal("wrong direction")
		}
		if other := c.LookDirection(eye, f.Scale(10)); other.Target.Sub(got.Target).Dot(other.Target.Sub(got.Target)) > 1e-8 {
			t.Fatal("direction magnitude affects target")
		}
	}
	for _, f := range []nt3dmath.Vec3{{}, {X: float32(math.NaN())}} {
		if c.LookDirection(eye, f) != c {
			t.Fatal("invalid direction changed camera")
		}
	}
}

func TestCameraForwardAndBasis(t *testing.T) {
	orbit := DefaultCamera().LookAt(nt3dmath.Vec3{X: 6, Y: -3, Z: 4}, nt3dmath.Vec3{X: 1, Y: 2, Z: 1})
	posed := DefaultCamera()
	posed.Pose = nt3dmath.Translation(nt3dmath.Vec3{X: -2, Y: 5, Z: 3}).Mul(nt3dmath.RotationZ(.7)).Mul(nt3dmath.Scaling(nt3dmath.Vec3{X: 2, Y: 3, Z: 4}))
	invalid := Camera{Alpha: math.NaN(), Beta: math.Inf(1), Distance: -1}

	for name, c := range map[string]Camera{"orbit": orbit, "posed": posed, "invalid": invalid} {
		r, u, f := c.Basis()
		if f != c.Forward() {
			t.Fatalf("%s: Forward %v != Basis forward %v", name, c.Forward(), f)
		}
		for axisName, v := range map[string]nt3dmath.Vec3{"right": r, "up": u, "forward": f} {
			if abs(v.Dot(v)-1) > 1e-5 {
				t.Fatalf("%s %s not unit length: %v (len^2=%g)", name, axisName, v, v.Dot(v))
			}
		}
		if abs(r.Dot(u)) > 1e-5 || abs(r.Dot(f)) > 1e-5 || abs(u.Dot(f)) > 1e-5 {
			t.Fatalf("%s basis not orthogonal: r=%v u=%v f=%v", name, r, u, f)
		}
		// Screen-right × screen-up points out of the screen (-forward).
		if r.Cross(u).Add(f).Dot(r.Cross(u).Add(f)) > 1e-5 {
			t.Fatalf("%s unexpected handedness: r×u=%v, -f=%v", name, r.Cross(u), f.Scale(-1))
		}
		if central := c.RayFromPixel(50, 50, 100, 100).Direction; central.Dot(f) < .99999 {
			t.Fatalf("%s central ray %v disagrees with Forward %v", name, central, f)
		}
	}
	wantOrbitForward := orbit.Target.Sub(orbit.Eye()).Normalize()
	if orbit.Forward().Sub(wantOrbitForward).Dot(orbit.Forward().Sub(wantOrbitForward)) > 1e-10 {
		t.Fatalf("orbit Forward %v, want %v", orbit.Forward(), wantOrbitForward)
	}
}
