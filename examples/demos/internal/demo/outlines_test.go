package demo

import (
	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/nt3d"
	nt3dwidget "nimbleterminal.dev/nt3d/widget"
	"strings"
	"testing"
)

func TestShapesOutlineToggle(t *testing.T) {
	d := &demo{scene: nt3d.NewScene(), meshes: &meshScene{shiny: true}}
	var err error
	d.group, err = d.scene.Add(nt3d.Root, "shapes")
	if err != nil {
		t.Fatal(err)
	}
	if err = d.setupShapes(); err != nil {
		t.Fatal(err)
	}
	d.view = nt3dwidget.New(d.scene, 80, 24)
	for _, on := range []bool{true, false} {
		d.Update(tea.KeyPressMsg{Code: 'b'})
		f, err := d.scene.Frame(nt3d.DefaultCamera(), 80, 60)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range f.Geometry {
			if (g.Outline.Width > 0) != on {
				t.Fatal("outline toggle lost a shape")
			}
		}
	}
	_, keys := d.sceneKeys()
	found := false
	for _, k := range keys.scene {
		if k.Help().Desc == "outlines" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing touch control")
	}
	if !strings.Contains(d.meshesView(""), "b outlines") {
		t.Fatal("missing control hint")
	}
}
