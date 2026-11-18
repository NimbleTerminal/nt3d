package nt3d

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// wgslLayout computes the size and alignment WGSL gives the types the scene
// shader declares, by the language's layout rules, so that the byte counts Go
// writes can be held to the structs the shader reads.
type wgslLayout struct {
	consts  map[string]int
	structs map[string]string // name -> body
}

func parseWGSLLayout(src string) wgslLayout {
	l := wgslLayout{consts: map[string]int{}, structs: map[string]string{}}
	for _, m := range regexp.MustCompile(`const\s+(\w+)\s*:\s*u32\s*=\s*(\d+)u?\s*;`).FindAllStringSubmatch(src, -1) {
		l.consts[m[1]], _ = strconv.Atoi(m[2])
	}
	for _, m := range regexp.MustCompile(`struct\s+(\w+)\s*\{([^}]*)\}`).FindAllStringSubmatch(src, -1) {
		l.structs[m[1]] = m[2]
	}
	return l
}

func roundUp(n, align int) int { return (n + align - 1) / align * align }

// sizeAlign is a type's size and alignment in bytes.
func (l wgslLayout) sizeAlign(typ string) (size, align int, err error) {
	typ = strings.ReplaceAll(typ, " ", "")
	switch typ {
	case "f32", "u32", "i32":
		return 4, 4, nil
	case "vec2<f32>", "vec2<u32>", "vec2<i32>":
		return 8, 8, nil
	case "vec3<f32>", "vec3<u32>", "vec3<i32>":
		return 12, 16, nil
	case "vec4<f32>", "vec4<u32>", "vec4<i32>":
		return 16, 16, nil
	case "mat4x4<f32>":
		return 64, 16, nil
	}
	if inner, ok := strings.CutPrefix(typ, "array<"); ok {
		inner = strings.TrimSuffix(inner, ">")
		i := strings.LastIndex(inner, ",")
		if i < 0 {
			return 0, 0, fmt.Errorf("runtime-sized %s has no size", typ)
		}
		count, isConst := l.consts[inner[i+1:]]
		if !isConst {
			if count, err = strconv.Atoi(inner[i+1:]); err != nil {
				return 0, 0, fmt.Errorf("array count %q", inner[i+1:])
			}
		}
		size, align, err = l.sizeAlign(inner[:i])
		return count * roundUp(size, align), align, err
	}
	body, ok := l.structs[typ]
	if !ok {
		return 0, 0, fmt.Errorf("unknown type %q", typ)
	}
	offset := 0
	align = 1
	for _, field := range splitFields(body) {
		name, ftyp, ok := strings.Cut(field, ":")
		if !ok {
			return 0, 0, fmt.Errorf("field %q of %s", field, typ)
		}
		if strings.Contains(name, "@") {
			return 0, 0, fmt.Errorf("%s is a shader interface, not a buffer layout", typ)
		}
		fsize, falign, err := l.sizeAlign(ftyp)
		if err != nil {
			return 0, 0, err
		}
		offset = roundUp(offset, falign) + fsize
		align = max(align, falign)
	}
	return roundUp(offset, align), align, nil
}

// splitFields splits a struct body at the commas between fields, not those
// inside a type's angle brackets.
func splitFields(body string) []string {
	var fields []string
	depth, start := 0, 0
	for i, c := range body {
		switch c {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				fields = append(fields, strings.TrimSpace(body[start:i]))
				start = i + 1
			}
		}
	}
	if last := strings.TrimSpace(body[start:]); last != "" {
		fields = append(fields, last)
	}
	return fields
}

// The scene shader reads buffers Go fills by appending values in order. The
// two are written apart, so a field added to one and not the other shifts
// everything after it without an error from either. Hold each Go byte count
// to the struct the shader declares for it.
func TestGPUBufferLayoutsMatchTheShader(t *testing.T) {
	layout := parseWGSLLayout(sceneShaderSource())
	for _, c := range []struct {
		wgsl string
		what string
		want int
	}{
		{"Uniforms", "uniformSize, the scene uniform", uniformSize},
		{"Instance", "instanceRecordSize, an instance's transform and material", instanceRecordSize},
		{"Item", "a mesh, box, line, or arrow record", 48},
		{"PointRecord", "pointRecord, a point", pointRecord},
		{"Light", "a light in the scene uniform", 48},
		{"ReflectionProbe", "a reflection probe in the scene uniform", 48},
	} {
		size, _, err := layout.sizeAlign(c.wgsl)
		if err != nil {
			t.Errorf("%s: %v", c.wgsl, err)
			continue
		}
		// A storage array's stride is the struct's size; a point's is packed
		// to its five words, which the shader's alignment of 4 allows.
		if size != c.want {
			t.Errorf("the shader's %s is %d bytes, but Go writes %d for %s", c.wgsl, size, c.want, c.what)
		}
	}
}

// The layout rules themselves, on shapes whose sizes are known.
func TestWGSLLayoutRules(t *testing.T) {
	l := parseWGSLLayout("const n: u32 = 3u;\nstruct A { a: vec3<f32>, b: f32 };\nstruct B { a: f32, b: vec3<f32> };\nstruct C { m: mat4x4<f32>, v: array<vec4<f32>, n>, a: array<A, 2> };")
	for typ, want := range map[string]int{"A": 16, "B": 32, "C": 64 + 48 + 32, "array<vec3<f32>,2>": 32} {
		if size, _, err := l.sizeAlign(typ); err != nil || size != want {
			t.Errorf("%s is %d bytes (%v), want %d", typ, size, err, want)
		}
	}
}

// The encoder for an instance writes exactly the record the shader declares.
func TestInstanceRecordIsItsDeclaredSize(t *testing.T) {
	record := transformRecord(nt3dmath.Identity(), 0, 0, 0, false, true, OutlineStyle{}, false, 0, [8]float32{}, ToonStyle{}, [4]float32{}, [4]float32{}, [4]float32{}, [4]float32{})
	if len(record) != instanceRecordSize {
		t.Fatalf("an instance record is %d bytes, instanceRecordSize is %d", len(record), instanceRecordSize)
	}
}

// Every scene shader file is joined into the module, and every part named
// has its file: a file added to wgsl/ and not to sceneShaderParts would
// otherwise be left out without a word.
func TestSceneShaderPartsAreAllJoined(t *testing.T) {
	entries, err := sceneShaderFiles.ReadDir("wgsl")
	check(t, err)
	listed := map[string]bool{}
	for _, part := range sceneShaderParts {
		listed["scene_"+part+".wgsl"] = true
	}
	for _, e := range entries {
		if !listed[e.Name()] {
			t.Errorf("wgsl/%s is embedded but not in sceneShaderParts", e.Name())
		}
		delete(listed, e.Name())
	}
	for name := range listed {
		t.Errorf("sceneShaderParts names %s, which is not in wgsl/", name)
	}
}
