//go:build webgpu

// Copyright (c) 2026 Neomantra Corp.
package nt3dclod_test

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/gogpu/wgpu"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/clod"
	"nimbleterminal.dev/nt3d/math"
	gpuimage "nimbleterminal.dev/ntgpuimage"
)

// cloud returns n seeded positions in [-6,6]³ with three dense knots.
func cloud(n int, seed uint64) []nt3dmath.Vec3 {
	r := rand.New(rand.NewPCG(seed, 11))
	out := make([]nt3dmath.Vec3, 0, n)
	for len(out) < n {
		out = append(out, nt3dmath.Vec3{X: float32(r.Float64()*12 - 6), Y: float32(r.Float64()*12 - 6), Z: float32(r.Float64()*12 - 6)})
	}
	for k := range 3 {
		c := nt3dmath.Vec3{X: float32(k*3 - 3), Y: float32(k) - 1, Z: .5}
		for i := k * 2000; i < (k+1)*2000; i++ {
			out[i] = c.Add(nt3dmath.Vec3{X: float32(r.NormFloat64() * .2), Y: float32(r.NormFloat64() * .2), Z: float32(r.NormFloat64() * .2)})
		}
	}
	return out
}

// boundary is how near cluster c sits to a decision boundary of the
// selection, relative: a box corner to one of the six planes, or its
// distance to a level's range. Below about 1e-4, rounding may decide.
func boundary(s *nt3dclod.Set, c int, v nt3dclod.View, levels []nt3dclod.Level) float32 {
	cl := s.Clusters[c]
	padX, padY := 2*v.Margin/float32(v.Width), 2*v.Margin/float32(v.Height)
	m := float32(math.Inf(1))
	for i := range 8 {
		p := cl.Bounds.Min
		if i&1 != 0 {
			p.X = cl.Bounds.Max.X
		}
		if i&2 != 0 {
			p.Y = cl.Bounds.Max.Y
		}
		if i&4 != 0 {
			p.Z = cl.Bounds.Max.Z
		}
		x, y, z, w := v.Matrix.Transform(p)
		scale := max(1e-6, float32(math.Abs(float64(w))))
		for _, e := range []float32{x + w*(1+padX), w*(1+padX) - x, y + w*(1+padY), w*(1+padY) - y, z, w - z} {
			m = min(m, float32(math.Abs(float64(e)))/scale)
		}
	}
	d := float32(2 / math.Sqrt(float64(v.Matrix[1]*v.Matrix[1]+v.Matrix[5]*v.Matrix[5]+v.Matrix[9]*v.Matrix[9])))
	if v.Perspective {
		e := cl.Center.Sub(v.Eye)
		d = max(0, float32(math.Sqrt(float64(e.Dot(e))))-cl.Radius)
	}
	for _, l := range levels[:len(levels)-1] {
		m = min(m, float32(math.Abs(float64(d-l.Range)))/max(1, l.Range))
	}
	return m
}

// agree compares a GPU selection with the CPU's, cluster by cluster,
// tolerating differences only at decision boundaries.
func agree(s *nt3dclod.Set, cpu, gpu *nt3dclod.Selection, v nt3dclod.View, levels []nt3dclod.Level) error {
	type pick struct {
		level uint32
		items []uint32
	}
	index := func(sel *nt3dclod.Selection) (map[uint32]pick, error) {
		out := make(map[uint32]pick, len(sel.Picks))
		for i, p := range sel.Picks {
			if i > 0 && p.Cluster <= sel.Picks[i-1].Cluster {
				return nil, fmt.Errorf("picks out of cluster order at %d", i)
			}
			out[p.Cluster] = pick{p.Level, sel.Items[p.First : p.First+p.Count]}
		}
		return out, nil
	}
	a, err := index(cpu)
	if err != nil {
		return fmt.Errorf("cpu: %v", err)
	}
	b, err := index(gpu)
	if err != nil {
		return fmt.Errorf("gpu: %v", err)
	}
	tolerated := 0
	for c := range s.Clusters {
		pa, oka := a[uint32(c)]
		pb, okb := b[uint32(c)]
		if oka == okb && (!oka || pa.level == pb.level && slices.Equal(pa.items, pb.items)) {
			continue
		}
		if boundary(s, c, v, levels) < 1e-4 {
			tolerated++
			continue
		}
		return fmt.Errorf("cluster %d: cpu %v %+v, gpu %v %+v (boundary margin %g)", c, oka, pa, okb, pb, boundary(s, c, v, levels))
	}
	if tolerated > len(s.Clusters)/50 {
		return fmt.Errorf("%d of %d clusters differed at boundaries", tolerated, len(s.Clusters))
	}
	return nil
}

