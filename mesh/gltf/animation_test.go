package nt3dgltf

import (
	"bytes"
	"encoding/json"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
	"math"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"os"
	"testing"
)

func TestAnimatedGLTFRoundTrip(t *testing.T) {
	data, err := os.ReadFile("testdata/animated.gltf")
	check(t, err)
	// Both JSON glTF and binary GLB use the same generic hierarchy and tracks.
	doc := new(gltf.Document)
	check(t, gltf.NewDecoder(bytes.NewReader(data)).Decode(doc))
	var binary bytes.Buffer
	enc := gltf.NewEncoder(&binary)
	enc.AsBinary = true
	check(t, enc.Encode(doc))
	for _, data := range [][]byte{data, binary.Bytes()} {
		m, err := Decode(data, nil)
		check(t, err)
		if m.Asset == nil || len(m.Asset.Clips) != 1 {
			t.Fatal("missing animation")
		}
		s := nt3d.NewScene()
		instance, err := m.Asset.Instantiate(s, nt3d.Root, "animated")
		check(t, err)
		player, err := nt3d.NewAnimation(s, instance.Clips[0])
		check(t, err)
		before := s.Bounds()
		check(t, player.Sample(1))
		after := s.Bounds()
		if before == after {
			t.Fatal("animation did not deform bounds")
		}
		for _, tr := range instance.Clips[0].Tracks {
			if tr.Channel == nt3d.MorphWeights {
				w, e := s.MorphWeights(tr.Node)
				check(t, e)
				if w[0] != 1 {
					t.Fatal("morph not applied")
				}
			}
		}
		root, err := m.AddTo(s, nt3d.Root, "second")
		check(t, err)
		if len(s.AnimationClips(root)) != 1 {
			t.Fatal("AddTo discarded clips")
		}
	}
}
func TestAnimatedGLTFInvalidReferences(t *testing.T) {
	data, err := os.ReadFile("testdata/animated.gltf")
	check(t, err)
	var raw map[string]any
	check(t, json.Unmarshal(data, &raw))
	raw["skins"].([]any)[0].(map[string]any)["joints"] = []int{999}
	bad, err := json.Marshal(raw)
	check(t, err)
	if _, err = Decode(bad, nil); err == nil {
		t.Fatal("missing joints accepted")
	}
}
func TestAnimatedQuaternionNormalization(t *testing.T) {
	data, err := os.ReadFile("testdata/animated.gltf")
	check(t, err)
	m, err := Decode(data, nil)
	check(t, err)
	for i := range m.Asset.Clips[0].Tracks {
		tr := &m.Asset.Clips[0].Tracks[i]
		if tr.Channel == nt3d.Rotation {
			for j := range tr.Values {
				tr.Values[j] *= 3
			}
		}
	}
	s := nt3d.NewScene()
	instance, err := m.Asset.Instantiate(s, nt3d.Root, "test")
	check(t, err)
	a, err := nt3d.NewAnimation(s, instance.Clips[0])
	check(t, err)
	check(t, a.Sample(.5))
	for _, tr := range instance.Clips[0].Tracks {
		if tr.Channel == nt3d.Rotation {
			node, _ := s.Node(tr.Node)
			if math.Abs(node.Local.Determinant3()-1) > 1e-5 {
				t.Fatal("quaternion scaled the mesh")
			}
		}
	}
}

