// Copyright (c) 2026 Neomantra Corp.
package main

import (
	"nimbleterminal.dev/nt3d/examples/terrain/internal/terraindemo"
	"nimbleterminal.dev/nt3d/examples/terrain/internal/terraindemo/models"
)

func main() {
	// Only this demo's program embeds the sample files.
	terraindemo.SampleFiles = models.Spot
	terraindemo.FurMain()
}
