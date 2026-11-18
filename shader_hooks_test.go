// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

func testWaveDisplacement(t *testing.T, time, amp, freq float32) *VertexDisplacement {
	t.Helper()
	const wgsl = `
fn nt3dDisplace(in: NT3DVertexInput) -> NT3DVertexOutput {
	let amp = nt3dDisplaceParam(0u).x;
	let freq = nt3dDisplaceParam(0u).y;
	let phase = in.world.x * freq + in.time * 2.0;
	let dz = amp * sin(phase);
	let slope = amp * freq * cos(phase);
	let n = normalize(vec3<f32>(-slope, 0.0, 1.0));
	return NT3DVertexOutput(in.position + vec3<f32>(0.0, 0.0, dz), n);
}
`
	d, err := NewVertexDisplacementWithParameters(wgsl, amp, time, [][4]float32{{amp, freq, 0, 0}}, func(in VertexDisplaceInput) VertexDisplaceOutput {
		a, f := in.Params[0][0], in.Params[0][1]
		phase := float64(in.World.X*f + in.Time*2)
		dz := a * float32(math.Sin(phase))
		slope := a * f * float32(math.Cos(phase))
		n := (nt3dmath.Vec3{X: -slope, Y: 0, Z: 1}).Normalize()
		return VertexDisplaceOutput{
			Position: in.Position.Add(nt3dmath.Vec3{Z: dz}),
			Normal:   n,
		}
	})
	check(t, err)
	return d
}

func testBandMaterialShader(t *testing.T, time, speed float32) *MaterialShader {
	t.Helper()
	const wgsl = `
fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> {
	let speed = nt3dMaterialParam(0u).x;
	let band = 0.5 + 0.5 * sin(in.world.x * 6.0 + in.time * speed);
	let rim = pow(clamp(1.0 - max(0.0, dot(in.normal, in.view)), 0.0, 1.0), 2.0);
	let rgb = mix(vec3<f32>(0.08, 0.28, 0.65), vec3<f32>(0.85, 0.95, 1.0), clamp(band * 0.7 + rim * 0.3, 0.0, 1.0)) * in.color.rgb;
	return vec4<f32>(rgb, in.color.a);
}
`
	s, err := NewMaterialShaderWithParameters(wgsl, time, [][4]float32{{speed, 0, 0, 0}}, func(in MaterialColorInput) [4]float32 {
		spd := in.Params[0][0]
		band := float32(0.5 + 0.5*math.Sin(float64(in.World.X*6+in.Time*spd)))
		facing := max(float32(0), in.Normal.Dot(in.View))
		oneMinus := min(float32(1), max(float32(0), 1-facing))
		rim := oneMinus * oneMinus
		w := min(float32(1), max(float32(0), band*0.7+rim*0.3))
		lo := [3]float32{0.08, 0.28, 0.65}
		hi := [3]float32{0.85, 0.95, 1.0}
		return [4]float32{
			(lo[0] + (hi[0]-lo[0])*w) * in.Color[0],
			(lo[1] + (hi[1]-lo[1])*w) * in.Color[1],
			(lo[2] + (hi[2]-lo[2])*w) * in.Color[2],
			in.Color[3],
		}
	})
	check(t, err)
	return s
}

func testCuboid(t *testing.T, size nt3dmath.Vec3, c color.RGBA) Geometry {
	t.Helper()
	g, err := BuildCuboid(Cuboid{Size: size, Color: c})
	check(t, err)
	return g
}

func hookImgMeanAbsDiff(a, b image.Image) float64 {
	r := a.Bounds()
	var sum float64
	var n int
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			sum += math.Abs(float64(ar>>8)-float64(br>>8)) +
				math.Abs(float64(ag>>8)-float64(bg>>8)) +
				math.Abs(float64(ab>>8)-float64(bb>>8)) +
				math.Abs(float64(aa>>8)-float64(ba>>8))
			n += 4
		}
	}
	return sum / float64(max(1, n))
}

