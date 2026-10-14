// Copyright (c) 2026 Neomantra Corp.
package terraindemo

import (
	"testing"

	"github.com/NimbleMarkets/go-booba/actions/bubblehelp"
)

// Every scene's keys become touch controls: all expressible, within the
// 2048-byte OSC 7501 message, with the scene's own controls in the compact bar.
func TestSceneActions(t *testing.T) {
	for _, d := range []*demo{
		{}, {dynamic: &dynamicScene{}}, {meshes: &meshScene{}}, {models: &modelScene{}, meshes: &meshScene{}},
		{large: &largeScene{}}, {fur: &furScene{}},
	} {
		context, k := d.sceneKeys()
		snapshot, diagnostics, err := bubblehelp.Snapshot(k, "nt3d-"+context, context, 1, bubblehelp.Options{})
		if err != nil || len(diagnostics) != 0 {
			t.Fatalf("%s: %v %+v", context, err, diagnostics)
		}
		if len(snapshot.Actions) < len(k.scene)+4 {
			t.Fatalf("%s: %d actions", context, len(snapshot.Actions))
		}
	}
}
