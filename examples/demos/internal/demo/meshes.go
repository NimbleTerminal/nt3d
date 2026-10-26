// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"math"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
)

// MeshesMain shows the shape builders, or the mesh files named as arguments.
func MeshesMain() { mainDemo(false, sceneMeshes) }

type meshScene struct {
	hasSH, dcOnly bool
	splats        bool // Gaussian splats, with or without spherical harmonics.
	capture       bool // A splat capture, framed on its subject rather than its bounds.
	nodes         []nt3d.NodeID
	sources       []nt3d.Geometry // As built or read, per node.
	normals       int             // Index into meshNormals.
	outlined      bool            // Expanded back-face contours.
	glass         bool            // Shapes drawn transparent.
	shiny         bool            // Shapes with specular highlights.
	triangles     int
	title         string
	palette       paletteBuild
}

// paletteBuild is the state of the "w" key's SH palette: a background build
// of one palette per splat piece, and, once done, the paletted geometries
// the scene shows in place of the originals.
type paletteBuild struct {
	building bool
	fraction atomic.Uint32 // Progress of the build, 0 to 1000; the build's goroutine stores it.
	cancel   context.CancelFunc
	built    []nt3d.Geometry // Per node; nil where the node was not paletted.
	reports  []nt3dmesh.PaletteReport
	active   bool // The scene shows built where it is not nil.
}

// paletteDoneMsg carries a finished background build to the update loop.
type paletteDoneMsg struct {
	built   []nt3d.Geometry
	reports []nt3dmesh.PaletteReport
	err     error
}

// paletteTickMsg refreshes the view while a build runs, so its status moves.
type paletteTickMsg struct{}

func paletteTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return paletteTickMsg{} })
}

// shCount is the number of SH coefficients g holds per splat, or zero.
func shCount(g nt3d.Geometry) int {
	if len(g.Points) == 0 {
		return 0
	}
	return len(g.GaussianSH) / len(g.Points)
}

// paletteable reports whether g has bands above DC and no palette to share them.
func paletteable(g nt3d.Geometry) bool {
	return g.GaussianSHPalette == nil && shCount(g) >= 4
}

// togglePalette is the "w" key: it cancels a build, swaps the originals back
// in, or starts a build of a palette for each piece that has bands to share.
func (d *demo) togglePalette() tea.Cmd {
	p := &d.meshes.palette
	switch {
	case p.building:
		p.cancel()
		return nil
	case p.active:
		p.active, p.built, p.reports = false, nil, nil
		if err := d.rebuildMeshes(); err != nil {
			d.status = err.Error()
		}
		return d.view.Refresh()
	}
	var pieces []int
	for i, g := range d.meshes.sources {
		if paletteable(g) {
			pieces = append(pieces, i)
		}
	}
	if len(pieces) == 0 {
		d.status = "no per-splat SH bands to palette"
		for _, g := range d.meshes.sources {
			if g.GaussianSHPalette != nil {
				d.status = "this cloud's SH is already a palette (from the file)"
				break
			}
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.building, p.cancel = true, cancel
	p.fraction.Store(0)
	sources := slices.Clone(d.meshes.sources)
	build := func() tea.Msg {
		defer cancel()
		msg := paletteDoneMsg{built: make([]nt3d.Geometry, len(sources))}
		for k, i := range pieces {
			o := nt3dmesh.DefaultPaletteOptions()
			o.Progress = func(f float64) {
				p.fraction.Store(uint32((float64(k) + f) / float64(len(pieces)) * 1000))
			}
			g := sources[i]
			report, err := nt3dmesh.BuildSHPalette(ctx, &g, o)
			if err != nil {
				return paletteDoneMsg{err: err}
			}
			msg.built[i] = g
			msg.reports = append(msg.reports, report)
		}
		return msg
	}
	return tea.Batch(build, paletteTick())
}

// finishPalette stores a finished build and shows it, on the update goroutine.
func (d *demo) finishPalette(msg paletteDoneMsg) error {
	p := &d.meshes.palette
	p.building = false
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			d.status = "SH palette canceled"
			return nil
		}
		return msg.err
	}
	p.built, p.reports, p.active = msg.built, msg.reports, true
	return d.rebuildMeshes()
}

// shBytes is the compact GPU storage of the scene's SH before and after the
// palette: 3 bytes a coefficient per splat, against 3 DC bytes, a 2-byte
// label, and 3 bytes for each centroid's coefficients.
func (p *paletteBuild) shBytes(sources []nt3d.Geometry) (before, after int) {
	for i, g := range p.built {
		if g.GaussianSHPalette == nil {
			continue
		}
		n := len(g.Points)
		before += 3 * shCount(sources[i]) * n
		after += 3*n + 2*n + 3*g.GaussianSHPalette.Stride()*g.GaussianSHPalette.Count()
	}
	return before, after
}

