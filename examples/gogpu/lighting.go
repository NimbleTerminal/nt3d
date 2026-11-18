// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dsh "nimbleterminal.dev/nt3d/sh"
)

var (
	envStudioHorizon = sync.OnceValue(func() *nt3d.Environment {
		return nt3d.GradientEnvironment(
			color.RGBA{35, 55, 95, 255},
			color.RGBA{120, 145, 185, 255},
			color.RGBA{25, 30, 40, 255},
		)
	})
	envClearDay = sync.OnceValue(func() *nt3d.Environment {
		return nt3d.GradientEnvironment(
			color.RGBA{72, 130, 220, 255},
			color.RGBA{218, 231, 245, 255},
			color.RGBA{115, 100, 77, 255},
		)
	})
	envGoldenSunset = sync.OnceValue(func() *nt3d.Environment {
		return nt3d.GradientEnvironment(
			color.RGBA{42, 35, 88, 255},
			color.RGBA{245, 135, 68, 255},
			color.RGBA{48, 32, 28, 255},
		)
	})
	envSoftboxPanorama = sync.OnceValue(func() *nt3d.Environment {
		img := image.NewNRGBA(image.Rect(0, 0, 192, 96))
		for y := range 96 {
			for x := range 192 {
				phi := ((float64(x)+0.5)/192 - 0.5) * 2 * math.Pi
				z := math.Cos((float64(y) + 0.5) / 96 * math.Pi)
				warm := math.Max(0, math.Cos(phi)) * math.Sqrt(max(0, 1-z*z))
				cool := math.Max(0, -math.Cos(phi)) * math.Sqrt(max(0, 1-z*z))
				img.SetNRGBA(x, y, color.NRGBA{
					R: uint8(min(255, 48+190*warm+30*cool)),
					G: uint8(min(255, 68+135*warm+85*cool)),
					B: uint8(min(255, 105+60*warm+135*cool+40*max(0, z))),
					A: 255,
				})
			}
		}
		e, _ := nt3d.NewEnvironment(img)
		return e
	})
	envSHBaseRadiance = sync.OnceValue(func() []nt3dsh.RGB {
		radiance, _ := nt3dsh.Project(2, func(d nt3dmath.Vec3) nt3dsh.RGB {
			warm := max(float32(0), d.X) * max(float32(0), d.X)
			cool := max(float32(0), -d.Y) * max(float32(0), -d.Y)
			return nt3dsh.RGB{0.12 + 1.5*warm, 0.12 + 0.65*warm + 0.3*cool, 0.16 + 0.15*warm + 1.5*cool}
		})
		return radiance
	})
	envHDRStudioSun = sync.OnceValue(func() *nt3d.Environment {
		const w, h = 192, 96
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y %d +X %d\n", h, w)
		rgbe := func(r, g, b float64) [4]byte {
			v := max(r, max(g, b))
			if v < 1e-32 {
				return [4]byte{}
			}
			m, exp := math.Frexp(v)
			s := m * 256 / v
			c := func(x float64) byte { return byte(max(0, min(255, int(x*s)))) }
			return [4]byte{c(r), c(g), c(b), byte(exp + 128)}
		}
		sunDir := nt3dmath.Vec3{X: 0.65, Y: -0.45, Z: 0.61}.Normalize()
		for y := range h {
			theta := (float64(y) + 0.5) / h * math.Pi
			sinT, cosT := math.Sin(theta), math.Cos(theta)
			for x := range w {
				phi := ((float64(x)+0.5)/w - 0.5) * 2 * math.Pi
				d := nt3dmath.Vec3{X: float32(sinT * math.Cos(phi)), Y: float32(sinT * math.Sin(phi)), Z: float32(cosT)}
				sky := max(0, float64(d.Z))
				warm := max(0, float64(d.X)) * sinT
				r := 0.08 + 0.30*warm + 0.06*(1-sky)
				g := 0.11 + 0.22*warm + 0.15*sky
				b := 0.19 + 0.08*warm + 0.45*sky
				if dot := float64(d.Dot(sunDir)); dot > 0.978 {
					s := math.Pow((dot-0.978)/0.022, 2)
					r += 14.0 * s
					g += 11.2 * s
					b += 7.8 * s
				}
				px := rgbe(r, g, b)
				buf.Write(px[:])
			}
		}
		e, _ := nt3d.NewHDREnvironment(buf.Bytes())
		return e
	})
)

