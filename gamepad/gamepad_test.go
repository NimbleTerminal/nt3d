// Copyright (c) 2026 Neomantra Corp.

package nt3dgamepad

import (
	"math"
	"testing"
)

func TestWithDeadzoneAndButtons(t *testing.T) {
	raw := State{
		Connected:    true,
		LeftX:        0.08,
		LeftY:        0.05,
		RightX:       0.6,
		RightY:       -0.8,
		LeftTrigger:  -0.2,
		RightTrigger: 1.4,
		Buttons:      ButtonSouth | ButtonR1,
	}
	got := raw.WithDeadzone(0.15)
	if got.LeftX != 0 || got.LeftY != 0 {
		t.Fatalf("inside deadzone not zeroed: (%v, %v)", got.LeftX, got.LeftY)
	}
	if mag := math.Hypot(got.RightX, got.RightY); math.Abs(mag-1) > 1e-6 {
		t.Fatalf("full deflection magnitude=%v, want 1", mag)
	}
	if got.LeftTrigger != 0 || got.RightTrigger != 1 {
		t.Fatalf("triggers=(%v, %v), want (0, 1)", got.LeftTrigger, got.RightTrigger)
	}
	if !got.Pressed(ButtonSouth) || !got.Pressed(ButtonR1) || got.Pressed(ButtonNorth) {
		t.Fatalf("unexpected Pressed state: %+v", got)
	}
	if (State{Buttons: ButtonSouth}).Pressed(ButtonSouth) {
		t.Fatal("disconnected state reported Pressed=true")
	}

	r := Open()
	defer r.Close()
	_ = r.Poll()
}
