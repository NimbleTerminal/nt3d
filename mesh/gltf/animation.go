// Copyright (c) 2026 Neomantra Corp.

package nt3dgltf

import (
	"fmt"
	"maps"
	"slices"

	"github.com/qmuntal/gltf"
	"github.com/qmuntal/gltf/modeler"
	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
)

type vertexOrigin struct {
	index uint32
	back  bool
}

type cachedMeshPiece struct {
	shared   *nt3d.SharedGeometry
	geometry *nt3d.Geometry
	variants map[int]*nt3d.Material
}

func matrix32(m mat4) (out nt3dmath.Mat4) {
	for i, v := range m {
		out[i] = float32(v)
	}
	return
}
func vec(v [3]float32) nt3dmath.Vec3 { return nt3dmath.Vec3{X: v[0], Y: v[1], Z: v[2]} }
func (d *gltfDecoder) sceneAsset() error {
	left := d.o.ByteLimit()
	d.decodedLeft = &left
	asset := &nt3d.SceneAsset{
		Nodes:    []nt3d.AssetNode{{Name: "Y-up", Parent: -1, Local: matrix32(yUp)}},
		Variants: slices.Clone(d.variantNames),
	}
	ids := map[int]int{}
	parents := make([]int, len(d.doc.Nodes))
	for i := range parents {
		parents[i] = -1
	}
	for i, n := range d.doc.Nodes {
		if n == nil {
			return fmt.Errorf("nt3dmesh: nil glTF node")
		}
		for _, c := range n.Children {
			if c < 0 || c >= len(parents) || parents[c] != -1 {
				return fmt.Errorf("nt3dmesh: invalid scene hierarchy")
			}
			parents[c] = i
		}
	}
	visiting := map[int]bool{}
	depth := 0
	var add func(int) error
	add = func(i int) error {
		depth++
		defer func() { depth-- }()
		if depth > 1024 {
			return fmt.Errorf("%w: scene hierarchy exceeds 1024 levels", nt3dmesh.ErrTooLarge)
		}
		if _, ok := ids[i]; ok {
			return nil
		}
		if visiting[i] {
			return fmt.Errorf("nt3dmesh: scene hierarchy cycle")
		}
		visiting[i] = true
		p := 0
		if parents[i] >= 0 {
			if err := add(parents[i]); err != nil {
				return err
			}
			p = ids[parents[i]]
		}
		ids[i] = len(asset.Nodes)
		n := d.doc.Nodes[i]
		asset.Nodes = append(asset.Nodes, nt3d.AssetNode{Name: n.Name, Parent: p, Local: matrix32(local(n))})
		visiting[i] = false
		return nil
	}
	if len(parents) > maxVisits {
		return nt3dmesh.ErrTooLarge
	}
	for i := range parents {
		if err := add(i); err != nil {
			return err
		}
	}
	// Keep nonselected nodes as joint dependencies, but instantiate mesh primitives
	// only for nodes in the default scene.
	selected := map[int]bool{}
	var mark func(int)
	mark = func(i int) {
		if selected[i] {
			return
		}
		selected[i] = true
		for _, c := range d.doc.Nodes[i].Children {
			mark(c)
		}
	}
	if len(d.doc.Scenes) > 0 {
		si := 0
		if d.doc.Scene != nil {
			si = *d.doc.Scene
		}
		if si < 0 || si >= len(d.doc.Scenes) {
			return fmt.Errorf("nt3dmesh: invalid scene")
		}
		for _, i := range d.doc.Scenes[si].Nodes {
			if i < 0 || i >= len(parents) {
				return fmt.Errorf("nt3dmesh: invalid scene node")
			}
			mark(i)
		}
	} else {
		for i := range parents {
			selected[i] = true
		}
	}
	for i, n := range d.doc.Nodes {
		if n.Camera != nil && selected[i] {
			camera, err := d.camera(*n.Camera)
			if err != nil {
				return err
			}
			asset.Nodes[ids[i]].Camera = camera
			if asset.Nodes[ids[i]].Name == "" {
				asset.Nodes[ids[i]].Name = d.doc.Cameras[*n.Camera].Name
			}
		}
		if light, name, err := d.nodeLight(n.Extensions); err != nil {
			return err
		} else if light != nil && selected[i] {
			asset.Nodes[ids[i]].Light = light
			if asset.Nodes[ids[i]].Name == "" {
				asset.Nodes[ids[i]].Name = name
			}
		}
	}
	meshes := map[int][]int{}
	staticPieces := map[[2]int][]cachedMeshPiece{}
	for ni, n := range d.doc.Nodes {
		if !selected[ni] {
			continue
		}
		insts, instanced, err := d.nodeInstances(ni)
		if err != nil {
			return err
		}
		if n.Mesh == nil {
			continue
		}
		if *n.Mesh < 0 || *n.Mesh >= len(d.doc.Meshes) || d.doc.Meshes[*n.Mesh] == nil {
			return fmt.Errorf("nt3dmesh: glTF node %d names mesh %d of %d", ni, *n.Mesh, len(d.doc.Meshes))
		}
		var instLocals []nt3dmath.Mat4
		if instanced {
			instLocals = make([]nt3dmath.Mat4, len(insts))
			for k, inst := range insts {
				instLocals[k] = matrix32(inst)
			}
		} else {
			instLocals = []nt3dmath.Mat4{nt3dmath.Identity()}
		}
		if len(instLocals) == 0 {
			continue
		}
		mesh := d.doc.Meshes[*n.Mesh]
		for pi, p := range mesh.Primitives {
			if p == nil {
				return fmt.Errorf("nt3dmesh: nil glTF primitive")
			}
			if n.Skin == nil && len(p.Targets) == 0 {
				key := [2]int{*n.Mesh, pi}
				pieces, cached := staticPieces[key]
				if !cached {
					temp := *d
					temp.m = &nt3dmesh.Model{}
					temp.capture = false
					temp.assetMode = true
					temp.primVariants = nil
					temp.points, temp.lines = 0, 0
					temp.sources = nil
					temp.budget = budget{limit: d.o.TriangleLimit()}
					primName := mesh.Name
					if primName == "" {
						primName = n.Name
					}
					if err := temp.primitive(primName, p, mat4(gltf.DefaultMatrix)); err != nil {
						return err
					}
					d.m.Warnings = append(d.m.Warnings, temp.m.Warnings...)
					pieces = make([]cachedMeshPiece, 0, len(temp.m.Objects))
					for _, obj := range temp.m.Objects {
						g := obj.Geometry
						cost := int64(len(g.Vertices)+len(g.Points)+len(g.Lines)) * 64
						if cost > *d.decodedLeft {
							return nt3dmesh.ErrTooLarge
						}
						*d.decodedLeft -= cost
						if len(g.Vertices) > 0 && len(g.Indices) > 0 && len(g.Points)+len(g.Boxes)+len(g.Lines)+len(g.Arrows) == 0 {
							sg, err := nt3d.NewSharedGeometry(g)
							if err != nil {
								return err
							}
							pieces = append(pieces, cachedMeshPiece{shared: sg, variants: temp.primVariants})
						} else {
							gCopy := g
							pieces = append(pieces, cachedMeshPiece{geometry: &gCopy, variants: temp.primVariants})
						}
					}
					staticPieces[key] = pieces
				}
				for _, localMat := range instLocals {
					for _, piece := range pieces {
						if len(asset.Nodes) >= maxVisits {
							return nt3dmesh.ErrTooLarge
						}
						instCost := int64(128)
						if piece.geometry != nil {
							instCost += int64(len(piece.geometry.Vertices)+len(piece.geometry.Points)+len(piece.geometry.Lines)) * 64
						}
						if instCost > *d.decodedLeft {
							return nt3dmesh.ErrTooLarge
						}
						*d.decodedLeft -= instCost
						an := nt3d.AssetNode{
							Name:             n.Name,
							Parent:           ids[ni],
							Local:            localMat,
							Shared:           piece.shared,
							Geometry:         piece.geometry,
							VariantMaterials: maps.Clone(piece.variants),
						}
						meshes[ni] = append(meshes[ni], len(asset.Nodes))
						asset.Nodes = append(asset.Nodes, an)
					}
				}
				continue
			}
			if (n.Skin != nil || len(p.Targets) > 0) && p.Mode != gltf.PrimitiveTriangles && p.Mode != gltf.PrimitiveTriangleStrip && p.Mode != gltf.PrimitiveTriangleFan {
				return fmt.Errorf("nt3dmesh: deformation requires triangle primitives")
			}
			temp := *d
			temp.m = &nt3dmesh.Model{}
			temp.capture = n.Skin != nil || len(p.Targets) > 0
			temp.assetMode = true
			temp.primVariants = nil
			temp.points, temp.lines = 0, 0
			temp.sources = nil
			temp.budget = budget{limit: d.o.TriangleLimit()}
			if err := temp.primitive(n.Name, p, mat4(gltf.DefaultMatrix)); err != nil {
				return err
			}
			d.m.Warnings = append(d.m.Warnings, temp.m.Warnings...)
			var morphs []nt3d.MorphTarget
			for _, target := range p.Targets {
				mt := nt3d.MorphTarget{}
				for key, index := range target {
					if key != gltf.POSITION && key != gltf.NORMAL && key != gltf.TANGENT {
						continue
					}
					a, err := d.accessor(index, key)
					if err != nil {
						return err
					}
					v, err := d.readMorphDeltas(a, key)
					if err != nil {
						return err
					}
					vs := make([]nt3dmath.Vec3, len(v))
					for i, x := range v {
						vs[i] = vec(x)
					}
					switch key {
					case gltf.POSITION:
						mt.Positions = vs
					case gltf.NORMAL:
						mt.Normals = vs
					default:
						mt.Tangents = vs
					}
				}
				morphs = append(morphs, mt)
			}
			weights := mesh.Weights
			if len(n.Weights) > 0 {
				weights = n.Weights
			}
			w := make([]float32, len(morphs))
			if len(weights) > 0 && len(weights) != len(w) {
				return fmt.Errorf("nt3dmesh: morph weights mismatch")
			}
			for i, v := range weights {
				w[i] = float32(v)
			}
			var sk *nt3d.Skeleton
			var influences []nt3d.Influence
			if n.Skin != nil {
				if *n.Skin < 0 || *n.Skin >= len(d.doc.Skins) || d.doc.Skins[*n.Skin] == nil {
					return fmt.Errorf("nt3dmesh: missing skin")
				}
				skin := d.doc.Skins[*n.Skin]
				sk = &nt3d.Skeleton{}
				for _, j := range skin.Joints {
					index, ok := ids[j]
					if !ok {
						return fmt.Errorf("nt3dmesh: missing joint")
					}
					sk.Joints = append(sk.Joints, nt3d.NodeID(index+1))
				}
				if skin.InverseBindMatrices != nil {
					a, err := d.accessor(*skin.InverseBindMatrices, "inverse bind")
					if err != nil {
						return err
					}
					ms, err := modeler.ReadInverseBindMatrices(d.doc, a, nil)
					if err != nil {
						return err
					}
					for _, m := range ms {
						var flat nt3dmath.Mat4
						for c := range 4 {
							for r := range 4 {
								flat[c*4+r] = m[r][c] // modeler exposes rows; nt3d stores columns
							}
						}
						sk.InverseBind = append(sk.InverseBind, flat)
					}
				}
				ji, jok := p.Attributes[gltf.JOINTS_0]
				wi, wok := p.Attributes[gltf.WEIGHTS_0]
				if !jok || !wok {
					return fmt.Errorf("nt3dmesh: skin requires JOINTS_0 and WEIGHTS_0")
				}
				ja, err := d.accessor(ji, "joints")
				if err != nil {
					return err
				}
				wa, err := d.accessor(wi, "weights")
				if err != nil {
					return err
				}
				js, err := modeler.ReadJoints(d.doc, ja, nil)
				if err != nil {
					return err
				}
				ws, err := modeler.ReadWeights(d.doc, wa, nil)
				if err != nil {
					return err
				}
				if len(js) != len(ws) {
					return fmt.Errorf("nt3dmesh: skin attribute counts differ")
				}
				for i, j := range js {
					in := nt3d.Influence{Weights: ws[i]}
					for k, v := range j {
						in.Joints[k] = uint32(v)
					}
					influences = append(influences, in)
				}
				if _, ok := p.Attributes["JOINTS_1"]; ok {
					return fmt.Errorf("nt3dmesh: more than four joint influences are unsupported")
				}
			}
			for oi, obj := range temp.m.Objects {
				g := obj.Geometry
				var def *nt3d.Deformation
				if len(morphs) > 0 || sk != nil {
					def = &nt3d.Deformation{Weights: w, Skeleton: sk}
					origins := temp.sources[oi]
					for _, mt := range morphs {
						out := nt3d.MorphTarget{}
						for _, o := range origins {
							if len(mt.Positions) > 0 {
								if int(o.index) >= len(mt.Positions) {
									return fmt.Errorf("nt3dmesh: short morph target")
								}
								out.Positions = append(out.Positions, mt.Positions[o.index])
							}
							if len(mt.Normals) > 0 {
								if int(o.index) >= len(mt.Normals) {
									return fmt.Errorf("nt3dmesh: short morph normal")
								}
								v := mt.Normals[o.index]
								if o.back {
									v = v.Scale(-1)
								}
								out.Normals = append(out.Normals, v)
							}
							if len(mt.Tangents) > 0 && len(g.Tangents) > 0 {
								if int(o.index) >= len(mt.Tangents) {
									return fmt.Errorf("nt3dmesh: short morph tangent")
								}
								out.Tangents = append(out.Tangents, mt.Tangents[o.index])
							}
						}
						def.Morphs = append(def.Morphs, out)
					}
					if sk != nil {
						for _, o := range origins {
							if int(o.index) >= len(influences) {
								return fmt.Errorf("nt3dmesh: short skin attributes")
							}
							def.Influences = append(def.Influences, influences[o.index])
						}
					}
				}
				for _, localMat := range instLocals {
					if len(asset.Nodes) >= maxVisits {
						return nt3dmesh.ErrTooLarge
					}
					cost := int64(len(g.Vertices)) * (64 + int64(len(morphs))*24)
					if cost > *d.decodedLeft {
						return nt3dmesh.ErrTooLarge
					}
					*d.decodedLeft -= cost
					gCopy := g
					var defCopy *nt3d.Deformation
					if def != nil {
						dCopy := *def
						defCopy = &dCopy
					}
					an := nt3d.AssetNode{
						Name:             obj.Name,
						Parent:           ids[ni],
						Local:            localMat,
						Geometry:         &gCopy,
						Deformation:      defCopy,
						VariantMaterials: maps.Clone(temp.primVariants),
					}
					meshes[ni] = append(meshes[ni], len(asset.Nodes))
					asset.Nodes = append(asset.Nodes, an)
				}
			}
		}
	}
	for _, anim := range d.doc.Animations {
		clip := nt3d.AnimationClip{Name: anim.Name}
		for _, ch := range anim.Channels {
			if ch.Target.Node == nil {
				continue
			}
			ni := *ch.Target.Node
			node, ok := ids[ni]
			if !ok || ch.Sampler < 0 || ch.Sampler >= len(anim.Samplers) {
				return fmt.Errorf("nt3dmesh: invalid animation target/sampler")
			}
			sam := anim.Samplers[ch.Sampler]
			if sam.Input < 0 || sam.Input >= len(d.doc.Accessors) || d.doc.Accessors[sam.Input].Type != gltf.AccessorScalar {
				return fmt.Errorf("nt3dmesh: animation input must be scalar")
			}
			times, err := d.floats(sam.Input)
			if err != nil {
				return err
			}
			values, err := d.animationValues(sam.Output, ch.Target.Path)
			if err != nil {
				return err
			}
			track := nt3d.Track{Node: nt3d.NodeID(node + 1), Times: times, Values: values, Width: 3}
			switch sam.Interpolation {
			case gltf.InterpolationLinear:
				track.Interpolation = nt3d.Linear
			case gltf.InterpolationStep:
				track.Interpolation = nt3d.Step
			case gltf.InterpolationCubicSpline:
				track.Interpolation = nt3d.CubicSpline
			default:
				return fmt.Errorf("nt3dmesh: unknown interpolation")
			}
			switch ch.Target.Path {
			case gltf.TRSTranslation:
				track.Channel = nt3d.Translation
			case gltf.TRSRotation:
				track.Channel = nt3d.Rotation
				track.Width = 4
			case gltf.TRSScale:
				track.Channel = nt3d.Scale
			case gltf.TRSWeights:
				track.Channel = nt3d.MorphWeights
			default:
				return fmt.Errorf("nt3dmesh: unknown animation path")
			}
			if track.Channel == nt3d.MorphWeights {
				for _, i := range meshes[ni] {
					def := asset.Nodes[i].Deformation
					if def == nil || len(def.Morphs) == 0 {
						continue
					}
					t := track
					t.Node = nt3d.NodeID(i + 1)
					t.Width = len(def.Morphs)
					clip.Tracks = append(clip.Tracks, t)
				}
			} else {
				clip.Tracks = append(clip.Tracks, track)
			}
		}
		if err := clip.Validate(); err != nil {
			return err
		}
		asset.Clips = append(asset.Clips, clip)
	}
	if err := staticReflections(asset); err != nil {
		return err
	}
	// Validate all bindings before returning a reusable asset.
	if _, err := asset.Instantiate(nt3d.NewScene(), nt3d.Root, "validate"); err != nil {
		return err
	}
	d.m.Asset = asset
	return nil
}
func (d *gltfDecoder) floats(index int) ([]float32, error) {
	a, err := d.accessor(index, "animation")
	if err != nil {
		return nil, err
	}
	return d.readFloatAnimation(a)
}

