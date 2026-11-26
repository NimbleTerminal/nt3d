// Copyright (c) 2026 Neomantra Corp.

package nt3dmesh

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"

	"nimbleterminal.dev/nt3d"
)

// flatPNG is a one-color PNG of the given side: few bytes in the file, side
// × side × 4 once decoded.
func flatPNG(t *testing.T, side int) []byte {
	t.Helper()
	var buf bytes.Buffer
	check(t, png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, side, side))))
	return buf.Bytes()
}

// textureOf is the texture of the first object that has one.
func textureOf(m *Model) *nt3d.Texture {
	for _, o := range m.Objects {
		if o.Geometry.Material != nil && o.Geometry.Material.Texture != nil {
			return o.Geometry.Material.Texture
		}
	}
	return nil
}

// A texture's decoded pixels count against MaxBytes in every decoder that
// reads one: a small file that decodes to more than the budget is left out
// with a warning, as any texture that cannot be used is, and not allocated.
func TestDecodedTexturePixelsCountAgainstMaxBytes(t *testing.T) {
	file := flatPNG(t, 512) // 1 MiB of pixels
	if len(file) > 16<<10 {
		t.Fatalf("the flat PNG is %d bytes; the test needs it small", len(file))
	}
	objFiles := cubeFiles(t)
	objFiles["tex/wood.png"] = file
	ply := "ply\nformat ascii 1.0\ncomment TextureFile wood.png\nelement vertex 3\nproperty float x\nproperty float y\nproperty float z\nproperty float s\nproperty float t\nelement face 1\nproperty list uchar uint vertex_indices\nend_header\n0 0 0 0 0\n1 0 0 1 0\n0 1 0 0 1\n3 0 1 2\n"
	textured := strings.Replace(tetrahedron, `<triangle v1="1" v2="2" v3="3"/>`, `<triangle v1="1" v2="2" v3="3" pid="7" p1="0" p2="1" p3="2"/>`, 1)
	model := model3MF(`xmlns:m="http://schemas.microsoft.com/3dmanufacturing/material/2015/02"`, `<resources>
		<m:texture2d id="6" path="/3D/Textures/wood.png" contenttype="image/png"/>
		<m:texture2dgroup id="7" texid="6"><m:tex2coord u="0" v="0"/><m:tex2coord u="1" v="0"/><m:tex2coord u="0" v="1"/></m:texture2dgroup>
		<object id="1">`+textured+`</object></resources><build><item objectid="1"/></build>`)
	package3MF := archive3MF(t, map[string]string{"3D/3dmodel.model": model, "3D/Textures/wood.png": string(file)})

	for _, c := range []struct {
		name   string
		decode func(o *Options) (*Model, error)
	}{
		{"OBJ", func(o *Options) (*Model, error) { o.Open = opener(objFiles); return DecodeOBJ([]byte(cubeOBJ), o) }},
		{"PLY", func(o *Options) (*Model, error) {
			o.Open = opener(map[string][]byte{"wood.png": file})
			return DecodePLY([]byte(ply), o)
		}},
		{"3MF", func(o *Options) (*Model, error) { return Decode3MF(package3MF, o) }},
	} {
		m, err := c.decode(&Options{MaxBytes: 2 << 20})
		if err != nil || textureOf(m) == nil || len(m.Warnings) != 0 {
			t.Errorf("%s within the budget: err %v, warnings %v, textured %v", c.name, err, m.Warnings, textureOf(m) != nil)
			continue
		}
		m, err = c.decode(&Options{MaxBytes: 64 << 10})
		if err != nil {
			t.Errorf("%s: %v, want the model without its texture", c.name, err)
			continue
		}
		if textureOf(m) != nil {
			t.Errorf("%s decoded 1 MiB of pixels in a 64 KiB budget", c.name)
		}
		if len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "wood.png") {
			t.Errorf("%s warnings %q, want one naming the texture", c.name, m.Warnings)
		}
	}
}

// OBJ materials that name one texture file share its pixels: the file is
// decoded and charged once.
func TestOBJMaterialsOfOneTextureDecodeItOnce(t *testing.T) {
	var mtl, obj strings.Builder
	obj.WriteString("mtllib m.mtl\nv 0 0 0\nv 1 0 0\nv 0 1 0\nvt 0 0\nvt 1 0\nvt 0 1\n")
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		mtl.WriteString("newmtl " + name + "\nmap_Kd t.png\n")
		obj.WriteString("usemtl " + name + "\nf 1/1 2/2 3/3\n")
	}
	files := map[string][]byte{"m.mtl": []byte(mtl.String()), "t.png": flatPNG(t, 512)}
	// Room for the image once (1 MiB), not eight times.
	m, err := DecodeOBJ([]byte(obj.String()), &Options{Open: opener(files), MaxBytes: 2 << 20})
	check(t, err)
	if len(m.Warnings) != 0 || len(m.Objects) != 8 {
		t.Fatalf("%d objects, warnings %v", len(m.Objects), m.Warnings)
	}
	first := m.Objects[0].Geometry.Material
	for i, o := range m.Objects {
		if o.Geometry.Material == nil || o.Geometry.Material.Texture == nil {
			t.Fatalf("object %d has no texture", i)
		}
		if o.Geometry.Material.Texture != first.Texture {
			t.Fatalf("object %d has its own copy of the texture", i)
		}
	}
}
