// Copyright (c) 2026 Neomantra Corp.

//go:build webgpu

package demo

import "testing"

// Every demo draws, on the GPU, the picture it drew when its reference was
// recorded. The software references cannot see a fault that is the GPU's
// alone, such as the order it draws in or a pass that never runs.
//
// The references were recorded on Apple Metal. Another adapter filters and
// rounds a little differently, which the allowance covers; if one differs by
// more without a fault, record references for it rather than widening the
// allowance:
//
//	go -C examples/demos test -tags webgpu ./internal/demo -run TestGPUDemosMatchReference -update-reference
func TestGPUDemosMatchReference(t *testing.T) {
	matchReference(t, "gpu", "reference-gpu")
}