// megabytes writes b as 48 MB, 7.93 MB, or 0.024 MB: three significant digits.
func megabytes(b int) string {
	if mb := float64(b) / 1e6; mb < 1000 {
		return fmt.Sprintf("%.3g MB", mb)
	}
	return fmt.Sprintf("%.0f MB", float64(b)/1e6)
}

// groupDigits writes n with commas, as 65,536.
func groupDigits(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// paletteStatus is the "w" key's status: progress while building, then the
// centroids and the SH storage saved; empty when there is none.
func (d *demo) paletteStatus() string {
	p := &d.meshes.palette
	switch {
	case p.building:
		return fmt.Sprintf("SH palette: building %d%%", p.fraction.Load()/10)
	case p.active:
		centroids := 0
		for _, r := range p.reports {
			centroids += r.Centroids
		}
		before, after := p.shBytes(d.meshes.sources)
		return fmt.Sprintf("SH palette %s centroids · SH %s → %s", groupDigits(centroids), megabytes(before), megabytes(after))
	}
	return ""
}

var meshNormals = []string{"as built", "flat", "smooth 40°"}

func (d *demo) setupMeshes(files []string, up nt3dmesh.Up) error {
	d.meshes = &meshScene{shiny: true}
	if len(files) == 0 {
		if err := d.setupShapes(); err != nil {
			return err
		}
		return d.rebuildMeshes()
	}
	d.meshes.title = filepath.Base(files[0])
	if len(files) > 1 {
		d.meshes.title = fmt.Sprintf("%d files", len(files))
	}
	// Lay files out in a row, each fitted to a cell two units wide.
	spacing := float32(2.2)
	for i, name := range files {
		m, err := nt3dmesh.ReadFile(name, &nt3dmesh.Options{Color: color.RGBA{100, 180, 230, 255}, Up: up})
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		for _, w := range m.Warnings {
			d.status = filepath.Base(name) + ": " + w
		}
		group, err := m.AddTo(d.scene, d.group, fmt.Sprintf("file %d", i))
		if err != nil {
			return err
		}
		// A capture's bounds are its shell of background splats; its
		// subject, the dense middle, is what fills the cell.
		box := m.Bounds()
		if framing, ok := splatFraming(m); ok {
			box, d.meshes.capture = framing, true
		}
		fit, _, _ := box.Normalization()
		offset := nt3dmath.Translation(nt3dmath.Vec3{X: spacing * (float32(i) - float32(len(files)-1)/2)})
		if err = d.scene.SetTransform(group, offset.Mul(fit)); err != nil {
			return err
		}
		node, _ := d.scene.Node(group)
		for j, id := range node.Children {
			d.meshes.nodes = append(d.meshes.nodes, id)
			d.meshes.sources = append(d.meshes.sources, m.Objects[j].Geometry)
			d.meshes.hasSH = d.meshes.hasSH || len(m.Objects[j].Geometry.GaussianSH) > 0
			d.meshes.splats = d.meshes.splats || m.Objects[j].Geometry.PointStyle.Shape == nt3d.GaussianPoint
		}
		d.meshes.triangles += m.Triangles()
	}
	return d.rebuildMeshes()
}

func (d *demo) setupShapes() error {
	d.meshes.title = "shape builders"
	grid, err := nt3d.NewTexture(mapImage(false))
	if err != nil {
		return err
	}
	topo, err := nt3d.NewTexture(mapImage(true))
	if err != nil {
		return err
	}
	builders := []func() (nt3d.Geometry, error){
		func() (nt3d.Geometry, error) {
			return nt3d.BuildSphere(nt3d.Sphere{Radius: .38, Material: &nt3d.Material{Texture: topo}})
		},
		func() (nt3d.Geometry, error) {
			return nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .3, TopRadius: .3, Height: .7, Color: color.RGBA{238, 133, 64, 255}})
		},
		func() (nt3d.Geometry, error) {
			return nt3d.BuildCylinder(nt3d.Cylinder{BottomRadius: .34, Height: .75, Color: color.RGBA{160, 119, 224, 255}})
		},
		func() (nt3d.Geometry, error) {
			return nt3d.BuildTorus(nt3d.Torus{MajorRadius: .3, MinorRadius: .11, Color: color.RGBA{234, 196, 83, 255}})
		},
		func() (nt3d.Geometry, error) {
			return nt3d.BuildCuboid(nt3d.Cuboid{Size: nt3dmath.Vec3{X: .55, Y: .55, Z: .55}, Material: &nt3d.Material{Texture: grid}})
		},
	}
	for i, build := range builders {
		g, err := build()
		if err != nil {
			return err
		}
		// Bake each shape's place into it: a merged or exported mesh keeps it.
		g, err = nt3d.TransformGeometry(g, nt3dmath.Translation(nt3dmath.Vec3{X: .8 * float32(i-2)}))
		if err != nil {
			return err
		}
		id, err := d.scene.SetGeometry(fmt.Sprintf("shape %d", i), g)
		if err != nil {
			return err
		}
		if err = d.scene.Reparent(id, d.group); err != nil {
			return err
		}
		d.meshes.nodes = append(d.meshes.nodes, id)
		d.meshes.sources = append(d.meshes.sources, g)
		d.meshes.triangles += len(g.Indices) / 3
	}
	return nil
}

