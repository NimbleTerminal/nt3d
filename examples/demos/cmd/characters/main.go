// Copyright (c) 2026 Neomantra Corp.
package main

import (
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo"
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo/gobkit"
	_ "nimbleterminal.dev/nt3d/mesh/gltf"
)

func main() {
	demo.SampleFiles = gobkit.Files
	demo.CharactersMain()
}
