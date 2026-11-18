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
	out, err := exec.Command("go", "list", "-deps", ".", "./water", "./fur", "./mesh/...", "./clod", "./cam/...", "./sh", "./aabbtree", "./math").Output()
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
