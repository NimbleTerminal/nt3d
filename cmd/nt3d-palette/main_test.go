// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"bytes"
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/sh"
)

// ringPLY is 576 degree-1 splats with 56 distinct band sets.
var ringPLY = filepath.Join("..", "..", "mesh", "sog", "testdata", "gaussian-ring.sog.ref.ply")

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// entries lists the names in dir.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

// runArgs runs the command and returns its exit code and outputs.
func runArgs(t *testing.T, ctx context.Context, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(ctx, args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// load decodes a written bundle with the default options.
func load(t *testing.T, name string) nt3d.Geometry {
	t.Helper()
	m, err := nt3dmesh.ReadFile(name, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != "sog" || len(m.Objects) != 1 {
		t.Fatalf("format %v, %d objects", m.Format, len(m.Objects))
	}
	return m.Objects[0].Geometry
}

// palettedRing is the fixture decoded and paletted with centroids, as the
// command does it.
func palettedRing(t *testing.T, centroids int) nt3d.Geometry {
	t.Helper()
	m, err := nt3dmesh.DecodePLY(readFile(t, ringPLY), nil)
	if err != nil {
		t.Fatal(err)
	}
	g := m.Objects[0].Geometry
	if _, err := nt3dmesh.BuildSHPalette(context.Background(), &g, nt3dmesh.PaletteOptions{Centroids: centroids}); err != nil {
		t.Fatal(err)
	}
	return g
}

// step is half a codebook step over values, the format's quantization.
func step(coefficients []nt3dsh.RGB) float64 {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, c := range coefficients {
		for _, v := range c {
			lo, hi = min(lo, float64(v)), max(hi, float64(v))
		}
	}
	return (hi-lo)/255/2 + 1e-6
}

func TestRunBuildsAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "ring.ply")
	copyFile(t, ringPLY, in)
	out := filepath.Join(dir, "out")
	code, stdout, stderr := runArgs(t, context.Background(), "-centroids", "32", "-out", out+"/", in)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	written := filepath.Join(out, "ring.sog")
	if !strings.HasPrefix(stdout, written+": 32 centroids, RMS ") || !strings.Contains(stdout, ", SH 20.7 kB -> 2.3 kB, ") || !strings.HasSuffix(stdout, "s\n") || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("report %q", stdout)
	}
	got := load(t, written)
	want := palettedRing(t, 32)
	if got.GaussianSHPalette == nil || got.GaussianSHPalette.Degree != 1 || got.GaussianSHPalette.Count() != 32 || len(got.GaussianSHLabels) != 576 || len(got.GaussianSH) != 576 {
		t.Fatalf("palette %+v, %d labels, %d SH", got.GaussianSHPalette, len(got.GaussianSHLabels), len(got.GaussianSH))
	}
	for i := range got.GaussianSHLabels {
		if got.GaussianSHLabels[i] != want.GaussianSHLabels[i] {
			t.Fatalf("label %d is %d, want %d", i, got.GaussianSHLabels[i], want.GaussianSHLabels[i])
		}
	}
	tolerance := max(step(want.GaussianSHPalette.Coefficients), step(want.GaussianSH))
	nt3d.ExpandSHPalette(&got)
	nt3d.ExpandSHPalette(&want)
	for i := range want.GaussianSH {
		for c := range 3 {
			if math.Abs(float64(got.GaussianSH[i][c]-want.GaussianSH[i][c])) > tolerance {
				t.Fatalf("coefficient %d channel %d is %g, want %g within %g", i, c, got.GaussianSH[i][c], want.GaussianSH[i][c], tolerance)
			}
		}
	}
	if !bytes.Equal(readFile(t, in), readFile(t, ringPLY)) {
		t.Fatal("the input changed")
	}
	// The temporary file it was written through must not leave it private.
	if info, err := os.Stat(written); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("output mode %v, %v", info.Mode(), err)
	}
}

