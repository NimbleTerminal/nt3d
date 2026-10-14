// Copyright (c) 2026 Neomantra Corp.
package main

import (
	"nimbleterminal.dev/nt3d/examples/terrain/internal/terraindemo"

	// Mesh files named on the command line may be in any format.
	_ "nimbleterminal.dev/nt3d/mesh/formats"
)

func main() { terraindemo.MeshesMain() }
