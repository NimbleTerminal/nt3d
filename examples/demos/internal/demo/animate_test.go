// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"encoding/json"
	"image"
	"image/color"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	gltfsamples "nimbleterminal.dev/nt3d/examples/demos/internal/demo/gltf"
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo/models"
	_ "nimbleterminal.dev/nt3d/mesh/gltf"
	nt3dwidget "nimbleterminal.dev/nt3d/widget"
)

func newAnimateDemo(t *testing.T) *demo {
	t.Helper()
	old := SampleFiles
	SampleFiles = gltfsamples.Files
	t.Cleanup(func() { SampleFiles = old })
	d := &demo{scene: nt3d.NewScene(), animate: true}
	var err error
	d.group, err = d.scene.Add(nt3d.Root, "samples")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.setupAnimate(); err != nil {
		t.Fatal(err)
	}
	d.view = nt3dwidget.New(d.scene, 80, 24, nt3dwidget.WithRenderMode(nt3d.Software))
	t.Cleanup(func() { d.view.Close() })
	d.frameAnimatedSample()
	return d
}
func TestAnimatedSamplesPlay(t *testing.T) {
	d := newAnimateDemo(t)
	for i, sample := range animatedSamples {
		if err := d.loadAnimatedSample(i, 0); err != nil {
			t.Fatalf("%s: %v", sample.title, err)
		}
		if len(d.animatedSamples.model.Warnings) > 0 {
			t.Fatalf("%s warnings: %v", sample.title, d.animatedSamples.model.Warnings)
		}
		d.frameAnimatedSample()
		before := d.scene.Revision()
		if err := d.animation.Sample(d.animation.Clip.Duration() * .37); err != nil {
			t.Fatal(err)
		}
		if d.scene.Revision() == before {
			t.Fatalf("%s did not animate", sample.title)
		}
		f, err := d.scene.Frame(d.view.Camera(), 160, 120)
		if err != nil {
			t.Fatal(err)
		}
		img, err := nt3d.NewSoftwareRenderer().Render(f)
		if err != nil {
			t.Fatal(err)
		}
		assertAnimatedSampleVisible(t, img, f.Background)
		if _, err = nt3d.RenderWireframe(f, 40, 20); err != nil {
			t.Fatal(err)
		}
		root, _ := d.scene.Node(d.group)
		if len(root.Children) != 1 {
			t.Fatal("old sample was retained")
		}
	}
}
func TestAnimateControls(t *testing.T) {
	d := newAnimateDemo(t)
	key := func(r rune) { d.Update(tea.KeyPressMsg{Code: r, Text: string(r)}) }
	key('[')
	if d.animatedSamples.index != len(animatedSamples)-1 {
		t.Fatal("previous model did not wrap")
	}
	key(']')
	if d.animatedSamples.index != 0 {
		t.Fatal("next model did not wrap")
	}
	key('a')
	if d.animate {
		t.Fatal("pause ignored")
	}
	before := d.animation.Time
	d.lastPhase = time.Now()
	d.Update(phaseMsg(d.lastPhase.Add(time.Second)))
	if d.animation.Time != before {
		t.Fatal("paused sample advanced")
	}
	key('a')
	if !d.animate {
		t.Fatal("resume ignored")
	}
	key('.')
	key(',')
	if d.animatedSamples.clip != 0 {
		t.Fatal("single clip should remain selected")
	}
	for i, r := range []rune{'1', '2', '3'} {
		key(r)
		if d.view.RenderMode() != nt3d.RenderMode(i) {
			t.Fatal("renderer key ignored")
		}
	}
	if !strings.Contains(d.animateView("frame"), "CC0") {
		t.Fatal("missing source attribution")
	}
}
func TestAnimationClipSwitchResetsPose(t *testing.T) {
	d := newAnimateDemo(t)
	files := fstest.MapFS{}
	err := fs.WalkDir(gltfsamples.Files, ".", func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			data, e := fs.ReadFile(gltfsamples.Files, p)
			if e != nil {
				return e
			}
			files[p] = &fstest.MapFile{Data: data}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	name := animatedSamples[2].file
	var doc map[string]any
	if err = json.Unmarshal(files[name].Data, &doc); err != nil {
		t.Fatal(err)
	}
	animations := doc["animations"].([]any)
	original := animations[0].(map[string]any)
	second := map[string]any{}
	for k, v := range original {
		second[k] = v
	}
	second["name"] = "Second clip"
	doc["animations"] = append(animations, second)
	files[name].Data, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	SampleFiles = files
	if err = d.loadAnimatedSample(2, 0); err != nil {
		t.Fatal(err)
	}
	if err = d.animation.Sample(.3); err != nil {
		t.Fatal(err)
	}
	oldRoot := d.animatedSamples.group
	d.Update(tea.KeyPressMsg{Code: '.', Text: "."})
	if d.animatedSamples.clip != 1 || d.animation.Time != 0 || d.animation.Clip.Name != "Second clip" {
		t.Fatal("next clip did not start at its initial pose")
	}
	if _, exists := d.scene.Node(oldRoot); exists {
		t.Fatal("clip change retained the previous instance")
	}
	d.Update(tea.KeyPressMsg{Code: '.', Text: "."})
	if d.animatedSamples.clip != 0 {
		t.Fatal("clip did not wrap")
	}
	d.Update(tea.KeyPressMsg{Code: ',', Text: ","})
	if d.animatedSamples.clip != 1 {
		t.Fatal("previous clip did not wrap")
	}
}
func TestEmbeddedAnimationAssetsAreSmall(t *testing.T) {
	var total int64
	err := fs.WalkDir(gltfsamples.Files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total >= 100*1024 {
		t.Fatalf("sample files total %d bytes", total)
	}
	for _, name := range []string{"Assembly", "AnimatedMorphCube", "SimpleSkin", "AnimatedTriangle"} {
		for _, file := range []string{"README.md", "LICENSE.md"} {
			if _, err := fs.ReadFile(gltfsamples.Files, name+"/"+file); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("embedded samples including provenance: %d bytes", total)
}
func TestGeneratedAnimationStillLoads(t *testing.T) {
	old := SampleFiles
	SampleFiles = models.Ribbon
	defer func() { SampleFiles = old }()
	d := &demo{scene: nt3d.NewScene()}
	var err error
	d.group, err = d.scene.Add(nt3d.Root, "island")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.setupAnimation(); err != nil {
		t.Fatal(err)
	}
}

func assertAnimatedSampleVisible(t *testing.T, img image.Image, background color.RGBA) {
	t.Helper()
	count := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			if color.RGBAModel.Convert(img.At(x, y)).(color.RGBA) != background {
				count++
			}
		}
	}
	if count < 25 {
		t.Fatalf("animated sample has only %d visible pixels", count)
	}
}

func TestAssemblyCameraControls(t *testing.T) {
	d := newAnimateDemo(t)
	if len(d.animatedSamples.cameras) != 2 {
		t.Fatal("assembly must expose both authored cameras")
	}
	orbit := d.view.Camera()
	for _, projection := range []nt3d.Projection{nt3d.Perspective, nt3d.Orthographic} {
		d.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
		if d.view.Camera().Projection != projection {
			t.Fatal("wrong authored projection")
		}
		f, err := d.scene.Frame(d.view.Camera(), 160, 120)
		if err != nil {
			t.Fatal(err)
		}
		img, err := nt3d.NewSoftwareRenderer().Render(f)
		if err != nil {
			t.Fatal(err)
		}
		assertAnimatedSampleVisible(t, img, f.Background)
		if _, err = nt3d.RenderWireframe(f, 40, 20); err != nil {
			t.Fatal(err)
		}
	}
	d.restartAnimation()
	if d.animatedSamples.camera != 2 {
		t.Fatal("restart lost camera selection")
	}
	d.cycleSampleCamera()
	if d.view.Camera() != orbit {
		t.Fatal("orbit camera was not restored")
	}
	d.cycleSampleCamera()
	d.Update(tea.KeyPressMsg{Code: '+', Text: "+"})
	if d.animatedSamples.camera != 0 || d.view.Camera() != orbit {
		t.Fatal("navigation did not restore orbit camera")
	}
	d.cycleSampleCamera()
	d.stepAnimatedSample(1)
	if d.animatedSamples.camera != 0 || d.view.Camera().Pose != nt3d.DefaultCamera().Pose {
		t.Fatal("changing model retained authored camera")
	}
}