func TestOutputPath(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		c      config
		input  string
		inputs int
		want   string
		fails  bool
	}{
		{config{}, "/a/b.ply", 1, "/a/b.paletted.sog", false},
		{config{inPlace: true}, "/a/b.ply", 1, "/a/b.sog", false},
		{config{out: dir}, "/a/b.ply", 2, filepath.Join(dir, "b.sog"), false},
		{config{out: filepath.Join(dir, "d2") + "/"}, "/a/b.ply", 1, filepath.Join(dir, "d2", "b.sog"), false},
		{config{out: "x.sog"}, "/a/b.ply", 1, "x.sog", false},
		{config{out: "x.sog"}, "/a/b.ply", 2, "", true},
	}
	for _, c := range cases {
		got, err := outputPath(c.c, c.input, c.inputs)
		if (err != nil) != c.fails || got != c.want {
			t.Errorf("%+v %s ×%d: %q, %v; want %q", c.c, c.input, c.inputs, got, err, c.want)
		}
	}
	if info, err := os.Stat(filepath.Join(dir, "d2")); err != nil || !info.IsDir() {
		t.Fatalf("d2 not created: %v", err)
	}
}

func TestRunUsage(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "ring.ply")
	copyFile(t, ringPLY, in)
	for _, args := range [][]string{
		{},
		{"-out", filepath.Join(dir, "f.sog"), in, in},
		{"-i", "-out", dir, in},
		{"-centroids", "x", in},
	} {
		if code, _, stderr := runArgs(t, context.Background(), args...); code != 2 || stderr == "" {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}
}

func TestRunContinuesAndFailFast(t *testing.T) {
	for _, failFast := range []bool{false, true} {
		dir := t.TempDir()
		in := filepath.Join(dir, "ring.ply")
		copyFile(t, ringPLY, in)
		out := filepath.Join(dir, "out")
		args := []string{"-centroids", "32", "-out", out + "/"}
		if failFast {
			args = append(args, "-fail-fast")
		}
		code, _, stderr := runArgs(t, context.Background(), append(args, filepath.Join(dir, "missing.ply"), in)...)
		if code != 1 || !strings.Contains(stderr, "missing.ply") {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		_, err := os.Stat(filepath.Join(out, "ring.sog"))
		if failFast && err == nil {
			t.Fatal("fail-fast still wrote the second input")
		}
		if !failFast && err != nil {
			t.Fatalf("second input not written: %v", err)
		}
	}
}

func TestRunCanceledLeavesNoOutput(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "ring.ply")
	copyFile(t, ringPLY, in)
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, _, stderr := runArgs(t, ctx, "-centroids", "32", "-out", out, in)
	if code != 1 || !strings.Contains(stderr, "context canceled") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if names := entries(t, out); len(names) != 0 {
		t.Fatalf("output directory holds %v", names)
	}
}

func TestRunOverBudget(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "ring.ply")
	copyFile(t, ringPLY, in)
	out := filepath.Join(dir, "out")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runArgs(t, context.Background(), "-centroids", "2", "-max-rms", "1e-9", "-out", out, in)
	if code != 1 || !strings.Contains(stdout, "2 centroids") || !strings.Contains(stdout, ", over budget,") {
		t.Fatalf("exit %d, report %q", code, stdout)
	}
	if names := entries(t, out); len(names) != 0 {
		t.Fatalf("output directory holds %v", names)
	}
	code, stdout, _ = runArgs(t, context.Background(), "-centroids", "2", "-max-rms", "10", "-out", out, in)
	if code != 0 || !strings.Contains(stdout, ", within budget,") {
		t.Fatalf("exit %d, report %q", code, stdout)
	}
}

