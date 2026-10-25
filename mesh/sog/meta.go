// Copyright (c) 2026 Neomantra Corp.

package nt3dsog

import (
	"encoding/json"
	"fmt"
	"math"
)

// codebookLen is the number of entries in a v2 codebook: one per byte value.
const codebookLen = 256

// table turns a byte into a value: a v2 codebook, or a v1 linear range per
// channel.
type table struct {
	codebook   []float64 // v2: 256 entries
	mins, maxs []float64 // v1: one per channel
}

// value maps b to its value for channel. A codebook ignores channel. A v1
// range is mins[channel] + (maxs[channel]-mins[channel])*b/255; a range with
// a single entry (the v1 shN) serves every channel, so channel is taken
// modulo the length.
func (t table) value(channel int, b uint8) float64 {
	if t.codebook != nil {
		return t.codebook[b]
	}
	i := channel % len(t.mins)
	return t.mins[i] + (t.maxs[i]-t.mins[i])*float64(b)/255
}

// meta is a parsed meta.json: the splat count, the ranges or codebooks, and
// the names of the images that hold the data.
type meta struct {
	version int // 1 or 2
	count   int
	means   struct {
		mins, maxs [3]float64
		low, high  string // the two file names
	}
	scales              table
	quats               string // file name
	sh0                 table  // v1: mins/maxs have 4 entries; [3] is the opacity logit range
	shN                 *shN   // nil when absent
	scalesFile, sh0File string
}

// shN describes the higher-order spherical harmonics.
type shN struct {
	count, bands      int // v1 only: bands is 0 until the centroid image says, and count may be too
	values            table
	centroids, labels string
}

// rawMeta is meta.json as encoding/json sees it, before validation.
type rawMeta struct {
	Version *int `json:"version"`
	Count   int  `json:"count"`
	Means   struct {
		Shape []int           `json:"shape"`
		Mins  json.RawMessage `json:"mins"`
		Maxs  json.RawMessage `json:"maxs"`
		Files []string        `json:"files"`
	} `json:"means"`
	Scales rawTable `json:"scales"`
	Quats  struct {
		Files []string `json:"files"`
	} `json:"quats"`
	Sh0 rawTable `json:"sh0"`
	ShN *struct {
		rawTable
		Count *int `json:"count"`
		Bands int  `json:"bands"`
	} `json:"shN"`
}

// rawTable is an attribute's value table: a codebook (v2) or per-channel
// ranges (v1; shN's are bare numbers) and the files that hold its bytes.
type rawTable struct {
	Codebook []*float64      `json:"codebook"`
	Mins     json.RawMessage `json:"mins"`
	Maxs     json.RawMessage `json:"maxs"`
	Files    []string        `json:"files"`
}

func metaErr(format string, args ...any) error {
	return fmt.Errorf("nt3dmesh: SOG "+format, args...)
}

// parseMeta reads a SOG meta.json of version 1 or 2. Fields it does not use
// are ignored.
func parseMeta(data []byte) (*meta, error) {
	var raw rawMeta
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, metaErr("meta.json: %v", err)
	}
	m := &meta{version: 1}
	if raw.Version != nil {
		m.version = *raw.Version
	}
	if m.version != 1 && m.version != 2 {
		return nil, metaErr("version %d is not supported", m.version)
	}
	v2 := m.version == 2

	// Count: v2 states it; v1 gives it as the first dimension of means.shape.
	if v2 {
		m.count = raw.Count
	} else if len(raw.Means.Shape) > 0 {
		m.count = raw.Means.Shape[0]
	}
	if m.count < 1 {
		return nil, metaErr("splat count %d is not positive", m.count)
	}

	// means
	meansMins, err := numbers("means.mins", raw.Means.Mins)
	if err != nil {
		return nil, err
	}
	meansMaxs, err := numbers("means.maxs", raw.Means.Maxs)
	if err != nil {
		return nil, err
	}
	if len(meansMins) != 3 || len(meansMaxs) != 3 {
		return nil, metaErr("means needs 3 mins and 3 maxs")
	}
	copy(m.means.mins[:], meansMins)
	copy(m.means.maxs[:], meansMaxs)
	files, err := fileNames("means", raw.Means.Files, 2)
	if err != nil {
		return nil, err
	}
	m.means.low, m.means.high = files[0], files[1]

	// quats
	files, err = fileNames("quats", raw.Quats.Files, 1)
	if err != nil {
		return nil, err
	}
	m.quats = files[0]

	// scales, sh0
	if m.scales, m.scalesFile, err = parseTable("scales", raw.Scales, v2, 3); err != nil {
		return nil, err
	}
	if m.sh0, m.sh0File, err = parseTable("sh0", raw.Sh0, v2, 4); err != nil {
		return nil, err
	}

	// shN
	if raw.ShN != nil {
		s := &shN{bands: raw.ShN.Bands}
		// The original v1 exporter wrote no count: every palette slot is valid,
		// which loadImages fills in from the centroid image's height.
		if raw.ShN.Count != nil {
			s.count = *raw.ShN.Count
			if s.count < 1 {
				return nil, metaErr("shN.count %d is not positive", s.count)
			}
		} else if v2 {
			return nil, metaErr("shN.count is missing")
		}
		if s.bands < 1 || s.bands > 3 {
			if v2 || s.bands != 0 {
				return nil, metaErr("shN.bands %d is not 1, 2, or 3", s.bands)
			}
		}
		files, err = fileNames("shN", raw.ShN.Files, 2)
		if err != nil {
			return nil, err
		}
		s.centroids, s.labels = files[0], files[1]
		if s.values, err = parseValues("shN", raw.ShN.rawTable, v2, 1); err != nil {
			return nil, err
		}
		m.shN = s
	}
	return m, nil
}

