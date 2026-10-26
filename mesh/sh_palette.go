// Copyright (c) 2026 Neomantra Corp.
package nt3dmesh

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/sh"
)

// DefaultCoarseCentroids is the number of first-level cells BuildSHPalette
// partitions the bands into before training each cell's centroids.
const DefaultCoarseCentroids = 256

// DefaultSubsample is the most splats BuildSHPalette trains its centroids on;
// every splat is then assigned to the trained centroids.
const DefaultSubsample = 262144

// paletteChunk is the number of splats a parallel task handles. Chunks are
// fixed whatever the worker count, so results do not depend on it.
const paletteChunk = 4096

// PaletteOptions controls BuildSHPalette.
type PaletteOptions struct {
	Centroids   int     // At most nt3d.MaxSHPaletteCentroids; 0 is the default 65536.
	Iterations  int     // K-means rounds at each level; 0 is the default 10.
	Workers     int     // Goroutines; 0 is runtime.GOMAXPROCS(0).
	MaxRMSError float32 // 0 ignores; else PaletteReport.MetBudget says whether RMSError <= it.
	Progress    func(fraction float64)
}

// DefaultPaletteOptions returns the options a zero PaletteOptions means.
func DefaultPaletteOptions() PaletteOptions {
	return PaletteOptions{Centroids: nt3d.MaxSHPaletteCentroids, Iterations: 10, Workers: runtime.GOMAXPROCS(0)}
}

// PaletteReport describes a palette BuildSHPalette made.
type PaletteReport struct {
	Centroids int     // Centroids in the palette.
	RMSError  float32 // Root-mean-square error per band coefficient channel.
	MetBudget bool    // MaxRMSError was 0 or RMSError is at most it.
}