type studioLighting struct {
	envIndex           int
	rigIndex           int
	postIndex          int
	sunAzimuth         float64 // Degrees around Z.
	sunElevation       float64 // Degrees above XY plane.
	shadows            bool
	oit                bool
	lampPhase          float64
	customEnv          *nt3d.Environment
	customEnvName      string
	overrideInDemo     bool
	overridePostInDemo bool
	overrideOITInDemo  bool
}

type postPreset struct {
	key  string
	name string
	post nt3d.PostProcess
}

var postPresets = []postPreset{
	{key: "off", name: "Off"},
	{key: "studio", name: "Studio", post: nt3d.PostProcess{
		SSAO:     nt3d.SSAO{Strength: 0.65, Radius: 0.35},
		Bloom:    nt3d.Bloom{Strength: 0.35, Threshold: 0.82, Radius: 1},
		ToneMap:  nt3d.ACESToneMap,
		Vignette: 0.32,
	}},
	{key: "bloom", name: "Bloom + ACES", post: nt3d.PostProcess{
		Bloom:   nt3d.Bloom{Strength: 0.65, Threshold: 0.70, Radius: 1},
		ToneMap: nt3d.ACESToneMap,
	}},
	{key: "ssao", name: "SSAO", post: nt3d.PostProcess{
		SSAO: nt3d.SSAO{Strength: 0.85, Radius: 0.40},
	}},
	{key: "cinema", name: "Cinema Warm", post: nt3d.PostProcess{
		Exposure: 0.25,
		Bloom:    nt3d.Bloom{Strength: 0.25, Threshold: 0.85, Radius: 1},
		ToneMap:  nt3d.ACESToneMap,
		Vignette: 0.55,
	}},
	{key: "reinhard", name: "Reinhard + Vignette", post: nt3d.PostProcess{
		Exposure: 0.30,
		ToneMap:  nt3d.ReinhardToneMap,
		Vignette: 0.48,
	}},
}

var envPresetNames = []string{
	"Studio Horizon",
	"HDR Studio Sun",
	"Clear Day",
	"Golden Sunset",
	"Softbox Panorama",
	"Analytic SH",
	"Solid Dark",
}

var rigPresetNames = []string{
	"Key + Sky Fill",
	"3-Point Studio",
	"Spotlights",
	"Orbiting RGB",
	"Night Lantern",
	"8-Lamp Ring",
	"Pure IBL",
}

func newStudioLighting() *studioLighting {
	return &studioLighting{
		envIndex:     0,
		rigIndex:     0,
		sunAzimuth:   -45,
		sunElevation: 48,
		oit:          true,
	}
}

func (l *studioLighting) envCount() int {
	if l.customEnv != nil {
		return len(envPresetNames) + 1
	}
	return len(envPresetNames)
}

func (l *studioLighting) envName() string {
	if l.customEnv != nil && l.envIndex == len(envPresetNames) {
		return "Panorama: " + l.customEnvName
	}
	if l.envIndex >= 0 && l.envIndex < len(envPresetNames) {
		return envPresetNames[l.envIndex]
	}
	return envPresetNames[0]
}

func (l *studioLighting) rigName() string {
	if l.rigIndex >= 0 && l.rigIndex < len(rigPresetNames) {
		return rigPresetNames[l.rigIndex]
	}
	return rigPresetNames[0]
}

