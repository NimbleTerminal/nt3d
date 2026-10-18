// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	_ "embed"
	"fmt"
	"nimbleterminal.dev/nt3d"
	nt3dgltf "nimbleterminal.dev/nt3d/mesh/gltf"
)

// animatedGLTF is an original, generated CC0 ribbon with two joints and a morph.
//
//go:embed models/animated.gltf
var animatedGLTF []byte

func AnimationMain() { mainDemo(false, sceneAnimation) }
func (d *demo) setupAnimation() error {
	model, err := nt3dgltf.Decode(animatedGLTF, nil)
	if err != nil {
		return err
	}
	if model.Asset == nil {
		return fmt.Errorf("animation demo has no asset")
	}
	instance, err := model.Asset.Instantiate(d.scene, d.group, "ribbon")
	if err != nil {
		return err
	}
	d.animation, err = nt3d.NewAnimation(d.scene, instance.Clips[0])
	if err != nil {
		return err
	}
	d.animation.Mode = nt3d.Loop
	d.animate = true
	d.animationTime = 1
	return d.animation.Sample(1)
}
