package nt3dgltf

import (
	"bytes"
	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
	"image/color"
	"math"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	"testing"
)

func TestStaticHierarchyAndCamera(t *testing.T) {
	doc := gltfDoc(upTriangle)
	doc.Nodes = []*gltf.Node{
		{Name: "assembly", Translation: [3]float64{2, 3, 4}, Children: []int{1, 2}},
		{Name: "part", Mesh: gltf.Index(0), Translation: [3]float64{1, 0, 0}},
		{Name: "view", Camera: gltf.Index(0), Translation: [3]float64{0, 0, 5}},
	}
	doc.Cameras = []*gltf.Camera{{Type: "perspective", Perspective: &gltf.Perspective{Yfov: math.Pi / 3, Znear: .1, AspectRatio: gltf.Float(1.5)}}}
	for _, binary := range []bool{false, true} {
		var buf bytes.Buffer
		enc := gltf.NewEncoder(&buf)
		enc.AsBinary = binary
		check(t, enc.Encode(doc))
		m, err := Decode(buf.Bytes(), nil)
		check(t, err)
		if m.Asset == nil || len(m.Asset.Clips) != 0 {
			t.Fatal("static hierarchy missing")
		}
		s := nt3d.NewScene()
		root, err := m.AddTo(s, nt3d.Root, "asset")
		check(t, err)
		if s.Bounds() != m.Bounds() {
			t.Fatalf("hierarchy changed bounds: %v vs %v", s.Bounds(), m.Bounds())
		}
		cameras := s.Cameras(root)
		if len(cameras) != 1 {
			t.Fatal("missing camera")
		}
		c, err := s.NodeCamera(cameras[0])
		check(t, err)
		if !near(c.Eye(), nt3dmath.Vec3{X: 2, Y: -9, Z: 3}) || c.YFOV != math.Pi/3 || c.AspectRatio != 1.5 || c.Far != 0 {
			t.Fatalf("camera %+v", c)
		}
		node, _ := s.Node(cameras[0])
		parent, _ := s.Node(node.Parent)
		if len(parent.Children) != 2 {
			t.Fatal("flattened parent")
		}
		check(t, s.SetTransform(parent.ID, nt3dmath.Translation(nt3dmath.Vec3{X: 4, Y: 3, Z: 4})))
		moved, err := s.NodeCamera(cameras[0])
		check(t, err)
		if moved.Eye().X-c.Eye().X != 2 {
			t.Fatal("camera detached from hierarchy")
		}
	}
}

func TestCameraOnlyAnimationAndOrthographic(t *testing.T) {
	doc := gltf.NewDocument()
	doc.Cameras = []*gltf.Camera{{Type: "orthographic", Orthographic: &gltf.Orthographic{Xmag: 2, Ymag: 1, Znear: 0, Zfar: 10}}}
	doc.Nodes = []*gltf.Node{{Camera: gltf.Index(0)}}
	doc.Scenes = []*gltf.Scene{{Nodes: []int{0}}}
	doc.Scene = gltf.Index(0)
	times := modeler.WriteAccessor(doc, gltf.TargetNone, []float32{0, 1})
	positions := modeler.WritePosition(doc, [][3]float32{{0, 0, 0}, {2, 0, 0}})
	doc.Animations = []*gltf.Animation{{Samplers: []*gltf.AnimationSampler{{Input: times, Output: positions}}, Channels: []*gltf.AnimationChannel{{Sampler: 0, Target: gltf.AnimationChannelTarget{Node: gltf.Index(0), Path: gltf.TRSTranslation}}}}}
	m, err := Decode(glb(t, doc), nil)
	check(t, err)
	s := nt3d.NewScene()
	root, err := m.AddTo(s, nt3d.Root, "camera")
	check(t, err)
	player, err := nt3d.NewAnimation(s, s.AnimationClips(root)[0])
	check(t, err)
	check(t, player.Sample(1))
	c, err := s.NodeCamera(s.Cameras(root)[0])
	check(t, err)
	if c.Eye().X != 2 || c.Xmag != 2 || c.Ymag != 1 || c.Near != 0 || c.Far != 10 {
		t.Fatal("camera animation or lens lost", c)
	}
	for _, bad := range []*gltf.Camera{{Type: "other"}, {Type: "perspective", Perspective: &gltf.Perspective{Yfov: 0, Znear: .1}}, {Type: "orthographic", Orthographic: &gltf.Orthographic{Xmag: 1, Ymag: 1, Znear: 2, Zfar: 1}}} {
		doc.Cameras[0] = bad
		if _, err := Decode(glb(t, doc), nil); err == nil {
			t.Fatal("invalid camera accepted")
		}
	}
}

func TestTextureTransformAndIndependentNormalUV(t *testing.T) {
	p := upTriangle
	p.material = gltf.Index(0)
	p.uv = [][2]float32{{0, 0}, {1, 0}, {0, 1}}
	doc := gltfDoc(p)
	imageIndex, err := modeler.WriteImage(doc, "image", "image/png", bytes.NewReader(pngTexture(t, color.NRGBA{180, 128, 230, 255})))
	check(t, err)
	doc.Textures = []*gltf.Texture{{Source: gltf.Index(imageIndex)}}
	doc.Meshes[0].Primitives[0].Attributes["TEXCOORD_1"] = modeler.WriteTextureCoord(doc, [][2]float32{{.25, .5}, {.5, .5}, {.25, .75}})
	base := &gltf.TextureInfo{Index: 0, Extensions: gltf.Extensions{"KHR_texture_transform": map[string]any{"offset": []float64{.2, .3}, "rotation": math.Pi / 2, "scale": []float64{2, -3}, "texCoord": 1}}}
	doc.Materials = []*gltf.Material{{PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorTexture: base}, NormalTexture: &gltf.NormalTexture{Index: gltf.Index(0)}}}
	doc.ExtensionsRequired = []string{"KHR_texture_transform"}
	doc.ExtensionsUsed = doc.ExtensionsRequired
	m, err := Decode(glb(t, doc), nil)
	check(t, err)
	g := m.Objects[0].Geometry
	if len(g.NormalUV) != 3 || g.NormalUV[1].U != 1 {
		t.Fatal("normal UV set lost", g.NormalUV)
	}
	if math.Abs(float64(g.UV[0].U)-1.7) > 1e-6 || math.Abs(float64(g.UV[0].V)-.8) > 1e-6 {
		t.Fatal("wrong transform order or UV override", g.UV)
	}
	for _, node := range m.Asset.Nodes {
		if node.Geometry != nil && node.Geometry.UV[0] != g.UV[0] {
			t.Fatal("asset and flattened UVs disagree")
		}
	}
	base.Extensions["KHR_texture_transform"] = map[string]any{"scale": []float64{1}}
	if _, err := Decode(glb(t, doc), nil); err == nil {
		t.Fatal("malformed transform accepted")
	}
}

func TestStaticMirroredHierarchyBounds(t *testing.T) {
	doc := gltfDoc(upTriangle)
	doc.Nodes = []*gltf.Node{{Name: "mirror", Scale: [3]float64{-2, 1, 1}, Children: []int{1}}, {Name: "nested", Scale: [3]float64{1, -1, 1}, Translation: [3]float64{1, 2, 3}, Mesh: gltf.Index(0)}}
	m, err := Decode(glb(t, doc), nil)
	check(t, err)
	s := nt3d.NewScene()
	_, err = m.AddTo(s, nt3d.Root, "mirrored")
	check(t, err)
	if s.Bounds() != m.Bounds() {
		t.Fatal("nested reflections changed world geometry", s.Bounds(), m.Bounds())
	}
}
