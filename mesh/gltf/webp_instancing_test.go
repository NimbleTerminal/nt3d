// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"

	"nimbleterminal.dev/nt3d"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

// webpTexture encodes a 4x4 lossless VP8L WebP image filled with c.
func webpTexture(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	var bits uint64
	var nBits uint
	var payload []byte
	put := func(val uint32, n uint) {
		bits |= uint64(val) << nBits
		nBits += n
		for nBits >= 8 {
			payload = append(payload, byte(bits))
			bits >>= 8
			nBits -= 8
		}
	}
	const w, h = 4, 4
	put(0x2f, 8) // VP8L signature
	put(w-1, 14) // width - 1
	put(h-1, 14) // height - 1
	if c.A < 255 {
		put(1, 1)
	} else {
		put(0, 1)
	}
	put(0, 3) // version 0
	put(0, 1) // no transforms
	put(0, 1) // no color cache
	put(0, 1) // single Huffman group (useMeta = 0)
	// Five single-symbol simple Huffman trees: Green, Red, Blue, Alpha, Distance.
	// Single-symbol trees emit 0 bits per pixel, so pixel data is 0 bits!
	for _, sym := range []uint8{c.G, c.R, c.B, c.A, 0} {
		put(1, 1) // useSimple = 1
		put(0, 1) // nSymbols - 1 = 0 (1 symbol)
		put(1, 1) // firstSymbolLengthCode = 1 (8-bit symbol)
		put(uint32(sym), 8)
	}
	if nBits > 0 {
		payload = append(payload, byte(bits))
	}
	padded := payload
	if len(padded)%2 == 1 {
		padded = append(append([]byte(nil), payload...), 0)
	}
	var riff bytes.Buffer
	riff.WriteString("RIFF")
	_ = binary.Write(&riff, binary.LittleEndian, uint32(4+8+len(padded)))
	riff.WriteString("WEBP")
	riff.WriteString("VP8L")
	_ = binary.Write(&riff, binary.LittleEndian, uint32(len(payload)))
	riff.Write(padded)
	out := riff.Bytes()

	decoded, fmtName, err := image.Decode(bytes.NewReader(out))
	check(t, err)
	if fmtName != "webp" {
		t.Fatalf("expected webp format, got %q", fmtName)
	}
	got := color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA)
	if got != c {
		t.Fatalf("webpTexture round-trip mismatch: got %v, want %v", got, c)
	}
	return out
}

func writeWebPImage(t *testing.T, doc *gltf.Document, name string, c color.NRGBA) int {
	t.Helper()
	data := webpTexture(t, c)
	bv := modeler.WriteBufferView(doc, gltf.TargetNone, data)
	idx := len(doc.Images)
	doc.Images = append(doc.Images, &gltf.Image{
		Name:       name,
		MimeType:   "image/webp",
		BufferView: gltf.Index(bv),
	})
	return idx
}

