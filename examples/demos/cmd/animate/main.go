// Copyright (c) 2026 Neomantra Corp.
package main

import (
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo"
	gltfsamples "nimbleterminal.dev/nt3d/examples/demos/internal/demo/gltf"
	_ "nimbleterminal.dev/nt3d/mesh/gltf"
)

func main() {
	demo.SampleFiles = gltfsamples.Files
	demo.AnimateMain()
}
