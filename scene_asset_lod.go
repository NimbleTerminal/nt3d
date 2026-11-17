// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	nt3dmath "nimbleterminal.dev/nt3d/math"
)

// AssetLOD pairs a distance threshold with a per-tier SceneAsset for CombineAssetLOD.
//
// MaxDistance follows LODLevel.MaxDistance: every non-final tier requires a
// finite positive distance strictly increasing across tiers; on the final tier,
// zero or +Inf makes it an unbounded fallback, while a finite positive distance
// hides the asset beyond that distance.
type AssetLOD struct {
	MaxDistance float32
	Asset       *SceneAsset
}

// LODTiers returns the maximum tier count (>= 2) across compilable sibling
// *_LOD0..N node groups in a (or across already-compiled AssetNode.LOD chains),
// or 0 when a has no multi-tier LOD groups. When LODTiers returns K >= 2 on an
// uncompiled asset, calling a.WithLOD with K-1 or K valid distances succeeds.
func (a *SceneAsset) LODTiers() int {
	if a == nil || validateAssetParents(a) != nil {
		return 0
	}
	compiledMax := 0
	for _, n := range a.Nodes {
		if len(n.LOD) > compiledMax {
			compiledMax = len(n.LOD)
		}
	}
	if compiledMax >= 2 {
		return compiledMax
	}
	children := make([][]int, len(a.Nodes))
	var roots []int
	for i, n := range a.Nodes {
		if n.Parent == -1 {
			roots = append(roots, i)
		} else {
			children[n.Parent] = append(children[n.Parent], i)
		}
	}
	dynamic := assetDynamicNodes(a)
	absorbed := make([]bool, len(a.Nodes))
	var markAbsorbed func(int)
	markAbsorbed = func(idx int) {
		absorbed[idx] = true
		for _, c := range children[idx] {
			markAbsorbed(c)
		}
	}
	type tierMatch struct {
		nodeIdx int
		tier    int
	}
	maxTiers := 0
	valid := true
	var scan func(parentName string, sibs []int)
	scan = func(parentName string, sibs []int) {
		type bucket struct {
			tiers []tierMatch
		}
		var order []string
		buckets := map[string]*bucket{}
		for _, idx := range sibs {
			stem, tier, ok := parseLODName(a.Nodes[idx].Name, parentName)
			if !ok {
				continue
			}
			key := strings.ToLower(stem)
			b := buckets[key]
			if b == nil {
				b = &bucket{}
				buckets[key] = b
				order = append(order, key)
			}
			b.tiers = append(b.tiers, tierMatch{nodeIdx: idx, tier: tier})
		}
		for _, key := range order {
			b := buckets[key]
			slices.SortFunc(b.tiers, func(x, y tierMatch) int {
				return cmp.Compare(x.tier, y.tier)
			})
			for i := 1; i < len(b.tiers); i++ {
				if b.tiers[i].tier == b.tiers[i-1].tier {
					valid = false
					return
				}
			}
			if len(b.tiers) < 2 {
				continue
			}
			groupLocal := a.Nodes[b.tiers[0].nodeIdx].Local
			if validateTransformMatrix(groupLocal) != nil {
				valid = false
				return
			}
			totalPrims := 0
			for _, tm := range b.tiers {
				rootPre, err := siblingTierRootAdjust(groupLocal, a.Nodes[tm.nodeIdx].Local)
				if err != nil {
					valid = false
					return
				}
				prims, err := collectSubtreeLODPrims(a, children, dynamic, tm.nodeIdx, rootPre, nil)
				if err != nil {
					valid = false
					return
				}
				totalPrims += len(prims)
				markAbsorbed(tm.nodeIdx)
			}
			if totalPrims == 0 {
				valid = false
				return
			}
			if len(b.tiers) > maxTiers {
				maxTiers = len(b.tiers)
			}
		}
		for _, idx := range sibs {
			if !valid {
				return
			}
			if absorbed[idx] {
				continue
			}
			scan(a.Nodes[idx].Name, children[idx])
		}
	}
	scan("", roots)
	if !valid || maxTiers < 2 {
		return 0
	}
	return maxTiers
}