func TestGLTFEXTTextureWebP(t *testing.T) {
	p := upTriangle
	p.material = gltf.Index(0)
	p.uv = [][2]float32{{0, 0}, {1, 0}, {0, 1}}
	doc := gltfDoc(p)
	doc.ExtensionsUsed = []string{"EXT_texture_webp", "KHR_materials_emissive_strength"}
	doc.ExtensionsRequired = []string{"EXT_texture_webp"}

	baseImg := writeWebPImage(t, doc, "base", color.NRGBA{210, 90, 40, 200})
	normalImg := writeWebPImage(t, doc, "normal", color.NRGBA{128, 128, 255, 255})
	mrImg := writeWebPImage(t, doc, "mr", color.NRGBA{0, 70, 190, 255})
	occImg := writeWebPImage(t, doc, "occ", color.NRGBA{96, 0, 0, 255})
	emImg := writeWebPImage(t, doc, "em", color.NRGBA{255, 120, 20, 255})

	webpTex := func(src int) *gltf.Texture {
		return &gltf.Texture{
			Extensions: gltf.Extensions{"EXT_texture_webp": rawJSON(t, map[string]any{"source": src})},
		}
	}
	doc.Textures = []*gltf.Texture{
		webpTex(baseImg),
		webpTex(normalImg),
		webpTex(mrImg),
		webpTex(occImg),
		webpTex(emImg),
	}
	doc.Materials = []*gltf.Material{{
		Name:      "webp-pbr",
		AlphaMode: gltf.AlphaBlend,
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{
			MetallicFactor:           gltf.Float(0.9),
			RoughnessFactor:          gltf.Float(0.4),
			BaseColorTexture:         &gltf.TextureInfo{Index: 0},
			MetallicRoughnessTexture: &gltf.TextureInfo{Index: 2},
		},
		NormalTexture:    &gltf.NormalTexture{Index: gltf.Index(1), Scale: gltf.Float(0.75)},
		OcclusionTexture: &gltf.OcclusionTexture{Index: gltf.Index(3), Strength: gltf.Float(0.8)},
		EmissiveFactor:   [3]float64{1, 0.5, 0.25},
		EmissiveTexture:  &gltf.TextureInfo{Index: 4},
		Extensions: gltf.Extensions{
			"KHR_materials_emissive_strength": rawJSON(t, map[string]any{"emissiveStrength": 2.5}),
		},
	}}

	m, err := Decode(glb(t, doc), nil)
	check(t, err)
	if len(m.Warnings) != 0 {
		t.Fatalf("unexpected EXT_texture_webp warnings: %v", m.Warnings)
	}
	mat := m.Objects[0].Geometry.Material
	if mat == nil || mat.Texture == nil || mat.NormalMap == nil || mat.ORMMap == nil || mat.EmissiveMap == nil {
		t.Fatalf("missing decoded WebP maps: %+v", mat)
	}
	if got := mat.Texture.NRGBAAt(0, 0); got != (color.NRGBA{210, 90, 40, 200}) {
		t.Fatalf("WebP base color texel %v, want {210 90 40 200}", got)
	}
	if got := mat.ORMMap.At(0, 0).(color.RGBA); got != (color.RGBA{96, 70, 190, 255}) {
		t.Fatalf("WebP merged ORM texel %v, want {96 70 190 255}", got)
	}
	if got := mat.EmissiveMap.NRGBAAt(0, 0); got != (color.NRGBA{255, 120, 20, 255}) {
		t.Fatalf("WebP emissive texel %v, want {255 120 20 255}", got)
	}

	// Dual-source texture: EXT_texture_webp is preferred over Texture.Source,
	// and Texture.Source serves as fallback when an external WebP side file is absent.
	dualDoc := gltfDoc(p)
	dualDoc.ExtensionsUsed = []string{"EXT_texture_webp"}
	dualDoc.Images = []*gltf.Image{
		{URI: "fallback.png"},
		{URI: "preferred.webp"},
	}
	dualDoc.Textures = []*gltf.Texture{{
		Source:     gltf.Index(0),
		Extensions: gltf.Extensions{"EXT_texture_webp": rawJSON(t, map[string]any{"source": 1})},
	}}
	dualDoc.Materials = []*gltf.Material{{
		PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorTexture: &gltf.TextureInfo{Index: 0}},
	}}
	var rawGLTF bytes.Buffer
	enc := gltf.NewEncoderFS(&rawGLTF, memFS{})
	enc.AsBinary = false
	// Keep buffer embedded so only images use Options.Open.
	dualFiles := memFS{}
	enc = gltf.NewEncoderFS(&rawGLTF, dualFiles)
	enc.AsBinary = false
	dualDoc.Buffers[0].URI = "geom.bin"
	rawGLTF.Reset()
	check(t, enc.Encode(dualDoc))

	pngBytes := pngTexture(t, color.NRGBA{30, 190, 60, 255})
	webpBytes := webpTexture(t, color.NRGBA{230, 110, 25, 255})
	bothSide := map[string][]byte{
		"geom.bin":       dualFiles["geom.bin"].Bytes(),
		"fallback.png":   pngBytes,
		"preferred.webp": webpBytes,
	}
	mBoth, err := Decode(rawGLTF.Bytes(), &nt3dmesh.Options{Open: opener(bothSide)})
	check(t, err)
	if got := mBoth.Objects[0].Geometry.Material.Texture.NRGBAAt(0, 0); got != (color.NRGBA{230, 110, 25, 255}) {
		t.Fatalf("expected EXT_texture_webp source preferred over PNG, got %v", got)
	}

	onlyPNG := map[string][]byte{
		"geom.bin":     dualFiles["geom.bin"].Bytes(),
		"fallback.png": pngBytes,
	}
	mFallback, err := Decode(rawGLTF.Bytes(), &nt3dmesh.Options{Open: opener(onlyPNG)})
	check(t, err)
	if len(mFallback.Warnings) != 0 {
		t.Fatalf("expected clean fallback without warnings, got %v", mFallback.Warnings)
	}
	if got := mFallback.Objects[0].Geometry.Material.Texture.NRGBAAt(0, 0); got != (color.NRGBA{30, 190, 60, 255}) {
		t.Fatalf("expected PNG fallback texel, got %v", got)
	}

	// Invalid EXT_texture_webp extension payloads must be rejected.
	for _, bad := range []any{
		"not-an-object",
		map[string]any{},
		map[string]any{"source": -1},
		map[string]any{"source": 99},
	} {
		badDoc := gltfDoc(p)
		badDoc.Images = []*gltf.Image{{URI: "a.webp"}}
		badDoc.Textures = []*gltf.Texture{{
			Extensions: gltf.Extensions{"EXT_texture_webp": rawJSON(t, bad)},
		}}
		if _, err := Decode(glb(t, badDoc), nil); err == nil {
			t.Fatalf("accepted invalid EXT_texture_webp %v", bad)
		}
	}
}

