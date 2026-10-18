// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"fmt"
	"io/fs"
	"nimbleterminal.dev/nt3d"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

func AnimationMain() { mainDemo(false, sceneAnimation) }
func (d *demo) setupAnimation() error {
	data, err := fs.ReadFile(SampleFiles, "animated.gltf")
	if err != nil {
		return err
	}
	format, ok := nt3dmesh.FormatOf("animated.gltf", data)
	if !ok {
		return fmt.Errorf("glTF loader is not registered")
	}
	model, err := nt3dmesh.Decode(data, format, nil)
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
