// Copyright (c) 2026 Neomantra Corp.

// Command nt3d-palette reads Gaussian splat files (PLY, compressed PLY, SPZ,
// SOG), builds a spherical-harmonic palette with nt3dmesh.BuildSHPalette or
// keeps the one a SOG holds, and writes a SOG v2 bundle that nt3dmesh loads
// with the default SHPaletteKeep, so later loads skip the k-means.
//
//	nt3d-palette [flags] input.ply|.spz|.sog ...
//
// Each input gets one report line on stdout; failures go to stderr and the
// exit status is 1 (2 for a usage error). Inputs are processed in order, and
// a failure moves on to the next unless -fail-fast. The output is written
// through a temporary file in its directory and renamed into place, so a
// failed or interrupted run leaves nothing behind. Inputs are never changed
// unless -i replaces them.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/mesh/sog"
	_ "nimbleterminal.dev/nt3d/mesh/spz" // SPZ inputs.
	"nimbleterminal.dev/nt3d/sh"
)

const usage = "usage: nt3d-palette [flags] input.ply|.spz|.sog ..."

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// config is the command line.
type config struct {
	centroids, iterations, workers              int
	maxRMS                                      float64
	out                                         string
	inPlace, rebuild, progress, quiet, failFast bool
}

// run parses args and converts each input in order. It returns 0 when every
// input succeeded, 1 when any failed, and 2 for a usage error.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("nt3d-palette", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, usage)
		fs.PrintDefaults()
	}
	var c config
	fs.IntVar(&c.centroids, "centroids", nt3d.MaxSHPaletteCentroids, "palette centroids, at most 65536")
	fs.IntVar(&c.iterations, "iterations", 10, "k-means rounds at each level")
	fs.Float64Var(&c.maxRMS, "max-rms", 0, "fail, writing nothing, when the palette's RMS error exceeds this; 0 ignores it")
	fs.IntVar(&c.workers, "workers", 0, "goroutines for the build; 0 is GOMAXPROCS")
	fs.StringVar(&c.out, "out", "", "output file (one input) or directory; default <stem>.paletted.sog beside the input")
	fs.BoolVar(&c.inPlace, "i", false, "write <stem>.sog beside the input and remove the input")
	fs.BoolVar(&c.rebuild, "rebuild", false, "build a new palette even when the input holds one")
	fs.BoolVar(&c.progress, "progress", false, "report build progress on stderr")
	fs.BoolVar(&c.quiet, "quiet", false, "print no reports, warnings, or progress")
	fs.BoolVar(&c.failFast, "fail-fast", false, "stop at the first input that fails")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	inputs := fs.Args()
	if len(inputs) == 0 {
		fs.Usage()
		return 2
	}
	if c.inPlace && c.out != "" {
		fmt.Fprintln(stderr, "nt3d-palette: -i and -out exclude each other")
		return 2
	}
	outputs := make([]string, len(inputs))
	for i, input := range inputs {
		output, err := outputPath(c, input, len(inputs))
		if err != nil {
			fmt.Fprintf(stderr, "nt3d-palette: %v\n", err)
			return 2
		}
		outputs[i] = output
	}
	reports, warnings := stdout, stderr
	if c.quiet {
		reports, warnings = io.Discard, io.Discard
	}
	code := 0
	for i, input := range inputs {
		if err := convert(ctx, c, input, outputs[i], reports, warnings); err != nil {
			fmt.Fprintf(stderr, "nt3d-palette: %s: %v\n", input, err)
			code = 1
			if c.failFast || ctx.Err() != nil {
				break
			}
		}
	}
	return code
}