func TestGLTFSceneAssetSharedGeometryAndCityInstancing(t *testing.T) {
	doc := gltfDoc(upTriangle)
	doc.Nodes = []*gltf.Node{
		{Name: "kit-root", Children: []int{1, 2, 3, 4, 5, 6}},
		{Name: "bollard-0", Mesh: gltf.Index(0), Translation: [3]float64{-2, 0, 0}},
		{Name: "bollard-1", Mesh: gltf.Index(0), Translation: [3]float64{-1, 0, 0}},
		{Name: "bollard-2", Mesh: gltf.Index(0), Translation: [3]float64{0, 0, 0}},
		{Name: "bollard-3", Mesh: gltf.Index(0), Translation: [3]float64{1, 0, 0}},
		{Name: "mirror-0", Mesh: gltf.Index(0), Translation: [3]float64{2, 0, 0}, Scale: [3]float64{-1, 1, 1}},
		{Name: "mirror-1", Mesh: gltf.Index(0), Translation: [3]float64{3, 0, 0}, Scale: [3]float64{-1, 1, 1}},
	}
	doc.Scenes = []*gltf.Scene{{Nodes: []int{0}}}
	doc.Scene = gltf.Index(0)

	m, err := Decode(glb(t, doc), nil)
	check(t, err)
	if len(m.Objects) != 6 {
		t.Fatalf("expected 6 flattened objects, got %d", len(m.Objects))
	}

	s := nt3d.NewScene()
	_, err = m.AddTo(s, nt3d.Root, "kit")
	check(t, err)
	if s.Bounds() != m.Bounds() {
		t.Fatalf("SceneAsset bounds %v != Model.Bounds %v", s.Bounds(), m.Bounds())
	}
	f, err := s.Frame(nt3d.DefaultCamera(), 128, 96)
	check(t, err)
	// 4 unreflected bollards share 1 SharedGeometry batch, and 2 mirrored bollards share 1 reflected SharedGeometry batch.
	if stats := f.Stats(); stats.DrawnInstances != 6 || stats.Batches != 2 {
		t.Fatalf("expected 6 instances across 2 shared batches (normal + mirrored), got %+v", stats)
	}

	// Instantiate a single-prop GLB 12 times across geographic city ENU coordinates:
	// all 12 copies must share 1 SharedGeometry ID and 1 draw batch!
	propDoc := gltfDoc(upTriangle)
	propModel, err := Decode(glb(t, propDoc), nil)
	check(t, err)

	city := nt3d.NewScene()
	cityOrigin := [3]float64{583960.5, 4507523.25, 18.0}
	for i := range 12 {
		root, err := propModel.AddTo(city, nt3d.Root, fmt.Sprintf("streetlamp-%d", i))
		check(t, err)
		check(t, city.SetNodeOrigin(root, [3]float64{cityOrigin[0] + float64(i)*0.25, cityOrigin[1], cityOrigin[2]}))
	}
	cam := nt3d.DefaultCamera()
	cam.Origin = cityOrigin
	cam.Distance = 8
	cityFrame, err := city.Frame(cam, 160, 120)
	check(t, err)
	if stats := cityFrame.Stats(); stats.DrawnInstances != 12 || stats.Batches != 1 {
		t.Fatalf("expected 12 city props in 1 shared batch, got %+v", stats)
	}
	firstShared := cityFrame.Shared[0]
	if firstShared == 0 {
		t.Fatal("expected nonzero Frame.Shared ID on instantiated glTF prop")
	}
	for i, id := range cityFrame.Shared {
		if id != firstShared {
			t.Fatalf("instance %d has Shared ID %d, want %d", i, id, firstShared)
		}
	}
}

