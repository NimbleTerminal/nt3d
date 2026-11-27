// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/geo"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/tiles3d"
	"nimbleterminal.dev/nt3d/tiles3d/scene"
)

// TilesMain streams a 3D Tiles tileset into the scene: the town generated
// below, or the tileset.json named on the command line by URL or path.
func TilesMain() { mainDemo(false, sceneTiles) }

// tilesScene is the streaming demo's state.
type tilesScene struct {
	tileset *nt3dtiles3dscene.Tileset
	// The size of the last frame drawn, which the next selection is for.
	width, height atomic.Int64
	stats         nt3dtiles3dscene.Stats
	radius        float32
}

// tilesFrame is where the generated town stands: the frame the scene is
// drawn in, east, north and up of a point on the Earth.
var tilesFrame = nt3dgeo.ENU{Origin: nt3dgeo.Geodetic{Lat: 41.0534, Lon: -73.5387, Height: 10}}

func (d *demo) setupTiles(args []string) error {
	fetch, uri := townTiles(), "town/tileset.json"
	options := nt3dtiles3dscene.Options{Frame: tilesFrame, Parent: d.group}
	if len(args) > 0 {
		// A tileset named on the command line, by URL or by path, drawn
		// around the middle of its own bounds.
		fetch, uri, options.AtCenter = &nt3dtiles3d.HTTP{}, args[0], true
		if !strings.HasPrefix(uri, "http://") && !strings.HasPrefix(uri, "https://") {
			fetch, uri = nt3dtiles3d.FS(os.DirFS(filepath.Dir(uri))), filepath.Base(uri)
		}
	}
	tileset, err := nt3dtiles3dscene.Open(context.Background(), d.scene, fetch, uri, options)
	if err != nil {
		return err
	}
	d.tiles = &tilesScene{tileset: tileset, radius: 150}
	if box, ok := tileset.Bounds(); ok {
		d.tiles.radius = float32(box.Radius())
	}
	return nil
}

// tilesCamera looks at the tileset from above its south-east, near enough
// that the tiles in front are fine and those behind stay coarse.
func (d *demo) tilesCamera(camera *nt3d.Camera) {
	camera.Projection, camera.Elevation, camera.Azimuth = nt3d.Perspective, 24, -55
	camera.Target, camera.Distance = nt3dmath.Vec3{Z: 10}, float64(d.tiles.radius)*1.6
}

// stepTiles makes the scene show what the camera needs now, and reports
// whether anything about the tileset changed.
func (d *demo) stepTiles() (bool, error) {
	ts := d.tiles
	width, height := int(ts.width.Load()), int(ts.height.Load())
	if width < 1 || height < 1 {
		width, height = snapshotSize[0], snapshotSize[1]
	}
	if err := ts.tileset.Update(d.view.Camera(), width, height); err != nil {
		return false, err
	}
	stats := ts.tileset.Stats()
	changed := stats.Selected != ts.stats.Selected || stats.Resident != ts.stats.Resident || stats.Loading != ts.stats.Loading
	ts.stats = stats
	d.status = tilesStatus(ts.tileset)
	return changed, nil
}

// settleTiles waits until the scene shows everything the camera needs at a
// picture of width by height: what a still picture is taken of.
func (d *demo) settleTiles(width, height int) error {
	if err := d.tiles.tileset.Settle(context.Background(), d.view.Camera(), width, height); err != nil {
		return err
	}
	d.status = tilesStatus(d.tiles.tileset)
	return nil
}

func tilesStatus(ts *nt3dtiles3dscene.Tileset) string {
	s := ts.Stats()
	status := fmt.Sprintf("3D Tiles · %d shown of %d loaded · %d loading · %s splats", s.Selected, s.Resident, s.Loading, commas(s.SplatsSelected))
	if s.Failed > 0 {
		status += fmt.Sprintf(" · %d failed", s.Failed)
	}
	if credits := ts.Credits(); len(credits) > 0 {
		status += " · " + strings.Join(credits, ", ")
	}
	return status
}

// tilesView is the demo's screen: its keys, the picture, and what the
// tileset is doing.
func (d *demo) tilesView(content string) string {
	return fmt.Sprintf("nt3d 3D Tiles · -/= zoom · c fit · d fog · 1/2/3 backend · q quit\n%s\n%s · drag orbit · wheel zoom · %s", content, d.rendering(), d.status)
}

