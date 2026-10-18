// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"math"
	"testing"

	"nimbleterminal.dev/nt3d/math"
)

func TestDropShadowValidation(t *testing.T) {
	g := quad(1, 0, white, nil, false)
	for _, c := range []struct {
		s, r float32
		ok   bool
	}{{0, 0, true}, {.5, 0, true}, {1, 2, true}, {1.5, 0, false}, {-.1, 0, false}, {.5, -1, false}, {float32(math.NaN()), 0, false}, {.5, float32(math.Inf(1)), false}} {
		g.DropShadow = DropShadow{c.s, c.r}
		if err := g.Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: %v", c, err)
		}
	}
}

func TestDropShadowSurvivesTransformAndMerge(t *testing.T) {
	g := quad(1, 0, white, nil, false)
	g.DropShadow = DropShadow{Strength: .4, Radius: 2}
	moved, err := TransformGeometry(g, nt3dmath.Translation(nt3dmath.Vec3{X: 1}))
	check(t, err)
	later := quad(1, 2, white, nil, false)
	later.DropShadow = DropShadow{Strength: .8, Radius: 3}
	merged, err := MergeGeometry(quad(1, 1, white, nil, false), g, later)
	check(t, err)
	if moved.DropShadow != g.DropShadow || merged.DropShadow != g.DropShadow {
		t.Fatal("lost drop shadow or failed to retain first active shadow")
	}
}
