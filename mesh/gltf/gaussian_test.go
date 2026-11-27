// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/sh"
)

// splatAttr is one attribute of a test splat primitive: its accessor type
// and component type, and its values, one row a splat.
type splatAttr struct {
	name       string
	kind       string // SCALAR, VEC3, VEC4
	component  int    // 5126 float, 5120 byte, 5121 ubyte, 5122 short, 5123 ushort
	normalized bool
	rows       [][]float64 // Written as the component type, unconverted.
}

func floats(name, kind string, rows ...[]float64) splatAttr {
	return splatAttr{name: name, kind: kind, component: 5126, rows: rows}
}

// splatDoc is a glTF of one node showing one splat primitive, its buffer in
// side.bin. edit may change the document before it is written.
func splatDoc(t testing.TB, attrs []splatAttr, extension string, node string) (doc []byte, files map[string][]byte) {
	t.Helper()
	var buffer []byte
	var views, accessors, names []string
	for i, a := range attrs {
		start := len(buffer)
		for _, row := range a.rows {
			for _, v := range row {
				switch a.component {
				case 5126:
					buffer = binary.LittleEndian.AppendUint32(buffer, math.Float32bits(float32(v)))
				case 5120:
					buffer = append(buffer, byte(int8(v)))
				case 5121:
					buffer = append(buffer, byte(v))
				case 5122:
					buffer = binary.LittleEndian.AppendUint16(buffer, uint16(int16(v)))
				case 5123:
					buffer = binary.LittleEndian.AppendUint16(buffer, uint16(v))
				}
			}
		}
		length := len(buffer) - start
		for len(buffer)%4 != 0 {
			buffer = append(buffer, 0)
		}
		views = append(views, fmt.Sprintf(`{"buffer":0,"byteOffset":%d,"byteLength":%d}`, start, length))
		accessors = append(accessors, fmt.Sprintf(`{"bufferView":%d,"componentType":%d,"normalized":%v,"count":%d,"type":%q}`, i, a.component, a.normalized, len(a.rows), a.kind))
		names = append(names, fmt.Sprintf(`%q:%d`, a.name, i))
	}
	if node == "" {
		node = `{"mesh":0}`
	}
	text := fmt.Sprintf(`{"asset":{"version":"2.0"},"extensionsUsed":["KHR_gaussian_splatting"],"buffers":[{"uri":"side.bin","byteLength":%d}],"bufferViews":[%s],"accessors":[%s],"meshes":[{"primitives":[{"attributes":{%s},"mode":0,"extensions":{"KHR_gaussian_splatting":%s}}]}],"nodes":[%s],"scenes":[{"nodes":[0]}],"scene":0}`,
		len(buffer), strings.Join(views, ","), strings.Join(accessors, ","), strings.Join(names, ","), extension, node)
	return []byte(text), map[string][]byte{"side.bin": buffer}
}

const ellipse = `{"kernel":"ellipse","colorSpace":"srgb_rec709_display"}`

// gaussianVector is the Gaussian of geo/testdata/geo-vectors.json, the one
// a writer of splat tiles is tested against too.
type gaussianVector struct {
	Position [3]float64
	Rotation [4]float64
	Scale    [3]float64
	Opacity  float64
	SH       [][3]float64
	Diffuse  [3]float64
	Axes     [3][3]float64
	ZUp      struct {
		Position [3]float64
		Axes     [3][3]float64
	}
}

func readGaussianVector(t *testing.T) gaussianVector {
	t.Helper()
	raw, err := os.ReadFile("../../geo/testdata/geo-vectors.json")
	check(t, err)
	var file struct{ Gaussian gaussianVector }
	check(t, json.Unmarshal(raw, &file))
	if len(file.Gaussian.SH) != 16 {
		t.Fatalf("the shared vector has %d SH coefficients, want 16", len(file.Gaussian.SH))
	}
	return file.Gaussian
}