// BuildSHPalette replaces a Gaussian geometry's per-splat bands above DC with
// a shared palette (g.GaussianSHPalette) and per-splat labels
// (g.GaussianSHLabels); g.GaussianSH keeps each splat's DC. When the geometry
// holds at most o.Centroids distinct band sets, the palette is exactly those
// sets. Otherwise it is trained by two-level k-means on a subsample of at
// most DefaultSubsample splats: DefaultCoarseCentroids coarse cells, then
// centroids within each cell; every splat then takes its nearest cell's
// nearest centroid, and each centroid becomes the mean of its splats. The
// result is deterministic for the same geometry and options, whatever
// o.Workers is. It never writes into g's existing slices; g changes only on
// success. An already paletted geometry is left as it is, and returns a
// report with RMSError 0 and MetBudget true, since the per-splat bands are no
// longer available to measure.
//
// The geometry need not fit one GPU geometry: decoders build before
// PointPieces splits a cloud, so BuildSHPalette checks only what it reads (a
// Gaussian style, splats, one degree 1 to 3 coefficient set each, all finite)
// and leaves bounds, point caps, and the rest to Validate after splitting.
func BuildSHPalette(ctx context.Context, g *nt3d.Geometry, o PaletteOptions) (PaletteReport, error) {
	if g.PointStyle.Shape != nt3d.GaussianPoint || len(g.Points) == 0 {
		return PaletteReport{}, errors.New("nt3dmesh: an SH palette needs Gaussian splats")
	}
	if o.Centroids < 0 || o.Centroids > nt3d.MaxSHPaletteCentroids || o.Iterations < 0 || o.Workers < 0 || !(o.MaxRMSError >= 0) || math.IsInf(float64(o.MaxRMSError), 0) {
		return PaletteReport{}, errors.New("nt3dmesh: invalid palette options")
	}
	d := DefaultPaletteOptions()
	if o.Centroids == 0 {
		o.Centroids = d.Centroids
	}
	if o.Iterations == 0 {
		o.Iterations = d.Iterations
	}
	if o.Workers == 0 {
		o.Workers = d.Workers
	}
	report := func(centroids int, rms float32) PaletteReport {
		return PaletteReport{Centroids: centroids, RMSError: rms, MetBudget: o.MaxRMSError == 0 || rms <= o.MaxRMSError}
	}
	last := -1.0
	progress := func(f float64) {
		if o.Progress != nil && f > last && ctx.Err() == nil {
			last = f
			o.Progress(f)
		}
	}
	if p := g.GaussianSHPalette; p != nil {
		// No per-splat bands remain to measure against: the palette is the truth.
		progress(0)
		progress(1)
		return report(p.Count(), 0), nil
	}
	n := len(g.Points)
	count := len(g.GaussianSH) / n
	if deg := nt3dsh.Degree(count); deg < 1 || deg > 3 || len(g.GaussianSH) != n*count {
		return PaletteReport{}, errors.New("nt3dmesh: an SH palette needs degree 1 to 3 coefficients")
	}
	for _, c := range g.GaussianSH {
		for _, v := range c {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return PaletteReport{}, errors.New("nt3dmesh: nonfinite Gaussian SH coefficient")
			}
		}
	}
	b := &paletteBuild{ctx: ctx, sh: g.GaussianSH, count: count, s: count - 1, n: n, workers: o.Workers}
	progress(0)
	if err := ctx.Err(); err != nil {
		return PaletteReport{}, err
	}

	var (
		coeffs []nt3dsh.RGB
		labels []uint16
		rms    float32
	)
	if exact, l, ok := b.distinct(o.Centroids); ok {
		coeffs, labels = exact, l
	} else {
		var err error
		if coeffs, labels, rms, err = b.cluster(o, progress); err != nil {
			return PaletteReport{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return PaletteReport{}, err
	}
	dc := make([]nt3dsh.RGB, n)
	for i := range dc {
		dc[i] = g.GaussianSH[i*count]
	}
	g.GaussianSH, g.GaussianSHLabels = dc, labels
	g.GaussianSHPalette = &nt3d.SHPalette{Degree: nt3dsh.Degree(count), Coefficients: coeffs}
	progress(1)
	return report(len(coeffs)/b.s, rms), nil
}

// paletteBuild is the state of one BuildSHPalette call. A splat's vector is
// its s band coefficients, sh[i*count+1 : (i+1)*count].
type paletteBuild struct {
	ctx      context.Context
	sh       []nt3dsh.RGB
	count, s int
	n        int
	workers  int
}

func (b *paletteBuild) vector(i int) []nt3dsh.RGB {
	return b.sh[i*b.count+1 : (i+1)*b.count]
}

// distinct returns the palette of distinct band sets in first-seen order and
// the labels into it, or false once there are more than limit.
func (b *paletteBuild) distinct(limit int) ([]nt3dsh.RGB, []uint16, bool) {
	heads := make(map[uint64]int32)
	var next []int32 // Next distinct set with the same hash, or -1.
	var first []int32
	labels := make([]uint16, b.n)
	for i := range b.n {
		v := b.vector(i)
		h := paletteHashBands(v)
		id := int32(-1)
		head, seen := heads[h]
		for c := head; seen && c >= 0; c = next[c] {
			if paletteSameBits(b.vector(int(first[c])), v) {
				id = c
				break
			}
		}
		if id < 0 {
			if len(first) == limit {
				return nil, nil, false
			}
			id = int32(len(first))
			first = append(first, int32(i))
			if seen {
				next = append(next, head)
			} else {
				next = append(next, -1)
			}
			heads[h] = id
		}
		labels[i] = uint16(id)
	}
	coeffs := make([]nt3dsh.RGB, 0, len(first)*b.s)
	for _, i := range first {
		coeffs = append(coeffs, b.vector(int(i))...)
	}
	return coeffs, labels, true
}

func paletteHashBands(v []nt3dsh.RGB) uint64 {
	h := uint64(14695981039346656037)
	for _, c := range v {
		for _, x := range c {
			h = (h ^ uint64(math.Float32bits(x))) * 1099511628211
		}
	}
	h ^= h >> 31
	h *= 0x9e3779b97f4a7c15
	return h ^ h>>29
}

func paletteSameBits(a, b []nt3dsh.RGB) bool {
	for j := range a {
		for k := range 3 {
			if math.Float32bits(a[j][k]) != math.Float32bits(b[j][k]) {
				return false
			}
		}
	}
	return true
}

// parallel runs task(0..tasks-1) on b.workers goroutines, calling done (on
// this goroutine) with the number of finished tasks after each one. Tasks
// write their results by index, so the outcome does not depend on the
// worker count. It returns the context's error if it was canceled.
func (b *paletteBuild) parallel(tasks int, task func(i int), done func(finished int)) error {
	var nextTask atomic.Int64
	finished := make(chan struct{}, tasks)
	var wg sync.WaitGroup
	for range min(b.workers, tasks) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(nextTask.Add(1) - 1)
				if i >= tasks {
					return
				}
				if b.ctx.Err() == nil {
					task(i)
				}
				finished <- struct{}{}
			}
		}()
	}
	for f := 1; f <= tasks; f++ {
		<-finished
		if done != nil {
			done(f)
		}
	}
	wg.Wait()
	return b.ctx.Err()
}

// paletteChunks is the number of paletteChunk tasks covering n items.
func paletteChunks(n int) int { return (n + paletteChunk - 1) / paletteChunk }

// paletteDistance is the squared distance between two band vectors, or some value
// above bound once it exceeds bound.
func paletteDistance(v, c []nt3dsh.RGB, bound float32) float32 {
	var sum float32
	c = c[:len(v)]
	for j := range v {
		x, y, z := v[j][0]-c[j][0], v[j][1]-c[j][1], v[j][2]-c[j][2]
		sum += x*x + y*y + z*z
		if sum > bound {
			return sum
		}
	}
	return sum
}

