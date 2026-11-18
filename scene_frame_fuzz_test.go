// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"encoding/binary"
	"image"
	"image/color"
	"math"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// fuzzStream reads a scene's worth of values out of a fuzzer's bytes. A
// float is usually one of a few values that matter (zero, one, the ends of a
// range, something huge, something not a number) and sometimes any bits at
// all, so that most of what it builds is accepted and the rest is anything.
type fuzzStream struct{ data []byte }

func (r *fuzzStream) u8() uint8 {
	if len(r.data) == 0 {
		return 0
	}
	b := r.data[0]
	r.data = r.data[1:]
	return b
}

func (r *fuzzStream) bit() bool { return r.u8()&1 == 1 }

var fuzzFloats = [...]float64{0, 0, 0, 1, 1, .5, .25, 2, 45, 89, -1, 1e-6, 1e6, 1e9, 1e30, 1e39, 1e300, -1e9, math.Inf(1), math.NaN()}

func (r *fuzzStream) f64() float64 {
	b := r.u8()
	if b < 220 {
		return fuzzFloats[int(b)%len(fuzzFloats)]
	}
	var raw [8]byte
	for i := range raw {
		raw[i] = r.u8()
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(raw[:]))
}

func (r *fuzzStream) f32() float32       { return float32(r.f64()) }
func (r *fuzzStream) vec() nt3dmath.Vec3 { return nt3dmath.Vec3{X: r.f32(), Y: r.f32(), Z: r.f32()} }
func (r *fuzzStream) rgba() color.RGBA   { return color.RGBA{r.u8(), r.u8(), r.u8(), r.u8()} }

// material is any material at all: usually with only the group its lighting
// model uses, since any other is refused.
func (r *fuzzStream) material() *Material {
	m := &Material{
		Shading: ShadingModel(r.u8() % 4), Alpha: AlphaMode(r.u8() % 5),
		Threshold: r.f32(), BaseColor: r.rgba(),
		Emission:     EmissionStyle{Color: r.rgba(), Intensity: r.f32()},
		Transmission: TransmissionStyle{Amount: r.f32(), Refraction: r.f32(), Absorption: r.vec()},
		Toon:         ToonStyle{Bands: int(r.u8() % 10), Softness: r.f32(), ShadowSoftness: r.f32(), Shade: r.rgba(), Rim: r.f32()},
	}
	stray := r.u8() < 16
	if m.Shading == ShadePhong || stray {
		m.Phong = PhongStyle{Specular: r.f32(), Shininess: r.f32(), Reflectivity: r.f32()}
	}
	if m.Shading == ShadePBR || stray {
		m.PBR = PBRStyle{
			Metallic: r.f32(), Roughness: r.f32(), IOR: r.f32(),
			Specular:  SpecularStyle{Color: r.rgba(), Intensity: r.f32(), Strength: r.f32(), Off: r.bit()},
			Clearcoat: ClearcoatStyle{Amount: r.f32(), Roughness: r.f32()},
			Sheen:     SheenStyle{Color: r.rgba(), Roughness: r.f32()},
		}
	}
	return m
}

func (r *fuzzStream) light() Light {
	return Light{
		Kind: LightKind(r.u8() % 4), Direction: r.vec(), Position: r.vec(), Color: r.rgba(),
		Intensity: r.f32(), Range: r.f32(), InnerAngle: r.f32(), OuterAngle: r.f32(),
		Shadows: r.bit(), Off: r.bit(),
	}
}