func TestMerge(t *testing.T) {
	g := palettedRing(t, 32)
	split := func(from, to int) nt3dmesh.Object {
		piece := g
		piece.Points = g.Points[from:to]
		piece.GaussianSH = g.GaussianSH[from:to]
		piece.GaussianSHLabels = g.GaussianSHLabels[from:to]
		return nt3dmesh.Object{Geometry: piece}
	}
	got, err := merge([]nt3dmesh.Object{split(0, 300), split(300, 576)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Points) != 576 || len(got.GaussianSH) != 576 || len(got.GaussianSHLabels) != 576 || got.GaussianSHPalette != g.GaussianSHPalette {
		t.Fatalf("%d points, %d SH, %d labels, palette shared %v", len(got.Points), len(got.GaussianSH), len(got.GaussianSHLabels), got.GaussianSHPalette == g.GaussianSHPalette)
	}
	for i := range g.GaussianSHLabels {
		if got.GaussianSHLabels[i] != g.GaussianSHLabels[i] || got.Points[i] != g.Points[i] {
			t.Fatalf("splat %d differs", i)
		}
	}
	plain := split(0, 300)
	plain.Geometry.PointStyle.Shape = 0
	if _, err := merge([]nt3dmesh.Object{plain}); err == nil {
		t.Fatal("merged a non-Gaussian object")
	}
	other := split(300, 576)
	other.Geometry.GaussianSHPalette = &nt3d.SHPalette{Degree: 1, Coefficients: append([]nt3dsh.RGB{}, g.GaussianSHPalette.Coefficients...)}
	other.Geometry.GaussianSHPalette.Coefficients[0][0] += 1
	if _, err := merge([]nt3dmesh.Object{split(0, 300), other}); err == nil {
		t.Fatal("merged pieces with different palettes")
	}
}

func TestSize(t *testing.T) {
	for b, want := range map[int]string{999: "999 B", 20736: "20.7 kB", 2_900_000: "2.9 MB"} {
		if got := size(b); got != want {
			t.Errorf("size(%d) = %q, want %q", b, got, want)
		}
	}
}

// ringSOG is the ring as a SOG v2 bundle holding a palette.
var ringSOG = filepath.Join("..", "..", "mesh", "sog", "testdata", "gaussian-ring.sog")

// writePLY0 writes an ASCII 3DGS PLY of three splats with no bands above DC.
func writePLY0(t *testing.T, path string) {
	t.Helper()
	header := "ply\nformat ascii 1.0\nelement vertex 3\n"
	for _, p := range []string{"x", "y", "z", "f_dc_0", "f_dc_1", "f_dc_2", "opacity", "scale_0", "scale_1", "scale_2", "rot_0", "rot_1", "rot_2", "rot_3"} {
		header += "property float " + p + "\n"
	}
	body := "0 0 0 0.5 0.2 -0.1 2 -1 -1 -1 1 0 0 0\n1 0 0 0.1 0.3 0.2 1 -2 -1 -1 0.7 0.7 0 0\n0 1 0 -0.2 0 0.4 0 -1 -2 -1 0 0 0 1\n"
	if err := os.WriteFile(path, []byte(header+"end_header\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunKeepsPalettedSOG(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "ring.sog")
	copyFile(t, ringSOG, in)
	out := filepath.Join(dir, "out")
	code, stdout, stderr := runArgs(t, context.Background(), "-out", out+"/", in)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	written := filepath.Join(out, "ring.sog")
	if !strings.HasPrefix(stdout, written+": kept ") || !strings.Contains(stdout, " centroids, SH ") {
		t.Fatalf("report %q", stdout)
	}
	if !bytes.Equal(readFile(t, written), readFile(t, in)) {
		t.Fatal("the kept bundle differs from the input")
	}
	out2 := filepath.Join(dir, "out2")
	code, stdout, stderr = runArgs(t, context.Background(), "-rebuild", "-centroids", "32", "-out", out2+"/", in)
	if code != 0 || stderr != "" {
		t.Fatalf("rebuild: exit %d, stderr %q", code, stderr)
	}
	rebuilt := filepath.Join(out2, "ring.sog")
	if bytes.Equal(readFile(t, rebuilt), readFile(t, in)) {
		t.Fatal("rebuild copied the input")
	}
	if !strings.Contains(stdout, "32 centroids, RMS") || load(t, rebuilt).GaussianSHPalette.Count() != 32 {
		t.Fatalf("rebuild report %q", stdout)
	}
}

func TestRunInPlaceSOGUnchanged(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "ring.sog")
	copyFile(t, ringSOG, in)
	before, err := os.Stat(in)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runArgs(t, context.Background(), "-i", in)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "kept") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	after, err := os.Stat(in)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) || !bytes.Equal(readFile(t, in), readFile(t, ringSOG)) {
		t.Fatal("the input changed")
	}
	if names := entries(t, dir); len(names) != 1 {
		t.Fatalf("directory holds %v", names)
	}
}

