// Copyright (c) 2026 Neomantra Corp.

// Package nt3dclod groups a large set of independently placed items (points,
// instances, labels) into spatial clusters once, and each frame selects the
// clusters inside the view and a level of detail for each: near clusters
// list all their items, far ones a spread subsample or none, leaving the
// caller to draw a representative. The output is a compact list of item
// indices (and the picked clusters) that the existing point, instance, and
// label paths draw. The package never draws, and knows nothing of what an
// item is beyond its position.
//
// Build makes a median-split hierarchy whose leaves are the clusters; each
// cluster's items are ordered so that any prefix is spread over the
// cluster, so one order serves every level of detail. Select runs on the
// CPU; GPU runs the same rules in WebGPU compute, writing storage buffers
// and indirect draw arguments, and reads them back for tests and CPU
// consumers. The two agree except where floating-point rounding moves a
// cluster across a plane or a range boundary.
//
// Limits: MaxItems items and MaxClusters clusters a set, MaxLevels levels
// of detail, cluster sizes of 1 to 65536 items. An item costs four bytes of
// order and a cluster thirty-two of record on the GPU.
package nt3dclod