// captureTilesView notes the size of the frame being drawn.
func (d *demo) captureTilesView(f *nt3d.Frame) {
	d.tiles.width.Store(int64(f.Width))
	d.tiles.height.Store(int64(f.Height))
}

// The generated town: a 240 m square of 64 lots, written as a tileset of
// three levels. The root masses the lots four at a time, its four children
// draw each building as a block, and their sixteen children give each
// building a base, a tower and a roof. The middle lots are a park of
// Gaussian splats, which the finest tiles add as a second content.
const (
	townLots = 8   // lots a side
	townLot  = 30. // meters a lot
)

// townTiles builds the town's tileset in memory.
func townTiles() nt3dtiles3d.Fetcher {
	files := map[string][]byte{}
	half := townLots * townLot / 2
	tile := func(level, x, y int) string {
		side := townLots >> level // lots a tile side
		size := float64(side) * townLot
		cx, cy := -half+(float64(x)+.5)*size, -half+(float64(y)+.5)*size
		name := fmt.Sprintf("%d_%d_%d", level, x, y)
		files["town/tiles/"+name+".glb"] = townBlock(level, x*side, y*side, side)
		contents := fmt.Sprintf(`"content":{"uri":"tiles/%s.glb"}`, name)
		if level == 2 && (x == 1 || x == 2) && (y == 1 || y == 2) {
			files["town/tiles/"+name+"_park.glb"] = townPark(x, y)
			contents = fmt.Sprintf(`"contents":[{"uri":"tiles/%s.glb"},{"uri":"tiles/%s_park.glb"}]`, name, name)
		}
		return fmt.Sprintf(`"boundingVolume":{"box":[%g,%g,25, %g,0,0, 0,%g,0, 0,0,25]},"geometricError":%g,%s`, cx, cy, size/2, size/2, [3]float64{24, 4, 0}[level], contents)
	}
	var quarters []string
	for qy := range 2 {
		for qx := range 2 {
			var leaves []string
			for ly := range 2 {
				for lx := range 2 {
					leaves = append(leaves, "{"+tile(2, 2*qx+lx, 2*qy+ly)+"}")
				}
			}
			quarters = append(quarters, "{"+tile(1, qx, qy)+`,"children":[`+strings.Join(leaves, ",")+"]}")
		}
	}
	var transform []string
	for _, v := range tilesFrame.ToECEF() {
		transform = append(transform, fmt.Sprintf("%.17g", v))
	}
	files["town/tileset.json"] = []byte(`{"asset":{"version":"1.1","copyright":"Generated town, nt3d demos"},"geometricError":60,"root":{"transform":[` +
		strings.Join(transform, ",") + `],"refine":"REPLACE",` + tile(0, 0, 0) + `,"children":[` + strings.Join(quarters, ",") + `]}}`)
	return nt3dtiles3d.FetcherFunc(func(_ context.Context, uri string) ([]byte, error) {
		if data, ok := files[uri]; ok {
			return data, nil
		}
		return nil, fmt.Errorf("no file %s", uri)
	})
}

// townHash is a lot's own number, the same every run.
func townHash(x, y, salt int) float64 {
	h := uint32(x*73856093) ^ uint32(y*19349663) ^ uint32(salt*83492791)
	h ^= h >> 13
	h *= 0x5bd1e995
	h ^= h >> 15
	return float64(h%10000) / 10000
}

// townBuilding is the building on lot (x, y): its height and color. The
// four middle lots are the park and have none.
func townBuilding(x, y int) (height float64, rgb [3]uint8, ok bool) {
	if (x == 3 || x == 4) && (y == 3 || y == 4) {
		return 0, rgb, false
	}
	height = 8 + 38*townHash(x, y, 1)*townHash(x, y, 2)
	palette := [][3]uint8{{196, 172, 148}, {150, 162, 178}, {186, 120, 96}, {214, 204, 180}, {120, 136, 150}}
	return height, palette[int(townHash(x, y, 3)*float64(len(palette)))%len(palette)], true
}