// outputPath is where input's bundle goes: c.out when it names a file (one
// input only); <out>/<stem>.sog when c.out is a directory (one that exists,
// or one ending in a separator, which is created); <dir>/<stem>.sog beside
// the input with -i; else <dir>/<stem>.paletted.sog beside the input.
func outputPath(c config, input string, inputs int) (string, error) {
	dir, base := filepath.Split(input)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if c.out == "" {
		if c.inPlace {
			return filepath.Join(dir, stem+".sog"), nil
		}
		return filepath.Join(dir, stem+".paletted.sog"), nil
	}
	isDir := false
	if info, err := os.Stat(c.out); err == nil {
		isDir = info.IsDir()
	} else if strings.HasSuffix(c.out, "/") || strings.HasSuffix(c.out, string(filepath.Separator)) {
		if err := os.MkdirAll(c.out, 0o755); err != nil {
			return "", err
		}
		isDir = true
	}
	if isDir {
		return filepath.Join(c.out, stem+".sog"), nil
	}
	if inputs > 1 {
		return "", fmt.Errorf("-out %s names a file, but there are %d inputs", c.out, inputs)
	}
	return c.out, nil
}

// convert reads input, gives its cloud a palette, writes the bundle to
// output, and prints one report line to reports.
func convert(ctx context.Context, c config, input, output string, reports, warnings io.Writer) error {
	start := time.Now()
	data, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	format, ok := nt3dmesh.FormatOf(input, data)
	if !ok {
		return errors.New("unknown splat format")
	}
	// The tool is offline and the files are the user's own, so only memory
	// bounds a decode: a terabyte, not math.MaxInt64, which the budget's
	// arithmetic would overflow.
	o := &nt3dmesh.Options{MaxBytes: 1 << 40, NoClusters: true, Open: nt3dmesh.DirOpener(filepath.Dir(input)), SHPalette: nt3dmesh.SHPaletteKeep}
	m, err := nt3dmesh.Decode(data, format, o)
	if err != nil {
		return err
	}
	g, err := merge(m.Objects)
	if err != nil {
		return err
	}
	if c.rebuild && g.GaussianSHPalette != nil {
		nt3d.ExpandSHPalette(&g)
	}
	n := len(g.Points)
	count := len(g.GaussianSH) / n // RGB entries a splat; 1 with a palette
	elapsed := func() float64 { return time.Since(start).Seconds() }
	// A SOG bundle that needs no new palette is copied as it is: a decode and
	// re-encode would only lose precision. An unbundled meta.json is
	// re-encoded, since a copy would leave its images behind.
	bundle := format == nt3dsog.SOG && bytes.HasPrefix(data, []byte("PK\x03\x04"))
	copyThrough := false
	var line string
	switch {
	case g.GaussianSHPalette != nil:
		p := g.GaussianSHPalette
		before, after := n*(nt3dsh.Count(p.Degree)-1)*12, p.Count()*p.Stride()*12+2*n
		line = fmt.Sprintf("%s: kept %d centroids, SH %s -> %s, ", output, p.Count(), size(before), size(after))
		copyThrough = bundle
	case count > 1:
		opts := nt3dmesh.PaletteOptions{Centroids: c.centroids, Iterations: c.iterations, Workers: c.workers, MaxRMSError: float32(c.maxRMS)}
		if c.progress {
			last := -1
			opts.Progress = func(f float64) {
				if p := int(f * 100); p != last {
					last = p
					fmt.Fprintf(warnings, "\r%s: building palette %3d%%", input, p)
				}
			}
		}
		r, err := nt3dmesh.BuildSHPalette(ctx, &g, opts)
		if c.progress {
			fmt.Fprintln(warnings)
		}
		if err != nil {
			return err
		}
		budget := ""
		if c.maxRMS > 0 {
			budget = ", within budget"
			if !r.MetBudget {
				budget = ", over budget"
			}
		}
		before, after := n*(count-1)*12, g.GaussianSHPalette.Count()*g.GaussianSHPalette.Stride()*12+2*n
		line = fmt.Sprintf("%s: %d centroids, RMS %.4g%s, SH %s -> %s, ", output, r.Centroids, r.RMSError, budget, size(before), size(after))
		if !r.MetBudget {
			fmt.Fprintf(reports, "%s%.1fs\n", line, elapsed())
			return fmt.Errorf("RMS %.4g over budget %.4g; nothing written", r.RMSError, c.maxRMS)
		}
	default:
		fmt.Fprintf(warnings, "warning: %s: no SH bands above DC; written without a palette\n", input)
		line = fmt.Sprintf("%s: no SH bands above DC, ", output)
		copyThrough = bundle
	}
	same, err := samePath(input, output)
	if err != nil {
		return err
	}
	write := func(w io.Writer) error { return nt3dsog.Encode(w, []nt3dmesh.Object{{Geometry: g}}) }
	if copyThrough {
		write = func(w io.Writer) error { _, err := w.Write(data); return err }
	}
	if !(copyThrough && same) {
		if err := writeAtomic(output, write); err != nil {
			return err
		}
	}
	if c.inPlace && !same {
		if err := os.Remove(input); err != nil {
			return fmt.Errorf("wrote %s but could not remove the input: %w", output, err)
		}
	}
	fmt.Fprintf(reports, "%s%.1fs\n", line, elapsed())
	return nil
}

