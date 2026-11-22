// Copyright (c) 2026 Neomantra Corp.

//go:build linux && !js

package nt3dgamepad

import (
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
)

const (
	jsEventButton = 0x01
	jsEventAxis   = 0x02
)

type linuxReader struct {
	file    *os.File
	name    string
	axes    [8]float64
	buttons [16]bool
	buf     [64]byte
}

func openPlatform() Reader { return &linuxReader{} }

func (r *linuxReader) Close() error {
	if r.file != nil {
		err := r.file.Close()
		r.file = nil
		return err
	}
	return nil
}

func (r *linuxReader) tryOpen() {
	if r.file != nil {
		return
	}
	for i := range 4 {
		path := fmt.Sprintf("/dev/input/js%d", i)
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		r.file = os.NewFile(uintptr(fd), path)
		r.name = path
		r.axes = [8]float64{}
		r.buttons = [16]bool{}
		return
	}
}

func (r *linuxReader) Poll() State {
	r.tryOpen()
	if r.file == nil {
		return State{}
	}
	for {
		n, err := r.file.Read(r.buf[:])
		for off := 0; off+8 <= n; off += 8 {
			val := int16(binary.LittleEndian.Uint16(r.buf[off+4 : off+6]))
			typ := r.buf[off+6] &^ 0x80 // strip JS_EVENT_INIT
			num := int(r.buf[off+7])
			switch typ {
			case jsEventButton:
				if num < len(r.buttons) {
					r.buttons[num] = val != 0
				}
			case jsEventAxis:
				if num < len(r.axes) {
					r.axes[num] = clamp(float64(val)/32767.0, -1, 1)
				}
			}
		}
		if err != nil {
			if pathErr, ok := err.(*os.PathError); ok && (pathErr.Err == syscall.EAGAIN || pathErr.Err == syscall.EWOULDBLOCK) {
				break
			}
			_ = r.Close()
			return State{}
		}
		if n < 8 {
			break
		}
	}
	s := State{
		Connected:    true,
		Name:         r.name,
		LeftX:        r.axes[0],
		LeftY:        -r.axes[1],
		RightX:       r.axes[3],
		RightY:       -r.axes[4],
		LeftTrigger:  clamp((r.axes[2]+1)*0.5, 0, 1),
		RightTrigger: clamp((r.axes[5]+1)*0.5, 0, 1),
	}
	if r.axes[7] < -0.5 {
		s.Buttons |= ButtonDPadUp
	} else if r.axes[7] > 0.5 {
		s.Buttons |= ButtonDPadDown
	}
	if r.axes[6] < -0.5 {
		s.Buttons |= ButtonDPadLeft
	} else if r.axes[6] > 0.5 {
		s.Buttons |= ButtonDPadRight
	}
	btnMap := [11]Button{
		ButtonSouth, ButtonEast, ButtonWest, ButtonNorth,
		ButtonL1, ButtonR1, ButtonSelect, ButtonStart,
		0, ButtonL3, ButtonR3,
	}
	for i, b := range btnMap {
		if b != 0 && r.buttons[i] {
			s.Buttons |= b
		}
	}
	if s.LeftTrigger > 0.5 {
		s.Buttons |= ButtonL2
	}
	if s.RightTrigger > 0.5 {
		s.Buttons |= ButtonR2
	}
	return s
}