// townBlock is the mesh of the lots from (x0, y0), side a side, at a level
// of detail: the ground under them, and their buildings massed four lots at
// a time (0), as blocks (1), or with a base, a tower and a roof (2).
func townBlock(level, x0, y0, side int) []byte {
	var m glbMesh
	half := townLots * townLot / 2
	corner := func(x, y int) (float64, float64) { return -half + float64(x)*townLot, -half + float64(y)*townLot }
	wx, wy := corner(x0, y0)
	ex, ey := corner(x0+side, y0+side)
	m.box(wx, wy, -.5, ex, ey, 0, [3]uint8{92, 104, 92})
	if level == 0 {
		// One mass for each two-by-two of lots: their mean height and color.
		for y := y0; y < y0+side; y += 2 {
			for x := x0; x < x0+side; x += 2 {
				var height float64
				var rgb [3]float64
				n := 0.
				for dy := range 2 {
					for dx := range 2 {
						if h, c, ok := townBuilding(x+dx, y+dy); ok {
							height += h
							n++
							for k := range rgb {
								rgb[k] += float64(c[k])
							}
						}
					}
				}
				if n == 0 {
					continue
				}
				ax, ay := corner(x, y)
				m.box(ax+8, ay+8, 0, ax+2*townLot-8, ay+2*townLot-8, height/n, [3]uint8{uint8(rgb[0] / n), uint8(rgb[1] / n), uint8(rgb[2] / n)})
			}
		}
		return m.bytes()
	}
	for y := y0; y < y0+side; y++ {
		for x := x0; x < x0+side; x++ {
			height, rgb, ok := townBuilding(x, y)
			if !ok {
				ax, ay := corner(x, y)
				m.box(ax+1, ay+1, 0, ax+townLot-1, ay+townLot-1, .2, [3]uint8{74, 128, 70}) // The park's lawn.
				continue
			}
			ax, ay := corner(x, y)
			inset := 5 + 3*townHash(x, y, 4)
			if level == 1 {
				m.box(ax+inset, ay+inset, 0, ax+townLot-inset, ay+townLot-inset, height, rgb)
				continue
			}
			dark := [3]uint8{uint8(int(rgb[0]) * 3 / 4), uint8(int(rgb[1]) * 3 / 4), uint8(int(rgb[2]) * 3 / 4)}
			m.box(ax+inset-1.5, ay+inset-1.5, 0, ax+townLot-inset+1.5, ay+townLot-inset+1.5, 4, dark)
			m.box(ax+inset, ay+inset, 4, ax+townLot-inset, ay+townLot-inset, height, rgb)
			m.box(ax+inset+3, ay+inset+3, height, ax+townLot-inset-3, ay+townLot-inset-3, height+2.5, dark)
		}
	}
	return m.bytes()
}

// townPark is the trees of the park's quarter in the finest tile (x, y):
// balls of Gaussian splats on trunks of them.
func townPark(x, y int) []byte {
	var position, rotation, scale, opacity, color []byte
	n := 0
	splat := func(px, py, pz, size float64, rgb [3]float64) {
		// Tile space is east, north, up; glTF is Y-up: (east, up, -north).
		position = appendFloats32(position, px, pz, -py)
		rotation = appendFloats32(rotation, 0, 0, 0, 1)
		scale = appendFloats32(scale, size, size, size)
		opacity = appendFloats32(opacity, .85)
		// The color at rest is 0.5 + Y00 times the coefficient.
		color = appendFloats32(color, (rgb[0]-.5)/.2820948, (rgb[1]-.5)/.2820948, (rgb[2]-.5)/.2820948)
		n++
	}
	// The park is the town's middle four lots, a quarter in each of the
	// four middle tiles: 15 m from the middle, toward this tile.
	tx, ty := float64(2*x-3)*15, float64(2*y-3)*15
	for tree := range 5 {
		ox, oy := tx+(townHash(x*8+tree, y, 5)-.5)*20, ty+(townHash(x, y*8+tree, 6)-.5)*20
		top := 7 + 5*townHash(x+tree, y+tree, 7)
		for k := range 6 {
			splat(ox, oy, top*float64(k)/6, .35, [3]float64{.36, .25, .16})
		}
		for k := range 90 {
			a, b := townHash(tree, k, 8+x)*2*math.Pi, math.Acos(2*townHash(k, tree, 9+y)-1)
			r := 3.2 * math.Cbrt(townHash(k+tree, x+y, 10))
			green := .45 + .3*townHash(k, tree, 11)
			splat(ox+r*math.Sin(b)*math.Cos(a), oy+r*math.Sin(b)*math.Sin(a), top+r*math.Cos(b), .9, [3]float64{.16, green, .14})
		}
	}
	var buffer []byte
	var views, accessors []string
	for _, part := range []struct {
		data []byte
		kind string
	}{{position, "VEC3"}, {rotation, "VEC4"}, {scale, "VEC3"}, {opacity, "SCALAR"}, {color, "VEC3"}} {
		views = append(views, fmt.Sprintf(`{"buffer":0,"byteOffset":%d,"byteLength":%d}`, len(buffer), len(part.data)))
		accessors = append(accessors, fmt.Sprintf(`{"bufferView":%d,"componentType":5126,"count":%d,"type":%q}`, len(views)-1, n, part.kind))
		buffer = append(buffer, part.data...)
	}
	return glbFile(fmt.Sprintf(`{"asset":{"version":"2.0"},"extensionsUsed":["KHR_gaussian_splatting"],"buffers":[{"byteLength":%d}],"bufferViews":[%s],"accessors":[%s],"meshes":[{"primitives":[{"attributes":{"POSITION":0,"KHR_gaussian_splatting:ROTATION":1,"KHR_gaussian_splatting:SCALE":2,"KHR_gaussian_splatting:OPACITY":3,"KHR_gaussian_splatting:SH_DEGREE_0_COEF_0":4},"mode":0,"extensions":{"KHR_gaussian_splatting":{"kernel":"ellipse","colorSpace":"srgb_rec709_display"}}}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`,
		len(buffer), strings.Join(views, ","), strings.Join(accessors, ",")), buffer)
}

