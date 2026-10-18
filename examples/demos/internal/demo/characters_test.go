// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"io/fs"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo/gobkit"
	nt3dwidget "nimbleterminal.dev/nt3d/widget"
)

func newCharactersDemo(t *testing.T) *demo {
	t.Helper()
	old := SampleFiles
	SampleFiles = gobkit.Files
	t.Cleanup(func() { SampleFiles = old })
	d := &demo{scene: nt3d.NewScene(), animate: true}
	var err error
	d.group, err = d.scene.Add(nt3d.Root, "characters")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.setupCharacters(); err != nil {
		t.Fatal(err)
	}
	d.view = nt3dwidget.New(d.scene, 80, 24, nt3dwidget.WithRenderMode(nt3d.Software))
	t.Cleanup(func() { d.view.Close() })
	d.frameAnimatedSample()
	return d
}

func TestGobkitClips(t *testing.T) {
	d := newCharactersDemo(t)
	for i, sample := range characterSamples {
		if err := d.loadAnimatedSample(i, 0); err != nil {
			t.Fatal(err)
		}
		names := []string{"idle", "attack", "dead"}
		if i > 0 {
			names = append(names, "walk")
		}
		if len(d.animatedSamples.clips) != len(names) {
			t.Fatalf("%s clips: %v", sample.title, d.animatedSamples.clips)
		}
		for j, name := range names {
			t.Run(sample.title+"/"+name, func(t *testing.T) {
				if err := d.loadAnimatedSample(i, j); err != nil {
					t.Fatal(err)
				}
				if d.animation.Clip.Name != name {
					t.Fatalf("got clip %q", d.animation.Clip.Name)
				}
				if warnings := d.animatedSamples.model.Warnings; len(warnings) > 0 {
					t.Fatal(warnings)
				}
				d.frameAnimatedSample()
				before := d.scene.Revision()
				for _, fraction := range []float64{.37, .75} {
					if err := d.animation.Sample(d.animation.Clip.Duration() * fraction); err != nil {
						t.Fatal(err)
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
					if _, err := nt3d.RenderWireframe(f, 40, 20); err != nil {
						t.Fatal(err)
					}
				}
				if d.scene.Revision() == before {
					t.Fatal("clip did not animate")
				}
			})
		}
	}
}

func TestGobkitControls(t *testing.T) {
	d := newCharactersDemo(t)
	key := func(r rune) { d.Update(tea.KeyPressMsg{Code: r, Text: string(r)}) }
	key('[')
	if d.animatedSamples.index != 2 {
		t.Fatal("previous character did not wrap")
	}
	key(',')
	if d.animation.Clip.Name != "walk" {
		t.Fatal("previous clip did not wrap")
	}
	key('.')
	if d.animation.Clip.Name != "idle" {
		t.Fatal("next clip did not wrap")
	}
	key('a')
	if err := d.animation.Sample(.5); err != nil {
		t.Fatal(err)
	}
	d.animationTime = .5
	d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if d.animation.Time != 0 || d.animationTime != 0 || d.animate {
		t.Fatal("restart did not reset paused playback")
	}
	key(']')
	if d.animatedSamples.index != 0 {
		t.Fatal("next character did not wrap")
	}
	status := d.animateView("")
	for _, want := range []string{"Minion A-1", "idle", "Gobkit CC0", "paused"} {
		if !strings.Contains(status, want) {
			t.Fatal(status)
		}
	}
	root, _ := d.scene.Node(d.group)
	if len(root.Children) != 1 {
		t.Fatal("old character retained")
	}
}

func TestGobkitAssetSize(t *testing.T) {
	var total int64
	err := fs.WalkDir(gobkit.Files, ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, e := d.Info()
			if e != nil {
				return e
			}
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if total >= 500*1024 {
		t.Fatalf("embedded characters too large: %d", total)
	}
	for _, name := range []string{"LICENSE.md", "SOURCES.md"} {
		if _, err := fs.ReadFile(gobkit.Files, name); err != nil {
			t.Fatal(err)
		}
	}
}