// FuzzSceneFrame holds the promise the whole API rests on, for any scene and
// camera: what a scene and Camera.Validate accept, a renderer can draw.
// Scene.Frame takes a frame, the frame is one renderers accept, and software
// draws it, whatever the material, lights, fog, reflection probe, and
// origins are. A value a setter refuses is simply not set: refusing is fine,
// and it is accepting and then failing to draw that is not.
//
// FuzzCamera holds the same for the camera's own fields against one fixed
// scene; here the scene varies, and the camera's place in it.
func FuzzSceneFrame(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{1, 0, 3, 3, 255, 255, 255, 255, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3})
	// An origin far out, shared by the camera and the ball, with a light, a
	// probe, and a lamp left near the world's own origin.
	f.Add([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 13, 0, 0, 1, 1, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 1, 3, 3, 3, 3, 3})
	// A light node left near the world's origin with the camera 1e39 away:
	// rebased into the frame, the light's position was infinite, and the
	// frame was refused.
	f.Add([]byte("000000000000000000000000000000000070000000000000000000000000000000000000000000000000000000000100000000"))

	sky, err := NewEnvironment(image.NewNRGBA(image.Rect(0, 0, 8, 4)))
	if err != nil {
		f.Fatal(err)
	}
	renderer := NewSoftwareRenderer()
	f.Cleanup(func() { renderer.Close() })

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 512 {
			return
		}
		r := &fuzzStream{data}
		s := NewScene()
		ball, err := BuildSphere(Sphere{Radius: .5, Segments: 8, Rings: 4, Color: r.rgba(), Material: r.material()})
		if err != nil {
			ball, err = BuildSphere(Sphere{Radius: .5, Segments: 8, Rings: 4, Color: color.RGBA{A: 255}})
			if err != nil {
				t.Fatal(err)
			}
		}
		id, err := s.SetGeometry("ball", ball)
		if err != nil {
			return // A material the scene refuses.
		}
		origin := [3]float64{r.f64(), r.f64(), r.f64()}
		if s.SetNodeOrigin(id, origin) != nil {
			origin = [3]float64{}
		}

		look := s.Look()
		if lighting := (Lighting{Ambient: Ambient{Sky: r.rgba(), Ground: r.rgba(), Intensity: r.f32(), Off: r.bit()}, Lights: []Light{r.light(), r.light()}}); validLighting(lighting) {
			look.Lighting = lighting
		}
		if fog := (Fog{Near: r.f32(), Far: r.f32(), Color: r.rgba()}); validFog(fog) {
			look.Fog = fog
		}
		look.Environment, look.EnvironmentStrength, look.EnvironmentBackgroundOnly = sky, 1, r.bit()
		s.SetLook(look)

		at, half := r.vec(), r.f32()
		_ = s.SetReflectionProbes([]ReflectionProbe{{
			Environment: sky, Position: at, Strength: r.f32(), Off: r.bit(),
			Bounds: nt3dmath.AABB{Min: at.Sub(nt3dmath.Vec3{X: half, Y: half, Z: half}), Max: at.Add(nt3dmath.Vec3{X: half, Y: half, Z: half})},
		}})
		if r.bit() {
			if lamp, err := s.Add(Root, "lamp"); err == nil {
				_ = s.SetTransform(lamp, nt3dmath.Translation(r.vec()))
				_ = s.SetNodeLight(lamp, r.light())
			}
		}

		c := DefaultCamera()
		c.Origin, c.Elevation, c.Azimuth, c.Distance = origin, r.f64(), r.f64(), r.f64()
		if r.bit() {
			c.Projection = Perspective
		}
		if c.Validate() != nil {
			return
		}
		frame, err := s.StillFrame(c, 16, 12)
		if err != nil {
			t.Fatalf("Scene.StillFrame refused a scene and camera it had accepted: %v", err)
		}
		if err := validateFrame(frame); err != nil {
			t.Fatalf("an accepted scene and camera made a frame no renderer draws: %v\norigin %v\nlights %+v\nprobes %d", err, frame.Origin, frame.Lighting.Lights, len(frame.ReflectionProbes))
		}
		if _, err := renderer.Render(frame); err != nil {
			t.Fatalf("software refused a frame that validates: %v", err)
		}
	})
}

// FuzzNewHDRLightMap feeds Radiance HDR bytes to the lightmap reader, which
// shares the environment's decoder and then builds a mip chain of its own.
// Refusing is fine; what it accepts is a lightmap a material can use.
func FuzzNewHDRLightMap(f *testing.F) {
	f.Add(encodeRadianceHDR(8, 4, true, 1.0, "", func(x, y int) [3]float64 {
		return [3]float64{float64(x) * 0.25, float64(y) * 0.5, 2.0}
	}))
	f.Add([]byte("FORMAT=32-bit_rle_rgbe\n\n-Y 1 +X 1\n\x80\x40\x20\x81"))
	f.Add([]byte("#?RADIANCE\n\n-Y 1 +X 8\n\x02\x02\x00\x08\x88\x10\x88\x20\x88\x30\x88\x81"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			return
		}
		m, err := NewHDRLightMap(data)
		if err != nil {
			return
		}
		if !m.valid() {
			t.Fatal("an accepted lightmap is not valid")
		}
		if err := (Material{LightMap: m}).Validate(); err != nil {
			t.Fatalf("a material refused an accepted lightmap: %v", err)
		}
	})
}
