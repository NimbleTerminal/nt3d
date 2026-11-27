// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
	"nimbleterminal.dev/nt3d/mesh"
	"nimbleterminal.dev/nt3d/mesh/spz"
	"nimbleterminal.dev/nt3d/sh"
)

// gaussianSplatting is the extension a splat primitive carries, and the
// prefix of its attributes.
const gaussianSplatting = "KHR_gaussian_splatting"

// gaussians reads a KHR_gaussian_splatting primitive as a Gaussian cloud and
// reports whether p was one. The extension stores what a 3DGS file does in
// plainer units: a unit x, y, z, w quaternion, linear scales, linear
// opacity, and real spherical-harmonic coefficients of the color, the
// degree-0 one giving the color at rest as 0.5 + Y00 times it.
//
// The cloud is built in the primitive's own space, which is glTF's and Y-up,
// and carried through world as any primitive is. world ends in the turn to
// Z-up in the flat view and is the identity in the asset's, whose root node
// turns; either way the cloud and its harmonics are turned once.
//
// A kernel other than "ellipse" is not drawn as splats: the primitive falls
// back to plain points, as the extension allows a renderer that does not
// know it, and false is returned with a warning.
func (d *gltfDecoder) gaussians(name string, p *gltf.Primitive, world mat4) (bool, error) {
	fail := func(format string, a ...any) (bool, error) {
		return false, fmt.Errorf("nt3dmesh: glTF %s splats: %s", name, fmt.Sprintf(format, a...))
	}
	// The draft that came before the ratified extension put the SPZ payload
	// on the primitive itself.
	var legacy spzCompression
	if ok, err := decodeExtension(p.Extensions, "KHR_spz_gaussian_splats_compression", &legacy); err != nil {
		return false, err
	} else if ok {
		return d.spzSplats(name, p, world, legacy)
	}
	var ext struct {
		Kernel     *string                    `json:"kernel"`
		ColorSpace *string                    `json:"colorSpace"`
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	ok, err := decodeExtension(p.Extensions, gaussianSplatting, &ext)
	if err != nil || !ok {
		return false, err
	}
	if p.Mode != gltf.PrimitivePoints {
		return fail("a %s primitive must be POINTS", gaussianSplatting)
	}
	if ext.Kernel == nil || ext.ColorSpace == nil {
		return fail("%s needs a kernel and a colorSpace", gaussianSplatting)
	}
	if *ext.Kernel != "ellipse" {
		d.warn("%s: splat kernel %q is not drawn; showing plain points", name, *ext.Kernel)
		return false, nil
	}
	// Colors are drawn as display sRGB, which is what a trained capture
	// holds. Linear coefficients cannot be converted exactly, harmonics
	// being a sum, so they are shown as they are, and said to be.
	if *ext.ColorSpace != "srgb_rec709_display" {
		d.warn("%s: splat color space %q is drawn as srgb_rec709_display", name, *ext.ColorSpace)
	}
	for _, compression := range spzCompressionNames {
		raw, ok := ext.Extensions[compression]
		if !ok {
			continue
		}
		var spz spzCompression
		if err := json.Unmarshal(raw, &spz); err != nil {
			return fail("%s: %v", compression, err)
		}
		return d.spzSplats(name, p, world, spz)
	}
	position, ok := p.Attributes[gltf.POSITION]
	if !ok {
		return fail("no POSITION attribute")
	}
	positionAccessor, err := d.accessor(position, gltf.POSITION)
	if err != nil {
		return false, err
	}
	positions, err := d.readPositions(positionAccessor)
	if err != nil {
		return fail("POSITION: %v", err)
	}
	n := len(positions)
	attr := func(key string, kind gltf.AccessorType) (*gltf.Accessor, error) {
		index, ok := p.Attributes[gaussianSplatting+":"+key]
		if !ok {
			return nil, fmt.Errorf("nt3dmesh: glTF %s splats: no %s attribute", name, key)
		}
		a, err := d.accessor(index, key)
		if err != nil {
			return nil, err
		}
		if a.Type != kind || a.Count != n {
			return nil, fmt.Errorf("nt3dmesh: glTF %s splats: %s must be %s with one value a splat, %d", name, key, kind, n)
		}
		if a.BufferView == nil && a.Sparse == nil && len(ext.Extensions) > 0 {
			// The data is somewhere an extension of the extension knows.
			// With none, an accessor with no buffer view is all zeros, as
			// glTF defines it, and is read as that.
			var names []string
			for compression := range ext.Extensions {
				names = append(names, compression)
			}
			sort.Strings(names)
			return nil, fmt.Errorf("nt3dmesh: glTF %s splats: %s is stored by %s, which is not read", name, key, names[0])
		}
		return a, nil
	}
	read := func(key string, kind gltf.AccessorType) (any, bool, error) {
		a, err := attr(key, kind)
		if err != nil {
			return nil, false, err
		}
		if a.ComponentType == gltf.ComponentFloat && a.Normalized {
			return nil, false, fmt.Errorf("nt3dmesh: glTF %s splats: float %s must not be normalized", name, key)
		}
		raw, err := modeler.ReadAccessor(d.doc, a, nil)
		if err != nil {
			return nil, false, fmt.Errorf("nt3dmesh: glTF %s splats: %s: %w", name, key, err)
		}
		return raw, a.Normalized, nil
	}
	// What the asset pass may hold decoded: 16 floats of geometry a splat.
	if err := d.decoded.Take(int64(n)*64, "glTF splats decoded"); err != nil {
		return false, err
	}
	raw, normalized, err := read("ROTATION", gltf.AccessorVec4)
	if err != nil {
		return false, err
	}
	rotations, err := splatRotations(raw, normalized)
	if err != nil {
		return fail("ROTATION: %v", err)
	}
	if raw, normalized, err = read("SCALE", gltf.AccessorVec3); err != nil {
		return false, err
	}
	scales, err := convertVec3(raw, normalized)
	if err != nil {
		return fail("SCALE: %v", err)
	}
	if raw, normalized, err = read("OPACITY", gltf.AccessorScalar); err != nil {
		return false, err
	}
	opacities, err := splatOpacities(raw, normalized)
	if err != nil {
		return fail("OPACITY: %v", err)
	}
	// Harmonics: whole degrees, each one's coefficients in order of m.
	var harmonics [][][3]float32
	for degree := 0; degree <= 3; degree++ {
		first := fmt.Sprintf("SH_DEGREE_%d_COEF_0", degree)
		if _, present := p.Attributes[gaussianSplatting+":"+first]; !present {
			if degree == 0 {
				return fail("no %s attribute", first)
			}
			break
		}
		for m := range 2*degree + 1 {
			key := fmt.Sprintf("SH_DEGREE_%d_COEF_%d", degree, m)
			a, err := attr(key, gltf.AccessorVec3)
			if err != nil {
				return false, err
			}
			if a.ComponentType != gltf.ComponentFloat || a.Normalized {
				return fail("%s must be float", key)
			}
			if err := d.decoded.Take(int64(n)*12, "glTF splat harmonics decoded"); err != nil {
				return false, err
			}
			raw, err := modeler.ReadAccessor(d.doc, a, nil)
			if err != nil {
				return fail("%s: %v", key, err)
			}
			values, ok := raw.([][3]float32)
			if !ok {
				return fail("%s must be float", key)
			}
			harmonics = append(harmonics, values)
		}
	}
	count := len(harmonics)
	g := nt3d.Geometry{
		PointStyle: nt3d.PointStyle{Shape: nt3d.GaussianPoint, Units: nt3d.WorldUnits, Blend: nt3d.AlphaBlend},
		Points:     make([]nt3d.Point, 0, n),
		GaussianSH: make([]nt3dsh.RGB, 0, n*count),
	}
	for i := range n {
		var logScales, dc [3]float64
		for k := range 3 {
			s := float64(scales[i][k])
			if !(s >= 0) {
				return fail("scale %g is negative or not a number", s)
			}
			// A scale of zero is allowed and flat; the smallest a splat is
			// drawn with stands in for it.
			logScales[k] = math.Log(math.Max(s, 1e-20))
			dc[k] = float64(harmonics[0][i][k])
		}
		opacity := float64(opacities[i])
		if !(opacity >= 0 && opacity <= 1) {
			return fail("opacity %g is outside 0 to 1", opacity)
		}
		q := rotations[i]
		point, err := nt3dmesh.GaussianPoint(nt3dmath.Vec3{X: positions[i][0], Y: positions[i][1], Z: positions[i][2]},
			[4]float64{float64(q[3]), float64(q[0]), float64(q[1]), float64(q[2])}, logScales, dc, opacity)
		if err != nil {
			return fail("splat %d: %v", i, err)
		}
		g.Points = append(g.Points, point)
		for _, coefficient := range harmonics {
			c := nt3dsh.RGB(coefficient[i])
			if !nt3dmath.IsFinite(float64(c[0])) || !nt3dmath.IsFinite(float64(c[1])) || !nt3dmath.IsFinite(float64(c[2])) {
				return fail("an SH coefficient of splat %d is not finite", i)
			}
			g.GaussianSH = append(g.GaussianSH, c)
		}
	}
	if err := d.indexSplats(name, p, &g); err != nil {
		return false, err
	}
	g.Bounds = nt3d.GeometryBounds(g)
	return d.finishSplats(name, g, world)
}

// indexSplats applies a splat primitive's indices to the cloud its
// attributes gave: the splats are the records the indices name, in their
// order, as an indexed POINTS primitive draws the vertices named. A
// primitive without indices is left as it is.
func (d *gltfDecoder) indexSplats(name string, p *gltf.Primitive, g *nt3d.Geometry) error {
	if p.Indices == nil {
		return nil
	}
	a, err := d.accessor(*p.Indices, "indices")
	if err != nil {
		return err
	}
	indices, err := modeler.ReadIndices(d.doc, a, nil)
	if err != nil {
		return fmt.Errorf("nt3dmesh: glTF %s splats: indices: %w", name, err)
	}
	n := len(g.Points)
	per := 0
	if n > 0 {
		per = len(g.GaussianSH) / n
	}
	// Indices may name a record many times: what they select is counted as
	// what the records were.
	if err := d.decoded.Take(int64(len(indices))*int64(64+12*per), "glTF splats decoded"); err != nil {
		return err
	}
	points := make([]nt3d.Point, 0, len(indices))
	harmonics := make([]nt3dsh.RGB, 0, len(indices)*per)
	for _, i := range indices {
		if int(i) >= n {
			return fmt.Errorf("nt3dmesh: glTF %s splats: index %d is past its %d records", name, i, n)
		}
		points = append(points, g.Points[i])
		harmonics = append(harmonics, g.GaussianSH[int(i)*per:int(i)*per+per]...)
	}
	g.Points, g.GaussianSH = points, harmonics
	g.Bounds = nt3d.GeometryBounds(*g)
	return nil
}

// spzCompressionNames are the names the SPZ compression of a splat primitive
// has gone by inside KHR_gaussian_splatting: the draft's, and the one its own
// examples use.
var spzCompressionNames = []string{"KHR_gaussian_splatting_compression_spz_2", "KHR_gaussian_splatting_compression_spz"}

// spzCompression is what each of those extensions holds: the buffer view of
// an SPZ payload.
type spzCompression struct {
	BufferView *int `json:"bufferView"`
}

// spzSplats reads a splat primitive whose data is one SPZ payload in a buffer
// view. The primitive's accessors then describe the result and hold nothing;
// only the count of POSITION is checked against the payload.
//
// The payload's stored values are taken as they are, as coordinates in
// glTF's frame: nothing is flipped. SPZ stores right-up-back, which is
// glTF's own right-handed Y-up, and it is what CesiumJS does (it unpacks with
// the coordinate system unspecified). The draft's text asks for a
// left-up-front conversion, which negates X and Z; that reading is disputed
// in the draft's own review, and this is the one place to change if it wins.
func (d *gltfDecoder) spzSplats(name string, p *gltf.Primitive, world mat4, spz spzCompression) (bool, error) {
	fail := func(format string, a ...any) (bool, error) {
		return false, fmt.Errorf("nt3dmesh: glTF %s splats: %s", name, fmt.Sprintf(format, a...))
	}
	if p.Mode != gltf.PrimitivePoints {
		return fail("an SPZ splat primitive must be POINTS")
	}
	if spz.BufferView == nil || *spz.BufferView < 0 || *spz.BufferView >= len(d.doc.BufferViews) || d.doc.BufferViews[*spz.BufferView] == nil {
		return fail("the SPZ payload names no buffer view of the %d there are", len(d.doc.BufferViews))
	}
	payload, err := modeler.ReadBufferView(d.doc, d.doc.BufferViews[*spz.BufferView])
	if err != nil {
		return fail("SPZ payload: %v", err)
	}
	g, err := nt3dspz.Unpack(payload, d.o)
	if err != nil {
		return false, fmt.Errorf("nt3dmesh: glTF %s splats: %w", name, err)
	}
	if index, ok := p.Attributes[gltf.POSITION]; ok {
		a, err := d.accessor(index, gltf.POSITION)
		if err != nil {
			return false, err
		}
		if a.Count != len(g.Points) {
			return fail("POSITION counts %d splats and the SPZ payload holds %d", a.Count, len(g.Points))
		}
	}
	if err := d.decoded.Take(int64(len(g.Points))*64+int64(len(g.GaussianSH))*12, "glTF splats decoded"); err != nil {
		return false, err
	}
	if err := d.indexSplats(name, p, &g); err != nil {
		return false, err
	}
	return d.finishSplats(name, g, world)
}

// finishSplats finishes a cloud built in the primitive's space as every
// Gaussian decoder finishes one, palette mode, pieces that each fit a
// geometry, and clusters for level of detail, and carries it through world.
// The cloud is split first and each piece carried through on its own: the
// whole of a cloud that needs splitting is more than one geometry may hold,
// and carrying it through whole would refuse it for that.
func (d *gltfDecoder) finishSplats(name string, g nt3d.Geometry, world mat4) (bool, error) {
	fail := func(format string, a ...any) (bool, error) {
		return false, fmt.Errorf("nt3dmesh: glTF %s splats: %s", name, fmt.Sprintf(format, a...))
	}
	sound := func(g *nt3d.Geometry) bool {
		for _, point := range g.Points {
			if !nt3dmesh.ValidCoordinate(point.Position.X, point.Position.Y, point.Position.Z) {
				return false
			}
		}
		return true
	}
	if !sound(&g) {
		return fail("a position is not finite or exceeds coordinate limits")
	}
	// Clusters describe where the points are, so they are built last, of
	// the pieces as carried through.
	turned := world != mat4(gltf.DefaultMatrix)
	o := d.o
	if turned {
		var unclustered nt3dmesh.Options
		if d.o != nil {
			unclustered = *d.o
		}
		unclustered.NoClusters = true
		o = &unclustered
	}
	model, err := nt3dmesh.GaussianModel(GLTF, g, o)
	if err != nil {
		return false, err
	}
	if turned {
		for i := range model.Objects {
			piece := &model.Objects[i].Geometry
			if *piece, err = nt3d.TransformGeometry(*piece, world.Mat4()); err != nil {
				return fail("%v", err)
			}
			if !sound(piece) {
				return fail("a position is not finite or exceeds coordinate limits")
			}
		}
		if err := nt3dmesh.ClusterPieces(model.Objects, d.o); err != nil {
			return false, err
		}
	}
	for _, piece := range model.Objects {
		if d.capture {
			d.sources = append(d.sources, nil)
		}
		piece.Name = name
		d.m.Objects = append(d.m.Objects, piece)
	}
	return true, nil
}

// splatRotations reads ROTATION as float quaternions: float32, or signed
// normalized bytes or shorts.
func splatRotations(raw any, normalized bool) ([][4]float32, error) {
	switch v := raw.(type) {
	case [][4]float32:
		return v, nil
	case [][4]int8:
		if !normalized {
			break
		}
		out := make([][4]float32, len(v))
		for i, c := range v {
			out[i] = [4]float32{gltf.DenormalizeByte(c[0]), gltf.DenormalizeByte(c[1]), gltf.DenormalizeByte(c[2]), gltf.DenormalizeByte(c[3])}
		}
		return out, nil
	case [][4]int16:
		if !normalized {
			break
		}
		out := make([][4]float32, len(v))
		for i, c := range v {
			out[i] = [4]float32{gltf.DenormalizeShort(c[0]), gltf.DenormalizeShort(c[1]), gltf.DenormalizeShort(c[2]), gltf.DenormalizeShort(c[3])}
		}
		return out, nil
	}
	return nil, errors.New("must be float, or normalized signed bytes or shorts")
}

// splatOpacities reads OPACITY as floats: float32, or unsigned normalized
// bytes or shorts.
func splatOpacities(raw any, normalized bool) ([]float32, error) {
	switch v := raw.(type) {
	case []float32:
		return v, nil
	case []uint8:
		if !normalized {
			break
		}
		out := make([]float32, len(v))
		for i, c := range v {
			out[i] = gltf.DenormalizeUbyte(c)
		}
		return out, nil
	case []uint16:
		if !normalized {
			break
		}
		out := make([]float32, len(v))
		for i, c := range v {
			out[i] = gltf.DenormalizeUshort(c)
		}
		return out, nil
	}
	return nil, errors.New("must be float, or normalized unsigned bytes or shorts")
}
