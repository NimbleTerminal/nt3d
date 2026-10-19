// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"nimbleterminal.dev/nt3d"
	"testing"

	"github.com/NimbleMarkets/go-booba/actions/bubblehelp"
)

// Every scene's keys become touch controls: all expressible, within the
// 2048-byte OSC 7501 message, with the scene's own controls in the compact bar.
func TestSceneActions(t *testing.T) {
	for _, d := range []*demo{
		{}, {dynamic: &dynamicScene{}}, {meshes: &meshScene{}}, {meshes: &meshScene{hasSH: true}}, {models: &modelScene{}, meshes: &meshScene{}},
		{animatedSamples: &animateScene{}}, {animatedSamples: &animateScene{cameras: []nt3d.NodeID{1, 2}}}, {large: &largeScene{}}, {fur: &furScene{}}, {mipmaps: &mipScene{}}, {forest: &forestScene{}}, {lights: &lightScene{}}, {particles: &particleScene{}}, {aabb: &aabbScene{}},
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

func TestShadowActions(t *testing.T) {
	for _, d := range []*demo{{lights: &lightScene{}}, {forest: &forestScene{}}} {
		context, k := d.sceneKeys()
		found := false
		for _, binding := range k.scene {
			help := binding.Help()
			if help.Key == "s" && help.Desc == "shadows" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s has no shadow action", context)
		}
	}
}

func TestParticleSpriteAction(t *testing.T) {
	d := &demo{particles: &particleScene{}}
	_, k := d.sceneKeys()
	for _, binding := range k.scene {
		if binding.Help().Key == "t" && binding.Help().Desc == "sprites" {
			return
		}
	}
	t.Fatal("particles have no sprite touch action")
}
