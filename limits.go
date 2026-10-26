// Copyright (c) 2026 Neomantra Corp.
package nt3d

import "sync/atomic"

// DefaultBindingLimit is the storage buffer binding, in bytes, that WebGPU
// guarantees on every device, and the limit nt3d measures a geometry's
// buffers against unless SetBindingLimit raises it. MaxPoints,
// MaxMeshTriangles, MaxMeshIndices, and the counts MaxGaussians and
// MaxPreciseGaussians give at this limit hold on every device.
const DefaultBindingLimit = 128 << 20

// MaxBindingLimit is the most SetBindingLimit accepts: a gibibyte keeps
// every byte count an int on 32-bit platforms and already exceeds what
// MaxPointIndex can use.
const MaxBindingLimit = 1 << 30

// MaxPointIndex is the most points one geometry holds whatever the binding
// limit: the level-of-detail selection packs a point's index into 24 bits.
const MaxPointIndex = 1 << 24

// bindingLimit is the current limit, DefaultBindingLimit until
// SetBindingLimit raises it.
var bindingLimit = func() (v atomic.Int64) { v.Store(DefaultBindingLimit); return }()

// SetBindingLimit sets the storage binding size, in bytes, that Validate,
// the GPU renderer's sort buffers, and the mesh decoders' splitting measure
// against, clamped to [DefaultBindingLimit, MaxBindingLimit], and returns
// the value in force. Raise it once at startup, before geometries are
// stored, to what the device offers: AdoptBindingLimit asks the GPU
// renderer. Geometries already stored are not re-validated; the GPU
// renderer refuses one its device cannot hold when it is drawn.
func SetBindingLimit(bytes int) int {
	bytes = max(DefaultBindingLimit, min(MaxBindingLimit, bytes))
	bindingLimit.Store(int64(bytes))
	return bytes
}

// BindingLimit is the storage binding size, in bytes, in force.
func BindingLimit() int { return int(bindingLimit.Load()) }

// setBindingLimit stores a limit without clamping it; tests lower it.
func setBindingLimit(bytes int) { bindingLimit.Store(int64(bytes)) }

// PointLimit is the most points one geometry holds at the current limit:
// MaxPoints by default, never more than MaxPointIndex.
func PointLimit() int { return min(BindingLimit()/20, MaxPointIndex) }

// MeshTriangleLimit is the most triangles one geometry's mesh holds at the
// current limit: MaxMeshTriangles by default.
func MeshTriangleLimit() int { return BindingLimit() / 48 / 3 }

// MeshIndexLimit is the most indices, and vertices, one geometry's mesh
// holds at the current limit: MaxMeshIndices by default.
func MeshIndexLimit() int { return 3 * MeshTriangleLimit() }
