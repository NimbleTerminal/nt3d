// Copyright (c) 2026 Neomantra Corp.

package nt3dsog

import (
	"archive/zip"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
)

// ramp is a JSON array of 256 numbers, 0 through 255.
func ramp() string {
	var parts []string
	for i := 0; i < 256; i++ {
		parts = append(parts, strconv.Itoa(i))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// v2JSON is a valid v2 meta.json with the given substitutions applied to it:
// each pair is (old, new).
func v2JSON(subs ...string) string {
	s := fmt.Sprintf(`{
		"version": 2, "asset": {"generator": "test"}, "count": 576,
		"means": {"mins": [-1, -2, -3], "maxs": [1, 2, 3], "files": ["means_l.webp", "means_u.webp"]},
		"scales": {"codebook": %[1]s, "files": ["scales.webp"]},
		"quats": {"files": ["quats.webp"]},
		"sh0": {"codebook": %[1]s, "files": ["sh0.webp"]},
		"shN": {"count": 512, "bands": 1, "codebook": %[1]s, "files": ["shN_centroids.webp", "shN_labels.webp"]}
	}`, ramp())
	return replace(s, subs)
}

// v1JSON is a valid v1 meta.json with the given substitutions applied.
func v1JSON(subs ...string) string {
	s := `{
		"means": {"shape": [576, 3], "mins": [-1, -2, -3], "maxs": [1, 2, 3], "files": ["means_l.webp", "means_u.webp"]},
		"quats": {"files": ["quats.webp"]},
		"scales": {"mins": [-3, -2, -1], "maxs": [1, 2, 3], "files": ["scales.webp"]},
		"sh0": {"mins": [-1, -1, -1, 0], "maxs": [1, 1, 1, 2], "files": ["sh0.webp"]},
		"shN": {"mins": -0.5, "maxs": 0.25, "files": ["shN_centroids.webp", "shN_labels.webp"], "count": 512}
	}`
	return replace(s, subs)
}

func replace(s string, subs []string) string {
	for i := 0; i+1 < len(subs); i += 2 {
		if !strings.Contains(s, subs[i]) {
			panic("substitution target not found: " + subs[i])
		}
		s = strings.Replace(s, subs[i], subs[i+1], 1)
	}
	return s
}

func TestParseMetaV2(t *testing.T) {
	m, err := parseMeta([]byte(v2JSON()))
	if err != nil {
		t.Fatal(err)
	}
	if m.version != 2 || m.count != 576 {
		t.Errorf("version %d count %d, want 2 and 576", m.version, m.count)
	}
	if m.means.low != "means_l.webp" || m.means.high != "means_u.webp" {
		t.Errorf("means files %q %q", m.means.low, m.means.high)
	}
	if m.means.mins != [3]float64{-1, -2, -3} || m.means.maxs != [3]float64{1, 2, 3} {
		t.Errorf("means range %v %v", m.means.mins, m.means.maxs)
	}
	if m.quats != "quats.webp" || m.scalesFile != "scales.webp" || m.sh0File != "sh0.webp" {
		t.Errorf("files %q %q %q", m.quats, m.scalesFile, m.sh0File)
	}
	if got := m.scales.value(1, 7); got != 7 {
		t.Errorf("scales.value(1, 7) = %v, want 7", got)
	}
	if got := m.sh0.value(0, 255); got != 255 {
		t.Errorf("sh0.value(0, 255) = %v, want 255", got)
	}
	if m.shN == nil {
		t.Fatal("shN is nil")
	}
	if m.shN.count != 512 || m.shN.bands != 1 {
		t.Errorf("shN count %d bands %d, want 512 and 1", m.shN.count, m.shN.bands)
	}
	if m.shN.centroids != "shN_centroids.webp" || m.shN.labels != "shN_labels.webp" {
		t.Errorf("shN files %q %q", m.shN.centroids, m.shN.labels)
	}
	if got := m.shN.values.value(0, 9); got != 9 {
		t.Errorf("shN.values.value(0, 9) = %v, want 9", got)
	}
}

func TestParseMetaV2NoSH(t *testing.T) {
	data := v2JSON()
	i := strings.Index(data, `"shN"`)
	data = strings.TrimRight(data[:i], ", \n\t") + "}"
	m, err := parseMeta([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if m.shN != nil {
		t.Errorf("shN = %+v, want nil", m.shN)
	}
}

func TestParseMetaV1(t *testing.T) {
	m, err := parseMeta([]byte(v1JSON()))
	if err != nil {
		t.Fatal(err)
	}
	if m.version != 1 || m.count != 576 {
		t.Errorf("version %d count %d, want 1 and 576", m.version, m.count)
	}
	if m.means.low != "means_l.webp" || m.means.high != "means_u.webp" || m.quats != "quats.webp" ||
		m.scalesFile != "scales.webp" || m.sh0File != "sh0.webp" {
		t.Errorf("files %q %q %q %q %q", m.means.low, m.means.high, m.quats, m.scalesFile, m.sh0File)
	}
	if got := m.scales.value(0, 255); got != m.scales.maxs[0] {
		t.Errorf("scales.value(0, 255) = %v, want %v", got, m.scales.maxs[0])
	}
	if got := m.scales.value(2, 0); got != -1 {
		t.Errorf("scales.value(2, 0) = %v, want -1", got)
	}
	if got := m.sh0.value(3, 0); got != m.sh0.mins[3] {
		t.Errorf("sh0.value(3, 0) = %v, want %v", got, m.sh0.mins[3])
	}
	if got := m.sh0.value(3, 255); got != 2 {
		t.Errorf("sh0.value(3, 255) = %v, want 2", got)
	}
	if m.shN == nil || m.shN.count != 512 || m.shN.bands != 0 {
		t.Fatalf("shN = %+v, want count 512, bands 0", m.shN)
	}
	// The scalar shN range serves every channel.
	for _, ch := range []int{0, 1, 44} {
		if got := m.shN.values.value(ch, 0); got != -0.5 {
			t.Errorf("shN.value(%d, 0) = %v, want -0.5", ch, got)
		}
		if got := m.shN.values.value(ch, 255); got != 0.25 {
			t.Errorf("shN.value(%d, 255) = %v, want 0.25", ch, got)
		}
	}
}

func TestParseMetaV1ShNBands(t *testing.T) {
	m, err := parseMeta([]byte(v1JSON(`"count": 512`, `"count": 512, "bands": 2`)))
	if err != nil {
		t.Fatal(err)
	}
	if m.shN.bands != 2 {
		t.Errorf("bands = %d, want 2", m.shN.bands)
	}
}

func TestParseMetaNullCodebook(t *testing.T) {
	data := v2JSON(`"scales": {"codebook": [0,`, `"scales": {"codebook": [null,`)
	m, err := parseMeta([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	first, last := 1.0, 255.0 // codebook[1] and codebook[255]; variables, so the arithmetic is float64, not exact constants
	want := first + (first-last)/255
	if got := m.scales.value(0, 0); got != want {
		t.Errorf("scales.value(0, 0) = %v, want %v", got, want)
	}
	if got := m.scales.value(0, 1); got != 1 {
		t.Errorf("scales.value(0, 1) = %v, want 1", got)
	}
}

func TestParseMetaRejects(t *testing.T) {
	short := "[" + strings.TrimPrefix(strings.TrimSuffix(ramp(), ",255]"), "[") + "]"
	nullAt5 := strings.Replace(ramp(), ",5,", ",null,", 1)
	cases := map[string]string{
		"version 3":           v2JSON(`"version": 2`, `"version": 3`),
		"count 0":             v2JSON(`"count": 576`, `"count": 0`),
		"v1 shape 0":          v1JSON(`[576, 3]`, `[0, 3]`),
		"v1 no shape":         v1JSON(`"shape": [576, 3], `, ``),
		"one means file":      v2JSON(`"means_l.webp", "means_u.webp"`, `"means_l.webp"`),
		"no means mins":       v2JSON(`"mins": [-1, -2, -3], `, ``),
		"null means min":      v2JSON(`"mins": [-1, -2, -3]`, `"mins": [-1, null, -3]`),
		"null means maxs":     v1JSON(`"maxs": [1, 2, 3], "files": ["means_l`, `"maxs": null, "files": ["means_l`),
		"v1 scales null max":  v1JSON(`"maxs": [1, 2, 3], "files": ["scales.webp"]`, `"maxs": [1, null, 3], "files": ["scales.webp"]`),
		"v1 sh0 null min":     v1JSON(`"mins": [-1, -1, -1, 0]`, `"mins": [-1, -1, -1, null]`),
		"v1 shN null mins":    v1JSON(`"mins": -0.5`, `"mins": null`),
		"v1 shN null maxs":    v1JSON(`"maxs": 0.25`, `"maxs": null`),
		"v1 shN missing mins": v1JSON(`"mins": -0.5, `, ``),
		"two means mins":      v2JSON(`"mins": [-1, -2, -3]`, `"mins": [-1, -2]`),
		"two quats files":     v2JSON(`"quats.webp"`, `"quats.webp", "x.webp"`),
		"empty file name":     v2JSON(`"quats.webp"`, `""`),
		"short codebook":      strings.Replace(v2JSON(), ramp(), short, 1),
		"null in codebook":    strings.Replace(v2JSON(), ramp(), nullAt5, 1),
		"null with null next": strings.Replace(v2JSON(), ramp(), strings.Replace(ramp(), "[0,1,", "[null,null,", 1), 1),
		"bands 0":             v2JSON(`"bands": 1`, `"bands": 0`),
		"bands 4":             v2JSON(`"bands": 1`, `"bands": 4`),
		"v1 bands 4":          v1JSON(`"count": 512`, `"count": 512, "bands": 4`),
		"shN count 0":         v2JSON(`"count": 512`, `"count": 0`),
		"v1 shN count 0":      v1JSON(`"count": 512`, `"count": 0`),
		"v1 no shN count":     v1JSON(`, "count": 512`, ``),
		"shN one file":        v2JSON(`"shN_centroids.webp", "shN_labels.webp"`, `"shN_centroids.webp"`),
		"v1 sh0 mins of 3":    v1JSON(`"mins": [-1, -1, -1, 0]`, `"mins": [-1, -1, -1]`),
		"v1 scales no maxs":   v1JSON(`"maxs": [1, 2, 3], "files": ["scales.webp"]`, `"files": ["scales.webp"]`),
		"v1 shN mins string":  v1JSON(`"mins": -0.5`, `"mins": "low"`),
		"v2 no codebook":      v2JSON(`"scales": {"codebook": `+ramp()+`,`, `"scales": {`),
		"not JSON":            `this is not json`,
		"empty":               ``,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			m, err := parseMeta([]byte(data))
			if err == nil {
				t.Fatalf("parseMeta succeeded: %+v", m)
			}
			if !strings.HasPrefix(err.Error(), "nt3dmesh: SOG ") {
				t.Errorf("error %q lacks the nt3dmesh: SOG prefix", err)
			}
		})
	}
}

// TestParseMetaFixtures parses the meta.json inside both checked-in bundles.
func TestParseMetaFixtures(t *testing.T) {
	for _, tc := range []struct {
		file    string
		version int
		bands   int
	}{
		{"testdata/gaussian-ring.sog", 2, 1},
		{"testdata/gaussian-ring.v1.sog", 1, 1},
	} {
		t.Run(tc.file, func(t *testing.T) {
			zr, err := zip.OpenReader(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			defer zr.Close()
			var data []byte
			for _, f := range zr.File {
				if f.Name != "meta.json" {
					continue
				}
				rc, err := f.Open()
				if err != nil {
					t.Fatal(err)
				}
				data, err = io.ReadAll(rc)
				rc.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			m, err := parseMeta(data)
			if err != nil {
				t.Fatal(err)
			}
			if m.version != tc.version || m.count != 576 {
				t.Errorf("version %d count %d, want %d and 576", m.version, m.count, tc.version)
			}
			if m.shN == nil || m.shN.count != 512 || m.shN.bands != tc.bands {
				t.Errorf("shN = %+v", m.shN)
			}
		})
	}
}
