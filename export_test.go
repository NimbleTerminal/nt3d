// Copyright (c) 2026 Neomantra Corp.

package nt3d

// SetBindingLimitForTest stores a binding limit without clamping it, for a
// test outside the package that needs clouds to split at a size it can hold.
func SetBindingLimitForTest(bytes int) { setBindingLimit(bytes) }
