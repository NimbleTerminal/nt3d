// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3dscene

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/geo"
	"nimbleterminal.dev/nt3d/math"
)

// stamford is the frame the test scenes are drawn in.
var stamford = nt3dgeo.ENU{Origin: nt3dgeo.Geodetic{Lat: 41.0465, Lon: -73.5418}}

// matrix writes a float64 transform as a tileset's "transform".
func matrix(m nt3dmath.Mat4d) string {
	parts := make([]string, 16)
	for i, v := range m {
		parts[i] = fmt.Sprintf("%.17g", v)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// glb is a binary glTF of the JSON and its one buffer.
func glb(json string, buffer []byte) []byte {
	for len(json)%4 != 0 {
		json += " "
	}
	for len(buffer)%4 != 0 {
		buffer = append(buffer, 0)
	}
	out := append([]byte("glTF"), 2, 0, 0, 0)
	out = binary.LittleEndian.AppendUint32(out, uint32(12+8+len(json)+8+len(buffer)))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(json)))
	out = append(append(out, "JSON"...), json...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(buffer)))
	return append(append(out, "BIN\x00"...), buffer...)
}

func floatBytes(values ...float32) []byte {
	var out []byte
	for _, v := range values {
		out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v))
	}
	return out
}

// triangle is a glb of one small triangle whose first vertex is at
// (east, north, up) in the tile's Z-up space, written in glTF's Y-up as
// (east, up, -north). node and root add JSON to the node and the document.
func triangle(east, north, up float32, node, root string) []byte {
	buffer := floatBytes(east, up, -north, east+1, up, -north, east, up+1, -north)
	return glb(fmt.Sprintf(`{"asset":{"version":"2.0"},%s"buffers":[{"byteLength":36}],"bufferViews":[{"buffer":0,"byteLength":36}],"accessors":[{"bufferView":0,"componentType":5126,"count":3,"type":"VEC3"}],"meshes":[{"primitives":[{"attributes":{"POSITION":0}}]}],"nodes":[{"mesh":0%s}],"scenes":[{"nodes":[0]}],"scene":0}`, root, node), buffer)
}

// splats is a glb of n Gaussian splats in a row along east.
func splats(n int) []byte {
	var position, rotation, scale, opacity, color []byte
	for i := range n {
		position = append(position, floatBytes(float32(i), 0, 0)...)
		rotation = append(rotation, floatBytes(0, 0, 0, 1)...)
		scale = append(scale, floatBytes(.5, .5, .5)...)
		opacity = append(opacity, floatBytes(.9)...)
		color = append(color, floatBytes(.5, .2, -.3)...)
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
	return glb(fmt.Sprintf(`{"asset":{"version":"2.0"},"extensionsUsed":["KHR_gaussian_splatting"],"buffers":[{"byteLength":%d}],"bufferViews":[%s],"accessors":[%s],"meshes":[{"primitives":[{"attributes":{"POSITION":0,"KHR_gaussian_splatting:ROTATION":1,"KHR_gaussian_splatting:SCALE":2,"KHR_gaussian_splatting:OPACITY":3,"KHR_gaussian_splatting:SH_DEGREE_0_COEF_0":4},"mode":0,"extensions":{"KHR_gaussian_splatting":{"kernel":"ellipse","colorSpace":"srgb_rec709_display"}}}]}],"nodes":[{"mesh":0}],"scenes":[{"nodes":[0]}],"scene":0}`,
		len(buffer), strings.Join(views, ","), strings.Join(accessors, ",")), buffer)
}

// b3dm wraps a glb with a feature table naming an RTC_CENTER.
func b3dm(center nt3dmath.Vec3d, inner []byte) []byte {
	table := fmt.Sprintf(`{"BATCH_LENGTH":0,"RTC_CENTER":[%.17g,%.17g,%.17g]}`, center.X, center.Y, center.Z)
	for len(table)%8 != 4 {
		table += " "
	}
	out := append([]byte("b3dm"), 1, 0, 0, 0)
	out = binary.LittleEndian.AppendUint32(out, uint32(28+len(table)+len(inner)))
	for _, n := range []int{len(table), 0, 0, 0} {
		out = binary.LittleEndian.AppendUint32(out, uint32(n))
	}
	return append(append(out, table...), inner...)
}

// files is a Fetcher over a map, which counts what was asked for and can
// hold chosen URIs back until they are released.
type files struct {
	mu      sync.Mutex
	data    map[string][]byte
	asked   map[string]int
	held    map[string]chan struct{}
	started chan string
}

func newFiles(data map[string][]byte) *files {
	return &files{data: data, asked: map[string]int{}, held: map[string]chan struct{}{}, started: make(chan string, 64)}
}

// hold makes fetches of uri wait until release(uri).
func (f *files) hold(uri string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.held[uri] = make(chan struct{})
}

func (f *files) release(uri string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.held[uri])
	delete(f.held, uri)
}

