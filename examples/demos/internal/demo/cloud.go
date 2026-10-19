// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"fmt"
	"image/color"
	"math"
	"strconv"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/clod"
	"nimbleterminal.dev/nt3d/math"
)

// CloudMain shows a galaxy of up to a million points, culled and thinned
// by clusters as the camera moves.
func CloudMain() { mainDemo(false, sceneCloud) }

// cloudCounts are the point counts n cycles through.
var cloudCounts = []int{100_000, 300_000, 1_000_000}

// cloudLevels thin clusters with distance: all their points within 6
// units, half within 12, a quarter within 24, and one point for the whole
// cluster beyond. The galaxy's radius is about 4.5, so the default view at
// 9 draws its near half in full.
var cloudLevels = []nt3dclod.Level{{Range: 6, Fraction: 1}, {Range: 12, Fraction: .5}, {Range: 24, Fraction: .25}, {Fraction: 0}}

// cloudMargin is the slack around the view, in pixels: a point's radius
// and one more, as nt3d pads its own culling.
const cloudMargin = 2

type cloudScene struct {
	points    []nt3d.Point
	positions []nt3dmath.Vec3
	bounds    nt3dmath.AABB
	set       *nt3dclod.Set
	mean      []color.RGBA // Each cluster's mean color, for its representative.
	sel       nt3dclod.Selection
	nodes     []nt3d.NodeID
	installed []int // Each node's point count, or -1 when hidden.
	count     int   // Indexes cloudCounts.
	lod       bool
	// view is what the last frame saw, from the frame decorator; selected
	// is the view the drawn points were selected for.
	view, selected nt3dclod.View
	have           bool
	drawn, visible int
	buffer         []nt3d.Point
}

// galaxyPoint is point i of n: a warm bulge of a ninth of them, the rest
// a disc of two spiral arms thinning outward, blue-white along the arms
// and warmer between, with a little dust of red.
func galaxyPoint(i int, n int) nt3d.Point {
	u := func(k int) float64 { return particleRandom(i, k) }
	gauss := func(k int) float64 {
		// Box-Muller from two of the point's fixed randoms.
		return math.Sqrt(-2*math.Log(max(1e-9, u(k)))) * math.Cos(2*math.Pi*u(k+1))
	}
	var p nt3dmath.Vec3
	var c color.RGBA
	if i%9 == 0 {
		r := .7 * math.Pow(u(1), 2)
		theta, phi := 2*math.Pi*u(2), math.Acos(2*u(3)-1)
		p = vec(r*math.Sin(phi)*math.Cos(theta), r*math.Sin(phi)*math.Sin(theta), .6*r*math.Cos(phi))
		c = mixRGBA(color.RGBA{255, 236, 200, 255}, color.RGBA{255, 200, 140, 255}, r/.7)
	} else {
		r := 4.5 * math.Pow(u(1), .6)
		arm := float64(i%2) * math.Pi
		spread := .25 + .12*r
		theta := arm + 1.7*math.Log1p(r) + spread*gauss(2)
		z := (.03 + .025*r) * gauss(4)
		p = vec(r*math.Cos(theta), r*math.Sin(theta), z)
		onArm := math.Exp(-math.Abs(spread*gauss(2)) * 2)
		c = mixRGBA(color.RGBA{235, 205, 165, 255}, color.RGBA{190, 212, 255, 255}, onArm)
		if u(6) < .04 {
			c = color.RGBA{255, 120, 110, 255}
		}
	}
	return nt3d.Point{Position: p, Radius: 1, Color: c}
}

func (d *demo) setupCloud() error {
	d.cloud = &cloudScene{count: 1, lod: true}
	most := cloudCounts[len(cloudCounts)-1]
	for i := range (most + nt3d.MaxPoints - 1) / nt3d.MaxPoints {
		id, err := d.scene.Add(nt3d.Root, "cloud"+strconv.Itoa(i))
		if err != nil {
			return err
		}
		d.cloud.nodes = append(d.cloud.nodes, id)
		d.cloud.installed = append(d.cloud.installed, -1)
	}
	return d.buildCloud()
}

