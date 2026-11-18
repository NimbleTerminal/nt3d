// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// A shader hook's WGSL is compiled into the scene shader's own module: WGSL
// has no namespaces, so the hook's module-scope declarations (functions,
// structs, constants, variables, aliases) and the scene shader's share one
// set of names, and a name declared twice fails the whole module. The names
// the scene shader declares are therefore reserved, and a hook that takes one
// is refused when it is made, by name.

var wgslDeclaration = regexp.MustCompile(`\b(?:fn|struct|alias|const|override|var(?:\s*<[^>]*>)?)\s+([A-Za-z_][A-Za-z0-9_]*)`)

// wgslDeclared returns the names src declares at module scope.
func wgslDeclared(src string) map[string]bool {
	// Blank out comments and everything inside braces, which leaves only
	// module-scope text: a function's parameters stay, its body does not.
	top := make([]byte, 0, len(src))
	depth := 0
	for i := 0; i < len(src); i++ {
		switch {
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
			top = append(top, '\n')
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				i = len(src)
			} else {
				i += end + 3
			}
			top = append(top, ' ')
		case src[i] == '{':
			depth++
			top = append(top, ' ')
		case src[i] == '}':
			depth = max(0, depth-1)
			top = append(top, ' ')
		case depth == 0:
			top = append(top, src[i])
		}
	}
	names := map[string]bool{}
	for _, m := range wgslDeclaration.FindAllSubmatch(top, -1) {
		names[string(m[1])] = true
	}
	return names
}

// sceneShaderNames is every name a hook module declares before the hook's own
// source is added: the scene shader's, the spherical-harmonics helpers', and
// the hook interface's.
var sceneShaderNames = sync.OnceValue(func() map[string]bool {
	return wgslDeclared(hookShaderSource("", ""))
})

// checkHookNames refuses hook source that declares a name the scene shader
// uses. entry is the function the hook must define, which is the hook's own.
func checkHookNames(src, entry string) error {
	reserved := sceneShaderNames()
	var taken []string
	for name := range wgslDeclared(src) {
		if name != entry && reserved[name] {
			taken = append(taken, name)
		}
	}
	if len(taken) == 0 {
		return nil
	}
	slices.Sort(taken)
	return fmt.Errorf("nt3d: shader hook declares %q, a name the scene shader uses; a hook is compiled into the scene shader's module and shares its names, so rename it", taken[0])
}

// checkHookPair refuses two hooks that declare the same name: a displacement
// and a material shader drawn on one mesh are compiled into one module.
func checkHookPair(displaceSrc, materialSrc string) error {
	if displaceSrc == "" || materialSrc == "" {
		return nil
	}
	material := wgslDeclared(materialSrc)
	var shared []string
	for name := range wgslDeclared(displaceSrc) {
		if material[name] {
			shared = append(shared, name)
		}
	}
	if len(shared) == 0 {
		return nil
	}
	slices.Sort(shared)
	return fmt.Errorf("nt3d: a displacement and a material shader on one mesh both declare %q; they are compiled into one module, so rename one", shared[0])
}