func TestShaderHookValidation(t *testing.T) {
	noopDisp := func(in VertexDisplaceInput) VertexDisplaceOutput {
		return VertexDisplaceOutput{Position: in.Position, Normal: in.Normal}
	}
	noopMat := func(in MaterialColorInput) [4]float32 { return in.Color }

	if _, err := NewVertexDisplacement("", 0.5, noopDisp); err == nil {
		t.Fatal("expected error for empty displacement WGSL")
	}
	if _, err := NewVertexDisplacement("fn nt3dDisplace(in: NT3DVertexInput) -> NT3DVertexOutput { return NT3DVertexOutput(in.position, in.normal); }", 0.5, nil); err == nil {
		t.Fatal("expected error for nil displacement fallback")
	}
	if _, err := NewVertexDisplacement("fn x() {}", -0.1, noopDisp); err == nil {
		t.Fatal("expected error for negative maxDisplacement")
	}
	if _, err := NewVertexDisplacementWithParameters("fn x() {}", float32(math.NaN()), 0, nil, noopDisp); err == nil {
		t.Fatal("expected error for NaN maxDisplacement")
	}
	if _, err := NewVertexDisplacementWithParameters("fn x() {}", 0.5, float32(math.Inf(1)), nil, noopDisp); err == nil {
		t.Fatal("expected error for Inf time")
	}
	tooMany := make([][4]float32, MaxShaderParameters+1)
	if _, err := NewVertexDisplacementWithParameters("fn x() {}", 0.5, 0, tooMany, noopDisp); err == nil {
		t.Fatal("expected error for > MaxShaderParameters")
	}
	if _, err := NewVertexDisplacementWithParameters("fn x() {}", 0.5, 0, [][4]float32{{float32(math.NaN())}}, noopDisp); err == nil {
		t.Fatal("expected error for NaN parameter")
	}

	d, err := NewVertexDisplacement("fn x() {}", 0.5, noopDisp)
	check(t, err)
	if same, err := d.WithParameters(0, nil); err != nil || same != d {
		t.Fatalf("WithParameters identical state should return receiver: got %p vs %p, err=%v", same, d, err)
	}
	if same, err := d.WithMaxDisplacement(0.5); err != nil || same != d {
		t.Fatalf("WithMaxDisplacement identical value should return receiver: got %p vs %p, err=%v", same, d, err)
	}
	if _, err := d.WithMaxDisplacement(-1); err == nil {
		t.Fatal("expected error for negative WithMaxDisplacement")
	}
	var zeroDisp VertexDisplacement
	if _, err := zeroDisp.WithParameters(1, nil); err == nil {
		t.Fatal("expected error on zero VertexDisplacement")
	}

	if _, err := NewMaterialShader("", noopMat); err == nil {
		t.Fatal("expected error for empty material WGSL")
	}
	if _, err := NewMaterialShader("fn x() {}", nil); err == nil {
		t.Fatal("expected error for nil material fallback")
	}
	if _, err := NewMaterialShaderWithParameters("fn x() {}", float32(math.NaN()), nil, noopMat); err == nil {
		t.Fatal("expected error for NaN material time")
	}
	if _, err := NewMaterialShaderWithParameters("fn x() {}", 0, tooMany, noopMat); err == nil {
		t.Fatal("expected error for > MaxShaderParameters on MaterialShader")
	}

	ms, err := NewMaterialShader("fn x() {}", noopMat)
	check(t, err)
	if same, err := ms.WithParameters(0, nil); err != nil || same != ms {
		t.Fatalf("MaterialShader.WithParameters identical state should return receiver: got %p vs %p, err=%v", same, ms, err)
	}
	var zeroMat MaterialShader
	if _, err := zeroMat.WithParameters(1, nil); err == nil {
		t.Fatal("expected error on zero MaterialShader")
	}

	// Zero-value hooks on Geometry / Material must be rejected by Scene / Frame validation.
	s := NewScene()
	badGeom := testCuboid(t, nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, color.RGBA{255, 255, 255, 255})
	badGeom.Displacement = &zeroDisp
	if _, err := s.SetGeometry("bad-disp", badGeom); err == nil {
		t.Fatal("expected SetGeometry error for invalid VertexDisplacement")
	}
	badMatGeom := testCuboid(t, nt3dmath.Vec3{X: 1, Y: 1, Z: 1}, color.RGBA{255, 255, 255, 255})
	badMatGeom.Material = &Material{Shader: &zeroMat}
	if _, err := s.SetGeometry("bad-mat", badMatGeom); err == nil {
		t.Fatal("expected SetGeometry error for invalid MaterialShader")
	}
}

