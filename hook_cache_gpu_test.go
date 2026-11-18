//go:build webgpu

package nt3d

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// tintFrame is a frame of one cube whose material shader paints it a flat
// tint: each tint is its own WGSL source, and so its own compiled program.
func tintFrame(t *testing.T, tint float32) Frame {
	t.Helper()
	shader, err := NewMaterialShader(
		fmt.Sprintf("fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> { return vec4<f32>(%.3f, 0.0, 0.0, 1.0); }", tint),
		func(in MaterialColorInput) [4]float32 { return [4]float32{tint, 0, 0, 1} })
	check(t, err)
	g := testCuboid(t, nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, color.RGBA{255, 255, 255, 255})
	g.Material = &Material{Unlit: true, Shader: shader}
	s := NewScene()
	_, err = s.SetGeometry("cube", g)
	check(t, err)
	f, err := s.Frame(DefaultCamera(), 48, 48)
	check(t, err)
	return f
}

// A hook program is compiled from a shader's source and kept while frames
// use it. A program whose source no frame has drawn for a while is released,
// so an application that generates shader sources does not grow the cache for
// as long as the renderer lives; drawing it again compiles it again.
func TestGPUHookProgramsAreReleasedWhenIdle(t *testing.T) {
	defer func(n uint64) { hookProgramIdleFrames = n }(hookProgramIdleFrames)
	hookProgramIdleFrames = 4

	r := &gpuRenderer{}
	defer r.Close()
	programs := func() int {
		r.shared.mu.Lock()
		defer r.shared.mu.Unlock()
		return len(r.shared.hookPrograms)
	}
	first, err := r.Render(tintFrame(t, 0.25))
	check(t, err)
	if programs() != 1 {
		t.Fatalf("after one hooked frame the cache holds %d programs, want 1", programs())
	}

	// A program drawn every frame is kept however many frames pass.
	steady := tintFrame(t, 0.25)
	for range 3 * int(hookProgramIdleFrames) {
		_, err = r.Render(steady)
		check(t, err)
	}
	if programs() != 1 {
		t.Fatalf("a program in use every frame was not kept: %d programs", programs())
	}

	// Sources come and go: only the recent ones stay.
	for i := range 20 {
		_, err = r.Render(tintFrame(t, 0.5+float32(i)/100))
		check(t, err)
	}
	if n := programs(); n > int(hookProgramIdleFrames)+1 {
		t.Fatalf("after 20 one-off shader sources the cache holds %d programs, want at most %d", n, hookProgramIdleFrames+1)
	}

	// A released program compiles again and draws the same picture.
	again, err := r.Render(tintFrame(t, 0.25))
	check(t, err)
	if at(again, 24, 24) != at(first, 24, 24) || at(first, 24, 24).R == 0 {
		t.Fatalf("the recompiled program draws %v, the first drew %v", at(again, 24, 24), at(first, 24, 24))
	}
}

// A displacement and a material shader on one mesh are compiled into one
// module; a helper name they both declare is reported by name.
func TestGPUPairedHooksRefuseASharedName(t *testing.T) {
	disp, err := NewVertexDisplacement(`
fn wobble(x: f32) -> f32 { return sin(x); }
fn nt3dDisplace(in: NT3DVertexInput) -> NT3DVertexOutput { return NT3DVertexOutput(in.position + in.normal * 0.01 * wobble(in.position.x), in.normal); }`,
		0.01, func(in VertexDisplaceInput) VertexDisplaceOutput {
			return VertexDisplaceOutput{Position: in.Position, Normal: in.Normal}
		})
	check(t, err)
	mat, err := NewMaterialShader(`
fn wobble(x: f32) -> f32 { return cos(x); }
fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> { return vec4<f32>(wobble(in.uv.x), 0.0, 0.0, 1.0); }`,
		func(in MaterialColorInput) [4]float32 { return in.Color })
	check(t, err)
	g := testCuboid(t, nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, color.RGBA{255, 255, 255, 255})
	g.Displacement = disp
	g.Material = &Material{Unlit: true, Shader: mat}
	s := NewScene()
	_, err = s.SetGeometry("cube", g)
	check(t, err)
	f, err := s.Frame(DefaultCamera(), 48, 48)
	check(t, err)
	r := &gpuRenderer{}
	defer r.Close()
	if _, err = r.Render(f); err == nil || !strings.Contains(err.Error(), `"wobble"`) {
		t.Fatalf("rendering hooks that share a name: error %v, want one naming wobble", err)
	}
}
