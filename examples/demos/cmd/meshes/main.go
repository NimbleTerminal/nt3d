// Copyright (c) 2026 Neomantra Corp.
package main

import (
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo"

	// Mesh files named on the command line may be in any format.
	_ "nimbleterminal.dev/nt3d/mesh/formats"
)

func main() { demo.MeshesMain() }