func (l *studioLighting) environment() *nt3d.Environment {
	if l.customEnv != nil && l.envIndex == len(envPresetNames) {
		return l.customEnv
	}
	switch l.envIndex {
	case 0:
		return envStudioHorizon()
	case 1:
		return envHDRStudioSun()
	case 2:
		return envClearDay()
	case 3:
		return envGoldenSunset()
	case 4:
		return envSoftboxPanorama()
	case 5:
		rad := l.sunAzimuth * math.Pi / 180
		if rotated, err := nt3dsh.Rotate(envSHBaseRadiance(), nt3dmath.RotationZ(rad)); err == nil {
			if env, err := nt3d.NewSHEnvironment(rotated); err == nil {
				return env
			}
		}
		return envStudioHorizon()
	case 6:
		return nil
	default:
		return envStudioHorizon()
	}
}

func (l *studioLighting) applyEnvironment(v *viewerState) {
	if v == nil || v.scene == nil {
		return
	}
	if len(v.splatNodes) > 0 && l.envIndex == 0 {
		v.scene.SetEnvironment(nil)
		return
	}
	v.scene.SetEnvironment(l.environment())
}

func (l *studioLighting) setCustomPanorama(name string, env *nt3d.Environment, v *viewerState) {
	l.customEnv = env
	l.customEnvName = name
	l.envIndex = len(envPresetNames)
	l.overrideInDemo = true
	l.applyEnvironment(v)
}

func (l *studioLighting) cycleEnvironment(v *viewerState) {
	l.envIndex = (l.envIndex + 1) % l.envCount()
	l.overrideInDemo = true
	l.applyEnvironment(v)
	fmt.Printf("Environment preset: %s\n", l.envName())
}

func (l *studioLighting) cycleRig() {
	l.rigIndex = (l.rigIndex + 1) % len(rigPresetNames)
	l.overrideInDemo = true
	fmt.Printf("Lighting rig: %s\n", l.rigName())
}

func (l *studioLighting) toggleShadows() {
	l.shadows = !l.shadows
	l.overrideInDemo = true
	fmt.Printf("GPU Shadow Mapping: %v\n", l.shadows)
}

func (l *studioLighting) toggleOIT() {
	l.oit = !l.oit
	l.overrideOITInDemo = true
	fmt.Printf("Weighted Blended OIT: %v\n", l.oit)
}

func (l *studioLighting) postName() string {
	if l.postIndex >= 0 && l.postIndex < len(postPresets) {
		return postPresets[l.postIndex].name
	}
	return postPresets[0].name
}

func (l *studioLighting) setPostByName(name string) error {
	clean := strings.ToLower(strings.TrimSpace(name))
	if clean == "" || clean == "none" {
		clean = "off"
	}
	for i, p := range postPresets {
		if p.key == clean || strings.EqualFold(p.name, clean) {
			l.postIndex = i
			if i > 0 {
				l.overridePostInDemo = true
			}
			return nil
		}
	}
	return fmt.Errorf("invalid post preset %q (want off, studio, bloom, ssao, cinema, or reinhard)", name)
}

func (l *studioLighting) cyclePost() {
	l.postIndex = (l.postIndex + 1) % len(postPresets)
	l.overridePostInDemo = true
	fmt.Printf("GPU Post-Processing: %s\n", l.postName())
}

func (l *studioLighting) applyFramePost(f *nt3d.Frame, isDemo bool) {
	if !isDemo || l.overrideOITInDemo {
		f.OIT = l.oit
	}
	if isDemo && !l.overridePostInDemo {
		return
	}
	if l.postIndex >= 0 && l.postIndex < len(postPresets) {
		f.PostProcess = postPresets[l.postIndex].post
	}
}

func (l *studioLighting) rotateSun(dAzimuth, dElevation float64, v *viewerState) {
	l.sunAzimuth = math.Mod(l.sunAzimuth+dAzimuth, 360)
	if l.sunAzimuth > 180 {
		l.sunAzimuth -= 360
	} else if l.sunAzimuth < -180 {
		l.sunAzimuth += 360
	}
	l.sunElevation = max(5, min(88, l.sunElevation+dElevation))
	l.overrideInDemo = true
	if l.envIndex == 5 {
		l.applyEnvironment(v)
	}
}

func (l *studioLighting) step(dt float64) {
	if dt > 0 {
		l.lampPhase = math.Mod(l.lampPhase+dt, 200*math.Pi)
	}
}