func withDevice(t *testing.T, f func(dev *wgpu.Device) error) {
	t.Helper()
	r, err := gpuimage.New(context.Background(), gpuimage.Options{Width: 1, Height: 1, Label: "clod test"})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Do(f); err != nil {
		t.Fatal(err)
	}
}

func TestGPUSelectionMatchesCPU(t *testing.T) {
	positions := cloud(60_000, 3)
	s, err := nt3dclod.Build(positions, nt3dclod.Options{ClusterSize: 128, Pad: .05})
	if err != nil {
		t.Fatal(err)
	}
	views := []nt3dclod.View{}
	for _, d := range []float64{3, 9, 30, .5} {
		c := nt3d.Camera{Alpha: 20, Beta: 35, Distance: d, Projection: nt3d.Perspective, ResumeAfter: 1}
		views = append(views, view(c, 160, 100, 2))
	}
	for _, d := range []float64{6, 20} {
		views = append(views, view(nt3d.Camera{Alpha: 20, Beta: 35, Distance: d, ResumeAfter: 1}, 160, 100, 0))
	}
	withDevice(t, func(dev *wgpu.Device) error {
		g, err := nt3dclod.NewGPU(dev)
		if err != nil {
			return err
		}
		defer g.Release()
		if err := g.Upload(dev, s); err != nil {
			return err
		}
		var cpu nt3dclod.Selection
		for i, v := range views {
			if err := s.Select(v, levels, &cpu); err != nil {
				return err
			}
			gpu, err := g.Read(context.Background(), dev, v, levels)
			if err != nil {
				return fmt.Errorf("view %d: %v", i, err)
			}
			if len(cpu.Picks) == 0 {
				return fmt.Errorf("view %d selects nothing on the CPU", i)
			}
			if err := agree(s, &cpu, gpu, v, levels); err != nil {
				return fmt.Errorf("view %d (%d cpu picks, %d gpu picks): %v", i, len(cpu.Picks), len(gpu.Picks), err)
			}
		}
		return nil
	})
}

func TestGPUEmptyAndResize(t *testing.T) {
	s, err := nt3dclod.Build(nil, nt3dclod.Options{})
	if err != nil {
		t.Fatal(err)
	}
	v := view(nt3d.Camera{Alpha: 20, Beta: 35, Distance: 5, Projection: nt3d.Perspective, ResumeAfter: 1}, 100, 100, 1)
	withDevice(t, func(dev *wgpu.Device) error {
		g, err := nt3dclod.NewGPU(dev)
		if err != nil {
			return err
		}
		defer g.Release()
		if err := g.Upload(dev, s); err != nil {
			return err
		}
		sel, err := g.Read(context.Background(), dev, v, levels)
		if err != nil {
			return err
		}
		if len(sel.Picks) != 0 || len(sel.Items) != 0 {
			return fmt.Errorf("empty set selected %+v", sel)
		}
		items := g.Items()
		if err := g.Upload(dev, s); err != nil {
			return err
		}
		if g.Items() != items {
			return fmt.Errorf("an unchanged set was uploaded again")
		}
		if err := s.Rebuild(cloud(20_000, 5)); err != nil {
			return err
		}
		if err := g.Upload(dev, s); err != nil {
			return err
		}
		var cpu nt3dclod.Selection
		if err := s.Select(v, levels, &cpu); err != nil {
			return err
		}
		gpu, err := g.Read(context.Background(), dev, v, levels)
		if err != nil {
			return err
		}
		return agree(s, &cpu, gpu, v, levels)
	})
}
