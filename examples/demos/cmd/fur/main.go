// Copyright (c) 2026 Neomantra Corp.
package main

import (
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo"
	"nimbleterminal.dev/nt3d/examples/demos/internal/demo/models"
)

func main() {
	// Only this demo's program embeds the sample files.
	demo.SampleFiles = models.Spot
	demo.FurMain()
}