// Bounds returns the rest-pose world-space bounding box enclosing all geometry,
// shared meshes, and LOD tiers in a.
func (a *SceneAsset) Bounds() (b nt3dmath.AABB) {
	if a == nil || validateAssetParents(a) != nil {
		return b
	}
	worlds := make([]nt3dmath.Mat4, len(a.Nodes))
	includeGeom := func(g Geometry, w nt3dmath.Mat4) {
		if !g.Bounds.IsValid() {
			g.Bounds = GeometryBounds(g)
		}
		if box := geometryWorldBounds(&g, w); box.IsValid() {
			b.Include(box.Min)
			b.Include(box.Max)
		}
	}
	for i, n := range a.Nodes {
		w := n.Local
		if n.Parent >= 0 {
			w = worlds[n.Parent].Mul(w)
		}
		worlds[i] = w
		switch {
		case len(n.LOD) > 0:
			for _, lvl := range n.LOD {
				if lvl.Shared != nil {
					includeGeom(lvl.Shared.geometry.Geometry, w)
				} else if hasPrimitives(&lvl.Geometry) || lvl.Geometry.Billboard != nil {
					includeGeom(lvl.Geometry, w)
				}
			}
		case n.Shared != nil:
			includeGeom(n.Shared.geometry.Geometry, w)
		case n.Geometry != nil:
			includeGeom(*n.Geometry, w)
		}
	}
	return b
}

// Triangles counts the rest-pose triangles in a (counting the finest non-empty
// tier on AssetNode.LOD nodes).
func (a *SceneAsset) Triangles() (total int) {
	if a == nil {
		return 0
	}
	for _, n := range a.Nodes {
		switch {
		case len(n.LOD) > 0:
			for _, lvl := range n.LOD {
				var g *Geometry
				if lvl.Shared != nil {
					g = &lvl.Shared.geometry.Geometry
				} else {
					g = &lvl.Geometry
				}
				if t := geometryTriangles(g); t > 0 {
					total += t
					break
				}
			}
		case n.Shared != nil:
			total += geometryTriangles(&n.Shared.geometry.Geometry)
		case n.Geometry != nil:
			total += geometryTriangles(n.Geometry)
		}
	}
	return total
}

