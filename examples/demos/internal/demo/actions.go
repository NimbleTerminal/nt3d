// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/NimbleMarkets/go-booba/actions/bubblehelp"
)

// keys is a scene's help: its own controls first, then the view's.
type keys struct {
	scene []key.Binding
}

func bind(keys, help, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys), key.WithHelp(help, desc))
}

var (
	backendKeys = []key.Binding{bind("1", "1", "GPU"), bind("2", "2", "software"), bind("3", "3", "wireframe")}
	fitKey      = bind("c", "c", "fit")
	fogKey      = bind("d", "d", "fog")
	// Dragging orbits, so the arrows are left out: they would push the
	// terrain scene past booba's 2048-byte limit on an action message.
	viewKeys = []key.Binding{
		bind("+", "+", "zoom in"),
		bind("-", "-", "zoom out"),
		bind("r", "r", "rotate"),
		bind("o", "o", "projection"),
	}
)

// ShortHelp is the compact bar: the scene's controls, fit, and backends.
func (k keys) ShortHelp() []key.Binding {
	return append(append(append([]key.Binding{}, k.scene...), fitKey, fogKey), backendKeys...)
}

// FullHelp adds the camera's controls.
func (k keys) FullHelp() [][]key.Binding {
	return [][]key.Binding{append(append([]key.Binding{}, k.scene...), fitKey, fogKey), backendKeys, viewKeys}
}

// sceneKeys are the keys the current scene answers to.
func (d *demo) sceneKeys() (string, keys) {
	switch {
	case d.animatedSamples != nil:
		return "animate", keys{[]key.Binding{bind("[", "[", "previous model"), bind("]", "]", "next model"), bind(",", ",", "previous clip"), bind(".", ".", "next clip"), bind("a", "a", "play/pause"), bind("enter", "enter", "restart"), bind("b", "b", "outlines")}}
	case d.animation != nil:
		return "animation", keys{[]key.Binding{bind("a", "a", "pause")}}
	case d.particles != nil:
		return "particles", keys{[]key.Binding{bind("b", "b", "blend"), bind("s", "s", "shape"), bind("n", "n", "count"), bind("a", "a", "pause")}}
	case d.lights != nil:
		return "lights", keys{[]key.Binding{bind("l", "l", "preset"), bind("f", "f", "falloff"), bind("e", "e", "highlights"), bind("n", "n", "normal map"), bind("s", "s", "shadows"), bind("a", "a", "pause")}}
	case d.forest != nil:
		return "forest", keys{[]key.Binding{bind("i", "i", "instancing"), bind("n", "n", "trees"), bind("s", "s", "shadows"), bind("a", "a", "wind")}}
	case d.mipmaps != nil:
		return "mipmaps", keys{[]key.Binding{bind("m", "m", "filtering"), bind("t", "t", "texture"), bind("x", "x", "supersample")}}
	case d.fur != nil:
		return "fur", keys{[]key.Binding{bind("a", "a", "wind"), bind("s", "s", "shells"), bind("l", "l", "length"), bind("f", "f", "fins"), bind("x", "x", "supersample"), bind("e", "e", "effect")}}
	case d.large != nil:
		return "dense-mesh", keys{[]key.Binding{bind("i", "i", "pick index"), bind("n", "n", "turn")}}
	case d.models != nil:
		return "models", keys{[]key.Binding{bind("[", "[", "previous"), bind("]", "]", "next"), bind("s", "s", "normals"), bind("n", "n", "turn")}}
	case d.meshes != nil:
		return "shapes", keys{[]key.Binding{bind("s", "s", "normals"), bind("t", "t", "glass"), bind("e", "e", "highlights"), bind("b", "b", "outlines"), bind("n", "n", "turn")}}
	case d.dynamic != nil:
		return "dynamic", keys{[]key.Binding{bind("a", "a", "pause"), bind("m", "m", "deform"), bind("b", "b", "add"), bind("x", "x", "remove"), bind("v", "v", "visibility")}}
	}
	return "terrain", keys{[]key.Binding{bind("a", "a", "animate"), bind("p", "p", "procedural"), bind("t", "t", "texture"), bind("f", "f", "flat"), bind("l", "l", "lighting"), bind("n", "n", "turn"), bind("v", "v", "visibility")}}
}

// withActions wraps the demo so the browser page can show its keys as touch
// controls (booba's Terminal Actions): the help is published through an OSC
// 7501 status message, and buttons send the keys back as input.
func (d *demo) withActions() (tea.Model, error) {
	context, _ := d.sceneKeys()
	return bubblehelp.NewModel(d, func(tea.Model) bubblehelp.Description {
		context, k := d.sceneKeys()
		return bubblehelp.Description{Context: context, Keys: k}
	}, bubblehelp.ModelOptions{Instance: "nt3d-" + context})
}
