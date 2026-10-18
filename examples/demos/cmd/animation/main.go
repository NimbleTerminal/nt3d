// Copyright (c) 2026 Neomantra Corp.
package main

import (
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo"
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo/models"
	_ "nimbleterminal.dev/nt3d/mesh/gltf"
)

func main() { demo.SampleFiles = models.Ribbon; demo.AnimationMain() }