// cycleNormals rebuilds every mesh with the next normal style.
func (d *demo) cycleNormals() error {
	d.meshes.normals = (d.meshes.normals + 1) % len(meshNormals)
	return d.rebuildMeshes()
}

// toggleGlass makes every mesh transparent, or opaque again.
func (d *demo) toggleGlass() error {
	d.meshes.glass = !d.meshes.glass
	return d.rebuildMeshes()
}

// toggleShine turns specular highlights on or off.
func (d *demo) toggleShine() error {
	d.meshes.shiny = !d.meshes.shiny
	return d.rebuildMeshes()
}

func (d *demo) toggleOutlines() error {
	d.meshes.outlined = !d.meshes.outlined
	return d.rebuildMeshes()
}

// glassAlpha is the glass shapes' opacity.
const glassAlpha = 110

// rebuildMeshes sets every mesh from its source, with the normal style,
// highlights if shiny, and, as glass, a transparent material and its
// vertices' alpha.
func (d *demo) rebuildMeshes() error {
	for i, id := range d.meshes.nodes {
		g, err := d.meshes.sources[i], error(nil)
		if d.meshes.palette.active && d.meshes.palette.built[i].GaussianSHPalette != nil {
			g = d.meshes.palette.built[i]
		}
		if len(g.GaussianSH) > 0 {
			g.PointStyle.SHDCOnly = d.meshes.dcOnly
		}
		if d.meshes.outlined {
			g.Outline = nt3d.OutlineStyle{Width: 2, Color: color.RGBA{18, 22, 30, 255}}
		}
		switch d.meshes.normals {
		case 1:
			g, err = nt3d.FlatNormals(g)
		case 2:
			g, err = nt3d.SmoothNormals(g, 40)
		}
		if err == nil && (d.meshes.glass || d.meshes.shiny) {
			m := nt3d.Material{}
			if g.Material != nil {
				m = *g.Material
			}
			if d.meshes.shiny {
				m.Specular, m.Shininess = .5, 48
			}
			if d.meshes.glass {
				m.Transparent, m.Cutout, m.EdgeOn = true, false, false
				g.Vertices = append([]nt3d.Vertex(nil), g.Vertices...)
				for j := range g.Vertices {
					g.Vertices[j].Color.A = glassAlpha
				}
			}
			g.Material = &m
		}
		if err == nil {
			err = d.scene.SetNodeGeometry(id, g)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// splatStats is the splat budget the keys set, and what the last frame
// drew of it. The budget is zero for the default, as on the scene.
type splatStats struct {
	budget, drawn, resident int
}

// minSplatBudget and maxSplatBudget are the least the budget keys halve
// to and the most they double to, so the budget never overflows.
const minSplatBudget, maxSplatBudget = 1000, 1 << 30

// showsSplats reports whether the scene is a splat cloud, the budget keys'
// scene.
func (d *demo) showsSplats() bool {
	return d.meshes != nil && d.models == nil && (d.meshes.hasSH || d.meshes.splats)
}

// adjustSplatBudget halves ("u"), doubles ("y"), or resets ("z") the splat
// budget on the scene.
func (d *demo) adjustSplatBudget(key string) {
	b := d.splats.budget
	if b <= 0 {
		b = nt3d.DefaultSplatBudget
	}
	switch key {
	case "u":
		b = max(minSplatBudget, b/2)
	case "y":
		b = min(maxSplatBudget, b*2)
	case "z":
		b = 0
	}
	d.splats.budget = b
	d.scene.SetSplatBudget(b)
}

// shortCount writes n as 12.5k or 1M.
func shortCount(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.3gM", float64(n)/1e6)
	}
	return fmt.Sprintf("%.3gk", float64(n)/1e3)
}

// splatStatus is the splats a frame drew of those resident, and the budget.
func (d *demo) splatStatus() string {
	budget := d.splats.budget
	if budget <= 0 {
		budget = nt3d.DefaultSplatBudget
	}
	drawn, resident := d.splats.drawn, d.splats.resident
	if resident == 0 { // Before the first frame.
		for _, g := range d.meshes.sources {
			resident += len(g.Points)
		}
		drawn = resident
	}
	count := fmt.Sprintf("%d splats", resident)
	if drawn != resident {
		count = fmt.Sprintf("%d / %d splats", drawn, resident)
	}
	return fmt.Sprintf("%s · budget %s", count, shortCount(budget))
}

func (d *demo) meshesView(content string) string {
	if d.showsSplats() {
		hint, mode := "", ""
		if d.meshes.hasSH {
			hint, mode = " · h DC/full SH", " · full SH"
			if d.meshes.dcOnly {
				mode = " · DC only"
			}
		}
		if d.meshes.hasSH {
			hint += " · w palette"
		}
		if palette := d.paletteStatus(); palette != "" {
			mode += " · " + palette
		}
		return fmt.Sprintf("nt3d splats · %s%s · u/y budget · z reset · d fog · n turn · 1/2/3 backend · q quit\n%s\n%s · %s%s · drag orbit · wheel zoom · c fit · r rotate · %s", d.meshes.title, hint, content, d.rendering(), d.splatStatus(), mode, d.status)
	}
	glass, shine, outlines := "opaque", "dull", "outlines off"
	if d.meshes.outlined {
		outlines = "outlines on"
	}
	if d.meshes.glass {
		glass = "glass"
	}
	if d.meshes.shiny {
		shine = "highlights"
	}
	return fmt.Sprintf("nt3d meshes · %s · s normals · t glass · e highlights · b outlines · d fog · n rotate · v visibility · 1/2/3 backend · q quit\n%s\n%s · %d triangles · normals %s · %s · %s · %s · drag orbit · wheel zoom · c fit · r rotate · %s",
		d.meshes.title, content, d.rendering(), d.meshes.triangles, meshNormals[d.meshes.normals], glass, shine, outlines, d.status)
}

// splatFraming is the box a splat capture is framed by: a cube around the
// median position of its splats, three quarters of their median distance
// from it to a side (the camera sits a few cells back, so the subject in
// the middle of a capture fills the view) rather than the shell of sky and
// ground splats around it, which the bounds would frame. A model with
// anything but Gaussian splats has none.
func splatFraming(m *nt3dmesh.Model) (nt3dmath.AABB, bool) {
	var xs, ys, zs []float32
	for _, o := range m.Objects {
		g := o.Geometry
		if g.PointStyle.Shape != nt3d.GaussianPoint || len(g.Vertices) > 0 || len(g.Lines) > 0 || len(g.Boxes) > 0 {
			return nt3dmath.AABB{}, false
		}
		for _, p := range g.Points {
			xs, ys, zs = append(xs, p.Position.X), append(ys, p.Position.Y), append(zs, p.Position.Z)
		}
	}
	if len(xs) == 0 {
		return nt3dmath.AABB{}, false
	}
	median := func(v []float32) float32 {
		sorted := slices.Clone(v)
		slices.Sort(sorted)
		return sorted[len(sorted)/2]
	}
	center := nt3dmath.Vec3{X: median(xs), Y: median(ys), Z: median(zs)}
	distances := make([]float32, len(xs))
	for i := range xs {
		d := nt3dmath.Vec3{X: xs[i], Y: ys[i], Z: zs[i]}.Sub(center)
		distances[i] = float32(math.Sqrt(float64(d.Dot(d))))
	}
	half := .75 * median(distances)
	if half <= 0 {
		return nt3dmath.AABB{}, false
	}
	var box nt3dmath.AABB
	box.Include(center.Sub(nt3dmath.Vec3{X: half, Y: half, Z: half}))
	box.Include(center.Add(nt3dmath.Vec3{X: half, Y: half, Z: half}))
	return box, true
}

// fitMeshes frames the meshes scene: the widget fits the scene's bounds,
// but a splat capture's bounds are its shell, so the capture takes the
// default view of the cell its subject was fitted to.
func (d *demo) fitMeshes() tea.Cmd {
	if d.meshes == nil || !d.meshes.capture {
		return d.view.Fit()
	}
	camera := d.view.Camera()
	camera.Target, camera.Distance = nt3dmath.Vec3{}, 4.3
	return d.view.SetCamera(camera)
}