// WithLOD returns a copy of a in which sibling LOD subtrees (such as
// Mesh_LOD0, Mesh_LOD1, Mesh_LOD2 or LOD0, LOD1) are collapsed into compact
// AssetNodes backed by LODLevel slices for Scene.SetLOD.
//
// Matched node names end with case-insensitive LOD<tier> (0 <= tier <= 31),
// optionally separated from the stem and tier index by '_', '-', '.', or ' '
// (for example "SM_Bench_01_LOD0", "bench-lod1", "Bench.LOD2", "Bench_LOD_0",
// or bare "LOD0"). Non-LOD siblings, hierarchy transforms, animation clips on
// non-LOD nodes, SharedGeometry pointers, and material variants are preserved.
//
// For a maximum of K tiers across matched groups, maxDistances may supply either
// K-1 finite positive transition distances (making the coarsest tier an
// unbounded fallback) or all K distances (allowing a finite hide-beyond cutoff
// on the final tier, or 0/+Inf for unbounded).
func (a *SceneAsset) WithLOD(maxDistances ...float32) (*SceneAsset, error) {
	if a == nil {
		return nil, errors.New("nt3d: nil scene asset")
	}
	if len(maxDistances) == 0 {
		return nil, errors.New("nt3d: WithLOD requires at least one max distance")
	}
	if err := validateAssetParents(a); err != nil {
		return nil, err
	}
	children := make([][]int, len(a.Nodes))
	var roots []int
	for i, n := range a.Nodes {
		if n.Parent == -1 {
			roots = append(roots, i)
		} else {
			children[n.Parent] = append(children[n.Parent], i)
		}
	}
	dynamic := assetDynamicNodes(a)

	type tierMatch struct {
		nodeIdx int
		tier    int
	}
	type lodGroup struct {
		parent  int
		stem    string
		firstAt int
		tiers   []tierMatch
	}

	var groups []lodGroup
	groupByFirst := map[int]int{}
	absorbed := make([]bool, len(a.Nodes))

	var markAbsorbed func(int)
	markAbsorbed = func(idx int) {
		absorbed[idx] = true
		for _, c := range children[idx] {
			markAbsorbed(c)
		}
	}

	var hasAbsorbedDescendant func(int) bool
	hasAbsorbedDescendant = func(idx int) bool {
		if absorbed[idx] {
			return true
		}
		for _, c := range children[idx] {
			if hasAbsorbedDescendant(c) {
				return true
			}
		}
		return false
	}

	var scanSiblings func(parent int, parentName string, sibs []int, singleTier bool) error
	scanSiblings = func(parent int, parentName string, sibs []int, singleTier bool) error {
		type candBucket struct {
			stem  string
			tiers []tierMatch
		}
		var order []string
		buckets := map[string]*candBucket{}
		for _, idx := range sibs {
			if absorbed[idx] {
				continue
			}
			stem, tier, ok := parseLODName(a.Nodes[idx].Name, parentName)
			if !ok {
				continue
			}
			key := strings.ToLower(stem)
			b := buckets[key]
			if b == nil {
				b = &candBucket{stem: stem}
				buckets[key] = b
				order = append(order, key)
			}
			b.tiers = append(b.tiers, tierMatch{nodeIdx: idx, tier: tier})
		}
		for _, key := range order {
			b := buckets[key]
			slices.SortFunc(b.tiers, func(x, y tierMatch) int {
				return cmp.Compare(x.tier, y.tier)
			})
			for i := 1; i < len(b.tiers); i++ {
				if b.tiers[i].tier == b.tiers[i-1].tier {
					return fmt.Errorf("nt3d: duplicate LOD%d node for %q", b.tiers[i].tier, b.stem)
				}
			}
			if singleTier {
				if len(b.tiers) != 1 || b.tiers[0].tier != 0 || hasAbsorbedDescendant(b.tiers[0].nodeIdx) {
					continue
				}
			} else if len(b.tiers) < 2 {
				continue
			}
			firstAt := b.tiers[0].nodeIdx
			for _, tm := range b.tiers[1:] {
				if tm.nodeIdx < firstAt {
					firstAt = tm.nodeIdx
				}
			}
			gi := len(groups)
			groups = append(groups, lodGroup{
				parent:  parent,
				stem:    b.stem,
				firstAt: firstAt,
				tiers:   b.tiers,
			})
			groupByFirst[firstAt] = gi
			for _, tm := range b.tiers {
				markAbsorbed(tm.nodeIdx)
			}
		}
		for _, idx := range sibs {
			if absorbed[idx] {
				continue
			}
			if err := scanSiblings(idx, a.Nodes[idx].Name, children[idx], singleTier); err != nil {
				return err
			}
		}
		return nil
	}

	// Pass 1: collect all multi-tier (>= 2) sibling LOD groups first so a parent
	// container named *_LOD0 never masks multi-tier LOD groups inside its subtree.
	if err := scanSiblings(-1, "", roots, false); err != nil {
		return nil, err
	}
	maxTiers := 0
	for _, g := range groups {
		if len(g.tiers) > maxTiers {
			maxTiers = len(g.tiers)
		}
	}
	// Pass 2: include standalone 1-tier *_LOD0 leaf props when the caller passed a
	// single cutoff distance (maxTiers == 0 && len == 1) or a pack-wide finite
	// hide-beyond cutoff (len == maxTiers).
	lastDist := maxDistances[len(maxDistances)-1]
	if (maxTiers == 0 && len(maxDistances) == 1) ||
		(maxTiers >= 2 && len(maxDistances) == maxTiers && nt3dmath.IsFinite(float64(lastDist)) && lastDist > 0) {
		if err := scanSiblings(-1, "", roots, true); err != nil {
			return nil, err
		}
		if maxTiers == 0 && len(groups) > 0 {
			maxTiers = 1
		}
	}
	if len(groups) == 0 {
		return nil, errors.New("nt3d: scene asset contains no LOD node groups")
	}

	fullDists, err := normalizeLODDistances(maxDistances, maxTiers)
	if err != nil {
		return nil, err
	}

	out := &SceneAsset{
		Variants: slices.Clone(a.Variants),
	}
	newIdx := make([]int, len(a.Nodes))
	for i := range newIdx {
		newIdx[i] = -1
	}

	for i, n := range a.Nodes {
		if gi, ok := groupByFirst[i]; ok {
			g := groups[gi]
			newParent := -1
			if g.parent >= 0 {
				newParent = newIdx[g.parent]
			}
			groupLocal := a.Nodes[g.tiers[0].nodeIdx].Local
			if err := validateTransformMatrix(groupLocal); err != nil {
				return nil, err
			}
			tierPrims := make([][]lodPrim, len(g.tiers))
			for t, tm := range g.tiers {
				rootPre, err := siblingTierRootAdjust(groupLocal, a.Nodes[tm.nodeIdx].Local)
				if err != nil {
					return nil, err
				}
				prims, err := collectSubtreeLODPrims(a, children, dynamic, tm.nodeIdx, rootPre, nil)
				if err != nil {
					return nil, err
				}
				tierPrims[t] = prims
			}
			td := groupTierDistances(fullDists, len(g.tiers))
			if err := emitCompiledLODGroup(out, newParent, g.stem, groupLocal, tierPrims, td); err != nil {
				return nil, err
			}
			continue
		}
		if absorbed[i] {
			continue
		}
		cp := cloneAssetNode(n)
		if n.Parent >= 0 {
			cp.Parent = newIdx[n.Parent]
		}
		newIdx[i] = len(out.Nodes)
		out.Nodes = append(out.Nodes, cp)
	}

	// Remap any non-LOD skeleton joints and animation clip targets.
	for i := range out.Nodes {
		d := out.Nodes[i].Deformation
		if d == nil || d.Skeleton == nil {
			continue
		}
		sk := *d.Skeleton
		sk.Joints = make([]NodeID, len(d.Skeleton.Joints))
		for k, j := range d.Skeleton.Joints {
			old := int(j) - 1
			if old < 0 || old >= len(newIdx) || newIdx[old] < 0 {
				return nil, errors.New("nt3d: skeleton joint references node inside LOD group")
			}
			sk.Joints[k] = NodeID(newIdx[old] + 1)
		}
		dc := *d
		dc.Skeleton = &sk
		out.Nodes[i].Deformation = &dc
	}
	for _, c := range a.Clips {
		clip := AnimationClip{Name: c.Name, Tracks: make([]Track, len(c.Tracks))}
		for k, t := range c.Tracks {
			if t.Channel != UserChannel {
				old := int(t.Node) - 1
				if old < 0 || old >= len(newIdx) || newIdx[old] < 0 {
					return nil, fmt.Errorf("nt3d: animation clip %q targets node inside LOD group", c.Name)
				}
				t.Node = NodeID(newIdx[old] + 1)
			}
			clip.Tracks[k] = t
		}
		out.Clips = append(out.Clips, clip)
	}
	return out, nil
}