// vectorAttrs writes v as the extension's attributes, with SH up to degree.
func vectorAttrs(v gaussianVector, degree int) []splatAttr {
	attrs := []splatAttr{
		floats("POSITION", "VEC3", v.Position[:]),
		floats("KHR_gaussian_splatting:ROTATION", "VEC4", v.Rotation[:]),
		floats("KHR_gaussian_splatting:SCALE", "VEC3", v.Scale[:]),
		floats("KHR_gaussian_splatting:OPACITY", "SCALAR", []float64{v.Opacity}),
	}
	at := 0
	for l := 0; l <= degree; l++ {
		for m := 0; m < 2*l+1; m++ {
			attrs = append(attrs, floats(fmt.Sprintf("KHR_gaussian_splatting:SH_DEGREE_%d_COEF_%d", l, m), "VEC3", v.SH[at][:]))
			at++
		}
	}
	return attrs
}

func vec3(v [3]float64) nt3dmath.Vec3 {
	return nt3dmath.Vec3{X: float32(v[0]), Y: float32(v[1]), Z: float32(v[2])}
}

func decodeSplats(t *testing.T, doc []byte, files map[string][]byte, o *nt3dmesh.Options) *nt3dmesh.Model {
	t.Helper()
	if o == nil {
		o = &nt3dmesh.Options{}
	}
	o.Open = opener(files)
	m, err := Decode(doc, o)
	check(t, err)
	return m
}

// A KHR_gaussian_splatting primitive becomes a Gaussian cloud that reads the
// shared vector as the extension defines it: an x, y, z, w rotation, linear
// scales, linear opacity, and SH coefficients in attribute order, with the
// color at rest 0.5 + Y00 * the degree-0 coefficient. The cloud is turned
// from glTF's Y-up to Z-up once, harmonics included.
func TestGaussianSplattingReadsTheSharedVector(t *testing.T) {
	v := readGaussianVector(t)
	doc, files := splatDoc(t, vectorAttrs(v, 3), ellipse, "")
	m := decodeSplats(t, doc, files, nil)
	if len(m.Objects) != 1 {
		t.Fatalf("%d objects, want one cloud", len(m.Objects))
	}
	g := m.Objects[0].Geometry
	if len(g.Points) != 1 || g.PointStyle.Shape != nt3d.GaussianPoint || g.PointStyle.Units != nt3d.WorldUnits || g.PointStyle.Blend != nt3d.AlphaBlend {
		t.Fatalf("%d points styled %+v, want one world-sized blended Gaussian", len(g.Points), g.PointStyle)
	}
	p := g.Points[0]
	if !near(p.Position, vec3(v.ZUp.Position)) {
		t.Fatalf("position %v, want %v", p.Position, v.ZUp.Position)
	}
	for k := range 3 {
		if !near(p.GaussianAxes[k], vec3(v.ZUp.Axes[k])) {
			t.Fatalf("axis %d is %v, want %v", k, p.GaussianAxes[k], v.ZUp.Axes[k])
		}
	}
	want := [4]uint8{uint8(math.Round(255 * v.Diffuse[0])), uint8(math.Round(255 * v.Diffuse[1])), uint8(math.Round(255 * v.Diffuse[2])), uint8(math.Round(255 * v.Opacity))}
	if got := [4]uint8{p.Color.R, p.Color.G, p.Color.B, p.Color.A}; got != want {
		t.Fatalf("color %v, want %v", got, want)
	}
	if len(g.GaussianSH) != 16 {
		t.Fatalf("%d SH coefficients, want 16", len(g.GaussianSH))
	}
	for i, c := range g.GaussianSH {
		for k := range 3 {
			if c[k] != float32(v.SH[i][k]) {
				t.Fatalf("SH coefficient %d is %v, want %v", i, c, v.SH[i])
			}
		}
	}
	// Seen along a direction of the Z-up world, the splat's color is what
	// its coefficients give along the same direction in glTF's frame.
	world := nt3dmath.Vec3{X: .3, Y: -.5, Z: .8}.Normalize()
	inFile := nt3dmath.Vec3{X: world.X, Y: world.Z, Z: -world.Y}
	frame := g.GaussianSHTransform
	if frame == (nt3dmath.Mat4{}) {
		frame = nt3dmath.Identity()
	}
	got, wantColor := nt3dsh.Evaluate(g.GaussianSH, frame.Direction(world).Normalize()), nt3dsh.Evaluate(g.GaussianSH, inFile)
	for k := range 3 {
		if math.Abs(float64(got[k]-wantColor[k])) > 1e-5 {
			t.Fatalf("view-dependent color %v, want %v", got, wantColor)
		}
	}
}

