// Copyright (c) 2026 Neomantra Corp.

package nt3dwidget_test

import (
	"image/color"
	"log"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	nt3dwidget "nimbleterminal.dev/nt3d/widget"
)

// app is a Bubble Tea program around one widget. The widget draws only the 3D
// view; quitting, sizing, and anything drawn around it belong to the program.
type app struct{ view *nt3dwidget.Model }

func (a app) Init() tea.Cmd { return a.view.Init() }

func (a app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			// Dispose removes the widget's images while the program still runs.
			return a, tea.Sequence(a.view.Dispose(), tea.Quit)
		}
	case tea.WindowSizeMsg:
		return a, a.view.SetSize(msg.Width, msg.Height)
	case nt3dwidget.PickMsg:
		if msg.Hit != nil {
			log.Println("clicked", msg.Hit.Name, msg.Hit.Data)
		}
	}
	// Forward every message, and run every command the widget returns.
	_, cmd := a.view.Update(msg)
	return a, cmd
}

func (a app) View() tea.View {
	v := a.view.View()
	v.AltScreen = true
	return v
}

// A scene in a terminal: drag to orbit, scroll to zoom, q to quit. The
// widget draws the scene's own look; options and setters on the widget stand
// over it for that widget alone.
func Example() {
	scene := nt3d.NewScene()
	ball, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .6, Color: color.RGBA{R: 230, G: 120, B: 40, A: 255}})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := scene.SetGeometry("ball", ball); err != nil {
		log.Fatal(err)
	}

	view := nt3dwidget.New(scene, 80, 24)
	defer view.Close() // Releases the GPU, after the program has exited.
	if _, err := tea.NewProgram(app{view}).Run(); err != nil {
		log.Fatal(err)
	}
}