func appendFloats32(out []byte, values ...float64) []byte {
	for _, v := range values {
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(v)))
	}
	return out
}

// glbMesh gathers colored boxes into one glTF mesh.
type glbMesh struct {
	positions, colors, indices []byte
	vertices                   uint32
}

// box adds the box from (x0, y0, z0) to (x1, y1, z1), in east, north, up.
func (m *glbMesh) box(x0, y0, z0, x1, y1, z1 float64, rgb [3]uint8) {
	corners := [8][3]float64{{x0, y0, z0}, {x1, y0, z0}, {x1, y1, z0}, {x0, y1, z0}, {x0, y0, z1}, {x1, y0, z1}, {x1, y1, z1}, {x0, y1, z1}}
	// Each face, counterclockwise seen from outside.
	for _, face := range [6][4]int{{0, 3, 2, 1}, {4, 5, 6, 7}, {0, 1, 5, 4}, {1, 2, 6, 5}, {2, 3, 7, 6}, {3, 0, 4, 7}} {
		for _, c := range face {
			p := corners[c]
			m.positions = appendFloats32(m.positions, p[0], p[2], -p[1]) // Y-up: (east, up, -north).
			m.colors = append(m.colors, rgb[0], rgb[1], rgb[2], 255)
		}
		for _, k := range [6]uint32{0, 1, 2, 0, 2, 3} {
			m.indices = binary.LittleEndian.AppendUint32(m.indices, m.vertices+k)
		}
		m.vertices += 4
	}
}

// bytes writes the mesh as a binary glTF.
func (m *glbMesh) bytes() []byte {
	buffer := append(append(append([]byte(nil), m.positions...), m.colors...), m.indices...)
	return glbFile(fmt.Sprintf(`{"asset":{"version":"2.0"},"buffers":[{"byteLength":%d}],"bufferViews":[{"buffer":0,"byteLength":%d},{"buffer":0,"byteOffset":%d,"byteLength":%d},{"buffer":0,"byteOffset":%d,"byteLength":%d}],"accessors":[{"bufferView":0,"componentType":5126,"count":%d,"type":"VEC3"},{"bufferView":1,"componentType":5121,"normalized":true,"count":%d,"type":"VEC4"},{"bufferView":2,"componentType":5125,"count":%d,"type":"SCALAR"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0,"COLOR_0":1},"indices":2}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`,
		len(buffer), len(m.positions), len(m.positions), len(m.colors), len(m.positions)+len(m.colors), len(m.indices), m.vertices, m.vertices, len(m.indices)/4), buffer)
}

// glbFile is a binary glTF of a document and its one buffer.
func glbFile(document string, buffer []byte) []byte {
	for len(document)%4 != 0 {
		document += " "
	}
	for len(buffer)%4 != 0 {
		buffer = append(buffer, 0)
	}
	out := append([]byte("glTF"), 2, 0, 0, 0)
	out = binary.LittleEndian.AppendUint32(out, uint32(12+8+len(document)+8+len(buffer)))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(document)))
	out = append(append(out, "JSON"...), document...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(buffer)))
	return append(append(out, "BIN\x00"...), buffer...)
}