// The extension's real harmonics are those nt3d evaluates: degree 0 is the
// constant Y00, and degree 1 is -y, z, -x times sqrt(3/(4π)), in attribute
// order.
func TestGaussianSplattingHarmonicsFollowTheExtensionsBasis(t *testing.T) {
	c := []nt3dsh.RGB{{1, 1, 1}, {2, 0, 0}, {0, 3, 0}, {0, 0, 5}}
	d := nt3dmath.Vec3{X: .6, Y: -.48, Z: .64}
	k := math.Sqrt(3 / (4 * math.Pi))
	want := [3]float64{nt3dsh.Y00 + 2*-k*float64(d.Y), nt3dsh.Y00 + 3*k*float64(d.Z), nt3dsh.Y00 + 5*-k*float64(d.X)}
	got := nt3dsh.Evaluate(c, d)
	for i := range 3 {
		if math.Abs(float64(got[i])-want[i]) > 1e-6 {
			t.Fatalf("harmonics give %v, want %v", got, want)
		}
	}
}

// A node's rotation turns the splat with it: position, axes, and the frame
// its harmonics are evaluated in, so the color along a world direction is the
// file's color along that direction carried back into the primitive.
func TestGaussianSplattingTurnsWithItsNode(t *testing.T) {
	v := readGaussianVector(t)
	// A quarter turn about glTF's +Y, and a move.
	node := `{"mesh":0,"rotation":[0,0.7071067811865476,0,0.7071067811865476],"translation":[10,0,0]}`
	doc, files := splatDoc(t, vectorAttrs(v, 3), ellipse, node)
	g := decodeSplats(t, doc, files, nil).Objects[0].Geometry
	// (1,2,3) turned about Y is (3,2,-1), moved to (13,2,-1), and Z-up (13,1,2).
	if p := g.Points[0].Position; !near(p, nt3dmath.Vec3{X: 13, Y: 1, Z: 2}) {
		t.Fatalf("position %v, want (13, 1, 2)", p)
	}
	// The first axis, (0,2,0) in the primitive, is along the turn's own
	// axis: still glTF +Y, which is world +Z.
	if a := g.Points[0].GaussianAxes[0]; !near(a, nt3dmath.Vec3{Z: 2}) {
		t.Fatalf("first axis %v, want (0, 0, 2)", a)
	}
	world := nt3dmath.Vec3{X: .3, Y: -.5, Z: .8}.Normalize()
	gltfWorld := nt3dmath.Vec3{X: world.X, Y: world.Z, Z: -world.Y}
	// Undo the quarter turn about Y: (x,y,z) -> (-z,y,x).
	primitive := nt3dmath.Vec3{X: -gltfWorld.Z, Y: gltfWorld.Y, Z: gltfWorld.X}
	got, want := nt3dsh.Evaluate(g.GaussianSH, g.GaussianSHTransform.Direction(world).Normalize()), nt3dsh.Evaluate(g.GaussianSH, primitive)
	for k := range 3 {
		if math.Abs(float64(got[k]-want[k])) > 1e-5 {
			t.Fatalf("view-dependent color %v, want %v", got, want)
		}
	}
}

