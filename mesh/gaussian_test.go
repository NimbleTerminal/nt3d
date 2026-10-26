package nt3dmesh

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/sh"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gaussianFile(format string, values []float32) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "ply\nformat %s 1.0\nelement vertex 1\n", format)
	for _, p := range gaussianProperties {
		fmt.Fprintf(&b, "property float %s\n", p)
	}
	b.WriteString("end_header\n")
	for _, v := range values {
		switch format {
		case "ascii":
			fmt.Fprintf(&b, "%g ", v)
		case "binary_little_endian":
			binary.Write(&b, binary.LittleEndian, v)
		default:
			binary.Write(&b, binary.BigEndian, v)
		}
	}
	return b.Bytes()
}
func TestGaussianPLY(t *testing.T) {
	values := []float32{1, 2, 3, 0, 1, -1, 0, 0, -1, -2, 2, 0, 0, 0}
	for _, format := range []string{"ascii", "binary_little_endian", "binary_big_endian"} {
		m, err := DecodePLY(gaussianFile(format, values), nil)
		if err != nil {
			t.Fatal(err)
		}
		g := m.Objects[0].Geometry
		p := g.Points[0]
		if g.PointStyle.Shape != nt3d.GaussianPoint || p.Color.R != 128 || p.Color.A != 128 || p.GaussianAxes[0].X != 1 || math.Abs(float64(p.GaussianAxes[1].Y)-math.Exp(-1)) > 1e-6 {
			t.Fatalf("decoded %s: %+v", format, p)
		}
		if _, err := DecodePLY(gaussianFile(format, values)[:40], nil); err == nil {
			t.Fatal("truncation accepted")
		}
	}
	data := gaussianFile("ascii", values)
	for _, bad := range []string{strings.Replace(string(data), "scale_0", "missing", 1), strings.Replace(string(data), "vertex 1", "vertex 500001", 1), strings.Replace(string(data), "1 2 3", "nan 2 3", 1)} {
		if _, err := DecodePLY([]byte(bad), nil); err == nil {
			t.Fatal("invalid file accepted")
		}
	}
	for _, index := range []int{7, 10} {
		v := append([]float32{}, values...)
		if index == 7 {
			v[index] = 1000
		} else {
			v[index] = 0
		}
		if _, err := DecodePLY(gaussianFile("ascii", v), nil); err == nil {
			t.Fatal("invalid scale/quaternion accepted")
		}
	}
	if _, err := DecodePLY(data, &Options{MaxBytes: 8}); err == nil {
		t.Fatal("byte limit ignored")
	}
}

func TestGaussianPLYRotationOrientationAndSH(t *testing.T) {
	// A non-unit quaternion rotating 90 degrees about Z, plus Y-up conversion.
	values := []float32{1, 2, 3, 0, 0, 0, 0, 0, -1, -2, 2, 0, 0, 2}
	data := gaussianFile("binary_little_endian", values)
	if _, err := DecodePLY(data[:len(data)-1], nil); err == nil {
		t.Fatal("truncated binary body accepted")
	}
	m, err := DecodePLY(data, &Options{Up: YUp})
	if err != nil {
		t.Fatal(err)
	}
	p := m.Objects[0].Geometry.Points[0]
	if math.Abs(float64(p.GaussianAxes[0].Z)-1) > 1e-6 || p.Position.Y != -3 || p.Position.Z != 2 {
		t.Fatalf("rotation/orientation lost: %+v", p)
	}
	text := string(gaussianFile("ascii", values))
	properties, rest := "", ""
	for i := 0; i < 45; i++ {
		properties += fmt.Sprintf("property float f_rest_%d\n", i)
		rest += fmt.Sprintf("%g ", float32(i)/100)
	}
	text = strings.Replace(text, "end_header", properties+"end_header", 1) + rest
	m, err = DecodePLY([]byte(text), nil)
	if err != nil {
		t.Fatal(err)
	}
	sh := m.Objects[0].Geometry.GaussianSH
	if len(sh) != 16 || sh[1][1] != .15 || sh[15][2] != .44 || len(m.Warnings) != 0 {
		t.Fatalf("SH channel ordering: %+v", sh)
	}
	malformed := strings.Replace(text, "f_rest_44", "f_rest_45", 1)
	if _, err := DecodePLY([]byte(malformed), nil); err == nil {
		t.Fatal("accepted missing SH property")
	}
	nonfinite := strings.TrimSuffix(text, "0.44 ") + "nan "
	if _, err := DecodePLY([]byte(nonfinite), nil); err == nil {
		t.Fatal("accepted nonfinite SH")
	}

}

// Real captures can contain extremely thin surface splats; the screen-space
// variance floor stabilizes them without inflating the stored world axes.
func TestGaussianPLYThinAxes(t *testing.T) {
	values := []float32{0, 0, 0, 0, 0, 0, 0, -24.1, -23.6, -2, 1, 0, 0, 0}
	m, err := DecodePLY(gaussianFile("binary_little_endian", values), nil)
	if err != nil {
		t.Fatal(err)
	}
	axis := m.Objects[0].Geometry.Points[0].GaussianAxes[0].X
	if math.Abs(float64(axis)/math.Exp(float64(values[7]))-1) > 1e-6 {
		t.Fatal("thin axis changed")
	}
	values[7] = -1000
	if _, err := DecodePLY(gaussianFile("ascii", values), nil); err == nil {
		t.Fatal("underflowed axis accepted")
	}
}

