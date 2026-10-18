// Copyright (c) 2026 Neomantra Corp.
package demo

// CharactersMain plays the Gobkit characters supplied in SampleFiles.
func CharactersMain() { mainDemo(false, sceneCharacters) }

var characterSamples = []sample{
	{file: "minion-a01.glb", title: "Minion A-1", about: "idle / attack / dead", beta: -55},
	{file: "Corgi.glb", title: "Corgi", about: "idle / attack / dead / walk", beta: -55},
	{file: "Duck.glb", title: "Duck", about: "idle / attack / dead / walk", beta: -55},
}

func (d *demo) setupCharacters() error {
	d.animatedSamples = &animateScene{samples: characterSamples, title: "characters", credit: "Gobkit CC0 · gobkit.com"}
	return d.loadAnimatedSample(0, 0)
}