// parseTable reads an attribute with one file and a value table.
func parseTable(name string, r rawTable, v2 bool, channels int) (table, string, error) {
	files, err := fileNames(name, r.Files, 1)
	if err != nil {
		return table{}, "", err
	}
	t, err := parseValues(name, r, v2, channels)
	return t, files[0], err
}

// parseValues reads the codebook (v2) or the mins and maxs (v1) of an
// attribute that has channels channels. The v1 shN's mins and maxs may be
// bare numbers, which read as one-element lists.
func parseValues(name string, r rawTable, v2 bool, channels int) (table, error) {
	if v2 {
		cb, err := parseCodebook(name, r.Codebook)
		return table{codebook: cb}, err
	}
	mins, err := numbers(name+".mins", r.Mins)
	if err != nil {
		return table{}, err
	}
	maxs, err := numbers(name+".maxs", r.Maxs)
	if err != nil {
		return table{}, err
	}
	if len(mins) != channels || len(maxs) != channels {
		return table{}, metaErr("%s needs %d mins and %d maxs, has %d and %d", name, channels, channels, len(mins), len(maxs))
	}
	return table{mins: mins, maxs: maxs}, nil
}

// parseCodebook checks a codebook for 256 numbers. A null first entry, which
// the encoder writes for an unused slot 0, is extrapolated from the entries
// at the other end; a null anywhere else is an error.
func parseCodebook(name string, cb []*float64) ([]float64, error) {
	if len(cb) != codebookLen {
		return nil, metaErr("%s.codebook has %d entries, not %d", name, len(cb), codebookLen)
	}
	out := make([]float64, codebookLen)
	for i, p := range cb {
		if p == nil {
			if i != 0 {
				return nil, metaErr("%s.codebook[%d] is null", name, i)
			}
			continue
		}
		out[i] = *p
	}
	if cb[0] == nil {
		if cb[1] == nil {
			return nil, metaErr("%s.codebook[1] is null", name)
		}
		out[0] = out[1] + (out[1]-out[codebookLen-1])/(codebookLen-1)
	}
	if err := finite(name+".codebook", out); err != nil {
		return nil, err
	}
	return out, nil
}

// numbers reads a JSON number or array of numbers. A null, whether the whole
// value or an element, is an error: encoding/json would read it as 0, and
// JavaScript encoders write NaN and Infinity as null.
func numbers(what string, raw json.RawMessage) ([]float64, error) {
	if len(raw) == 0 {
		return nil, metaErr("%s is missing", what)
	}
	var ptrs []*float64
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &ptrs); err != nil {
			return nil, metaErr("%s: %v", what, err)
		}
	} else {
		var one *float64
		if err := json.Unmarshal(raw, &one); err != nil {
			return nil, metaErr("%s: %v", what, err)
		}
		ptrs = []*float64{one}
	}
	list := make([]float64, len(ptrs))
	for i, p := range ptrs {
		if p == nil {
			return nil, metaErr("%s is null", what)
		}
		list[i] = *p
	}
	if err := finite(what, list); err != nil {
		return nil, err
	}
	return list, nil
}

// fileNames checks that an attribute names exactly n non-empty files.
func fileNames(name string, files []string, n int) ([]string, error) {
	if len(files) != n {
		return nil, metaErr("%s.files has %d entries, not %d", name, len(files), n)
	}
	for _, f := range files {
		if f == "" {
			return nil, metaErr("%s.files has an empty name", name)
		}
	}
	return files, nil
}

func finite(what string, vs []float64) error {
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return metaErr("%s has a non-finite number", what)
		}
	}
	return nil
}