// samePath reports whether a and b name the same file.
func samePath(a, b string) (bool, error) {
	a, err := filepath.Abs(a)
	if err != nil {
		return false, err
	}
	b, err = filepath.Abs(b)
	if err != nil {
		return false, err
	}
	return a == b, nil
}

// merge concatenates the pieces of one Gaussian cloud into one geometry:
// points, DC, and labels in order, the palette shared. Clusters are
// dropped. Pieces must all be Gaussian splats, hold the same coefficients a
// splat, and share one palette or none.
func merge(objects []nt3dmesh.Object) (nt3d.Geometry, error) {
	var g nt3d.Geometry
	count := -1
	for i := range objects {
		p := &objects[i].Geometry
		if p.PointStyle.Shape != nt3d.GaussianPoint {
			return nt3d.Geometry{}, fmt.Errorf("object %d is not Gaussian splats", i)
		}
		if len(p.Points) == 0 {
			continue
		}
		per := len(p.GaussianSH) / len(p.Points)
		if per*len(p.Points) != len(p.GaussianSH) || (count >= 0 && per != count) {
			return nt3d.Geometry{}, fmt.Errorf("object %d: spherical harmonics do not match the other pieces", i)
		}
		if count < 0 {
			count = per
			g.PointStyle, g.GaussianSHTransform, g.GaussianSHPalette = p.PointStyle, p.GaussianSHTransform, p.GaussianSHPalette
		} else if q := p.GaussianSHPalette; (q == nil) != (g.GaussianSHPalette == nil) || q != nil && q != g.GaussianSHPalette && (q.Degree != g.GaussianSHPalette.Degree || !slices.Equal(q.Coefficients, g.GaussianSHPalette.Coefficients)) {
			return nt3d.Geometry{}, fmt.Errorf("object %d: SH palette differs from the other pieces", i)
		}
		if g.GaussianSHPalette != nil && len(p.GaussianSHLabels) != len(p.Points) {
			return nt3d.Geometry{}, fmt.Errorf("object %d: %d SH labels for %d splats", i, len(p.GaussianSHLabels), len(p.Points))
		}
		g.Points = append(g.Points, p.Points...)
		g.GaussianSH = append(g.GaussianSH, p.GaussianSH...)
		g.GaussianSHLabels = append(g.GaussianSHLabels, p.GaussianSHLabels...)
	}
	if len(g.Points) == 0 {
		return nt3d.Geometry{}, errors.New("no Gaussian splats")
	}
	g.Bounds = nt3d.GeometryBounds(g)
	return g, nil
}

// writeAtomic writes through a temporary file beside path and renames it
// into place; on any error the temporary file is removed and path is left
// as it was.
func writeAtomic(path string, write func(w io.Writer) error) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".nt3d-palette-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(f.Name())
		}
	}()
	w := bufio.NewWriter(f)
	if err = write(w); err != nil {
		return err
	}
	if err = w.Flush(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// size formats a byte count for a report.
func size(bytes int) string {
	switch {
	case bytes >= 1e6:
		return fmt.Sprintf("%.1f MB", float64(bytes)/1e6)
	case bytes >= 1e3:
		return fmt.Sprintf("%.1f kB", float64(bytes)/1e3)
	}
	return fmt.Sprintf("%d B", bytes)
}