// The same cloud written as a Y-up Gaussian PLY and as a glTF splat
// primitive decodes to the same geometry, so a splat tile draws as its
// source file does and is turned to Z-up exactly once.
func TestGaussianSplattingMatchesAGaussianPLY(t *testing.T) {
	v := readGaussianVector(t)
	names := []string{"x", "y", "z", "f_dc_0", "f_dc_1", "f_dc_2", "opacity", "scale_0", "scale_1", "scale_2", "rot_0", "rot_1", "rot_2", "rot_3"}
	logit := math.Log(v.Opacity / (1 - v.Opacity))
	values := []float64{v.Position[0], v.Position[1], v.Position[2], v.SH[0][0], v.SH[0][1], v.SH[0][2], logit,
		math.Log(v.Scale[0]), math.Log(v.Scale[1]), math.Log(v.Scale[2]), v.Rotation[3], v.Rotation[0], v.Rotation[1], v.Rotation[2]}
	// f_rest is channel by channel; the attributes are coefficient by coefficient.
	for channel := range 3 {
		for j := 1; j < 16; j++ {
			names = append(names, fmt.Sprintf("f_rest_%d", (channel)*15+j-1))
			values = append(values, v.SH[j][channel])
		}
	}
	var ply strings.Builder
	ply.WriteString("ply\nformat ascii 1.0\nelement vertex 1\n")
	for _, n := range names {
		ply.WriteString("property float " + n + "\n")
	}
	ply.WriteString("end_header\n")
	for _, x := range values {
		fmt.Fprintf(&ply, "%.9g ", x)
	}
	ply.WriteString("\n")
	fromPLY, err := nt3dmesh.DecodeGaussianPLY([]byte(ply.String()), &nt3dmesh.Options{Up: nt3dmesh.YUp})
	check(t, err)
	doc, files := splatDoc(t, vectorAttrs(v, 3), ellipse, "")
	a, b := fromPLY.Objects[0].Geometry, decodeSplats(t, doc, files, nil).Objects[0].Geometry
	if !near(a.Points[0].Position, b.Points[0].Position) || a.Points[0].Color != b.Points[0].Color {
		t.Fatalf("PLY %+v, glTF %+v", a.Points[0], b.Points[0])
	}
	for k := range 3 {
		if !near(a.Points[0].GaussianAxes[k], b.Points[0].GaussianAxes[k]) {
			t.Fatalf("axis %d: PLY %v, glTF %v", k, a.Points[0].GaussianAxes[k], b.Points[0].GaussianAxes[k])
		}
	}
	for d := range 6 {
		world := nt3dmath.Vec3{X: float32(d) - 2, Y: 1.5 - float32(d%3), Z: float32(d%2) + .25}.Normalize()
		ca := nt3dsh.Evaluate(a.GaussianSH, a.GaussianSHTransform.Direction(world).Normalize())
		cb := nt3dsh.Evaluate(b.GaussianSH, b.GaussianSHTransform.Direction(world).Normalize())
		for k := range 3 {
			if math.Abs(float64(ca[k]-cb[k])) > 1e-5 {
				t.Fatalf("along %v the PLY is %v and the glTF %v", world, ca, cb)
			}
		}
	}
}

// Rotation, scale, and opacity may be stored as normalized integers, and
// scale as plain ones, as the extension allows; harmonics may stop at any
// whole degree.
func TestGaussianSplattingReadsIntegerAttributes(t *testing.T) {
	attrs := []splatAttr{
		floats("POSITION", "VEC3", []float64{0, 0, 0}, []float64{1, 0, 0}),
		{name: "KHR_gaussian_splatting:ROTATION", kind: "VEC4", component: 5122, normalized: true, rows: [][]float64{{0, 0, 0, 32767}, {0, 0, 23170, 23170}}},
		{name: "KHR_gaussian_splatting:SCALE", kind: "VEC3", component: 5121, rows: [][]float64{{4, 2, 1}, {4, 2, 1}}},
		{name: "KHR_gaussian_splatting:OPACITY", kind: "SCALAR", component: 5121, normalized: true, rows: [][]float64{{255}, {51}}},
		floats("KHR_gaussian_splatting:SH_DEGREE_0_COEF_0", "VEC3", []float64{0, 0, 0}, []float64{0, 0, 0}),
	}
	doc, files := splatDoc(t, attrs, ellipse, "")
	g := decodeSplats(t, doc, files, nil).Objects[0].Geometry
	if len(g.Points) != 2 || len(g.GaussianSH) != 2 {
		t.Fatalf("%d points, %d coefficients", len(g.Points), len(g.GaussianSH))
	}
	// Unturned: scales 4, 2, 1 along glTF x, y, z, which are world x, z, -y.
	if a := g.Points[0].GaussianAxes; !near(a[0], nt3dmath.Vec3{X: 4}) || !near(a[1], nt3dmath.Vec3{Z: 2}) || !near(a[2], nt3dmath.Vec3{Y: -1}) {
		t.Fatalf("axes %v", a)
	}
	// A quarter turn about glTF +Z: x goes to y, which is world +Z.
	if a := g.Points[1].GaussianAxes[0]; math.Abs(float64(a.Z-4)) > 1e-3 || math.Abs(float64(a.X)) > 1e-3 {
		t.Fatalf("turned axis %v, want (0, 0, 4)", a)
	}
	if g.Points[0].Color.A != 255 || g.Points[1].Color.A != 51 {
		t.Fatalf("alphas %d and %d, want 255 and 51", g.Points[0].Color.A, g.Points[1].Color.A)
	}
	v := readGaussianVector(t)
	for degree, count := range []int{1, 4, 9, 16} {
		doc, files := splatDoc(t, vectorAttrs(v, degree), ellipse, "")
		if g := decodeSplats(t, doc, files, nil).Objects[0].Geometry; len(g.GaussianSH) != count {
			t.Errorf("degree %d: %d coefficients, want %d", degree, len(g.GaussianSH), count)
		}
	}
}

