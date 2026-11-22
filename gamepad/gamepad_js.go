// Copyright (c) 2026 Neomantra Corp.

//go:build js && wasm

package nt3dgamepad

import "syscall/js"

type webReader struct{}

func openPlatform() Reader { return webReader{} }

func (webReader) Close() error { return nil }

func (webReader) Poll() State {
	nav := js.Global().Get("navigator")
	if !nav.Truthy() || nav.Get("getGamepads").Type() != js.TypeFunction {
		return State{}
	}
	pads := nav.Call("getGamepads")
	if !pads.Truthy() {
		return State{}
	}
	n := pads.Length()
	for i := range n {
		gp := pads.Index(i)
		if !gp.Truthy() || !gp.Get("connected").Bool() {
			continue
		}
		var s State
		s.Connected = true
		s.Name = gp.Get("id").String()
		axes := gp.Get("axes")
		if axes.Truthy() {
			alen := axes.Length()
			if alen > 0 {
				s.LeftX = clamp(axes.Index(0).Float(), -1, 1)
			}
			if alen > 1 {
				// W3C gamepad +Y is down; negate so +Y is up/forward.
				s.LeftY = clamp(-axes.Index(1).Float(), -1, 1)
			}
			if alen > 2 {
				s.RightX = clamp(axes.Index(2).Float(), -1, 1)
			}
			if alen > 3 {
				s.RightY = clamp(-axes.Index(3).Float(), -1, 1)
			}
		}
		btns := gp.Get("buttons")
		if btns.Truthy() {
			blen := btns.Length()
			pressed := func(idx int) (bool, float64) {
				if idx >= blen {
					return false, 0
				}
				b := btns.Index(idx)
				if !b.Truthy() {
					return false, 0
				}
				return b.Get("pressed").Bool(), clamp(b.Get("value").Float(), 0, 1)
			}
			mapping := [16]Button{
				ButtonSouth, ButtonEast, ButtonWest, ButtonNorth,
				ButtonL1, ButtonR1, ButtonL2, ButtonR2,
				ButtonSelect, ButtonStart, ButtonL3, ButtonR3,
				ButtonDPadUp, ButtonDPadDown, ButtonDPadLeft, ButtonDPadRight,
			}
			for idx, bit := range mapping {
				down, val := pressed(idx)
				if down || val > 0.5 {
					s.Buttons |= bit
				}
				if idx == 6 {
					s.LeftTrigger = val
				} else if idx == 7 {
					s.RightTrigger = val
				}
			}
		}
		return s
	}
	return State{}
}
