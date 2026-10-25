// Copyright (c) 2026 Neomantra Corp.

package formats

import (
	"testing"

	"nimbleterminal.dev/nt3d/mesh"
)

// Importing the package registers the optional formats' extensions.
func TestRegistered(t *testing.T) {
	for name, want := range map[string]nt3dmesh.Format{"a.sog": "sog", "a.spz": "spz"} {
		if got, _ := nt3dmesh.FormatOf(name, nil); got != want {
			t.Errorf("%s: %q, want %q", name, got, want)
		}
	}
}
