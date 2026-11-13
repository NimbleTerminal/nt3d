// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"nimbleterminal.dev/nt3d"
)

func savePNG(path string, img image.Image) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

var turntablePalette = sync.OnceValue(func() color.Palette {
	pal := make(color.Palette, 0, 256)
	// 6x6x6 perceptual RGB cube (216 colors).
	steps := [6]uint8{0, 51, 102, 153, 204, 255}
	for _, r := range steps {
		for _, g := range steps {
			for _, b := range steps {
				pal = append(pal, color.RGBA{R: r, G: g, B: b, A: 255})
			}
		}
	}
	// 24-step neutral grayscale ramp + 16-step studio sky/slate ramp (40 colors).
	for i := range 24 {
		v := uint8((i*255 + 11) / 23)
		pal = append(pal, color.RGBA{R: v, G: v, B: v, A: 255})
	}
	for i := range 16 {
		t := float64(i) / 15.0
		pal = append(pal, color.RGBA{
			R: uint8(18 + t*110),
			G: uint8(24 + t*125),
			B: uint8(36 + t*160),
			A: 255,
		})
	}
	return pal
})

func saveTurntableGIF(path string, frames []*image.NRGBA, fps int) error {
	if len(frames) == 0 {
		return fmt.Errorf("saveTurntableGIF: no frames to encode")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if fps <= 0 {
		fps = 24
	}
	delay := max(1, (100+fps/2)/fps)
	pal := turntablePalette()
	paletted := make([]*image.Paletted, len(frames))
	delays := make([]int, len(frames))

	var wg sync.WaitGroup
	wg.Add(len(frames))
	for i, src := range frames {
		delays[i] = delay
		go func(idx int, img *image.NRGBA) {
			defer wg.Done()
			dst := image.NewPaletted(img.Bounds(), pal)
			draw.FloydSteinberg.Draw(dst, img.Bounds(), img, img.Bounds().Min)
			paletted[idx] = dst
		}(i, src)
	}
	wg.Wait()

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := gif.EncodeAll(f, &gif.GIF{
		Image: paletted,
		Delay: delays,
	}); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func buildCaptureFrame(v *viewerState, cam nt3d.Camera, lighting *studioLighting, w, h, supersample int) (nt3d.Frame, error) {
	if v.runner != nil {
		v.runner.SetViewportSize(w, h)
	}
	f, err := v.buildFrame(cam, w, h, max(1, supersample))
	if err != nil {
		return nt3d.Frame{}, err
	}
	if lighting != nil {
		lighting.applyFrameLighting(&f, v.runner != nil)
	}
	if len(v.splatNodes) > 0 && (lighting == nil || lighting.envIndex == 0) {
		f.Background = color.RGBA{18, 20, 26, 255}
	}
	v.decorateFrame(&f, cam, false, false)
	return f, nil
}

func captureSingleFrame(session *nt3d.GPUSession, v *viewerState, cam nt3d.Camera, lighting *studioLighting, w, h, supersample int) (*image.NRGBA, error) {
	f, err := buildCaptureFrame(v, cam, lighting, w, h, supersample)
	if err != nil {
		return nil, err
	}
	return session.RenderToImage(f)
}

func captureSingleFrameInto(session *nt3d.GPUSession, v *viewerState, cam nt3d.Camera, lighting *studioLighting, w, h, supersample int, dst *image.NRGBA) error {
	f, err := buildCaptureFrame(v, cam, lighting, w, h, supersample)
	if err != nil {
		return err
	}
	return session.RenderToImageInto(f, dst)
}

func captureScreenshot(session *nt3d.GPUSession, v *viewerState, cam nt3d.Camera, lighting *studioLighting, w, h, supersample int, path string) error {
	img, err := captureSingleFrame(session, v, cam, lighting, w, h, max(2, supersample))
	if err != nil {
		return err
	}
	return savePNG(path, img)
}

func captureTurntable(session *nt3d.GPUSession, v *viewerState, baseCam nt3d.Camera, lighting *studioLighting, w, h, supersample, numFrames, fps int, path string) error {
	numFrames = max(1, numFrames)
	fps = max(1, fps)
	dt := 1.0 / float64(fps)
	if v.runner != nil {
		v.runner.DetachSampleCamera()
		defer v.runner.SetCamera(baseCam)
	}

	ext := filepath.Ext(path)
	if strings.EqualFold(ext, ".png") {
		buf := image.NewNRGBA(image.Rect(0, 0, w, h))
		stem := strings.TrimSuffix(path, ext)
		for i := range numFrames {
			if i > 0 {
				if lighting != nil {
					lighting.step(dt)
				}
				if v.runner != nil {
					v.runner.Step(dt)
				} else if v.player != nil {
					_ = v.player.Step(dt)
				}
			}
			cam := baseCam.Orbit(float64(i)*360.0/float64(numFrames), 0)
			if v.runner != nil {
				v.runner.SetCamera(cam)
			}
			if err := captureSingleFrameInto(session, v, cam, lighting, w, h, max(1, supersample), buf); err != nil {
				return err
			}
			framePath := path
			if numFrames > 1 {
				framePath = fmt.Sprintf("%s-%03d%s", stem, i, ext)
			}
			if err := savePNG(framePath, buf); err != nil {
				return err
			}
		}
		return nil
	}

	frames := make([]*image.NRGBA, 0, numFrames)
	for i := range numFrames {
		if i > 0 {
			if lighting != nil {
				lighting.step(dt)
			}
			if v.runner != nil {
				v.runner.Step(dt)
			} else if v.player != nil {
				_ = v.player.Step(dt)
			}
		}
		cam := baseCam.Orbit(float64(i)*360.0/float64(numFrames), 0)
		if v.runner != nil {
			v.runner.SetCamera(cam)
		}
		img, err := captureSingleFrame(session, v, cam, lighting, w, h, max(1, supersample))
		if err != nil {
			return err
		}
		frames = append(frames, img)
	}
	return saveTurntableGIF(path, frames, fps)
}