// paletteNearest returns the index of the centroid nearest v among the k centroids
// of stride s in cs, the first on ties, and its squared distance.
func paletteNearest(v, cs []nt3dsh.RGB, s int) (int, float32) {
	best, bestD := 0, float32(math.MaxFloat32)
	for c, o := 0, 0; o < len(cs); c, o = c+1, o+s {
		if d := paletteDistance(v, cs[o:o+s], bestD); d < bestD {
			best, bestD = c, d
		}
	}
	return best, bestD
}

// paletteSeed picks up to k centroids from the m vectors of stride s in data by
// k-means++, stopping early when every vector equals a centroid, so it never
// picks one vector twice. dist is m scratch values. run evaluates
// f(lo, hi) over [0, m) in chunks, possibly in parallel.
func paletteSeed(data []nt3dsh.RGB, s, k int, rng *rand.Rand, dist []float32, run func(m int, f func(lo, hi int) float64) (float64, error)) ([]nt3dsh.RGB, error) {
	m := len(data) / s
	cs := make([]nt3dsh.RGB, 0, k*s)
	pick := rng.IntN(m)
	for {
		cs = append(cs, data[pick*s:(pick+1)*s]...)
		if len(cs) == k*s {
			return cs, nil
		}
		c := cs[len(cs)-s:]
		first := len(cs) == s
		total, err := run(m, func(lo, hi int) float64 {
			var sum float64
			for i := lo; i < hi; i++ {
				d := paletteDistance(data[i*s:(i+1)*s], c, math.MaxFloat32)
				if first || d < dist[i] {
					dist[i] = d
				}
				sum += float64(dist[i])
			}
			return sum
		})
		if err != nil {
			return nil, err
		}
		if !(total > 0) {
			return cs, nil
		}
		r := rng.Float64() * total
		pick = -1
		var cum float64
		for i, d := range dist {
			if d > 0 {
				pick = i
				if cum += float64(d); cum > r {
					break
				}
			}
		}
	}
}

// paletteLloyd runs rounds of k-means on the m vectors of stride s in data from the
// centroids cs, updating cs in place; an empty centroid keeps its place. It
// leaves each vector's centroid in labels. run assigns [lo, hi) chunks,
// possibly in parallel; round is called after each round.
func paletteLloyd(data, cs []nt3dsh.RGB, s, rounds int, labels []int32, run func(m int, f func(lo, hi int) float64) (float64, error), round func(r int)) error {
	m, k := len(data)/s, len(cs)/s
	assign := func() error {
		_, err := run(m, func(lo, hi int) float64 {
			for i := lo; i < hi; i++ {
				c, _ := paletteNearest(data[i*s:(i+1)*s], cs, s)
				labels[i] = int32(c)
			}
			return 0
		})
		return err
	}
	sums := make([]float64, k*s*3)
	counts := make([]int, k)
	for r := range rounds {
		if err := assign(); err != nil {
			return err
		}
		clear(sums)
		clear(counts)
		for i, l := range labels[:m] {
			counts[l]++
			paletteAccumulate(sums[int(l)*s*3:], data[i*s:(i+1)*s])
		}
		paletteMean(cs, sums, counts, s)
		if round != nil {
			round(r)
		}
	}
	return assign()
}

func paletteAccumulate(sum []float64, v []nt3dsh.RGB) {
	sum = sum[:len(v)*3]
	for j, c := range v {
		sum[3*j] += float64(c[0])
		sum[3*j+1] += float64(c[1])
		sum[3*j+2] += float64(c[2])
	}
}

// paletteMean sets each centroid with members to their mean.
func paletteMean(cs []nt3dsh.RGB, sums []float64, counts []int, s int) {
	for c, n := range counts {
		if n == 0 {
			continue
		}
		for j := range s {
			o := (c*s + j) * 3
			cs[c*s+j] = nt3dsh.RGB{float32(sums[o] / float64(n)), float32(sums[o+1] / float64(n)), float32(sums[o+2] / float64(n))}
		}
	}
}