// On a case-insensitive filesystem (macOS's default), an input named in a
// different case from the output it resolves to is the same file: -i must
// neither remove it nor treat a copy-through as a write onto itself.
func TestRunInPlaceCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "ring.SOG")
	copyFile(t, ringSOG, in)
	if _, err := os.Stat(filepath.Join(dir, "ring.sog")); err != nil {
		t.Skip("case-sensitive filesystem")
	}
	code, stdout, stderr := runArgs(t, context.Background(), "-i", in)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "kept") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if names := entries(t, dir); len(names) != 1 || !bytes.Equal(readFile(t, in), readFile(t, ringSOG)) {
		t.Fatalf("directory holds %v; input intact %v", names, bytes.Equal(readFile(t, in), readFile(t, ringSOG)))
	}
	code, _, stderr = runArgs(t, context.Background(), "-i", "-rebuild", "-centroids", "32", in)
	if code != 0 || stderr != "" {
		t.Fatalf("rebuild: exit %d, stderr %q", code, stderr)
	}
	if names := entries(t, dir); len(names) != 1 {
		t.Fatalf("directory holds %v", names)
	}
	if g := load(t, in); g.GaussianSHPalette == nil || g.GaussianSHPalette.Count() != 32 {
		t.Fatalf("palette %+v", g.GaussianSHPalette)
	}
}

// Two inputs that would write the same output are a usage error, not a
// silent overwrite.
func TestRunDuplicateOutputs(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for _, d := range []string{a, b} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
		copyFile(t, ringPLY, filepath.Join(d, "ring.ply"))
	}
	out := filepath.Join(dir, "out")
	code, _, stderr := runArgs(t, context.Background(), "-out", out+"/", filepath.Join(a, "ring.ply"), filepath.Join(b, "ring.ply"))
	if code != 2 || !strings.Contains(stderr, "ring.sog") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if names := entries(t, out); len(names) != 0 {
		t.Fatalf("output directory holds %v", names)
	}
}

func TestRunInPlacePLY(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "ring.ply")
	copyFile(t, ringPLY, in)
	code, _, stderr := runArgs(t, context.Background(), "-i", "-centroids", "64", in)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if g := load(t, filepath.Join(dir, "ring.sog")); g.GaussianSHPalette == nil || g.GaussianSHPalette.Count() != 56 {
		t.Fatalf("palette %+v", g.GaussianSHPalette)
	}
	if _, err := os.Stat(in); !os.IsNotExist(err) {
		t.Fatalf("input still there: %v", err)
	}
}

func TestRunDegreeZero(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "flat.ply")
	writePLY0(t, in)
	out := filepath.Join(dir, "out")
	code, stdout, stderr := runArgs(t, context.Background(), "-out", out+"/", in)
	if code != 0 || !strings.Contains(stderr, "no SH bands above DC") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	written := filepath.Join(out, "flat.sog")
	if !strings.HasPrefix(stdout, written+": no SH bands above DC, ") || !strings.HasSuffix(stdout, "s\n") {
		t.Fatalf("report %q", stdout)
	}
	if g := load(t, written); g.GaussianSHPalette != nil || len(g.Points) != 3 || len(g.GaussianSH) != 3 {
		t.Fatalf("palette %v, %d points, %d SH", g.GaussianSHPalette, len(g.Points), len(g.GaussianSH))
	}
	code, stdout, stderr = runArgs(t, context.Background(), "-quiet", "-out", out+"/", in)
	if code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("quiet: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}
