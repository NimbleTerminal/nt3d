//go:build webgpu

package nt3d

import (
	"errors"
	"image/color"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

// The GPU renderer offers its device's binding limit, and AdoptBindingLimit
// applies it.
func TestGPUBindingLimitAdopted(t *testing.T) {
	defer setBindingLimit(DefaultBindingLimit)
	r := NewGPURenderer()
	defer r.Close()
	got, err := AdoptBindingLimit(r)
	if err != nil {
		t.Fatal(err)
	}
	if got < DefaultBindingLimit || got > MaxBindingLimit || BindingLimit() != got {
		t.Fatalf("adopted %d, limit %d", got, BindingLimit())
	}
	again, err := r.(BindingLimiter).BindingLimit()
	if err != nil || again != got {
		t.Fatalf("BindingLimit() = %d, %v, want %d", again, err, got)
	}
}

// pointsScene is a scene of n opaque hard points, 20 bytes a record on the GPU.
func pointsScene(t *testing.T, n int) Frame {
	t.Helper()
	points := make([]Point, n)
	for i := range points {
		points[i] = Point{Position: nt3dmath.Vec3{X: float32(i) / float32(n)}, Radius: 2, Color: color.RGBA{255, 255, 255, 255}}
	}
	s := NewScene()
	_, err := s.SetGeometry("points", styled(PointStyle{Shape: HardPoint, Units: PixelUnits}, points...))
	check(t, err)
	f, err := s.Frame(DefaultCamera(), 32, 32)
	check(t, err)
	return f
}

// A geometry whose records exceed the device's binding is refused as a
// frame error, not a device failure, before any GPU validation can fail.
func TestGPURefusesOverDeviceLimit(t *testing.T) {
	g := &gpuRenderer{}
	defer g.Close()
	if _, err := g.Render(pointsScene(t, 20)); err != nil {
		t.Fatal(err)
	}
	g.deviceLimit = 300 // Fifteen 20-byte records.
	_, err := g.Render(pointsScene(t, 20))
	if err == nil || !strings.Contains(err.Error(), "binding limit") || errors.Is(err, ErrGPUUnavailable) {
		t.Fatalf("20 points over a 300-byte device limit: %v", err)
	}
	if _, err := g.Render(pointsScene(t, 15)); err != nil {
		t.Fatalf("15 points within it: %v", err)
	}
}
