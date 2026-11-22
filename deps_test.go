package nt3d

import (
	"os/exec"
	"strings"
	"testing"
)

// The engine draws to images and to wireframe text; it is not a terminal
// program. Whoever imports it, or water, fur, mesh, and the rest that build on
// it, must not inherit the terminal UI stack: only package widget may.
func TestEngineLinksNoTerminalUI(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".", "./water", "./fur", "./mesh/...", "./clod", "./cam/...", "./sh", "./aabbtree", "./collide/...", "./math").Output()
	if err != nil {
		t.Skipf("go list is unavailable: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		for _, banned := range []string{"charm.land/bubbletea", "charm.land/lipgloss", "charm.land/bubbles", "github.com/lrstanley/bubblezone", "github.com/charmbracelet/ultraviolet"} {
			if strings.HasPrefix(dep, banned) {
				t.Errorf("the engine links %s", dep)
			}
		}
	}
}

// Camera paths are built on their own, in float64, and joined to a scene by
// nt3d/cam/scene. The engine does not import them, so a program that never
// flies a path does not build one.
func TestEngineDoesNotImportCameraPaths(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list is unavailable: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, "nimbleterminal.dev/nt3d/cam") {
			t.Errorf("package nt3d imports %s", dep)
		}
	}
}

// Collision queries work on triangles and depend only on nt3d/math, so a tool
// or a server can ask them without linking the renderer. nt3d/collide/scene
// joins them to an nt3d.Geometry.
func TestCollideDoesNotImportTheEngine(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "./collide").Output()
	if err != nil {
		t.Skipf("go list is unavailable: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, "nimbleterminal.dev/nt3d") && dep != "nimbleterminal.dev/nt3d/collide" && dep != "nimbleterminal.dev/nt3d/math" {
			t.Errorf("package nt3d/collide imports %s", dep)
		}
	}
}