func TestSetDisplacementBoundsPaddingLODAndDropShadowStability(t *testing.T) {
	s := NewScene()
	mesh := testCuboid(t, nt3dmath.Vec3{X: 2, Y: 2, Z: 1}, color.RGBA{200, 200, 200, 255})
	mesh.DropShadow = DropShadow{Strength: 0.5}
	id, err := s.SetGeometry("box", mesh)
	check(t, err)

	restBounds := s.Bounds()
	if restBounds.Min != (nt3dmath.Vec3{X: -1, Y: -1, Z: -0.5}) || restBounds.Max != (nt3dmath.Vec3{X: 1, Y: 1, Z: 0.5}) {
		t.Fatalf("unexpected initial bounds: %+v", restBounds)
	}

	d1 := testWaveDisplacement(t, 0, 0.75, 2.0)
	check(t, s.ApplyDisplacement(id, d1))
	padded := s.Bounds()
	wantMin := nt3dmath.Vec3{X: -1.75, Y: -1.75, Z: -1.25}
	wantMax := nt3dmath.Vec3{X: 1.75, Y: 1.75, Z: 1.25}
	if padded.Min != wantMin || padded.Max != wantMax {
		t.Fatalf("padded bounds = %+v, want [%v, %v]", padded, wantMin, wantMax)
	}

	// Repeated parameter updates and MaxDisplacement changes must never accumulate bounds drift.
	for step := 1; step <= 10; step++ {
		next, err := d1.WithParameters(float32(step)*0.1, [][4]float32{{0.75, 2.0, 0, 0}})
		check(t, err)
		check(t, s.ApplyDisplacement(id, next))
	}
	if got := s.Bounds(); got != padded {
		t.Fatalf("bounds drifted across WithParameters updates: got %+v, want %+v", got, padded)
	}

	dSmall, err := d1.WithMaxDisplacement(0.25)
	check(t, err)
	check(t, s.ApplyDisplacement(id, dSmall))
	if got := s.Bounds(); got.Min != (nt3dmath.Vec3{X: -1.25, Y: -1.25, Z: -0.75}) || got.Max != (nt3dmath.Vec3{X: 1.25, Y: 1.25, Z: 0.75}) {
		t.Fatalf("unexpected resized bounds: %+v", got)
	}

	// DropShadow stays anchored to rest bounds rather than sinking by maxDisplacement.
	f, err := s.Frame(DefaultCamera(), 64, 64)
	check(t, err)
	center, radius, _, ok := dropDisc(&f.Draws[0].Geometry, &f.Draws[0].state, nt3dmath.Identity())
	if !ok || center.Z != -0.5 || radius != 1 {
		t.Fatalf("dropDisc drifted under displacement padding: center=%v radius=%v ok=%v", center, radius, ok)
	}

	// Clearing displacement restores exact rest bounds.
	check(t, s.ApplyDisplacement(id, nil))
	if got := s.Bounds(); got != restBounds {
		t.Fatalf("restored bounds = %+v, want %+v", got, restBounds)
	}

	// LOD tiers also receive ApplyDisplacement and ApplyMaterialShader cleanly across distance switches.
	lodScene := NewScene()
	lodID, err := lodScene.Add(Root, "lod-mesh")
	check(t, err)
	nearMesh, err := BuildSphere(Sphere{Radius: 0.8, Segments: 24, Rings: 12, Color: color.RGBA{220, 220, 220, 255}})
	check(t, err)
	farMesh := testCuboid(t, nt3dmath.Vec3{X: 1.6, Y: 1.6, Z: 1.6}, color.RGBA{220, 220, 220, 255})
	check(t, lodScene.SetLOD(lodID, []LODLevel{
		{MaxDistance: 4.0, Geometry: nearMesh},
		{MaxDistance: 0, Geometry: farMesh},
	}))

	ms := testBandMaterialShader(t, 0.5, 2.0)
	check(t, lodScene.ApplyDisplacement(lodID, d1))
	check(t, lodScene.ApplyMaterialShader(lodID, ms))

	nearCam := DefaultCamera()
	nearCam.Distance = 2.5
	fNear, err := lodScene.Frame(nearCam, 64, 64)
	check(t, err)
	if fNear.Draws[0].Geometry.Displacement != d1 || fNear.Draws[0].Geometry.Material == nil || fNear.Draws[0].Geometry.Material.Shader != ms {
		t.Fatal("near LOD tier missing hooks")
	}

	farCam := DefaultCamera()
	farCam.Distance = 9.0
	fFar, err := lodScene.Frame(farCam, 64, 64)
	check(t, err)
	if fFar.Draws[0].Geometry.Displacement != d1 || fFar.Draws[0].Geometry.Material == nil || fFar.Draws[0].Geometry.Material.Shader != ms {
		t.Fatal("far LOD tier missing hooks")
	}
}