func (f *files) count(uri string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked[uri]
}

func (f *files) Fetch(ctx context.Context, uri string) ([]byte, error) {
	f.mu.Lock()
	f.asked[uri]++
	gate := f.held[uri]
	data, ok := f.data[uri]
	f.mu.Unlock()
	if gate != nil {
		select {
		case f.started <- uri:
		default:
		}
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if !ok {
		return nil, fmt.Errorf("no file %s", uri)
	}
	return data, nil
}

// shown returns the first URI of every tile the scene draws for camera, sorted.
func shown(t *testing.T, scene *nt3d.Scene, camera nt3d.Camera) []string {
	t.Helper()
	frame, err := scene.StillFrame(camera, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, d := range frame.Draws {
		if tile, ok := d.Data.(*Tile); ok && !d.Hidden {
			seen[tile.URIs[0]] = true
		}
	}
	var out []string
	for uri := range seen {
		out = append(out, uri)
	}
	sort.Strings(out)
	return out
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// lookAt is a perspective camera at eye looking at target, in the frame.
func lookAt(eye, target nt3dmath.Vec3) nt3d.Camera {
	c := nt3d.DefaultCamera().LookAt(eye, target)
	c.Projection = nt3d.Perspective
	return c
}

// block is a tileset whose root tile is a 100 m square block at the frame's
// origin with error 10, replaced by a west and an east half with error 0.
// The root's transform is the frame's own, so tile coordinates are east,
// north, up of the frame's origin.
func block(refine string) map[string][]byte {
	return map[string][]byte{
		"city/tileset.json": []byte(`{"asset":{"version":"1.1","copyright":"© City Survey"},"geometricError":100,"root":{
			"transform":` + matrix(stamford.ToECEF()) + `,
			"boundingVolume":{"box":[0,0,5, 50,0,0, 0,50,0, 0,0,5]},"geometricError":10,"refine":"` + refine + `",
			"content":{"uri":"root.glb"},
			"children":[
				{"boundingVolume":{"box":[-25,0,5, 25,0,0, 0,50,0, 0,0,5]},"geometricError":0,"content":{"uri":"tiles/west.glb"}},
				{"boundingVolume":{"box":[25,0,5, 25,0,0, 0,50,0, 0,0,5]},"geometricError":0,"content":{"uri":"tiles/east.glb"}}]}}`),
		"city/root.glb":       triangle(0, 0, 0, "", ""),
		"city/tiles/west.glb": triangle(-25, 0, 0, "", ""),
		"city/tiles/east.glb": triangle(25, 0, 0, "", ""),
	}
}

// At 100 pixels tall and 45 degrees, an error of 10 m is 16 pixels at 75 m:
// nearer than that a tile wants its children.
var (
	farAway = lookAt(nt3dmath.Vec3{X: 0, Y: -400, Z: 100}, nt3dmath.Vec3{Z: 5})
	nearBy  = lookAt(nt3dmath.Vec3{X: 0, Y: -60, Z: 30}, nt3dmath.Vec3{Z: 5})
)

func open(t *testing.T, scene *nt3d.Scene, f *files, uri string, o Options) *Tileset {
	t.Helper()
	o.Frame = stamford
	ts, err := Open(context.Background(), scene, f, uri, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ts.Close)
	return ts
}

func settle(t *testing.T, ts *Tileset, camera nt3d.Camera) {
	t.Helper()
	if err := ts.Settle(context.Background(), camera, 100, 100); err != nil {
		t.Fatal(err)
	}
}
