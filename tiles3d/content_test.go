// Copyright (c) 2026 Neomantra Corp.

package nt3dtiles3d

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"nimbleterminal.dev/nt3d/math"
)

// b3dmFile wraps glb in a b3dm with the given feature table.
func b3dmFile(featureJSON string, featureBinary, glb []byte) []byte {
	out := append([]byte("b3dm"), 1, 0, 0, 0)
	out = binary.LittleEndian.AppendUint32(out, uint32(28+len(featureJSON)+len(featureBinary)+len(glb)))
	for _, n := range []int{len(featureJSON), len(featureBinary), 0, 0} {
		out = binary.LittleEndian.AppendUint32(out, uint32(n))
	}
	return append(append(append(out, featureJSON...), featureBinary...), glb...)
}

// A b3dm gives up the glb inside it and the center its positions are
// relative to, whether the center is written in the table's JSON or kept in
// its binary part.
func TestUnwrapB3DM(t *testing.T) {
	glb := []byte("glTF\x02\x00\x00\x00rest of the model")
	got, center, err := UnwrapB3DM(b3dmFile(`{"BATCH_LENGTH":0,"RTC_CENTER":[1364789.25,-4619824.5,4166319.125]}`, nil, glb))
	if err != nil || string(got) != string(glb) || center != (nt3dmath.Vec3d{X: 1364789.25, Y: -4619824.5, Z: 4166319.125}) {
		t.Fatalf("glb %q, center %+v, %v", got, center, err)
	}
	var floats []byte
	for _, v := range []float32{0, 10, 20, 30} {
		floats = binary.LittleEndian.AppendUint32(floats, math.Float32bits(v))
	}
	_, center, err = UnwrapB3DM(b3dmFile(`{"RTC_CENTER":{"byteOffset":4}}`, floats, glb))
	if err != nil || center != (nt3dmath.Vec3d{X: 10, Y: 20, Z: 30}) {
		t.Fatalf("center from the binary table %+v, %v", center, err)
	}
	if _, center, err = UnwrapB3DM(b3dmFile("", nil, glb)); err != nil || center != (nt3dmath.Vec3d{}) {
		t.Fatalf("no feature table: center %+v, %v", center, err)
	}
	long := b3dmFile("{}", nil, glb)
	binary.LittleEndian.PutUint32(long[8:], uint32(len(long)+1))
	tables := b3dmFile("{}", nil, glb)
	binary.LittleEndian.PutUint32(tables[16:], 1<<30)
	for name, file := range map[string][]byte{
		"not a b3dm":           glb,
		"cut short":            []byte("b3dm\x01\x00\x00\x00"),
		"length past the file": long,
		"tables past the file": tables,
		"no glb inside":        b3dmFile("{}", nil, []byte("nope")),
		"center of two":        b3dmFile(`{"RTC_CENTER":[1,2]}`, nil, glb),
		"center past table":    b3dmFile(`{"RTC_CENTER":{"byteOffset":8}}`, floats, glb),
		"bad table":            b3dmFile(`{`, nil, glb),
	} {
		if _, _, err := UnwrapB3DM(file); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// Content is told apart by how it starts, and JSON by whether it has a
// tileset's members or only a glTF's.
func TestSniffContent(t *testing.T) {
	for text, want := range map[string]ContentKind{
		"glTF\x02\x00\x00\x00": ContentGLB,
		"b3dm\x01\x00\x00\x00": ContentB3DM,
		` {"asset": {"version": "1.1"}, "geometricError": 1, "root": {}}`: ContentTileset,
		"\xef\xbb\xbf{\"asset\": {\"version\": \"2.0\"}, \"scenes\": []}": ContentGLTF,
		"pnts\x01\x00\x00\x00": ContentUnknown,
		`{"hello": 1}`:         ContentUnknown,
		"":                     ContentUnknown,
	} {
		if got := SniffContent([]byte(text)); got != want {
			t.Errorf("%q: kind %d, want %d", text, got, want)
		}
	}
}

// A tile's relative URI resolves beside the document that names it, as a
// link does, for URLs and for plain paths, and cannot climb out of a path
// root or smuggle data in.
func TestResolve(t *testing.T) {
	for _, c := range []struct{ base, ref, want string }{
		{"https://tiles.example/city/tileset.json", "content/0.glb", "https://tiles.example/city/content/0.glb"},
		{"https://tiles.example/city/tileset.json?v=3", "../shared/a.glb", "https://tiles.example/shared/a.glb"},
		{"https://tiles.example/city/tileset.json", "https://other.example/b.glb", "https://other.example/b.glb"},
		{"https://tiles.example/city/tileset.json", "/root.glb", "https://tiles.example/root.glb"},
		{"city/tileset.json", "content/0.glb", "city/content/0.glb"},
		{"city/sub/tileset.json", "../a.glb", "city/a.glb"},
		{"tileset.json", "a b.glb", "a b.glb"},
		{"city/tileset.json", "https://other.example/b.glb", "https://other.example/b.glb"},
	} {
		if got, err := Resolve(c.base, c.ref); err != nil || got != c.want {
			t.Errorf("Resolve(%q, %q) = %q, %v; want %q", c.base, c.ref, got, err, c.want)
		}
	}
	for _, c := range [][2]string{{"tileset.json", "../../etc/passwd"}, {"tileset.json", "data:model/gltf-binary;base64,AAAA"}, {"tileset.json", "%zz"}} {
		if got, err := Resolve(c[0], c[1]); err == nil {
			t.Errorf("Resolve(%q, %q) = %q, want an error", c[0], c[1], got)
		}
	}
}

// The file-system fetcher reads paths under its root and nothing above it.
func TestFSFetcher(t *testing.T) {
	f := FS(fstest.MapFS{"city/a b.glb": {Data: []byte("model")}})
	for _, uri := range []string{"city/a b.glb", "/city/a%20b.glb", "city/a b.glb?v=2"} {
		if got, err := f.Fetch(context.Background(), uri); err != nil || string(got) != "model" {
			t.Errorf("%q: %q, %v", uri, got, err)
		}
	}
	if _, err := f.Fetch(context.Background(), "../secret"); err == nil {
		t.Error("a path above the root was read")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Fetch(ctx, "city/a b.glb"); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled fetch: %v", err)
	}
}

// The HTTP fetcher returns a response's bytes, refuses a status that is not
// OK and a body over its limit, and Bearer adds its token only for https on
// the tileset's own host.
func TestHTTPFetcher(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tile.glb":
			_, _ = w.Write([]byte("glTF" + r.Header.Get("X-Key")))
		case "/big":
			_, _ = w.Write(make([]byte, 4096))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	h := &HTTP{Prepare: func(r *http.Request) { r.Header.Set("X-Key", "!") }, MaxBytes: 1024}
	if got, err := h.Fetch(context.Background(), server.URL+"/tile.glb"); err != nil || string(got) != "glTF!" {
		t.Fatalf("%q, %v", got, err)
	}
	if _, err := h.Fetch(context.Background(), server.URL+"/missing"); err == nil {
		t.Error("a 404 was returned as content")
	}
	if _, err := h.Fetch(context.Background(), server.URL+"/big"); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a body over the limit: %v, want ErrTooLarge", err)
	}
	if _, err := (&HTTP{}).Fetch(context.Background(), "file:///etc/passwd"); err == nil {
		t.Error("a file URI was fetched over HTTP")
	}
	prepare := Bearer("secret", "https://tiles.example/city/tileset.json")
	for uri, want := range map[string]string{
		"https://tiles.example/city/0.glb": "Bearer secret",
		"http://tiles.example/city/0.glb":  "",
		"https://other.example/0.glb":      "",
	} {
		req, _ := http.NewRequest(http.MethodGet, uri, nil)
		prepare(req)
		if got := req.Header.Get("Authorization"); got != want {
			t.Errorf("%s: Authorization %q, want %q", uri, got, want)
		}
	}
}