func TestSharedGeometryHooksPreserveInstancing(t *testing.T) {
	s := NewScene()
	shared, err := NewSharedGeometry(testCuboid(t, nt3dmath.Vec3{X: 0.6, Y: 0.6, Z: 1.2}, color.RGBA{90, 190, 100, 255}))
	check(t, err)

	group, err := s.Add(Root, "trees")
	check(t, err)
	for i := range 6 {
		id, err := s.Add(group, fmt.Sprintf("tree-%d", i))
		check(t, err)
		check(t, s.SetNodeShared(id, shared))
		check(t, s.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{X: float32(i-2) * 0.9})))
	}

	cam := DefaultCamera()
	cam.Distance = 12
	disp := testWaveDisplacement(t, 0.4, 0.3, 1.5)
	matShader := testBandMaterialShader(t, 0.4, 2.0)

	check(t, s.ApplyDisplacement(group, disp))
	check(t, s.ApplyMaterialShader(group, matShader))

	f, err := s.Frame(cam, 120, 90)
	check(t, err)
	stats := f.Stats()
	if stats.DrawnInstances != 6 || stats.Batches != 1 {
		t.Fatalf("expected 6 shared instances in 1 batch, got DrawnInstances=%d Batches=%d", stats.DrawnInstances, stats.Batches)
	}
}

func TestSoftwareWireframeShadowAndPickHooks(t *testing.T) {
	// Lift a flat quad from Z=0 to Z=+1.2 via VertexDisplacement and verify:
	// 1. Software & Wireframe renderers draw at the displaced Z=+1.2 height.
	// 2. Scene.Pick hits the displaced Z=+1.2 surface.
	// 3. Displaced mesh shifts its directional shadow on a floor at Z=-1.
	const liftWGSL = `
fn nt3dDisplace(in: NT3DVertexInput) -> NT3DVertexOutput {
	let dz = nt3dDisplaceParam(0u).x;
	return NT3DVertexOutput(in.position + vec3<f32>(0.0, 0.0, dz), in.normal);
}
`
	lift, err := NewVertexDisplacementWithParameters(liftWGSL, 1.5, 0, [][4]float32{{1.2, 0, 0, 0}}, func(in VertexDisplaceInput) VertexDisplaceOutput {
		return VertexDisplaceOutput{
			Position: in.Position.Add(nt3dmath.Vec3{Z: in.Params[0][0]}),
			Normal:   in.Normal,
		}
	})
	check(t, err)

	s := NewScene()
	floor := quad(4.0, -1.0, color.RGBA{210, 210, 210, 255}, nil, false)
	_, err = s.SetGeometry("floor", floor)
	check(t, err)

	caster := quad(0.8, 0.0, color.RGBA{230, 80, 60, 255}, nil, false)
	casterID, err := s.SetGeometry("caster", caster)
	check(t, err)

	cam := DefaultCamera()
	cam.Projection = Perspective
	cam.Elevation = 42
	cam.Azimuth = 25
	cam.Distance = 6.5

	lighting := Lighting{
		Ambient: Ambient{
			Sky:    color.RGBA{70, 70, 80, 255},
			Ground: color.RGBA{40, 40, 45, 255},
		},
		Lights: []Light{{
			Kind:      DirectionalLight,
			Direction: nt3dmath.Vec3{X: 0.6, Y: 0.2, Z: 1.0}, // Toward the light (+Z above)
			Color:     color.RGBA{255, 255, 255, 255},
			Intensity: 1,
			Shadows:   true,
		}},
	}

	fRest, err := s.Frame(cam, 96, 96)
	check(t, err)
	fRest.Lighting = lighting
	imgRest := softwareRender(fRest)
	wireRest := wireRender(fRest, 48, 24)

	check(t, s.ApplyDisplacement(casterID, lift))
	fLifted, err := s.Frame(cam, 96, 96)
	check(t, err)
	fLifted.Lighting = lighting
	imgLifted := softwareRender(fLifted)
	wireLifted := wireRender(fLifted, 48, 24)

	if hookImgMeanAbsDiff(imgRest, imgLifted) < 2.0 {
		t.Fatal("expected software render to reflect displaced caster and shifted shadow")
	}
	if wireRest == wireLifted {
		t.Fatal("expected wireframe render to reflect displaced vertices")
	}

	// Project world point (0, 0, 1.2) on the lifted quad and pick it.
	px, py, _, ok := fLifted.Matrix.Project(nt3dmath.Vec3{Z: 1.2}, 96, 96)
	if !ok {
		t.Fatal("displaced quad center not in view")
	}
	hit := s.Pick(cam, fLifted, px, py, 2)
	if hit == nil || hit.Node != casterID || math.Abs(float64(hit.Position.Z-1.2)) > 0.05 {
		t.Fatalf("Pick on lifted quad = %+v, want caster at Z≈1.2", hit)
	}

	// MaterialShader with HDR output (> 1.0) drives Bloom in Software.
	hdrShader, err := NewMaterialShader(`
fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> {
	return vec4<f32>(vec3<f32>(4.0, 2.0, 0.5), 1.0);
}
`, func(MaterialColorInput) [4]float32 {
		return [4]float32{4.0, 2.0, 0.5, 1.0}
	})
	check(t, err)
	sHDR := NewScene()
	orb, err := BuildSphere(Sphere{
		Radius:   0.4,
		Segments: 16,
		Rings:    12,
		Color:    color.RGBA{255, 255, 255, 255},
		Material: &Material{Unlit: true, Shader: hdrShader},
	})
	check(t, err)
	_, err = sHDR.SetGeometry("orb", orb)
	check(t, err)
	fHDR, err := sHDR.Frame(DefaultCamera(), 64, 64)
	check(t, err)
	fHDR.Linear = true
	fHDR.Background = color.RGBA{0, 0, 0, 255}
	noBloom := softwareRender(fHDR)
	fHDR.PostProcess = PostProcess{Bloom: Bloom{Strength: 0.8, Threshold: 1.0, Radius: 1.0}, Exposure: 1, ToneMap: ACESToneMap}
	withBloom := softwareRender(fHDR)
	if hookImgMeanAbsDiff(noBloom, withBloom) < 1.0 {
		t.Fatal("expected HDR MaterialShader values > 1.0 to drive Bloom halo")
	}
}

