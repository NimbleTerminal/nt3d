// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d/math"
)

// Decal projects an oriented box onto a target triangle mesh, clipping
// front-facing triangles to [-Width/2, +Width/2] × [-Height/2, +Height/2] ×
// [-Depth/2, +Depth/2] around Center and mapping planar [0, 1]² UVs.
//
// Normal points out of the surface toward the viewer. Up defines the decal's
// +V-upward axis before projection onto the plane perpendicular to Normal; UV
// (0, 0) is top-left and (1, 1) is bottom-right when viewed along -Normal.
// Neither need be unit length. NormalOffset lifts the clipped vertices along
// Normal, to prevent depth fighting against the target mesh.
type Decal struct {
	Center       nt3dmath.Vec3
	Normal       nt3dmath.Vec3
	Up           nt3dmath.Vec3
	Width        float32
	Height       float32
	Depth        float32
	NormalOffset float32
	Color        color.RGBA
	Material     *Material
}

type decalVert struct {
	pos  nt3dmath.Vec3
	norm nt3dmath.Vec3
	x, y float64 // local coordinates along right and up
	z    float64 // local coordinate along normal
}

// BuildDecal clips target's indexed triangles to d's oriented box and returns
// a UV-mapped mesh ready to draw over target. Triangles facing away from
// d.Normal (dot <= 0.1) are skipped so decals do not wrap onto back faces or
// perpendicular floors. An empty Geometry (zero vertices) is returned without
// error when no front-facing triangles intersect the box.
func BuildDecal(target Geometry, d Decal) (Geometry, error) {
	frame, err := newSurfaceFrame("decal", d.Center, d.Normal, d.Up, d.NormalOffset)
	if err != nil {
		return Geometry{}, err
	}
	if !positive(d.Width, d.Height, d.Depth) {
		return Geometry{}, fmt.Errorf("nt3d: decal dimensions must be positive and finite")
	}
	if d.Material != nil {
		if err := d.Material.Validate(); err != nil {
			return Geometry{}, err
		}
	}
	n, right, up := frame.normal, frame.right, frame.up

	if len(target.Indices)%3 != 0 {
		return Geometry{}, fmt.Errorf("nt3d: decal target indices must be a multiple of 3")
	}
	vCount := uint32(len(target.Vertices))
	for _, idx := range target.Indices {
		if idx >= vCount {
			return Geometry{}, fmt.Errorf("nt3d: decal target index %d out of bounds (%d)", idx, vCount)
		}
	}

	hx := 0.5 * float64(d.Width)
	hy := 0.5 * float64(d.Height)
	hz := 0.5 * float64(d.Depth)
	col := shapeColor(d.Color)
	lift := n.Scale(d.NormalOffset)

	var out Geometry
	out.Material = cloneMaterial(d.Material)

	toLocal := func(p, fn nt3dmath.Vec3) decalVert {
		rel := p.Sub(d.Center)
		return decalVert{
			pos:  p,
			norm: fn,
			x:    float64(rel.Dot(right)),
			y:    float64(rel.Dot(up)),
			z:    float64(rel.Dot(n)),
		}
	}

	var polyBufA, polyBufB [16]decalVert
	for i := 0; i+2 < len(target.Indices); i += 3 {
		v0 := target.Vertices[target.Indices[i]].Position
		v1 := target.Vertices[target.Indices[i+1]].Position
		v2 := target.Vertices[target.Indices[i+2]].Position
		e1 := v1.Sub(v0)
		e2 := v2.Sub(v0)
		fnRaw := e1.Cross(e2)
		fnLen := math.Sqrt(float64(fnRaw.Dot(fnRaw)))
		if fnLen < 1e-9 {
			continue
		}
		fn := fnRaw.Scale(float32(1 / fnLen))
		if fn.Dot(n) <= 0.1 {
			continue
		}

		poly := polyBufA[:3]
		poly[0] = toLocal(v0, fn)
		poly[1] = toLocal(v1, fn)
		poly[2] = toLocal(v2, fn)

		// Fast reject per axis before clipping.
		if max(poly[0].x, poly[1].x, poly[2].x) < -hx || min(poly[0].x, poly[1].x, poly[2].x) > hx ||
			max(poly[0].y, poly[1].y, poly[2].y) < -hy || min(poly[0].y, poly[1].y, poly[2].y) > hy ||
			max(poly[0].z, poly[1].z, poly[2].z) < -hz || min(poly[0].z, poly[1].z, poly[2].z) > hz {
			continue
		}

		poly = clipDecalPlane(poly, polyBufB[:0], func(v decalVert) float64 { return v.x + hx })
		poly = clipDecalPlane(poly, polyBufA[:0], func(v decalVert) float64 { return hx - v.x })
		poly = clipDecalPlane(poly, polyBufB[:0], func(v decalVert) float64 { return v.y + hy })
		poly = clipDecalPlane(poly, polyBufA[:0], func(v decalVert) float64 { return hy - v.y })
		poly = clipDecalPlane(poly, polyBufB[:0], func(v decalVert) float64 { return v.z + hz })
		poly = clipDecalPlane(poly, polyBufA[:0], func(v decalVert) float64 { return hz - v.z })
		if len(poly) < 3 {
			continue
		}

		base := uint32(len(out.Vertices))
		triLimit := MeshTriangleLimit()
		if len(out.Indices)/3+(len(poly)-2) > triLimit || int(base)+len(poly) > 3*triLimit {
			return Geometry{}, fmt.Errorf("nt3d: decal exceeds mesh limits")
		}
		invW := 1.0 / float64(d.Width)
		invH := 1.0 / float64(d.Height)
		for _, pv := range poly {
			uCoord := float32(0.5 + pv.x*invW)
			vCoord := float32(0.5 - pv.y*invH)
			out.Vertices = append(out.Vertices, Vertex{
				Position: pv.pos.Add(lift),
				Normal:   pv.norm,
				Color:    col,
			})
			out.UV = append(out.UV, UV{U: uCoord, V: vCoord})
		}
		for k := 1; k+1 < len(poly); k++ {
			out.Indices = append(out.Indices, base, base+uint32(k), base+uint32(k+1))
		}
	}
	if len(out.Vertices) > 0 {
		out.Bounds = GeometryBounds(out)
	}
	return out, nil
}

func clipDecalPlane(in, dst []decalVert, dist func(decalVert) float64) []decalVert {
	if len(in) < 3 {
		return nil
	}
	prev := in[len(in)-1]
	dPrev := dist(prev)
	for _, cur := range in {
		dCur := dist(cur)
		if dCur >= 0 {
			if dPrev < 0 {
				dst = append(dst, lerpDecalVert(prev, cur, dPrev/(dPrev-dCur)))
			}
			dst = append(dst, cur)
		} else if dPrev >= 0 {
			dst = append(dst, lerpDecalVert(prev, cur, dPrev/(dPrev-dCur)))
		}
		prev, dPrev = cur, dCur
	}
	return dst
}

func lerpDecalVert(a, b decalVert, t float64) decalVert {
	ft := float32(max(0, min(1, t)))
	return decalVert{
		pos:  a.pos.Add(b.pos.Sub(a.pos).Scale(ft)),
		norm: a.norm,
		x:    a.x + (b.x-a.x)*t,
		y:    a.y + (b.y-a.y)*t,
		z:    a.z + (b.z-a.z)*t,
	}
}