func TestGLTFEXTMeshGPUInstancing(t *testing.T) {
	for _, rotEncoding := range []string{"float", "byte", "short"} {
		t.Run(rotEncoding, func(t *testing.T) {
			doc := gltfDoc(upTriangle)
			doc.ExtensionsUsed = []string{"EXT_mesh_gpu_instancing"}
			doc.ExtensionsRequired = []string{"EXT_mesh_gpu_instancing"}

			translations := [][3]float32{
				{-1.5, 0, 0},
				{0, 0.5, -0.25},
				{1.5, -0.25, 0.5},
				{2.5, 0, 0},
			}
			s45 := float32(math.Sin(math.Pi / 8))
			c45 := float32(math.Cos(math.Pi / 8))
			quats := [][4]float32{
				{0, 0, 0, 1},
				{0, s45, 0, c45},
				{0, 0, s45, c45},
				{0, 0, 0, 1},
			}
			scales := [][3]float32{
				{1, 1, 1},
				{0.8, 1.2, 0.9},
				{1.1, 0.9, 1.0},
				{-1, 1, 1}, // Mirrored instance scale!
			}

			tAcc := modeler.WritePosition(doc, translations)
			sAcc := modeler.WritePosition(doc, scales)
			idAcc := modeler.WriteAccessor(doc, gltf.TargetNone, []uint32{10, 20, 30, 40})

			var rAcc int
			switch rotEncoding {
			case "float":
				rAcc = modeler.WriteAccessor(doc, gltf.TargetNone, quats)
			case "byte":
				q8 := make([][4]int8, len(quats))
				for i, q := range quats {
					q8[i] = [4]int8{gltf.NormalizeByte(q[0]), gltf.NormalizeByte(q[1]), gltf.NormalizeByte(q[2]), gltf.NormalizeByte(q[3])}
				}
				rAcc = modeler.WriteAccessor(doc, gltf.TargetNone, q8)
				doc.Accessors[rAcc].Normalized = true
			case "short":
				q16 := make([][4]int16, len(quats))
				for i, q := range quats {
					q16[i] = [4]int16{gltf.NormalizeShort(q[0]), gltf.NormalizeShort(q[1]), gltf.NormalizeShort(q[2]), gltf.NormalizeShort(q[3])}
				}
				rAcc = modeler.WriteAccessor(doc, gltf.TargetNone, q16)
				doc.Accessors[rAcc].Normalized = true
			}

			// Add a child mesh node under node 0 to verify Khronos rule:
			// EXT_mesh_gpu_instancing applies ONLY to node 0's mesh, NOT to children!
			doc.Meshes = append(doc.Meshes, &gltf.Mesh{
				Name:       "sign-cap",
				Primitives: []*gltf.Primitive{doc.Meshes[0].Primitives[0]},
			})
			doc.Nodes = []*gltf.Node{
				{
					Name:        "instanced-trees",
					Mesh:        gltf.Index(0),
					Translation: [3]float64{0.25, 0, -0.5},
					Children:    []int{1},
					Extensions: gltf.Extensions{
						"EXT_mesh_gpu_instancing": rawJSON(t, map[string]any{
							"attributes": map[string]any{
								"TRANSLATION": tAcc,
								"ROTATION":    rAcc,
								"SCALE":       sAcc,
								"_ID":         idAcc,
							},
						}),
					},
				},
				{
					Name:        "child-marker",
					Mesh:        gltf.Index(1),
					Translation: [3]float64{0, 2, 0},
				},
			}
			doc.Scenes = []*gltf.Scene{{Nodes: []int{0}}}
			doc.Scene = gltf.Index(0)

			m, err := Decode(glb(t, doc), nil)
			check(t, err)
			if len(m.Warnings) != 0 {
				t.Fatalf("unexpected warnings: %v", m.Warnings)
			}
			// 4 instanced trees + 1 non-instanced child marker = 5 objects!
			if len(m.Objects) != 5 {
				t.Fatalf("expected 5 objects (4 instances + 1 child), got %d", len(m.Objects))
			}

			sAsset := nt3d.NewScene()
			_, err = m.AddTo(sAsset, nt3d.Root, "instanced")
			check(t, err)
			for c := range 3 {
				gotMin := [3]float32{sAsset.Bounds().Min.X, sAsset.Bounds().Min.Y, sAsset.Bounds().Min.Z}[c]
				wantMin := [3]float32{m.Bounds().Min.X, m.Bounds().Min.Y, m.Bounds().Min.Z}[c]
				gotMax := [3]float32{sAsset.Bounds().Max.X, sAsset.Bounds().Max.Y, sAsset.Bounds().Max.Z}[c]
				wantMax := [3]float32{m.Bounds().Max.X, m.Bounds().Max.Y, m.Bounds().Max.Z}[c]
				if math.Abs(float64(gotMin-wantMin)) > 1e-4 || math.Abs(float64(gotMax-wantMax)) > 1e-4 {
					t.Fatalf("SceneAsset bounds %v != Model.Bounds %v", sAsset.Bounds(), m.Bounds())
				}
			}

			sFlat := nt3d.NewScene()
			for i, o := range m.Objects {
				_, err := sFlat.SetGeometry(fmt.Sprintf("flat-%d", i), o.Geometry)
				check(t, err)
			}
			cam := nt3d.DefaultCamera()
			cam.Distance = 8
			fAsset, err := sAsset.Frame(cam, 120, 90)
			check(t, err)
			fFlat, err := sFlat.Frame(cam, 120, 90)
			check(t, err)
			// 3 positive-parity instances share 1 SharedGeometry, 1 mirrored instance has its reflected SharedGeometry, and 1 child has its own SharedGeometry -> 3 batches total!
			if stats := fAsset.Stats(); stats.DrawnInstances != 5 || stats.Batches != 3 {
				t.Fatalf("unexpected SceneAsset stats: %+v", stats)
			}
			imgAsset, err := nt3d.NewSoftwareRenderer().Render(fAsset)
			check(t, err)
			imgFlat, err := nt3d.NewSoftwareRenderer().Render(fFlat)
			check(t, err)
			if !bytes.Equal(imgAsset.(*image.RGBA).Pix, imgFlat.(*image.RGBA).Pix) {
				t.Fatal("SceneAsset instanced render differs from flattened Model.Objects render")
			}
		})
	}

	// Empty attributes object renders 0 instances of that node while keeping sibling meshes.
	emptyAttrDoc := gltfDoc(upTriangle)
	emptyAttrDoc.Nodes = []*gltf.Node{
		{
			Name: "empty-instanced",
			Mesh: gltf.Index(0),
			Extensions: gltf.Extensions{
				"EXT_mesh_gpu_instancing": rawJSON(t, map[string]any{"attributes": map[string]any{}}),
			},
		},
		{
			Name: "visible-sibling",
			Mesh: gltf.Index(0),
		},
	}
	emptyAttrDoc.Scenes = []*gltf.Scene{{Nodes: []int{0, 1}}}
	emptyAttrm, err := Decode(glb(t, emptyAttrDoc), nil)
	check(t, err)
	if len(emptyAttrm.Objects) != 1 || emptyAttrm.Objects[0].Name != "visible-sibling" {
		t.Fatalf("expected only visible-sibling object, got %+v", emptyAttrm.Objects)
	}

	// Budget limit enforced across instances.
	budgetDoc := gltfDoc(upTriangle)
	tAcc := modeler.WritePosition(budgetDoc, [][3]float32{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}})
	budgetDoc.Nodes[0].Extensions = gltf.Extensions{
		"EXT_mesh_gpu_instancing": rawJSON(t, map[string]any{"attributes": map[string]any{"TRANSLATION": tAcc}}),
	}
	if _, err := Decode(glb(t, budgetDoc), &nt3dmesh.Options{MaxTriangles: 2}); !errors.Is(err, nt3dmesh.ErrTooManyTriangles) {
		t.Fatalf("expected ErrTooManyTriangles, got %v", err)
	}

	// Validation refusals.
	t.Run("validation-errors", func(t *testing.T) {
		// Mismatched counts.
		mismatchDoc := gltfDoc(upTriangle)
		a2 := modeler.WritePosition(mismatchDoc, [][3]float32{{0, 0, 0}, {1, 0, 0}})
		a3 := modeler.WritePosition(mismatchDoc, [][3]float32{{1, 1, 1}, {1, 1, 1}, {1, 1, 1}})
		mismatchDoc.Nodes[0].Extensions = gltf.Extensions{
			"EXT_mesh_gpu_instancing": rawJSON(t, map[string]any{"attributes": map[string]any{"TRANSLATION": a2, "SCALE": a3}}),
		}
		fails(t, func() error { _, e := Decode(glb(t, mismatchDoc), nil); return e }(), "counts differ")

		// Unnormalized int8 ROTATION.
		unnormDoc := gltfDoc(upTriangle)
		rUnnorm := modeler.WriteAccessor(unnormDoc, gltf.TargetNone, [][4]int8{{0, 0, 0, 127}})
		unnormDoc.Nodes[0].Extensions = gltf.Extensions{
			"EXT_mesh_gpu_instancing": rawJSON(t, map[string]any{"attributes": map[string]any{"ROTATION": rUnnorm}}),
		}
		fails(t, func() error { _, e := Decode(glb(t, unnormDoc), nil); return e }(), "normalized")

		// Zero quaternion.
		zeroQuatDoc := gltfDoc(upTriangle)
		rZero := modeler.WriteAccessor(zeroQuatDoc, gltf.TargetNone, [][4]float32{{0, 0, 0, 0}})
		zeroQuatDoc.Nodes[0].Extensions = gltf.Extensions{
			"EXT_mesh_gpu_instancing": rawJSON(t, map[string]any{"attributes": map[string]any{"ROTATION": rZero}}),
		}
		fails(t, func() error { _, e := Decode(glb(t, zeroQuatDoc), nil); return e }(), "zero")

		// Zero scale.
		zeroScaleDoc := gltfDoc(upTriangle)
		sZero := modeler.WritePosition(zeroScaleDoc, [][3]float32{{0, 1, 1}})
		zeroScaleDoc.Nodes[0].Extensions = gltf.Extensions{
			"EXT_mesh_gpu_instancing": rawJSON(t, map[string]any{"attributes": map[string]any{"SCALE": sZero}}),
		}
		fails(t, func() error { _, e := Decode(glb(t, zeroScaleDoc), nil); return e }(), "scale")

		// Unknown non-underscore attribute.
		unknownAttrDoc := gltfDoc(upTriangle)
		tValid := modeler.WritePosition(unknownAttrDoc, [][3]float32{{0, 0, 0}})
		unknownAttrDoc.Nodes[0].Extensions = gltf.Extensions{
			"EXT_mesh_gpu_instancing": rawJSON(t, map[string]any{"attributes": map[string]any{"WEIGHTS": tValid}}),
		}
		fails(t, func() error { _, e := Decode(glb(t, unknownAttrDoc), nil); return e }(), "unknown")
	})
}
