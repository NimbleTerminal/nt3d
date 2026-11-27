// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	stdflag "flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	gltfsamples "nimbleterminal.dev/nt3d/examples/demos/internal/demo/gltf"
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo/gobkit"
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo/models"
	_ "nimbleterminal.dev/nt3d/mesh/formats"
	_ "nimbleterminal.dev/nt3d/mesh/gltf"
)

var updateReference = stdflag.Bool("update-reference", false, "rewrite testdata/reference from what the demos draw now")

// Reference pictures are small: they are tracked files, and a regression
// worth catching shows at this size.
const referenceWidth, referenceHeight = 240, 160

// A pixel counts as changed when a channel moves by more than
// referenceLevels, and a picture as changed when more than referenceShare of
// its pixels do. The software renderer is deterministic on one machine, but
// floating point differs a little between architectures, which moves a few
// edge pixels a lot and many pixels by a level; a real change (a light, a
// material, an object gone, the order things are drawn) moves far more.
const (
	referenceLevels = 8
	referenceShare  = .005
)

// referenceDemos is every demo, as its command starts it.
var referenceDemos = []struct {
	name       string
	procedural bool
	kind       sceneMode
	files      fs.FS
}{
	{"terrain", false, sceneTerrain, nil},
	{"procedural", true, sceneTerrain, nil},
	{"dynamic", false, sceneDynamic, nil},
	{"shapes", false, sceneMeshes, nil},
	{"models", false, sceneModels, models.Files},
	{"large", false, sceneLarge, nil},
	{"fur", false, sceneFur, models.Spot},
	{"mipmaps", true, sceneMipmaps, nil},
	{"forest", true, sceneForest, nil},
	{"lights", true, sceneLights, nil},
	{"room", false, sceneRoom, nil},
	{"particles", true, sceneParticles, nil},
	{"gaussians", false, sceneGaussians, nil},
	{"animation", false, sceneAnimation, models.Ribbon},
	{"animate", false, sceneAnimate, gltfsamples.Files},
	{"characters", false, sceneCharacters, gobkit.Files},
	{"cloud", false, sceneCloud, nil},
	{"aabb", false, sceneAABB, nil},
	{"water", false, sceneWater, nil},
	{"toon", false, sceneToon, models.Toon},
	{"campath", false, sceneCamPath, nil},
	{"tiles", false, sceneTiles, nil},
}

// demoSnapshot is the picture a demo's command writes for --snapshot in the
// given render mode: the same code path, flags and all, at the reference
// size.
func demoSnapshot(t *testing.T, mode string, procedural bool, kind sceneMode, files fs.FS) image.Image {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.png")
	args, commandLine, sample, size := os.Args, pflag.CommandLine, SampleFiles, snapshotSize
	stdout := os.Stdout
	defer func() {
		os.Args, pflag.CommandLine, SampleFiles, snapshotSize, os.Stdout = args, commandLine, sample, size, stdout
	}()
	os.Args = []string{"demo", "--render-mode", mode, "--snapshot", path}
	pflag.CommandLine = pflag.NewFlagSet("demo", pflag.ContinueOnError)
	SampleFiles, snapshotSize = files, [2]int{referenceWidth, referenceHeight}
	if null, err := os.Open(os.DevNull); err == nil {
		defer null.Close()
		os.Stdout, _ = os.OpenFile(os.DevNull, os.O_WRONLY, 0) // The command reports what it wrote.
	}
	if err := run(procedural, kind); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// changed is how many of two pictures' pixels differ by more than
// referenceLevels in some channel, and a picture of where.
func changed(a, b image.Image) (int, *image.RGBA) {
	r := a.Bounds()
	where := image.NewRGBA(r)
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := color.RGBAModel.Convert(a.At(x, y)).(color.RGBA)
			q := color.RGBAModel.Convert(b.At(x, y)).(color.RGBA)
			worst := 0
			for _, c := range [3][2]uint8{{p.R, q.R}, {p.G, q.G}, {p.B, q.B}} {
				worst = max(worst, max(int(c[0])-int(c[1]), int(c[1])-int(c[0])))
			}
			where.SetRGBA(x, y, color.RGBA{p.R / 4, p.G / 4, p.B / 4, 255})
			if worst > referenceLevels {
				n++
				where.SetRGBA(x, y, color.RGBA{255, 0, 255, 255})
			}
		}
	}
	return n, where
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// matchReference holds every demo, drawn in mode, to the pictures in dir.
func matchReference(t *testing.T, mode, dir string) {
	for _, d := range referenceDemos {
		t.Run(d.name, func(t *testing.T) {
			got := demoSnapshot(t, mode, d.procedural, d.kind, d.files)
			path := filepath.Join("testdata", dir, d.name+".png")
			if *updateReference {
				writePNG(t, path, got)
				return
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatalf("no reference picture (record one with -update-reference): %v", err)
			}
			defer f.Close()
			want, err := png.Decode(f)
			if err != nil {
				t.Fatal(err)
			}
			if got.Bounds() != want.Bounds() {
				t.Fatalf("the picture is %v, its reference %v", got.Bounds(), want.Bounds())
			}
			n, where := changed(want, got)
			total := got.Bounds().Dx() * got.Bounds().Dy()
			if float64(n) <= referenceShare*float64(total) {
				return
			}
			out := filepath.Join(os.TempDir(), "nt3d-"+dir)
			writePNG(t, filepath.Join(out, d.name+".png"), got)
			writePNG(t, filepath.Join(out, d.name+".changed.png"), where)
			t.Errorf("%s: %d of %d pixels changed (%.1f%%, over the %.1f%% allowed); see %s", d.name, n, total,
				100*float64(n)/float64(total), 100*referenceShare, fmt.Sprintf("%s/%s.png and .changed.png", out, d.name))
		})
	}
}

// Every demo draws, in software, the picture it drew when its reference was
// recorded. The other rendering tests ask about a few pixels of small scenes,
// or compare one renderer with the other; this is the one that notices a
// whole picture change, or both renderers change together, since the demos
// between them use most of what the engine draws.
//
// When a picture changes on purpose, look at it, then record it again:
//
//	go -C examples/demos test ./internal/demo -run TestDemosMatchReference -update-reference
//
// A failure writes what was drawn, and a map of where it differs, beside
// the path it names.
func TestDemosMatchReference(t *testing.T) {
	if testing.Short() {
		t.Skip("draws every demo")
	}
	matchReference(t, "software", "reference")
}

// A demo draws the same picture twice. A reference is only worth keeping if
// it is: a demo whose picture depends on the order a map is ranged over, on
// the clock, or on a seed taken from either, would fail at random.
func TestDemosDrawTheSamePictureTwice(t *testing.T) {
	if testing.Short() {
		t.Skip("draws every demo twice")
	}
	for _, d := range referenceDemos {
		t.Run(d.name, func(t *testing.T) {
			a := demoSnapshot(t, "software", d.procedural, d.kind, d.files)
			b := demoSnapshot(t, "software", d.procedural, d.kind, d.files)
			if n, _ := changed(a, b); n > 0 {
				t.Errorf("%d pixels differ between two snapshots of the same demo", n)
			}
		})
	}
}