func sunDirection(azimuthDeg, elevationDeg float64) nt3dmath.Vec3 {
	az := azimuthDeg * math.Pi / 180
	el := elevationDeg * math.Pi / 180
	cosEl := math.Cos(el)
	return (nt3dmath.Vec3{
		X: float32(cosEl * math.Cos(az)),
		Y: float32(cosEl * math.Sin(az)),
		Z: float32(math.Sin(el)),
	}).Normalize()
}

func hueWheel(h float64) color.RGBA {
	ch := func(shift float64) uint8 {
		v := math.Abs(math.Mod(h*6+shift, 6)-3) - 1
		return uint8(255 * max(0, min(1, v)))
	}
	return color.RGBA{ch(0), ch(4), ch(2), 255}
}

func (l *studioLighting) applyFrameLighting(f *nt3d.Frame, v *viewerState) {
	isDemo := v != nil && v.runner != nil
	if isDemo && !l.overrideInDemo {
		return
	}
	if l.envIndex == 6 {
		f.Environment = nil
		f.Background = color.RGBA{20, 24, 32, 255}
	} else if isDemo {
		f.Environment = l.environment()
	}
	// Preserve authored glTF KHR_lights_punctual SceneLights until the user cycles rigs.
	hasSceneLights := v != nil && v.scene != nil && len(v.scene.Lights(nt3d.Root)) > 0
	if !isDemo && !l.overrideInDemo && hasSceneLights && len(f.Lighting.Lights) > 0 {
		if l.shadows {
			f.Lighting.Lights[0].Shadows = true
		}
		return
	}
	sunDir := sunDirection(l.sunAzimuth, l.sunElevation)
	switch l.rigIndex {
	case 0: // Key + Sky Fill
		f.Lighting = nt3d.Lighting{
			Ambient: nt3d.Ambient{
				Sky:       color.RGBA{95, 105, 120, 255},
				Ground:    color.RGBA{55, 50, 45, 255},
				Intensity: 1,
			},
			Lights: []nt3d.Light{{
				Kind:      nt3d.DirectionalLight,
				Direction: sunDir,
				Color:     color.RGBA{255, 250, 242, 255},
				Intensity: 0.9,
				Shadows:   l.shadows,
			}},
		}
	case 1: // 3-Point Studio
		fillDir := sunDirection(l.sunAzimuth+115, 20)
		rimDir := sunDirection(l.sunAzimuth+180, 35)
		f.Lighting = nt3d.Lighting{
			Ambient: nt3d.Ambient{
				Sky:       color.RGBA{60, 68, 82, 255},
				Ground:    color.RGBA{34, 34, 40, 255},
				Intensity: 0.65,
			},
			Lights: []nt3d.Light{
				{
					Kind:      nt3d.DirectionalLight,
					Direction: sunDir,
					Color:     color.RGBA{255, 242, 224, 255},
					Intensity: 1.0,
					Shadows:   l.shadows,
				},
				{
					Kind:      nt3d.DirectionalLight,
					Direction: fillDir,
					Color:     color.RGBA{150, 188, 255, 255},
					Intensity: 0.48,
				},
				{
					Kind:      nt3d.DirectionalLight,
					Direction: rimDir,
					Color:     color.RGBA{255, 255, 255, 255},
					Intensity: 0.62,
				},
			},
		}
	case 2: // Spotlights
		f.Lighting = nt3d.Lighting{
			Ambient: nt3d.Ambient{
				Sky:       color.RGBA{50, 56, 72, 255},
				Ground:    color.RGBA{20, 20, 26, 255},
				Intensity: 0.25,
			},
		}
		spotColors := [3]color.RGBA{{255, 226, 188, 255}, {80, 188, 255, 255}, {255, 95, 165, 255}}
		speeds := [3]float64{0.45, -0.35, 0.55}
		intensities := [3]float32{3.4, 2.5, 2.3}
		for i := range 3 {
			a := l.lampPhase*speeds[i] + float64(i)*2*math.Pi/3
			pos := nt3dmath.Vec3{X: 1.85 * float32(math.Cos(a)), Y: 1.85 * float32(math.Sin(a)), Z: 1.75}
			aim := nt3dmath.Vec3{Z: 0.1}
			f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
				Kind:       nt3d.SpotLight,
				Position:   pos,
				Direction:  aim.Sub(pos).Normalize(),
				Color:      spotColors[i],
				Intensity:  intensities[i],
				Range:      5.5,
				InnerAngle: 11.5,
				OuterAngle: 27.5,
				Shadows:    i == 0 && l.shadows,
			})
		}
	case 3: // Orbiting RGB
		f.Lighting = nt3d.Lighting{
			Ambient: nt3d.Ambient{
				Sky:       color.RGBA{45, 48, 58, 255},
				Ground:    color.RGBA{22, 22, 28, 255},
				Intensity: 0.35,
			},
		}
		if l.shadows {
			f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
				Kind:      nt3d.DirectionalLight,
				Direction: sunDir,
				Color:     color.RGBA{200, 210, 230, 255},
				Intensity: 0.35,
				Shadows:   true,
			})
		}
		rgbColors := [3]color.RGBA{{255, 65, 55, 255}, {65, 255, 95, 255}, {75, 120, 255, 255}}
		speeds := [3]float64{0.9, -0.7, 0.55}
		heights := [3]float32{0.45, 0.75, 1.0}
		for i := range 3 {
			a := l.lampPhase*speeds[i] + float64(i)*2*math.Pi/3
			f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
				Kind:      nt3d.PointLight,
				Position:  nt3dmath.Vec3{X: 1.55 * float32(math.Cos(a)), Y: 1.55 * float32(math.Sin(a)), Z: heights[i]},
				Color:     rgbColors[i],
				Intensity: 1.85,
				Range:     3.8,
			})
		}
	case 4: // Night Lantern
		a := l.lampPhase * 0.65
		f.Lighting = nt3d.Lighting{
			Ambient: nt3d.Ambient{
				Sky:       color.RGBA{24, 30, 56, 255},
				Ground:    color.RGBA{10, 10, 14, 255},
				Intensity: 0.85,
			},
		}
		if l.shadows {
			f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
				Kind:      nt3d.DirectionalLight,
				Direction: sunDir,
				Color:     color.RGBA{120, 140, 200, 255},
				Intensity: 0.3,
				Shadows:   true,
			})
		}
		f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
			Kind:      nt3d.PointLight,
			Position:  nt3dmath.Vec3{X: 1.5 * float32(math.Cos(a)), Y: 1.5 * float32(math.Sin(a)), Z: 0.42},
			Color:     color.RGBA{255, 175, 95, 255},
			Intensity: 2.25,
			Range:     3.5,
		})
	case 5: // 8-Lamp Ring
		f.Lighting = nt3d.Lighting{
			Ambient: nt3d.Ambient{
				Sky:       color.RGBA{38, 38, 46, 255},
				Ground:    color.RGBA{18, 18, 22, 255},
				Intensity: 0.22,
			},
		}
		count := nt3d.MaxLights
		if l.shadows {
			f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
				Kind:      nt3d.DirectionalLight,
				Direction: sunDir,
				Color:     color.RGBA{220, 225, 235, 255},
				Intensity: 0.35,
				Shadows:   true,
			})
			count = nt3d.MaxLights - 1
		}
		for i := range count {
			h := float64(i) / float64(count)
			a := l.lampPhase*0.45 + h*2*math.Pi
			f.Lighting.Lights = append(f.Lighting.Lights, nt3d.Light{
				Kind:      nt3d.PointLight,
				Position:  nt3dmath.Vec3{X: 1.8 * float32(math.Cos(a)), Y: 1.8 * float32(math.Sin(a)), Z: 0.55},
				Color:     hueWheel(h),
				Intensity: 1.35,
				Range:     2.6,
			})
		}
	case 6: // Pure IBL
		f.Lighting = nt3d.Lighting{}
	}
}
