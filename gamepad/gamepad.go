// Copyright (c) 2026 Neomantra Corp.

// Package nt3dgamepad polls standard game controllers across browser (WASM) and
// native platforms with CGO_ENABLED=0.
package nt3dgamepad

import "math"

// Button identifies a standard gamepad digital button (W3C Standard Gamepad /
// SDL / Apple Extended Gamepad layout).
type Button uint16

const (
	ButtonSouth  Button = 1 << iota // Bottom face button (A / Cross)
	ButtonEast                      // Right face button (B / Circle)
	ButtonWest                      // Left face button (X / Square)
	ButtonNorth                     // Top face button (Y / Triangle)
	ButtonL1                        // Left shoulder bumper
	ButtonR1                        // Right shoulder bumper
	ButtonL2                        // Left trigger digital press
	ButtonR2                        // Right trigger digital press
	ButtonSelect                    // Back / Select / Share
	ButtonStart                     // Start / Options / Menu
	ButtonL3                        // Left stick click
	ButtonR3                        // Right stick click
	ButtonDPadUp
	ButtonDPadDown
	ButtonDPadLeft
	ButtonDPadRight
)

// DefaultDeadzone is the radial thumbstick deadzone ApplyDeadzone uses when
// given a non-positive threshold.
const DefaultDeadzone = 0.15

// State is one polled snapshot of the primary connected gamepad.
// Stick axes are in [-1, 1] with +X right and +Y up/forward; triggers are [0, 1].
type State struct {
	Connected                 bool
	Name                      string
	LeftX, LeftY              float64
	RightX, RightY            float64
	LeftTrigger, RightTrigger float64
	Buttons                   Button
}

// Pressed reports whether every button bit in b is down.
func (s State) Pressed(b Button) bool { return s.Connected && s.Buttons&b == b }

// WithDeadzone applies a radial deadzone to both thumbsticks, rescaling the
// live range smoothly from 0 at dz to 1 at full deflection.
func (s State) WithDeadzone(dz float64) State {
	if !(dz > 0) {
		dz = DefaultDeadzone
	}
	if dz >= 0.95 {
		dz = 0.95
	}
	s.LeftX, s.LeftY = radialDeadzone(s.LeftX, s.LeftY, dz)
	s.RightX, s.RightY = radialDeadzone(s.RightX, s.RightY, dz)
	s.LeftTrigger = clamp(s.LeftTrigger, 0, 1)
	s.RightTrigger = clamp(s.RightTrigger, 0, 1)
	return s
}

func radialDeadzone(x, y, dz float64) (ox, oy float64) {
	x, y = clamp(x, -1, 1), clamp(y, -1, 1)
	mag := math.Hypot(x, y)
	if !(mag > dz) {
		return 0, 0
	}
	scaled := math.Min(1, (mag-dz)/(1-dz))
	k := scaled / mag
	return float64(x * k), float64(y * k)
}

func clamp(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return math.Max(lo, math.Min(hi, v))
}

// Reader polls the active gamepad. Implementations must never block the frame.
type Reader interface {
	Poll() State
	Close() error
}

// Open returns a platform gamepad reader (W3C navigator.getGamepads on js/wasm,
// GameController.framework on macOS, /dev/input/js* on Linux, or an inert
// reader elsewhere). Tests should supply their own Reader rather than calling
// Open.
func Open() Reader { return openPlatform() }
