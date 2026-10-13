// nt3d - Copyright (c) 2026 Neomantra Corp.

package nt3dmath

import "math"

// IsFinite reports whether v is neither NaN nor infinite.
func IsFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