// A splat primitive that breaks the extension's rules is an error that says
// which: a wrong mode, a missing or mistyped attribute, counts that differ,
// a scale below zero, an opacity outside 0 to 1, a degree with coefficients
// missing, or data left to a compression this decoder does not read.
func TestGaussianSplattingRefusals(t *testing.T) {
	v := readGaussianVector(t)
	base := func() []splatAttr { return vectorAttrs(v, 1) }
	replace := func(name string, with splatAttr) []splatAttr {
		attrs := base()
		for i := range attrs {
			if attrs[i].name == name {
				attrs[i] = with
			}
		}
		return attrs
	}
	without := func(name string) []splatAttr {
		var out []splatAttr
		for _, a := range base() {
			if a.name != name {
				out = append(out, a)
			}
		}
		return out
	}
	cases := []struct {
		name  string
		attrs []splatAttr
		ext   string
		edit  func(string) string
		want  string
	}{
		{name: "triangles", attrs: base(), ext: ellipse, edit: func(s string) string { return strings.Replace(s, `"mode":0`, `"mode":4`, 1) }, want: "POINTS"},
		{name: "no rotation", attrs: without("KHR_gaussian_splatting:ROTATION"), ext: ellipse, want: "ROTATION"},
		{name: "no opacity", attrs: without("KHR_gaussian_splatting:OPACITY"), ext: ellipse, want: "OPACITY"},
		{name: "no degree 0", attrs: without("KHR_gaussian_splatting:SH_DEGREE_0_COEF_0"), ext: ellipse, want: "SH_DEGREE_0_COEF_0"},
		{name: "partial degree", attrs: without("KHR_gaussian_splatting:SH_DEGREE_1_COEF_1"), ext: ellipse, want: "SH_DEGREE_1"},
		{name: "rotation as VEC3", attrs: replace("KHR_gaussian_splatting:ROTATION", floats("KHR_gaussian_splatting:ROTATION", "VEC3", []float64{0, 0, 1})), ext: ellipse, want: "ROTATION"},
		{name: "zero rotation", attrs: replace("KHR_gaussian_splatting:ROTATION", floats("KHR_gaussian_splatting:ROTATION", "VEC4", []float64{0, 0, 0, 0})), ext: ellipse, want: "quaternion"},
		{name: "counts differ", attrs: replace("KHR_gaussian_splatting:OPACITY", floats("KHR_gaussian_splatting:OPACITY", "SCALAR", []float64{1}, []float64{1})), ext: ellipse, want: "OPACITY"},
		{name: "negative scale", attrs: replace("KHR_gaussian_splatting:SCALE", floats("KHR_gaussian_splatting:SCALE", "VEC3", []float64{1, -1, 1})), ext: ellipse, want: "scale"},
		{name: "opacity over 1", attrs: replace("KHR_gaussian_splatting:OPACITY", floats("KHR_gaussian_splatting:OPACITY", "SCALAR", []float64{1.5})), ext: ellipse, want: "opacity"},
		{name: "harmonics not finite", attrs: replace("KHR_gaussian_splatting:SH_DEGREE_1_COEF_0", floats("KHR_gaussian_splatting:SH_DEGREE_1_COEF_0", "VEC3", []float64{math.Inf(1), 0, 0})), ext: ellipse, want: "SH"},
		{name: "no kernel", attrs: base(), ext: `{"colorSpace":"srgb_rec709_display"}`, want: "kernel"},
		{name: "compressed elsewhere", attrs: base(), ext: `{"kernel":"ellipse","colorSpace":"srgb_rec709_display","extensions":{"VENDOR_packed_splats":{"bufferView":0}}}`,
			edit: func(s string) string { return strings.Replace(s, `"bufferView":1,`, ``, 1) }, want: "VENDOR_packed_splats"},
	}
	for _, c := range cases {
		doc, files := splatDoc(t, c.attrs, c.ext, "")
		if c.edit != nil {
			doc = []byte(c.edit(string(doc)))
		}
		_, err := Decode(doc, &nt3dmesh.Options{Open: opener(files)})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error naming %q", c.name, err, c.want)
		}
	}
}

