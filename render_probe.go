// Copyright (c) 2026 Neomantra Corp.

package nt3d

import "github.com/gogpu/wgpu"

// Probe panoramas share the environment texture cache, including their GGX
// mips. Rebuild lighting bindings only when a source image changes; positions,
// influence boxes, and strengths are per-frame uniform data.
func (g *gpuRenderer) prepareReflectionProbes(dev *wgpu.Device, probes []ReflectionProbe) error {
	old := g.probeTextures
	next := old
	changed := false
	releaseNew := func() {
		for i, t := range next {
			if t != nil && t != old[i] {
				t.release()
			}
		}
	}
	for i := range next {
		var source *Environment
		if i < len(probes) {
			source = probes[i].Environment
		}
		if old[i] != nil && old[i].key == source {
			continue
		}
		if source == nil && old[i] == nil {
			continue
		}
		changed = true
		next[i] = nil
		if source != nil {
			t, err := g.acquireEnvironmentTexture(dev, source)
			if err != nil {
				releaseNew()
				return err
			}
			next[i] = t
		}
	}
	if !changed {
		return nil
	}
	g.probeTextures = next
	targets := []*shadowTarget{&g.noShadow, &g.shadowTarget}
	var binds [2]*wgpu.BindGroup
	for i, t := range targets {
		if t.view == nil {
			continue
		}
		b, err := g.sceneLightingBind(dev, t.view, &g.reflection)
		if err != nil {
			for _, b := range binds {
				if b != nil {
					b.Release()
				}
			}
			g.probeTextures = old
			releaseNew()
			return err
		}
		binds[i] = b
	}
	for i, t := range targets {
		if binds[i] != nil {
			if t.bind != nil {
				t.bind.Release()
			}
			t.bind = binds[i]
		}
	}
	for i, t := range old {
		if t != nil && t != next[i] {
			t.release()
		}
	}
	return nil
}