func hookFallbackColor(in MaterialColorInput) [4]float32 { return in.Color }

// A hook's WGSL is compiled into the scene shader's own module, so its helpers
// share that module's names. A helper that takes a name the scene shader uses
// is refused when the hook is made, by name, rather than failing to compile
// on the first GPU frame with the shader translator's error.
func TestShaderHookRefusesNamesTheSceneShaderUses(t *testing.T) {
	for _, taken := range []string{"decode", "shadeSurface", "Light", "u", "maxLights", "nt3d_material"} {
		decl := "fn " + taken + "() -> f32 { return 1.0; }"
		switch taken {
		case "Light":
			decl = "struct Light { a: f32 };"
		case "u":
			decl = "var<private> u: f32;"
		case "maxLights":
			decl = "const maxLights: u32 = 2u;"
		}
		src := decl + "\nfn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> { return in.color; }"
		_, err := NewMaterialShader(src, hookFallbackColor)
		if err == nil || !strings.Contains(err.Error(), `"`+taken+`"`) {
			t.Errorf("a material hook declaring %s: error %v, want one naming it", taken, err)
		}
		dsrc := decl + "\nfn nt3dDisplace(in: NT3DVertexInput) -> NT3DVertexOutput { return NT3DVertexOutput(in.position, in.normal); }"
		_, err = NewVertexDisplacement(dsrc, 0, func(in VertexDisplaceInput) VertexDisplaceOutput {
			return VertexDisplaceOutput{Position: in.Position, Normal: in.Normal}
		})
		if err == nil || !strings.Contains(err.Error(), `"`+taken+`"`) {
			t.Errorf("a displacement hook declaring %s: error %v, want one naming it", taken, err)
		}
	}
}

// Names of its own, and names used only inside its functions, are a hook's to choose.
func TestShaderHookKeepsItsOwnNames(t *testing.T) {
	src := `
// fn decode() in a comment declares nothing.
struct Stripe { width: f32, u: f32 };
const stripeWidth: f32 = 0.25;
fn stripe(x: f32) -> f32 {
 let u = fract(x / stripeWidth);   // a local may shadow a module name
 var decode = u;
 return step(0.5, decode);
}
fn nt3dMaterial(in: NT3DMaterialInput) -> vec4<f32> { return vec4<f32>(vec3<f32>(stripe(in.uv.x)), 1.0); }`
	if _, err := NewMaterialShader(src, hookFallbackColor); err != nil {
		t.Fatalf("a hook with names of its own was refused: %v", err)
	}
}

func TestWGSLDeclaredNames(t *testing.T) {
	got := wgslDeclared(`
struct A { x: f32, y: vec2<f32> };
alias B = vec3<f32>;
const c: f32 = 1.0; override d: f32;
@group(0) @binding(0) var<uniform> e: A;
var<private> f: f32;
/* fn hidden() {} */ // fn alsoHidden() {}
@vertex fn g(@builtin(vertex_index) i: u32) -> @builtin(position) vec4<f32> { let inner = 1; var local: f32; return vec4<f32>(); }
fn h(a: f32) -> f32 { return a; }`)
	want := []string{"A", "B", "c", "d", "e", "f", "g", "h"}
	if len(got) != len(want) {
		t.Fatalf("declared %v, want %v", got, want)
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("%s is not among the declared names %v", name, got)
		}
	}
}