func (d *gltfDecoder) readFloatAnimation(a *gltf.Accessor) ([]float32, error) {
	if a.ComponentType != gltf.ComponentFloat || a.Normalized {
		return nil, fmt.Errorf("nt3dmesh: animation accessor must be float")
	}
	v, err := modeler.ReadAccessor(d.doc, a, nil)
	if err != nil {
		return nil, err
	}
	var out []float32
	switch x := v.(type) {
	case []float32:
		out = x
	case [][3]float32:
		for _, v := range x {
			out = append(out, v[:]...)
		}
	case [][4]float32:
		for _, v := range x {
			out = append(out, v[:]...)
		}
	default:
		return nil, fmt.Errorf("nt3dmesh: invalid animation accessor shape")
	}
	return out, nil
}

func (d *gltfDecoder) animationValues(index int, path gltf.TRSProperty) ([]float32, error) {
	a, err := d.accessor(index, "animation")
	if err != nil {
		return nil, err
	}
	if a.ComponentType == gltf.ComponentFloat {
		return d.readFloatAnimation(a)
	}
	if !a.Normalized {
		return nil, fmt.Errorf("nt3dmesh: integer animation accessor must be normalized")
	}
	switch path {
	case gltf.TRSRotation:
		if a.Type != gltf.AccessorVec4 || (a.ComponentType != gltf.ComponentByte && a.ComponentType != gltf.ComponentShort) {
			return nil, fmt.Errorf("nt3dmesh: invalid rotation animation accessor")
		}
		v, err := modeler.ReadAccessor(d.doc, a, nil)
		if err != nil {
			return nil, err
		}
		switch x := v.(type) {
		case [][4]int8:
			out := make([]float32, 0, len(x)*4)
			for _, q := range x {
				out = append(out,
					gltf.DenormalizeByte(q[0]),
					gltf.DenormalizeByte(q[1]),
					gltf.DenormalizeByte(q[2]),
					gltf.DenormalizeByte(q[3]),
				)
			}
			return out, nil
		case [][4]int16:
			out := make([]float32, 0, len(x)*4)
			for _, q := range x {
				out = append(out,
					gltf.DenormalizeShort(q[0]),
					gltf.DenormalizeShort(q[1]),
					gltf.DenormalizeShort(q[2]),
					gltf.DenormalizeShort(q[3]),
				)
			}
			return out, nil
		default:
			return nil, fmt.Errorf("nt3dmesh: invalid rotation animation accessor")
		}
	case gltf.TRSWeights:
		if a.Type != gltf.AccessorScalar {
			return nil, fmt.Errorf("nt3dmesh: invalid weights animation accessor")
		}
		v, err := modeler.ReadAccessor(d.doc, a, nil)
		if err != nil {
			return nil, err
		}
		switch x := v.(type) {
		case []int8:
			out := make([]float32, len(x))
			for i, w := range x {
				out[i] = gltf.DenormalizeByte(w)
			}
			return out, nil
		case []uint8:
			out := make([]float32, len(x))
			for i, w := range x {
				out[i] = gltf.DenormalizeUbyte(w)
			}
			return out, nil
		case []int16:
			out := make([]float32, len(x))
			for i, w := range x {
				out[i] = gltf.DenormalizeShort(w)
			}
			return out, nil
		case []uint16:
			out := make([]float32, len(x))
			for i, w := range x {
				out[i] = gltf.DenormalizeUshort(w)
			}
			return out, nil
		default:
			return nil, fmt.Errorf("nt3dmesh: invalid weights animation accessor")
		}
	default:
		return nil, fmt.Errorf("nt3dmesh: animation accessor must be float")
	}
}
