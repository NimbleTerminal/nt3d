// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"fmt"
	"image/color"
	"io/fs"
	"path"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

// AnimateMain plays the Khronos glTF samples supplied by cmd/animate in SampleFiles.
func AnimateMain() { mainDemo(false, sceneAnimate) }

var animatedSamples = []sample{
	{file: "AnimatedMorphCube/glTF-Binary/AnimatedMorphCube.glb", title: "Animated Morph Cube", about: "two morph targets: thin cube and ramp", beta: 40},
	{file: "SimpleSkin/glTF/SimpleSkin.gltf", title: "Simple Skin", about: "two joints bending a strip with blended vertex weights", beta: -90},
	{file: "AnimatedTriangle/glTF/AnimatedTriangle.gltf", title: "Animated Triangle", about: "quaternion rotation of a triangle", beta: -90},
}

type animateScene struct {
	samples       []sample
	title, credit string
	index, clip   int
	group         nt3d.NodeID
	model         *nt3dmesh.Model
	clips         []nt3d.AnimationClip
	serial        int
}

func (d *demo) setupAnimate() error {
	d.animatedSamples = &animateScene{samples: animatedSamples, title: "animate", credit: "Khronos samples, CC0"}
	return d.loadAnimatedSample(0, 0)
}

// embeddedModel uses the regular mesh registry, resolving side files relative
// to the model, with the same path confinement as the disk opener.
func embeddedModel(files fs.FS, name string) (*nt3dmesh.Model, error) {
	if files == nil {
		return nil, fmt.Errorf("sample files are not configured")
	}
	data, err := fs.ReadFile(files, name)
	if err != nil {
		return nil, err
	}
	format, ok := nt3dmesh.FormatOf(name, data)
	if !ok {
		return nil, fmt.Errorf("no mesh loader registered for %s", name)
	}
	dir := path.Dir(name)
	open := func(relative string) ([]byte, error) {
		relative = path.Clean(strings.ReplaceAll(relative, `\`, "/"))
		if !fs.ValidPath(relative) {
			return nil, fmt.Errorf("sample side file outside model directory")
		}
		return fs.ReadFile(files, path.Join(dir, relative))
	}
	return nt3dmesh.Decode(data, format, &nt3dmesh.Options{Open: open, Color: color.RGBA{80, 180, 235, 255}})
}

// Reinstantiating on clip changes resets channels absent from the new clip.
// Keep the previous instance alive until the replacement is ready.
func (d *demo) loadAnimatedSample(index, clip int) error {
	state := d.animatedSamples
	index = (index%len(state.samples) + len(state.samples)) % len(state.samples)
	sample := state.samples[index]
	model, err := embeddedModel(SampleFiles, sample.file)
	if err != nil {
		return fmt.Errorf("%s: %w", sample.title, err)
	}
	if model.Asset == nil || len(model.Asset.Clips) == 0 {
		return fmt.Errorf("%s has no animation clips", sample.title)
	}
	state.serial++
	root, err := model.AddTo(d.scene, d.group, fmt.Sprintf("animated-sample-%d", state.serial))
	if err != nil {
		return err
	}
	ready := false
	defer func() {
		if !ready {
			_ = d.scene.Remove(root)
		}
	}()
	fit, _, _ := model.Bounds().Normalization()
	if err = d.scene.SetTransform(root, fit); err != nil {
		return err
	}
	clips := d.scene.AnimationClips(root)
	clip = (clip%len(clips) + len(clips)) % len(clips)
	player, err := nt3d.NewAnimation(d.scene, clips[clip])
	if err != nil {
		return err
	}
	player.Mode = nt3d.Loop
	if err = player.Sample(0); err != nil {
		return err
	}
	if state.group != nt3d.Root {
		if err = d.scene.Remove(state.group); err != nil {
			return err
		}
	}
	state.index, state.clip, state.group, state.model, state.clips = index, clip, root, model, clips
	d.animation, d.animationTime = player, 0
	d.lastPhase = time.Now()
	d.status = sample.about
	if len(model.Warnings) > 0 {
		d.status += " · " + strings.Join(model.Warnings, "; ")
	}
	ready = true
	return nil
}

func (d *demo) stepAnimatedSample(step int) tea.Cmd {
	if err := d.loadAnimatedSample(d.animatedSamples.index+step, 0); err != nil {
		d.status = err.Error()
		return d.view.Refresh()
	}
	return tea.Batch(d.view.Refresh(), d.frameAnimatedSample())
}
func (d *demo) stepAnimationClip(step int) tea.Cmd {
	state := d.animatedSamples
	if len(state.clips) < 2 {
		return nil
	}
	if err := d.loadAnimatedSample(state.index, state.clip+step); err != nil {
		d.status = err.Error()
	}
	return d.view.Refresh()
}
func (d *demo) frameAnimatedSample() tea.Cmd {
	camera := d.view.Camera()
	camera.Beta = d.animatedSamples.samples[d.animatedSamples.index].beta
	camera.Alpha = 15
	first := d.view.SetCamera(camera)
	fit := d.view.Fit()
	camera = d.view.Camera()
	camera.Distance *= 1.6 // Room for the animated pose.
	return tea.Batch(first, fit, d.view.SetCamera(camera))
}
func (d *demo) animateView(content string) string {
	state := d.animatedSamples
	play := "playing"
	if !d.animate {
		play = "paused"
	}
	name := state.clips[state.clip].Name
	if name == "" {
		name = fmt.Sprintf("clip %d", state.clip+1)
	}
	return fmt.Sprintf("nt3d %s · %s (%d/%d) · [ ] model · , . clip · a play/pause · enter restart · 1/2/3 backend · q quit\n%s\n%s · %s · %s (%d/%d) · %s · %s",
		state.title, state.samples[state.index].title, state.index+1, len(state.samples), content, d.rendering(), play, name, state.clip+1, len(state.clips), state.credit, d.status)
}

func (d *demo) restartAnimation() tea.Cmd {
	state := d.animatedSamples
	if err := d.loadAnimatedSample(state.index, state.clip); err != nil {
		d.status = err.Error()
	}
	return d.view.Refresh()
}