// A kernel this decoder does not draw falls back to what the extension
// allows any renderer: plain points, with a warning, and not an error.
func TestGaussianSplattingUnknownKernelFallsBackToPoints(t *testing.T) {
	doc, files := splatDoc(t, vectorAttrs(readGaussianVector(t), 0), `{"kernel":"hexagon","colorSpace":"srgb_rec709_display"}`, "")
	m := decodeSplats(t, doc, files, nil)
	g := m.Objects[0].Geometry
	if len(g.Points) != 1 || g.PointStyle.Shape == nt3d.GaussianPoint || len(g.GaussianSH) != 0 {
		t.Fatalf("an unknown kernel was drawn as %+v", g.PointStyle)
	}
	if len(m.Warnings) == 0 || !strings.Contains(strings.Join(m.Warnings, " "), "hexagon") {
		t.Fatalf("warnings %q, want one naming the kernel", m.Warnings)
	}
}

// The scene asset carries the same cloud under its node, in the primitive's
// own space, so an instantiated splat tile is placed by its node's transform.
func TestGaussianSplattingInTheSceneAsset(t *testing.T) {
	v := readGaussianVector(t)
	doc, files := splatDoc(t, vectorAttrs(v, 3), ellipse, `{"mesh":0,"translation":[10,0,0]}`)
	m := decodeSplats(t, doc, files, &nt3dmesh.Options{AssetOnly: true})
	if m.Asset == nil {
		t.Fatal("no asset")
	}
	scene := nt3d.NewScene()
	_, err := m.Asset.Instantiate(scene, nt3d.Root, "tile")
	check(t, err)
	frame, err := scene.StillFrame(nt3d.DefaultCamera(), 32, 32)
	check(t, err)
	splats := 0
	for _, d := range frame.Draws {
		if d.Geometry.PointStyle.Shape != nt3d.GaussianPoint {
			continue
		}
		splats += len(d.Geometry.Points)
		// (1,2,3) moved to (11,2,3) and turned Z-up: (11,-3,2).
		if p := d.World.Point(d.Geometry.Points[0].Position); !near(p, nt3dmath.Vec3{X: 11, Y: -3, Z: 2}) {
			t.Fatalf("the splat is drawn at %v, want (11, -3, 2)", p)
		}
	}
	if splats != 1 {
		t.Fatalf("the asset draws %d splats, want 1", splats)
	}
}

// An accessor with no buffer view is all zeros, as glTF defines it, and a
// splat attribute may be written so: a degree-0 coefficient of zero is the
// color at rest, mid gray. Only when an extension inside the splat extension
// says it holds the data is a missing buffer view data that cannot be read.
func TestGaussianSplattingReadsZeroInitializedAttributes(t *testing.T) {
	v := readGaussianVector(t)
	doc, files := splatDoc(t, vectorAttrs(v, 0), ellipse, "")
	// Accessor 4 is SH_DEGREE_0_COEF_0.
	edited := strings.Replace(string(doc), `{"bufferView":4,`, `{`, 1)
	if edited == string(doc) {
		t.Fatal("the document has no accessor 4 to edit")
	}
	m := decodeSplats(t, []byte(edited), files, nil)
	if len(m.Objects) != 1 || len(m.Objects[0].Geometry.Points) != 1 {
		t.Fatalf("decoded %d objects, want one splat", len(m.Objects))
	}
	c := m.Objects[0].Geometry.Points[0].Color
	if c.R < 127 || c.R > 128 || c.R != c.G || c.G != c.B {
		t.Fatalf("a zero degree-0 coefficient gives the color %v, want mid gray", c)
	}
}