// GaussianPoint builds the point DecodeGaussianPLY builds from the same
// values, and refuses what it refuses.
func TestGaussianPointHelper(t *testing.T) {
	values := []float32{1, 2, 3, 0, 1, -1, 0, 0, -1, -2, 2, 0, 0, 0}
	m, err := DecodePLY(gaussianFile("ascii", values), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := m.Objects[0].Geometry.Points[0]
	got, err := GaussianPoint(nt3dmath.Vec3{X: 1, Y: 2, Z: 3}, [4]float64{2, 0, 0, 0}, [3]float64{0, -1, -2}, [3]float64{0, 1, -1}, .5)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("helper %+v\nPLY    %+v", got, want)
	}
	if _, err := GaussianPoint(nt3dmath.Vec3{}, [4]float64{}, [3]float64{}, [3]float64{}, 1); err == nil {
		t.Fatal("zero quaternion accepted")
	}
	if _, err := GaussianPoint(nt3dmath.Vec3{}, [4]float64{1, 0, 0, 0}, [3]float64{17, 0, 0}, [3]float64{}, 1); err == nil {
		t.Fatal("scale of e^17 accepted")
	}
	if _, err := GaussianPoint(nt3dmath.Vec3{X: float32(math.NaN())}, [4]float64{1, 0, 0, 0}, [3]float64{}, [3]float64{}, 1); err == nil {
		t.Fatal("non-finite position accepted")
	}
}

func TestGaussianSH(t *testing.T) {
	dc := [3]float64{.1, .2, .3}
	rest := make([]float64, 9) // degree 1: 3 coefficients × 3 channels, channel-major
	for i := range rest {
		rest[i] = float64(i)
	}
	sh, err := GaussianSH(dc, rest)
	if err != nil || len(sh) != 4 || (sh[0] != nt3dsh.RGB{.1, .2, .3}) {
		t.Fatal(sh, err)
	}
	// coefficient j, channel c reads rest[c*3 + j-1]
	if (sh[1] != nt3dsh.RGB{0, 3, 6}) || (sh[3] != nt3dsh.RGB{2, 5, 8}) {
		t.Fatal(sh)
	}
	if sh, err := GaussianSH(dc, nil); err != nil || len(sh) != 1 {
		t.Fatal("DC only", sh, err)
	}
	if _, err := GaussianSH(dc, make([]float64, 6)); err == nil {
		t.Fatal("accepted 6 coefficients")
	}
	if _, err := GaussianSH(dc, []float64{math.NaN(), 0, 0, 0, 0, 0, 0, 0, 0}); err == nil {
		t.Fatal("accepted NaN")
	}
}

// The Gaussian decoders cluster each splat piece for level of detail, unless
// Options.NoClusters says not to.
func TestDecodersCluster(t *testing.T) {
	compressed, err := os.ReadFile(filepath.Join("testdata", "gaussian-ring.compressed.ply"))
	if err != nil {
		t.Fatal(err)
	}
	decoders := map[string]func(o *Options) *Model{
		"PLY": func(o *Options) *Model { return decodeRing(t, o) },
		"compressed PLY": func(o *Options) *Model {
			m, err := DecodePLY(compressed, o)
			if err != nil {
				t.Fatal(err)
			}
			return m
		},
	}
	for name, decode := range decoders {
		g := decode(nil).Objects[0].Geometry
		if g.Clusters == nil || len(g.Clusters.Order) != 576 || len(g.Points) != 576 {
			t.Errorf("%s: clusters %v", name, g.Clusters)
		}
		if g := decode(&Options{NoClusters: true}).Objects[0].Geometry; g.Clusters != nil || len(g.Points) != 576 {
			t.Errorf("%s: NoClusters left clusters %v", name, g.Clusters)
		}
	}
}

// Each piece of a split cloud is clustered over its own points; other
// geometry is left alone.
func TestClusterPieces(t *testing.T) {
	whole := decodeRing(t, nil).Objects[0]
	defer func(g func(int) int) { maxGaussians = g }(maxGaussians)
	maxGaussians = func(int) int { return 200 }
	whole.Geometry.Clusters = nil
	pieces := PointPieces(whole)
	if len(pieces) != 3 {
		t.Fatalf("%d pieces", len(pieces))
	}
	pieces = append(pieces, Object{Geometry: nt3d.Geometry{Points: whole.Geometry.Points[:5]}})
	if err := ClusterPieces(pieces, nil); err != nil {
		t.Fatal(err)
	}
	for i, p := range pieces[:3] {
		if p.Geometry.Clusters == nil || len(p.Geometry.Clusters.Order) != len(p.Geometry.Points) {
			t.Fatalf("piece %d: clusters %v", i, p.Geometry.Clusters)
		}
		if err := p.Geometry.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	if pieces[3].Geometry.Clusters != nil {
		t.Fatal("clustered a plain point cloud")
	}
	for i := range pieces {
		pieces[i].Geometry.Clusters = nil
	}
	if err := ClusterPieces(pieces, &Options{NoClusters: true}); err != nil || pieces[0].Geometry.Clusters != nil {
		t.Fatalf("NoClusters: %v", err)
	}
}