func TestAnimatedSparseMorphAndNormalizedWeights(t *testing.T) {
	data, err := os.ReadFile("testdata/animated.gltf")
	check(t, err)
	doc := new(gltf.Document)
	check(t, gltf.NewDecoder(bytes.NewReader(data)).Decode(doc))
	p := doc.Meshes[0].Primitives[0]
	target := doc.Accessors[p.Targets[0][gltf.POSITION]]
	indices := modeler.WriteIndices(doc, []uint16{4, 5})
	values := modeler.WritePosition(doc, [][3]float32{{-.2, 0, .15}, {.2, 0, .15}})
	target.BufferView = nil
	target.Sparse = &gltf.Sparse{Count: 2, Indices: gltf.SparseIndices{BufferView: *doc.Accessors[indices].BufferView, ComponentType: gltf.ComponentUshort}, Values: gltf.SparseValues{BufferView: *doc.Accessors[values].BufferView}}
	wa := modeler.WriteWeights(doc, [][4]uint8{{255, 0, 0, 0}, {255, 0, 0, 0}, {127, 128, 0, 0}, {127, 128, 0, 0}, {255, 0, 0, 0}, {255, 0, 0, 0}})
	p.Attributes[gltf.WEIGHTS_0] = wa
	for _, b := range doc.Buffers {
		b.URI = ""
	}
	var buf bytes.Buffer
	enc := gltf.NewEncoder(&buf)
	enc.AsBinary = true
	check(t, enc.Encode(doc))
	m, err := Decode(buf.Bytes(), nil)
	check(t, err)
	s := nt3d.NewScene()
	instance, err := m.Asset.Instantiate(s, nt3d.Root, "sparse")
	check(t, err)
	a, err := nt3d.NewAnimation(s, instance.Clips[0])
	check(t, err)
	check(t, a.Sample(1))
	if !s.Bounds().IsValid() {
		t.Fatal("invalid sparse pose")
	}
}

func TestGLTFMorphInterpolation(t *testing.T) {
	for _, mode := range []gltf.Interpolation{gltf.InterpolationLinear, gltf.InterpolationStep, gltf.InterpolationCubicSpline} {
		data, err := os.ReadFile("testdata/animated.gltf")
		check(t, err)
		doc := new(gltf.Document)
		check(t, gltf.NewDecoder(bytes.NewReader(data)).Decode(doc))
		sam := doc.Animations[0].Samplers[1]
		sam.Interpolation = mode
		if mode == gltf.InterpolationCubicSpline {
			sam.Output = modeler.WriteAccessor(doc, gltf.TargetNone, []float32{0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0})
		}
		for _, b := range doc.Buffers {
			b.URI = ""
		}
		var buf bytes.Buffer
		enc := gltf.NewEncoder(&buf)
		enc.AsBinary = true
		check(t, enc.Encode(doc))
		m, err := Decode(buf.Bytes(), nil)
		check(t, err)
		s := nt3d.NewScene()
		instance, err := m.Asset.Instantiate(s, nt3d.Root, "interpolation")
		check(t, err)
		a, err := nt3d.NewAnimation(s, instance.Clips[0])
		check(t, err)
		check(t, a.Sample(.25))
		want := float32(.25)
		if mode == gltf.InterpolationStep {
			want = 0
		}
		if mode == gltf.InterpolationCubicSpline {
			want = .15625
		}
		for _, tr := range instance.Clips[0].Tracks {
			if tr.Channel == nt3d.MorphWeights {
				w, err := s.MorphWeights(tr.Node)
				check(t, err)
				if math.Abs(float64(w[0]-want)) > 1e-6 {
					t.Fatalf("%v: weight %v, want %v", mode, w[0], want)
				}
			}
		}
	}
}

func TestGLTFMorphTangents(t *testing.T) {
	p := upTriangle
	p.uv = [][2]float32{{0, 0}, {1, 0}, {0, 1}}
	p.normals = [][3]float32{{0, 0, 1}, {0, 0, 1}, {0, 0, 1}}
	doc := gltfDoc(p)
	normalMapped(t, doc)
	doc.Meshes[0].Primitives[0].Targets = []gltf.PrimitiveAttributes{{gltf.TANGENT: modeler.WritePosition(doc, [][3]float32{{.5, 0, 0}, {.5, 0, 0}, {.5, 0, 0}})}}
	m, err := Decode(glb(t, doc), nil)
	check(t, err)
	var def *nt3d.Deformation
	for _, n := range m.Asset.Nodes {
		if n.Deformation != nil {
			def = n.Deformation
		}
	}
	if def == nil || len(def.Morphs) != 1 {
		t.Fatal("morph target lost")
	}
	if got := def.Morphs[0].Tangents; len(got) != 3 || !near(got[0], nt3dmath.Vec3{X: .5}) {
		t.Fatalf("morph tangents %v", got)
	}
}