// CombineAssetLOD combines separate per-tier SceneAssets (such as
// bench_lod0.glb, bench_lod1.glb, bench_lod2.glb) into one reusable SceneAsset
// whose mesh nodes carry LODLevel slices for Scene.SetLOD.
//
// SharedGeometry pointers are preserved without copying vertex data whenever
// compatible, and named material variants across tiers are merged by name so
// Scene.SetMaterialVariant switches colorways across all LOD levels.
func CombineAssetLOD(tiers []AssetLOD) (*SceneAsset, error) {
	if len(tiers) == 0 {
		return nil, errors.New("nt3d: empty LOD tiers")
	}
	tierDists := make([]float32, len(tiers))
	for i, t := range tiers {
		if t.Asset == nil {
			return nil, fmt.Errorf("nt3d: nil asset in LOD tier %d", i)
		}
		if err := validateAssetParents(t.Asset); err != nil {
			return nil, err
		}
		tierDists[i] = t.MaxDistance
	}
	if err := validateExplicitLODDistances(tierDists); err != nil {
		return nil, err
	}

	// Merge variant names in order across all tiers and build per-tier index remaps.
	var variants []string
	variantIndex := map[string]int{}
	tierVariantRemaps := make([][]int, len(tiers))
	for t, tr := range tiers {
		seen := make(map[string]bool, len(tr.Asset.Variants))
		remap := make([]int, len(tr.Asset.Variants))
		for vi, name := range tr.Asset.Variants {
			if name == "" {
				return nil, errors.New("nt3d: asset material variant name cannot be empty")
			}
			if seen[name] {
				return nil, fmt.Errorf("nt3d: duplicate asset material variant %q", name)
			}
			seen[name] = true
			dst, ok := variantIndex[name]
			if !ok {
				dst = len(variants)
				variantIndex[name] = dst
				variants = append(variants, name)
			}
			remap[vi] = dst
		}
		tierVariantRemaps[t] = remap
	}

	// If every tier has a single root node with the same Local transform (for
	// example glTF's "Y-up" root), keep that shared root transform at node 0 so
	// child SharedGeometry pointers need no coordinate-system bake.
	sharedRoot := true
	var rootLocal nt3dmath.Mat4
	var rootName string
	tierRoots := make([][]int, len(tiers))
	tierChildren := make([][][]int, len(tiers))
	for t, tr := range tiers {
		children := make([][]int, len(tr.Asset.Nodes))
		var roots []int
		for i, n := range tr.Asset.Nodes {
			if n.Parent == -1 {
				roots = append(roots, i)
			} else {
				children[n.Parent] = append(children[n.Parent], i)
			}
		}
		tierRoots[t] = roots
		tierChildren[t] = children
		if len(roots) != 1 {
			sharedRoot = false
			continue
		}
		rn := tr.Asset.Nodes[roots[0]]
		if rn.Geometry != nil || rn.Shared != nil || len(rn.LOD) > 0 || rn.Camera != nil || rn.Light != nil || rn.Deformation != nil {
			sharedRoot = false
			continue
		}
		if t == 0 {
			rootLocal = rn.Local
			rootName = rn.Name
			if err := validateTransformMatrix(rootLocal); err != nil {
				return nil, err
			}
		} else if !matApproxEqual(rootLocal, rn.Local) {
			sharedRoot = false
		}
	}

	tierPrims := make([][]lodPrim, len(tiers))
	stem := "LOD"
	for t, tr := range tiers {
		dyn := assetDynamicNodes(tr.Asset)
		var prims []lodPrim
		if sharedRoot {
			rIdx := tierRoots[t][0]
			if t == 0 && len(tierChildren[t][rIdx]) == 1 {
				if cname := tr.Asset.Nodes[tierChildren[t][rIdx][0]].Name; cname != "" {
					stem = stripLODSuffix(cname)
				}
			}
			for _, c := range tierChildren[t][rIdx] {
				cp, err := collectSubtreeLODPrims(tr.Asset, tierChildren[t], dyn, c, tr.Asset.Nodes[c].Local, tierVariantRemaps[t])
				if err != nil {
					return nil, err
				}
				prims = append(prims, cp...)
			}
		} else {
			if t == 0 && len(tierRoots[t]) == 1 {
				if rname := tr.Asset.Nodes[tierRoots[t][0]].Name; rname != "" && rname != "Y-up" {
					stem = stripLODSuffix(rname)
				}
			}
			for _, rIdx := range tierRoots[t] {
				cp, err := collectSubtreeLODPrims(tr.Asset, tierChildren[t], dyn, rIdx, tr.Asset.Nodes[rIdx].Local, tierVariantRemaps[t])
				if err != nil {
					return nil, err
				}
				prims = append(prims, cp...)
			}
		}
		tierPrims[t] = prims
	}

	out := &SceneAsset{Variants: variants}
	parent := -1
	if sharedRoot {
		if rootName == "" {
			rootName = "LOD"
		}
		out.Nodes = append(out.Nodes, AssetNode{
			Name:   rootName,
			Parent: -1,
			Local:  rootLocal,
		})
		parent = 0
	}
	if err := emitCompiledLODGroup(out, parent, stem, nt3dmath.Identity(), tierPrims, tierDists); err != nil {
		return nil, err
	}
	return out, nil
}

