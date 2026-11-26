// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"testing"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"

	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

// flatPNG is a one-color PNG of the given side: few bytes in the file, side
// × side × 4 once decoded.
func flatPNG(t *testing.T, side int) []byte {
	t.Helper()
	var buf bytes.Buffer
	check(t, png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, side, side))))
	return buf.Bytes()
}

// texturedTriangles is a document of n triangles, each with its own material
// and its own texture, every texture naming the one image.
func texturedTriangles(t *testing.T, n int, file []byte) *gltf.Document {
	t.Helper()
	prims := make([]prim, n)
	for i := range prims {
		prims[i] = upTriangle
		prims[i].material = gltf.Index(i)
		prims[i].uv = [][2]float32{{0, 0}, {1, 0}, {0, 1}}
	}
	doc := gltfDoc(prims...)
	bv := modeler.WriteBufferView(doc, gltf.TargetNone, file)
	doc.Images = []*gltf.Image{{MimeType: "image/png", BufferView: gltf.Index(bv)}}
	for i := range prims {
		doc.Textures = append(doc.Textures, &gltf.Texture{Source: gltf.Index(0)})
		doc.Materials = append(doc.Materials, &gltf.Material{
			PBRMetallicRoughness: &gltf.PBRMetallicRoughness{BaseColorTexture: &gltf.TextureInfo{Index: i}},
		})
	}
	return doc
}

// A PNG's decoded pixels count against MaxBytes, as a KTX2 image's do: a
// small file that decodes to more than the budget is refused, not allocated.
func TestDecodedImagePixelsCountAgainstMaxBytes(t *testing.T) {
	file := flatPNG(t, 512) // 1 MiB of pixels
	if len(file) > 16<<10 {
		t.Fatalf("the flat PNG is %d bytes; the test needs it small", len(file))
	}
	data := glb(t, texturedTriangles(t, 1, file))
	if _, err := Decode(data, &nt3dmesh.Options{MaxBytes: 64 << 10}); !errors.Is(err, nt3dmesh.ErrTooLarge) {
		t.Fatalf("Decode = %v, want ErrTooLarge for 1 MiB of pixels in a 64 KiB budget", err)
	}
	if _, err := Decode(data, &nt3dmesh.Options{MaxBytes: 2 << 20}); err != nil {
		t.Fatalf("Decode within the budget: %v", err)
	}
}

// Textures that name one image share its pixels: the image is decoded and
// charged once, however many textures use it.
func TestTexturesOfOneImageDecodeItOnce(t *testing.T) {
	data := glb(t, texturedTriangles(t, 8, flatPNG(t, 512)))
	// Room for the image once (1 MiB), not eight times.
	if _, err := Decode(data, &nt3dmesh.Options{MaxBytes: 2 << 20}); err != nil {
		t.Fatalf("Decode = %v, want eight textures of one image to fit a budget that holds it once", err)
	}
}