// cluster trains the two-level palette and assigns every splat to it.
func (b *paletteBuild) cluster(o PaletteOptions, progress func(float64)) ([]nt3dsh.RGB, []uint16, float32, error) {
	s, n := b.s, b.n

	// The subsample, gathered contiguously.
	rng := rand.New(rand.NewPCG(11, 17))
	sub := rng.Perm(n)[:min(n, DefaultSubsample)]
	m := len(sub)
	data := make([]nt3dsh.RGB, m*s)
	for k, i := range sub {
		copy(data[k*s:], b.vector(i))
	}

	// run evaluates f over paletteChunk chunks of [0, m) in parallel and
	// sums the results in chunk order.
	run := func(onProgress func(f int, total int)) func(m int, f func(lo, hi int) float64) (float64, error) {
		return func(m int, f func(lo, hi int) float64) (float64, error) {
			parts := make([]float64, paletteChunks(m))
			var done func(int)
			if onProgress != nil {
				done = func(fin int) { onProgress(fin, len(parts)) }
			}
			err := b.parallel(len(parts), func(c int) {
				parts[c] = f(c*paletteChunk, min(m, (c+1)*paletteChunk))
			}, done)
			var total float64
			for _, p := range parts {
				total += p
			}
			return total, err
		}
	}

	// Coarse cells.
	dist := make([]float32, m)
	coarse, err := paletteSeed(data, s, min(DefaultCoarseCentroids, o.Centroids), rng, dist, run(nil))
	if err != nil {
		return nil, nil, 0, err
	}
	steps := float64(o.Iterations + 1)
	progress(.3 / steps)
	cell := make([]int32, m)
	if err := paletteLloyd(data, coarse, s, o.Iterations, cell, run(nil), func(r int) { progress(.3 * float64(r+2) / steps) }); err != nil {
		return nil, nil, 0, err
	}

	// Drop the coarse cells the final assignment left empty and gather each
	// remaining cell's members.
	k1 := len(coarse) / s
	members := make([][]int32, k1)
	for i, c := range cell {
		members[c] = append(members[c], int32(i))
	}
	var kept []nt3dsh.RGB
	var cells [][]int32
	for c, mem := range members {
		if len(mem) > 0 {
			kept = append(kept, coarse[c*s:(c+1)*s]...)
			cells = append(cells, mem)
		}
	}
	coarse, k1 = kept, len(cells)

	// Fine centroids per cell, Centroids split evenly between the cells.
	fine := make([][]nt3dsh.RGB, k1)
	err = b.parallel(k1, func(c int) {
		quota := o.Centroids / k1
		if c < o.Centroids%k1 {
			quota++
		}
		mem := cells[c]
		local := make([]nt3dsh.RGB, len(mem)*s)
		for k, i := range mem {
			copy(local[k*s:], data[int(i)*s:(int(i)+1)*s])
		}
		serial := func(m int, f func(lo, hi int) float64) (float64, error) { return f(0, m), b.ctx.Err() }
		r := rand.New(rand.NewPCG(23, uint64(c)))
		cs, err := paletteSeed(local, s, quota, r, make([]float32, len(mem)), serial)
		if err == nil {
			err = paletteLloyd(local, cs, s, o.Iterations, make([]int32, len(mem)), serial, nil)
		}
		if err == nil {
			fine[c] = cs
		}
	}, func(f int) { progress(.3 + .3*float64(f)/float64(k1)) })
	if err != nil {
		return nil, nil, 0, err
	}
	offsets := make([]int, k1+1)
	for c, cs := range fine {
		offsets[c+1] = offsets[c] + len(cs)/s
	}

	// Assign every splat: its nearest cell, then that cell's nearest centroid.
	labels := make([]int32, n)
	_, err = run(func(f, total int) { progress(.6 + .3*float64(f)/float64(total)) })(n, func(lo, hi int) float64 {
		for i := lo; i < hi; i++ {
			v := b.vector(i)
			c, _ := paletteNearest(v, coarse, s)
			f, _ := paletteNearest(v, fine[c], s)
			labels[i] = int32(offsets[c] + f)
		}
		return 0
	})
	if err != nil {
		return nil, nil, 0, err
	}

	// Each centroid becomes the mean of its splats; empty ones are dropped.
	total := offsets[k1]
	sums := make([]float64, total*s*3)
	counts := make([]int, total)
	for i, l := range labels {
		counts[l]++
		paletteAccumulate(sums[int(l)*s*3:], b.vector(i))
	}
	all := make([]nt3dsh.RGB, total*s)
	paletteMean(all, sums, counts, s)
	renumber := make([]int32, total)
	coeffs := make([]nt3dsh.RGB, 0, total*s)
	for c, k := range counts {
		if k > 0 {
			renumber[c] = int32(len(coeffs) / s)
			coeffs = append(coeffs, all[c*s:(c+1)*s]...)
		}
	}
	out := make([]uint16, n)
	sq, err := run(func(f, total int) { progress(.9 + .05*float64(f)/float64(total)) })(n, func(lo, hi int) float64 {
		var sum float64
		for i := lo; i < hi; i++ {
			l := renumber[labels[i]]
			out[i] = uint16(l)
			sum += float64(paletteDistance(b.vector(i), coeffs[int(l)*s:(int(l)+1)*s], math.MaxFloat32))
		}
		return sum
	})
	if err != nil {
		return nil, nil, 0, err
	}
	return slices.Clip(coeffs), out, float32(math.Sqrt(sq / float64(n*s*3))), nil
}
