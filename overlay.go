// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"image"
	"image/draw"
)

func drawOverlays(img *image.RGBA, f Frame) {
	for _, r := range f.Overlays {
		if r.Width > 0 && r.Height > 0 {
			draw.Draw(img, image.Rect(r.X, r.Y, r.X+r.Width, r.Y+r.Height), image.NewUniform(r.Color), image.Point{}, draw.Src)
		}
	}
}