// buildCloud makes the points of the chosen count, clusters them, and
// draws them for the last view.
func (d *demo) buildCloud() error {
	cs := d.cloud
	n := cloudCounts[cs.count]
	cs.points, cs.positions = cs.points[:0], cs.positions[:0]
	cs.bounds = nt3dmath.AABB{}
	for i := range n {
		p := galaxyPoint(i, n)
		cs.points = append(cs.points, p)
		cs.positions = append(cs.positions, p.Position)
		cs.bounds.Include(p.Position)
	}
	var err error
	if cs.set == nil {
		cs.set, err = nt3dclod.Build(cs.positions, nt3dclod.Options{ClusterSize: 256})
	} else {
		err = cs.set.Rebuild(cs.positions)
	}
	if err != nil {
		return err
	}
	cs.mean = cs.mean[:0]
	for c := range cs.set.Clusters {
		var r, g, b int
		items := cs.set.Items(c)
		for _, i := range items {
			r, g, b = r+int(cs.points[i].Color.R), g+int(cs.points[i].Color.G), b+int(cs.points[i].Color.B)
		}
		k := len(items)
		cs.mean = append(cs.mean, color.RGBA{uint8(r / k), uint8(g / k), uint8(b / k), 255})
	}
	return d.applyCloud()
}

// representative is the one point drawn for a far cluster: at its center,
// its mean color, sized by how many points it stands for.
func (cs *cloudScene) representative(c int) nt3d.Point {
	cl := cs.set.Clusters[c]
	return nt3d.Point{Position: cl.Center, Radius: float32(min(32, max(1, math.Sqrt(float64(cl.Count))/4))), Color: cs.mean[c]}
}

// applyCloud draws every point, or the selection for the last view, split
// across the scene's nodes.
func (d *demo) applyCloud() error {
	cs := d.cloud
	cs.selected = cs.view
	if !cs.lod || !cs.have {
		cs.buffer = append(cs.buffer[:0], cs.points...)
		cs.visible = len(cs.set.Clusters)
	} else {
		if err := cs.set.Select(cs.view, cloudLevels, &cs.sel); err != nil {
			return err
		}
		cs.buffer = nt3dclod.Gather(cs.buffer, cs.points, cs.sel.Items)
		for _, p := range cs.sel.Picks {
			if p.Count == 0 {
				cs.buffer = append(cs.buffer, cs.representative(int(p.Cluster)))
			}
		}
		cs.visible = len(cs.sel.Picks)
	}
	cs.drawn = len(cs.buffer)
	for i, id := range cs.nodes {
		lo := min(len(cs.buffer), i*nt3d.MaxPoints)
		chunk := cs.buffer[lo:min(len(cs.buffer), lo+nt3d.MaxPoints)]
		var err error
		switch {
		case len(chunk) == 0:
			if cs.installed[i] != -1 {
				err = d.scene.SetVisible(id, false)
				cs.installed[i] = -1
			}
		case cs.installed[i] == len(chunk):
			err = d.scene.SetNodePoints(id, chunk, cs.bounds)
		default:
			if err = d.scene.SetNodeGeometry(id, nt3d.Geometry{Points: chunk, PointStyle: nt3d.PointStyle{Shape: nt3d.HardPoint}}); err == nil {
				err = d.scene.SetVisible(id, true)
				cs.installed[i] = len(chunk)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// captureCloudView keeps what a frame sees, so the next step can select for it.
func (d *demo) captureCloudView(f *nt3d.Frame) {
	cs := d.cloud
	cs.view = nt3dclod.View{Matrix: f.Matrix, Eye: f.Eye, Perspective: f.Perspective, Width: f.Width, Height: f.Height, Margin: cloudMargin}
	cs.have = true
	f.Background = color.RGBA{6, 6, 12, 255}
}

// stepCloud reselects when the view changed since the points were
// selected, reporting whether it did.
func (d *demo) stepCloud() (bool, error) {
	cs := d.cloud
	if !cs.lod || !cs.have || cs.view == cs.selected {
		return false, nil
	}
	return true, d.applyCloud()
}

func (d *demo) cycleCloudCount() error {
	d.cloud.count = (d.cloud.count + 1) % len(cloudCounts)
	return d.buildCloud()
}

func (d *demo) toggleCloudLOD() error {
	d.cloud.lod = !d.cloud.lod
	return d.applyCloud()
}

// zoomCloud moves the camera in or out by the wheel's step.
func (d *demo) zoomCloud(in bool) {
	factor := 1 / .9
	if in {
		factor = .9
	}
	d.view.SetCamera(d.view.Camera().Zoom(factor))
}

// commas writes n with thousands separators.
func commas(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func (d *demo) cloudView(content string) string {
	cs := d.cloud
	lod := "LOD off"
	if cs.lod {
		lod = "LOD on"
	}
	return fmt.Sprintf("nt3d cloud · l LOD · n count · -/= zoom · d fog · 1/2/3 backend · q quit\n%s\n%s · %s · %s clusters · %s visible · %s / %s points · drag orbit · wheel zoom · r rotate · %s",
		content, d.rendering(), lod, commas(len(cs.set.Clusters)), commas(cs.visible), commas(cs.drawn), commas(len(cs.points)), d.status)
}
