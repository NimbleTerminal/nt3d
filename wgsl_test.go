// Copyright (c) 2026 Neomantra Corp.
package nt3d

import (
	"strings"
	"testing"
)

// Browsers compile WGSL with Tint, which rejects a shift mixed with a
// bitwise operator at one parenthesis level ("mixing '<<' and '|' requires
// parenthesis"), while the native translator accepts it. A shader that only
// the native tests exercise can therefore break every browser frame that
// uses it, so each embedded shader is scanned for the mix.
func TestWGSLShiftsAreParenthesized(t *testing.T) {
	for name, source := range map[string]string{"scene.wgsl": sceneShader, "skin.wgsl": skinShader, "sort.wgsl": sortShader} {
		for _, problem := range wgslShiftMixes(source) {
			t.Errorf("%s:%s", name, problem)
		}
	}
}

// wgslShiftMixes lists the lines (as "line: text") where a shift and a
// bitwise or, and, or xor meet at the same parenthesis depth within one
// expression. Expressions end at ';', ',', '{', '}', and '=' (an
// assignment's sides are separate expressions); comments are skipped.
func wgslShiftMixes(source string) []string {
	var problems []string
	for n, line := range strings.Split(source, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		var shift, bitwise [64]bool
		depth := 0
		reset := func() { shift[depth], bitwise[depth] = false, false }
		for i := 0; i < len(line); i++ {
			switch c := line[i]; {
			case c == '(':
				depth++
				reset()
			case c == ')':
				if depth > 0 {
					depth--
				}
			case c == ';' || c == ',' || c == '{' || c == '}':
				reset()
			case c == '=' && (i == 0 || !strings.ContainsRune("<>!=&|^", rune(line[i-1]))) && (i+1 >= len(line) || line[i+1] != '='):
				reset()
			case (c == '<' || c == '>') && i+1 < len(line) && line[i+1] == c:
				shift[depth] = true
				i++
			case c == '&' || c == '|':
				if i+1 < len(line) && line[i+1] == c {
					i++ // && and || are logical, not bitwise.
					continue
				}
				bitwise[depth] = true
			case c == '^':
				bitwise[depth] = true
			}
			if shift[depth] && bitwise[depth] {
				problems = append(problems, strings.TrimSpace(strings.Join([]string{itoa(n + 1), line}, ": ")))
				break
			}
		}
	}
	return problems
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}
