// Copyright (c) 2026 Neomantra Corp.

// Package gallery exposes the 18 interactive NT3D demo scenes and their
// embedded sample assets to external host windows such as examples/gogpu.
package gallery

import (
	"io/fs"

	"nimbleterminal.dev/nt3d/examples/demos/internal/demo"
	gltfsamples "nimbleterminal.dev/nt3d/examples/demos/internal/demo/gltf"
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo/gobkit"
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo/models"
	nt3dmesh "nimbleterminal.dev/nt3d/mesh"
	_ "nimbleterminal.dev/nt3d/mesh/formats"
)

// Runner drives one of the 18 interactive demo scenes inside an external host window.
type Runner = demo.Runner

// Names returns the ordered list of all 18 interactive demo scene names.
func Names() []string {
	return demo.Names()
}

// NewRunner constructs and initializes the named demo scene with its embedded
// sample assets.
func NewRunner(name string, files []string, up nt3dmesh.Up, yDown bool) (*Runner, error) {
	var samples fs.FS
	switch name {
	case "animate":
		samples = gltfsamples.Files
	case "animation":
		samples = models.Ribbon
	case "characters":
		samples = gobkit.Files
	case "fur":
		samples = models.Spot
	case "models":
		samples = models.Files
	}
	return demo.NewRunner(name, samples, files, up, yDown)
}