type lodPrim struct {
	name     string
	relLocal nt3dmath.Mat4
	geom     *Geometry
	shared   *SharedGeometry
	variants map[int]*Material
}

func (p *lodPrim) baseMaterial() *Material {
	if p.shared != nil {
		return p.shared.geometry.Material
	}
	if p.geom != nil {
		return p.geom.Material
	}
	return nil
}

func (p *lodPrim) bounds() nt3dmath.AABB {
	if p.shared != nil {
		return p.shared.Bounds()
	}
	if p.geom != nil {
		if p.geom.Bounds.IsValid() {
			return p.geom.Bounds
		}
		return GeometryBounds(*p.geom)
	}
	return nt3dmath.AABB{}
}

func collectSubtreeLODPrims(a *SceneAsset, children [][]int, dynamic []bool, rootIdx int, rootRel nt3dmath.Mat4, variantRemap []int) ([]lodPrim, error) {
	var out []lodPrim
	var walk func(idx int, rel nt3dmath.Mat4) error
	walk = func(idx int, rel nt3dmath.Mat4) error {
		if err := validateTransformMatrix(rel); err != nil {
			return err
		}
		n := a.Nodes[idx]
		if dynamic[idx] || n.Deformation != nil || n.Camera != nil || n.Light != nil {
			return fmt.Errorf("nt3d: LOD tier node %q cannot contain cameras, lights, deformations, or animations", n.Name)
		}
		if len(n.LOD) > 0 {
			return fmt.Errorf("nt3d: LOD tier node %q already has LOD levels", n.Name)
		}
		if n.Geometry != nil && n.Shared != nil {
			return errors.New("nt3d: asset node cannot set both Geometry and Shared")
		}
		if n.Geometry != nil || n.Shared != nil {
			var vmap map[int]*Material
			if len(n.VariantMaterials) > 0 {
				vmap = make(map[int]*Material, len(n.VariantMaterials))
				for vi, vm := range n.VariantMaterials {
					if vi < 0 || vi >= len(a.Variants) {
						return fmt.Errorf("nt3d: invalid asset material variant index %d", vi)
					}
					dst := vi
					if variantRemap != nil {
						dst = variantRemap[vi]
					}
					vmap[dst] = cloneMaterial(vm)
				}
			}
			var gcopy *Geometry
			if n.Geometry != nil {
				gc := cloneGeometry(*n.Geometry)
				gcopy = &gc
			}
			out = append(out, lodPrim{
				name:     n.Name,
				relLocal: rel,
				geom:     gcopy,
				shared:   n.Shared,
				variants: vmap,
			})
		}
		for _, c := range children[idx] {
			if err := walk(c, rel.Mul(a.Nodes[c].Local)); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(rootIdx, rootRel); err != nil {
		return nil, err
	}
	return out, nil
}

func emitCompiledLODGroup(out *SceneAsset, parent int, stem string, groupLocal nt3dmath.Mat4, tierPrims [][]lodPrim, tierDists []float32) error {
	type primSlot struct {
		name      string
		local     nt3dmath.Mat4
		sampleMat *Material
		entries   []*lodPrim
	}

	numTiers := len(tierPrims)
	var slots []primSlot
	var propBounds nt3dmath.AABB

	for t := 0; t < numTiers; t++ {
		prims := tierPrims[t]
		usedSlot := make([]bool, len(slots))
		matchedPrim := make([]bool, len(prims))

		// Pass 1: match by identical relative transform and equal material.
		if t > 0 {
			for pi := range prims {
				p := &prims[pi]
				pMat := p.baseMaterial()
				for si := range slots {
					if usedSlot[si] {
						continue
					}
					if matApproxEqual(slots[si].local, p.relLocal) && materialsEqual(slots[si].sampleMat, pMat) {
						slots[si].entries[t] = p
						usedSlot[si] = true
						matchedPrim[pi] = true
						break
					}
				}
			}
			// Pass 2: match by identical relative transform.
			for pi := range prims {
				if matchedPrim[pi] {
					continue
				}
				p := &prims[pi]
				for si := range slots {
					if usedSlot[si] {
						continue
					}
					if matApproxEqual(slots[si].local, p.relLocal) {
						slots[si].entries[t] = p
						usedSlot[si] = true
						matchedPrim[pi] = true
						break
					}
				}
			}
			// Pass 3: single-primitive tier aligns onto a single existing slot
			// even when exported with a differing root scale or orientation.
			if len(prims) == 1 && !matchedPrim[0] && len(slots) == 1 && !usedSlot[0] {
				slots[0].entries[t] = &prims[0]
				usedSlot[0] = true
				matchedPrim[0] = true
			}
		}

		// Pass 4: remaining primitives start new slots.
		for pi := range prims {
			if matchedPrim[pi] {
				continue
			}
			p := &prims[pi]
			entries := make([]*lodPrim, numTiers)
			entries[t] = p
			slots = append(slots, primSlot{
				name:      p.name,
				local:     p.relLocal,
				sampleMat: p.baseMaterial(),
				entries:   entries,
			})
		}

		for pi := range prims {
			if b := prims[pi].bounds(); b.IsValid() {
				tb := b.Transformed(prims[pi].relLocal)
				propBounds.Include(tb.Min)
				propBounds.Include(tb.Max)
			}
		}
	}

	if len(slots) == 0 {
		return fmt.Errorf("nt3d: LOD group %q contains no geometry", stem)
	}

	buildSlotNode := func(s *primSlot, nodeName string, nodeParent int, nodeLocal nt3dmath.Mat4, multiPart bool) (AssetNode, error) {
		lastTier := -1
		for t := numTiers - 1; t >= 0; t-- {
			if s.entries[t] != nil {
				lastTier = t
				break
			}
		}
		levels := make([]LODLevel, lastTier+1)
		var lodVariants []map[int]*Material
		hasAnyVariants := false
		invSlot, ok := s.local.InverseAffine()
		if !ok {
			return AssetNode{}, errors.New("nt3d: singular LOD slot transform")
		}
		for t := 0; t <= lastTier; t++ {
			lvl := LODLevel{MaxDistance: tierDists[t]}
			e := s.entries[t]
			if e != nil {
				if err := populateAlignedLODLevel(&lvl, e, s.local, invSlot); err != nil {
					return AssetNode{}, err
				}
				if len(e.variants) > 0 {
					if lodVariants == nil {
						lodVariants = make([]map[int]*Material, lastTier+1)
					}
					lodVariants[t] = e.variants
					hasAnyVariants = true
				}
			}
			levels[t] = lvl
		}
		an := AssetNode{
			Name:   nodeName,
			Parent: nodeParent,
			Local:  nodeLocal,
			LOD:    levels,
		}
		if hasAnyVariants {
			an.LODVariantMaterials = lodVariants
		}
		if multiPart && propBounds.IsValid() {
			an.LODBounds = propBounds
			an.lodBoundsParent = true
		}
		return an, nil
	}

	if len(slots) == 1 {
		combinedLocal := groupLocal.Mul(slots[0].local)
		if err := validateTransformMatrix(combinedLocal); err != nil {
			return err
		}
		name := stem
		if name == "" {
			name = stripLODSuffix(slots[0].name)
		}
		an, err := buildSlotNode(&slots[0], name, parent, combinedLocal, false)
		if err != nil {
			return err
		}
		out.Nodes = append(out.Nodes, an)
		return nil
	}

	groupIdx := len(out.Nodes)
	out.Nodes = append(out.Nodes, AssetNode{
		Name:   stem,
		Parent: parent,
		Local:  groupLocal,
	})
	for si := range slots {
		slotName := stripLODSuffix(slots[si].name)
		if slotName == "" {
			slotName = fmt.Sprintf("%s/%d", stem, si)
		}
		an, err := buildSlotNode(&slots[si], slotName, groupIdx, slots[si].local, true)
		if err != nil {
			return err
		}
		out.Nodes = append(out.Nodes, an)
	}
	return nil
}

func populateAlignedLODLevel(lvl *LODLevel, e *lodPrim, slotLocal, invSlot nt3dmath.Mat4) error {
	if matApproxEqual(slotLocal, e.relLocal) {
		if e.shared != nil {
			lvl.Shared = e.shared
			return nil
		}
		lvl.Geometry = *e.geom
		return nil
	}
	delta := invSlot.Mul(e.relLocal)
	if e.shared != nil {
		tg, err := TransformGeometry(e.shared.geometry.Geometry, delta)
		if err != nil {
			return err
		}
		sg, err := NewSharedGeometry(tg)
		if err != nil {
			return err
		}
		lvl.Shared = sg
		return nil
	}
	tg, err := TransformGeometry(*e.geom, delta)
	if err != nil {
		return err
	}
	lvl.Geometry = tg
	return nil
}

// siblingTierRootAdjust returns the relative root transform for a sibling tier
// node compared against tier 0's root transform. Showcase packs frequently park
// sibling *_LOD0, *_LOD1, *_LOD2 nodes at different translations along an axis
// while authoring each mesh around its own local origin; translation offsets on
// the sibling root itself are therefore ignored, while rotation/scale differences
// (such as a centimeter-scaled LOD tier) are preserved.
func siblingTierRootAdjust(lod0Local, tierLocal nt3dmath.Mat4) (nt3dmath.Mat4, error) {
	if err := validateTransformMatrix(tierLocal); err != nil {
		return nt3dmath.Mat4{}, err
	}
	l0 := clearTranslation(lod0Local)
	lt := clearTranslation(tierLocal)
	if matApproxEqual(l0, lt) {
		return nt3dmath.Identity(), nil
	}
	inv0, ok := l0.InverseAffine()
	if !ok {
		return nt3dmath.Mat4{}, errors.New("nt3d: singular LOD root transform")
	}
	return inv0.Mul(lt), nil
}

func clearTranslation(m nt3dmath.Mat4) nt3dmath.Mat4 {
	m[12], m[13], m[14] = 0, 0, 0
	return m
}

func validateTransformMatrix(m nt3dmath.Mat4) error {
	if _, ok := m.InverseAffine(); !ok || m.Determinant3() <= 0 {
		return errors.New("nt3d: transform must be invertible, affine, and orientation-preserving")
	}
	return nil
}

func matApproxEqual(a, b nt3dmath.Mat4) bool {
	const eps = 1e-5
	for i := range a {
		if float32(math.Abs(float64(a[i]-b[i]))) > eps {
			return false
		}
	}
	return true
}

func validateAssetParents(a *SceneAsset) error {
	for i, n := range a.Nodes {
		if n.Parent >= i || n.Parent < -1 {
			return errors.New("nt3d: invalid asset parent")
		}
		if err := validateTransformMatrix(n.Local); err != nil {
			return err
		}
	}
	return nil
}

func assetDynamicNodes(a *SceneAsset) []bool {
	dyn := make([]bool, len(a.Nodes))
	for i, n := range a.Nodes {
		if n.Deformation != nil {
			dyn[i] = true
			if n.Deformation.Skeleton != nil {
				for _, j := range n.Deformation.Skeleton.Joints {
					if idx := int(j) - 1; idx >= 0 && idx < len(dyn) {
						dyn[idx] = true
					}
				}
			}
		}
	}
	for _, c := range a.Clips {
		for _, t := range c.Tracks {
			if t.Channel != UserChannel {
				if idx := int(t.Node) - 1; idx >= 0 && idx < len(dyn) {
					dyn[idx] = true
				}
			}
		}
	}
	return dyn
}

func cloneAssetNode(n AssetNode) AssetNode {
	cp := n
	if n.Camera != nil {
		cam := *n.Camera
		cp.Camera = &cam
	}
	if n.Light != nil {
		lt := *n.Light
		cp.Light = &lt
	}
	if n.Geometry != nil {
		g := cloneGeometry(*n.Geometry)
		cp.Geometry = &g
	}
	if len(n.LOD) > 0 {
		cp.LOD = make([]LODLevel, len(n.LOD))
		for i, l := range n.LOD {
			cp.LOD[i] = l
			if l.Shared == nil && !isZeroGeometry(l.Geometry) {
				cp.LOD[i].Geometry = cloneGeometry(l.Geometry)
			}
		}
	}
	if len(n.VariantMaterials) > 0 {
		cp.VariantMaterials = make(map[int]*Material, len(n.VariantMaterials))
		for k, v := range n.VariantMaterials {
			cp.VariantMaterials[k] = cloneMaterial(v)
		}
	}
	if len(n.LODVariantMaterials) > 0 {
		cp.LODVariantMaterials = make([]map[int]*Material, len(n.LODVariantMaterials))
		for i, m := range n.LODVariantMaterials {
			if len(m) > 0 {
				cp.LODVariantMaterials[i] = make(map[int]*Material, len(m))
				for k, v := range m {
					cp.LODVariantMaterials[i][k] = cloneMaterial(v)
				}
			}
		}
	}
	return cp
}

func normalizeLODDistances(maxDistances []float32, maxTiers int) ([]float32, error) {
	if maxTiers >= 2 && len(maxDistances) == maxTiers-1 {
		for i, d := range maxDistances {
			if !nt3dmath.IsFinite(float64(d)) || d <= 0 {
				return nil, fmt.Errorf("nt3d: non-final LOD level requires a finite positive MaxDistance (got %v)", d)
			}
			if i > 0 && d <= maxDistances[i-1] {
				return nil, errors.New("nt3d: LOD MaxDistance must strictly increase")
			}
		}
		out := make([]float32, maxTiers)
		copy(out, maxDistances)
		out[maxTiers-1] = 0
		return out, nil
	}
	if len(maxDistances) != maxTiers {
		if maxTiers >= 2 {
			return nil, fmt.Errorf("nt3d: WithLOD got %d max distances for %d LOD tiers (want %d or %d)", len(maxDistances), maxTiers, maxTiers-1, maxTiers)
		}
		return nil, fmt.Errorf("nt3d: WithLOD got %d max distances for 1 LOD tier (want 1)", len(maxDistances))
	}
	if err := validateExplicitLODDistances(maxDistances); err != nil {
		return nil, err
	}
	return slices.Clone(maxDistances), nil
}

func validateExplicitLODDistances(dists []float32) error {
	for i, d := range dists {
		if math.IsNaN(float64(d)) || d < 0 {
			return fmt.Errorf("nt3d: invalid LOD distance %v", d)
		}
		if i == len(dists)-1 && (d == 0 || math.IsInf(float64(d), 1)) {
			continue
		}
		if !nt3dmath.IsFinite(float64(d)) || d <= 0 {
			return errors.New("nt3d: non-final LOD level requires a finite positive MaxDistance")
		}
		if i > 0 && d <= dists[i-1] {
			return errors.New("nt3d: LOD MaxDistance must strictly increase")
		}
	}
	return nil
}

func groupTierDistances(fullDists []float32, count int) []float32 {
	if count == len(fullDists) {
		return slices.Clone(fullDists)
	}
	out := make([]float32, count)
	if count > 1 {
		copy(out, fullDists[:count-1])
	}
	out[count-1] = fullDists[len(fullDists)-1]
	return out
}

func stripLODSuffix(name string) string {
	if stem, _, ok := parseLODName(name, ""); ok && stem != "LOD" {
		return stem
	}
	return name
}

func isLODSeparator(b byte) bool {
	return b == '_' || b == '-' || b == '.' || b == ' '
}

func parseLODName(name, parentName string) (stem string, tier int, ok bool) {
	n := len(name)
	if n < 4 {
		return "", 0, false
	}
	// Parse 1 or 2 trailing ASCII digits.
	dEnd := n
	dStart := dEnd
	for dStart > 0 && name[dStart-1] >= '0' && name[dStart-1] <= '9' {
		dStart--
	}
	digits := dEnd - dStart
	if digits == 0 || digits > 2 {
		return "", 0, false
	}
	val := 0
	for i := dStart; i < dEnd; i++ {
		val = val*10 + int(name[i]-'0')
	}
	if val > 31 {
		return "", 0, false
	}
	pos := dStart
	if pos > 0 && isLODSeparator(name[pos-1]) {
		pos--
	}
	if pos < 3 || !strings.EqualFold(name[pos-3:pos], "lod") {
		return "", 0, false
	}
	prefixEnd := pos - 3
	if prefixEnd == 0 {
		if parentName != "" && parentName != "Y-up" {
			return parentName, val, true
		}
		return "LOD", val, true
	}
	if !isLODSeparator(name[prefixEnd-1]) {
		return "", 0, false
	}
	for prefixEnd > 0 && isLODSeparator(name[prefixEnd-1]) {
		prefixEnd--
	}
	if prefixEnd == 0 {
		return "", 0, false
	}
	return name[:prefixEnd], val, true
}
