// Copyright (c) 2026 Neomantra Corp.

package nt3d

// A slot is one kind of primitive the GPU renderer draws. Each geometry, and
// the renderer itself for the frame's own overlays and lines, keeps a batch
// of records and a bind group per slot (gpuBatch, instanceSet.binds), and
// the slots up to slotPipelines each have a pipeline whose vertex entry
// point is slotEntryPoints[slot].
const (
	slotPoint    = iota // Points and Gaussian splats.
	slotMesh            // Triangles shaded from vertex colors alone.
	slotBox             // Boxes.
	slotLine            // A geometry's line pairs.
	slotUI              // The frame's overlay rectangles; the renderer's own batch.
	slotDynamic         // The frame's dynamic lines; the renderer's own batch.
	slotArrow           // Arrows.
	slotTextured        // Triangles with a texture or maps; drawn with the texture pipelines.
	slotDrop            // Drop-shadow discs; drawn with the drop pipeline.
	slotCount

	// slotPipelines is how many slots have a pipeline in the pipelines array:
	// those before slotTextured.
	slotPipelines = slotTextured
)

// slotEntryPoints names the vertex entry point of each slot's pipeline.
var slotEntryPoints = [slotPipelines]string{
	slotPoint:   "vs_point",
	slotMesh:    "vs_mesh",
	slotBox:     "vs_box",
	slotLine:    "vs_line",
	slotUI:      "vs_ui",
	slotDynamic: "vs_dynamic",
	slotArrow:   "vs_arrow",
}

// slotDrawOrder is the order of a scene pass: the frame's dynamic lines
// first; then, where slotPoint stands, every geometry's opaque primitives;
// and the overlays last, over everything. Drop shadows and blended geometry
// are drawn where the pass reaches slotUI.
var slotDrawOrder = [...]int{slotDynamic, slotPoint, slotUI}

// geometrySlots is the order one geometry's opaque primitives are drawn in.
// A pass draws geometry by geometry in the frame's order, not slot by slot:
// of two surfaces at one depth the later draw shows, whatever kinds of
// primitive they are. Textured triangles, which have a slot of their own,
// are drawn just before the boxes.
var geometrySlots = [...]int{slotPoint, slotMesh, slotBox, slotLine, slotArrow}
