// Copyright (c) 2026 Neomantra Corp.
package terraindemo

import (
	"fmt"
	"image/color"
	"io/fs"
	"path"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

// SampleFiles holds the sample models the models and fur demos show. Their
// commands set it from the models package, which embeds them, so that the
// other demos' programs do not carry the files.
var SampleFiles fs.FS

// ModelsMain shows sample STL, OBJ, and 3MF files from SampleFiles.
func ModelsMain() { mainDemo(false, sceneModels) }

// sample is a model file and what it shows of its format.
type sample struct {
	file, title, about string
	up                 nt3dmesh.Up
	beta               float64 // The camera's azimuth: each model's best side.
}

var samples = []sample{
	{"robot.3mf", "Robot", "3MF: mirrored arm, corner colors, painted stripes, textured screen", nt3dmesh.ZUp, -70},
	{"star.obj", "Star", "Y-up OBJ: concave caps ear-clipped, wood texture, MTL color", nt3dmesh.YUp, 40},
	{"knot.stl", "Trefoil knot", "binary STL with VisCAM face colors", nt3dmesh.ZUp, 40},
}

type modelScene struct {
	index int
	group nt3d.NodeID
	model *nt3dmesh.Model
}

func (d *demo) setupModels() error {
	d.models = &modelScene{}
	return d.loadSample(0)
}

// loadSample replaces the shown model with sample i, read from the embedded
// files as nt3dmesh would read them from disk.
func (d *demo) loadSample(i int) error {
	s := samples[i]
	data, err := fs.ReadFile(SampleFiles, s.file)
	if err != nil {
		return err
	}
	format, _ := nt3dmesh.FormatOf(s.file, data)
	open := func(name string) ([]byte, error) { return fs.ReadFile(SampleFiles, path.Clean(name)) }
	m, err := nt3dmesh.Decode(data, format, &nt3dmesh.Options{Color: color.RGBA{100, 180, 230, 255}, Open: open, Up: s.up})
	if err != nil {
		return fmt.Errorf("%s: %w", s.file, err)
	}
	if d.models.group != 0 {
		if err := d.scene.Remove(d.models.group); err != nil {
			return err
		}
	}
	group, err := m.AddTo(d.scene, d.group, fmt.Sprintf("%s %d", filepath.Base(s.file), i))
	if err != nil {
		return err
	}
	fit, _, _ := m.Bounds().Normalization()
	if err = d.scene.SetTransform(group, fit); err != nil {
		return err
	}
	d.models.index, d.models.group, d.models.model = i, group, m
	d.meshes = &meshScene{title: s.title}
	node, _ := d.scene.Node(group)
	for j, id := range node.Children {
		d.meshes.nodes = append(d.meshes.nodes, id)
		d.meshes.sources = append(d.meshes.sources, m.Objects[j].Geometry)
	}
	d.meshes.triangles = m.Triangles()
	d.status = s.about
	for _, w := range m.Warnings {
		d.status = w
	}
	return nil
}

// stepSample shows the next or previous sample, framed from its best side.
func (d *demo) stepSample(step int) tea.Cmd {
	i := (d.models.index + step + len(samples)) % len(samples)
	if err := d.loadSample(i); err != nil {
		d.status = err.Error()
		return d.view.Refresh()
	}
	return tea.Batch(d.view.Refresh(), d.frameSample())
}

func (d *demo) frameSample() tea.Cmd {
	camera := d.view.Camera()
	camera.Beta = samples[d.models.index].beta
	return tea.Batch(d.view.SetCamera(camera), d.view.Fit())
}

func (d *demo) modelsView(content string) string {
	m := d.models.model
	return fmt.Sprintf("nt3d models · %s (%d of %d) · [ ] model · s normals · c fit · 1/2/3 backend · q quit\n%s\n%s · %s · %d triangles in %d objects · normals %s · %s",
		samples[d.models.index].file, d.models.index+1, len(samples), content, d.rendering(), m.Format, m.Triangles(), len(m.Objects), meshNormals[d.meshes.normals], d.status)
}

// largeScene is a dense procedural mesh, for full software drawing,
// wireframe outlines, and indexed picking.
type largeScene struct {
	triangles int
	indexing  bool
}

// LargeMain shows a dense mesh of some 735,000 triangles.
func LargeMain() { mainDemo(false, sceneLarge) }

func (d *demo) setupLarge() error {
	g, err := asteroid()
	if err != nil {
		return err
	}
	id, err := d.scene.SetGeometry("asteroid", g)
	if err != nil {
		return err
	}
	if err = d.scene.Reparent(id, d.group); err != nil {
		return err
	}
	d.large = &largeScene{triangles: len(g.Indices) / 3, indexing: true}
	d.status = "click a triangle to pick it"
	return nil
}

func (d *demo) toggleIndexing() {
	d.large.indexing = !d.large.indexing
	d.scene.SetPickIndexing(d.large.indexing)
	if d.large.indexing {
		d.status = "pick index on: the first pick scans and starts it; later picks use it"
	} else {
		d.status = "pick index off: every pick scans all triangles"
	}
}

func (d *demo) largeView(content string) string {
	return fmt.Sprintf("nt3d dense mesh · click picks · i pick index · c fit · n rotate · 1/2/3 backend · q quit\n%s\n%s · %d triangles · %s",
		content, d.rendering(), d.large.triangles, d.status)
}

// asteroid is a lumpy, cratered sphere of 1024 × 360 cells.
func asteroid() (nt3d.Geometry, error) {
	const segments, rings = 1024, 360
	g, err := nt3d.BuildSphere(nt3d.Sphere{Radius: 1, Segments: segments, Rings: rings})
	if err != nil {
		return g, err
	}
	low, high := color.RGBA{72, 66, 62, 255}, color.RGBA{204, 190, 166, 255}
	for i := range g.Vertices {
		v := &g.Vertices[i]
		r := 1 + .22*fractal(v.Position.Scale(1.6)) + .05*fractal(v.Position.Scale(9))
		v.Position = v.Position.Scale(float32(r))
		t := float32(min(1, max(0, (r-.82)/.36)))
		v.Color = color.RGBA{uint8(float32(low.R) + t*float32(high.R-low.R)), uint8(float32(low.G) + t*float32(high.G-low.G)), uint8(float32(low.B) + t*float32(high.B-low.B)), 255}
		v.Normal = nt3dmath.Vec3{}
	}
	// Smooth normals over the lattice: each face adds its normal, weighted by
	// area, to its corners; the seam's twin columns and the poles share sums.
	for i := 0; i+2 < len(g.Indices); i += 3 {
		a, b, c := g.Indices[i], g.Indices[i+1], g.Indices[i+2]
		n := g.Vertices[b].Position.Sub(g.Vertices[a].Position).Cross(g.Vertices[c].Position.Sub(g.Vertices[a].Position))
		for _, k := range []uint32{a, b, c} {
			g.Vertices[k].Normal = g.Vertices[k].Normal.Add(n)
		}
	}
	at := func(i, j int) *nt3d.Vertex { return &g.Vertices[i*(segments+1)+j] }
	for i := 0; i <= rings; i++ {
		first, last := at(i, 0), at(i, segments)
		first.Normal = first.Normal.Add(last.Normal)
		last.Normal = first.Normal
	}
	for _, i := range []int{0, rings} {
		var sum nt3dmath.Vec3
		for j := 0; j <= segments; j++ {
			sum = sum.Add(at(i, j).Normal)
		}
		for j := 0; j <= segments; j++ {
			at(i, j).Normal = sum
		}
	}
	for i := range g.Vertices {
		g.Vertices[i].Normal = g.Vertices[i].Normal.Normalize()
	}
	g.Bounds = nt3d.GeometryBounds(g)
	return g, g.Validate()
}

// fractal is value noise summed over five octaves, in about [-1, 1].
func fractal(p nt3dmath.Vec3) float64 {
	total, amplitude, frequency := 0.0, .5, 1.0
	for range 5 {
		total += amplitude * valueNoise(float64(p.X)*frequency, float64(p.Y)*frequency, float64(p.Z)*frequency)
		amplitude, frequency = amplitude*.5, frequency*2.03
	}
	return total
}

func valueNoise(x, y, z float64) float64 {
	hash := func(i, j, k int) float64 {
		h := uint32(i)*374761393 + uint32(j)*668265263 + uint32(k)*2147483647
		h = (h ^ h>>13) * 1274126177
		return float64(h^h>>16)/float64(1<<32)*2 - 1
	}
	smooth := func(t float64) float64 { return t * t * (3 - 2*t) }
	fx, fy, fz := floor(x), floor(y), floor(z)
	ix, iy, iz := int(fx), int(fy), int(fz)
	tx, ty, tz := smooth(x-fx), smooth(y-fy), smooth(z-fz)
	lerp := func(a, b, t float64) float64 { return a + (b-a)*t }
	plane := func(k int) float64 {
		return lerp(lerp(hash(ix, iy, k), hash(ix+1, iy, k), tx), lerp(hash(ix, iy+1, k), hash(ix+1, iy+1, k), tx), ty)
	}
	return lerp(plane(iz), plane(iz+1), tz)
}

func floor(v float64) float64 {
	f := float64(int(v))
	if f > v {
		f--
	}
	return f
}
