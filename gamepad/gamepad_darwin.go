// Copyright (c) 2026 Neomantra Corp.

//go:build darwin && !js

package nt3dgamepad

import (
	"sync"
	"unsafe"

	"github.com/go-webgpu/goffi/ffi"
	"github.com/go-webgpu/goffi/types"
)

type objcID uintptr
type objcSEL uintptr

type gcBridge struct {
	once sync.Once
	ok   bool
	mu   sync.Mutex

	objcGetClass    unsafe.Pointer
	selRegisterName unsafe.Pointer
	objcMsgSend     unsafe.Pointer

	cifPtr1  types.CallInterface
	cifMsg0  types.CallInterface
	cifMsgU  types.CallInterface
	cifFloat types.CallInterface
	cifBool  types.CallInterface

	clsGCController objcID
	selControllers  objcSEL
	selCount        objcSEL
	selObjectAtIdx  objcSEL
	selVendorName   objcSEL
	selUTF8String   objcSEL
	selExtGamepad   objcSEL
	selLeftStick    objcSEL
	selRightStick   objcSEL
	selDPad         objcSEL
	selXAxis        objcSEL
	selYAxis        objcSEL
	selUp           objcSEL
	selDown         objcSEL
	selLeft         objcSEL
	selRight        objcSEL
	selValue        objcSEL
	selIsPressed    objcSEL
	selButtonA      objcSEL
	selButtonB      objcSEL
	selButtonX      objcSEL
	selButtonY      objcSEL
	selLeftShoulder objcSEL
	selRightShould  objcSEL
	selLeftTrigger  objcSEL
	selRightTrigger objcSEL
	selButtonOpts   objcSEL
	selButtonMenu   objcSEL
	selLeftThumbBtn objcSEL
	selRightThmbBtn objcSEL
}

var bridge gcBridge

func (b *gcBridge) init() {
	b.once.Do(func() {
		libobjc, err := ffi.LoadLibrary("/usr/lib/libobjc.A.dylib")
		if err != nil {
			return
		}
		if _, err := ffi.LoadLibrary("/System/Library/Frameworks/Foundation.framework/Foundation"); err != nil {
			return
		}
		if _, err := ffi.LoadLibrary("/System/Library/Frameworks/GameController.framework/GameController"); err != nil {
			return
		}
		if b.objcGetClass, err = ffi.GetSymbol(libobjc, "objc_getClass"); err != nil {
			return
		}
		if b.selRegisterName, err = ffi.GetSymbol(libobjc, "sel_registerName"); err != nil {
			return
		}
		if b.objcMsgSend, err = ffi.GetSymbol(libobjc, "objc_msgSend"); err != nil {
			return
		}
		ptr := types.PointerTypeDescriptor
		if ffi.PrepareCallInterface(&b.cifPtr1, types.DefaultCall, ptr, []*types.TypeDescriptor{ptr}) != nil {
			return
		}
		if ffi.PrepareCallInterface(&b.cifMsg0, types.DefaultCall, ptr, []*types.TypeDescriptor{ptr, ptr}) != nil {
			return
		}
		if ffi.PrepareCallInterface(&b.cifMsgU, types.DefaultCall, ptr, []*types.TypeDescriptor{ptr, ptr, types.UInt64TypeDescriptor}) != nil {
			return
		}
		if ffi.PrepareCallInterface(&b.cifFloat, types.DefaultCall, types.FloatTypeDescriptor, []*types.TypeDescriptor{ptr, ptr}) != nil {
			return
		}
		if ffi.PrepareCallInterface(&b.cifBool, types.DefaultCall, types.UInt8TypeDescriptor, []*types.TypeDescriptor{ptr, ptr}) != nil {
			return
		}

		b.clsGCController = b.getClass("GCController")
		if b.clsGCController == 0 {
			return
		}
		b.selControllers = b.sel("controllers")
		b.selCount = b.sel("count")
		b.selObjectAtIdx = b.sel("objectAtIndex:")
		b.selVendorName = b.sel("vendorName")
		b.selUTF8String = b.sel("UTF8String")
		b.selExtGamepad = b.sel("extendedGamepad")
		b.selLeftStick = b.sel("leftThumbstick")
		b.selRightStick = b.sel("rightThumbstick")
		b.selDPad = b.sel("dpad")
		b.selXAxis = b.sel("xAxis")
		b.selYAxis = b.sel("yAxis")
		b.selUp = b.sel("up")
		b.selDown = b.sel("down")
		b.selLeft = b.sel("left")
		b.selRight = b.sel("right")
		b.selValue = b.sel("value")
		b.selIsPressed = b.sel("isPressed")
		b.selButtonA = b.sel("buttonA")
		b.selButtonB = b.sel("buttonB")
		b.selButtonX = b.sel("buttonX")
		b.selButtonY = b.sel("buttonY")
		b.selLeftShoulder = b.sel("leftShoulder")
		b.selRightShould = b.sel("rightShoulder")
		b.selLeftTrigger = b.sel("leftTrigger")
		b.selRightTrigger = b.sel("rightTrigger")
		b.selButtonOpts = b.sel("buttonOptions")
		b.selButtonMenu = b.sel("buttonMenu")
		b.selLeftThumbBtn = b.sel("leftThumbstickButton")
		b.selRightThmbBtn = b.sel("rightThumbstickButton")
		b.ok = true
	})
}

func (b *gcBridge) getClass(name string) objcID {
	buf := append([]byte(name), 0)
	p := unsafe.Pointer(&buf[0])
	var res uintptr
	_, _ = ffi.CallFunction(&b.cifPtr1, b.objcGetClass, unsafe.Pointer(&res), []unsafe.Pointer{unsafe.Pointer(&p)})
	return objcID(res)
}

func (b *gcBridge) sel(name string) objcSEL {
	buf := append([]byte(name), 0)
	p := unsafe.Pointer(&buf[0])
	var res uintptr
	_, _ = ffi.CallFunction(&b.cifPtr1, b.selRegisterName, unsafe.Pointer(&res), []unsafe.Pointer{unsafe.Pointer(&p)})
	return objcSEL(res)
}

func (b *gcBridge) send0(self objcID, cmd objcSEL) objcID {
	if self == 0 {
		return 0
	}
	var res uintptr
	_, _ = ffi.CallFunction(&b.cifMsg0, b.objcMsgSend, unsafe.Pointer(&res), []unsafe.Pointer{unsafe.Pointer(&self), unsafe.Pointer(&cmd)})
	return objcID(res)
}

func (b *gcBridge) sendIdx(self objcID, cmd objcSEL, idx uint64) objcID {
	if self == 0 {
		return 0
	}
	var res uintptr
	_, _ = ffi.CallFunction(&b.cifMsgU, b.objcMsgSend, unsafe.Pointer(&res), []unsafe.Pointer{unsafe.Pointer(&self), unsafe.Pointer(&cmd), unsafe.Pointer(&idx)})
	return objcID(res)
}

func (b *gcBridge) sendFloat(self objcID, cmd objcSEL) float64 {
	if self == 0 {
		return 0
	}
	var res float32
	_, _ = ffi.CallFunction(&b.cifFloat, b.objcMsgSend, unsafe.Pointer(&res), []unsafe.Pointer{unsafe.Pointer(&self), unsafe.Pointer(&cmd)})
	return float64(res)
}

func (b *gcBridge) sendBool(self objcID, cmd objcSEL) bool {
	if self == 0 {
		return false
	}
	var res uint8
	_, _ = ffi.CallFunction(&b.cifBool, b.objcMsgSend, unsafe.Pointer(&res), []unsafe.Pointer{unsafe.Pointer(&self), unsafe.Pointer(&cmd)})
	return res != 0
}

func cString(ptr unsafe.Pointer) string {
	if ptr == nil {
		return ""
	}
	var out []byte
	for i := range 128 {
		c := *(*byte)(unsafe.Add(ptr, i))
		if c == 0 {
			break
		}
		out = append(out, c)
	}
	return string(out)
}

type darwinReader struct{}

func openPlatform() Reader { return darwinReader{} }

func (darwinReader) Close() error { return nil }

func (darwinReader) Poll() State {
	bridge.init()
	if !bridge.ok {
		return State{}
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()

	arr := bridge.send0(bridge.clsGCController, bridge.selControllers)
	if arr == 0 {
		return State{}
	}
	count := uint64(bridge.send0(arr, bridge.selCount))
	for i := range count {
		ctrl := bridge.sendIdx(arr, bridge.selObjectAtIdx, i)
		gp := bridge.send0(ctrl, bridge.selExtGamepad)
		if gp == 0 {
			continue
		}
		var s State
		s.Connected = true
		if ns := bridge.send0(ctrl, bridge.selVendorName); ns != 0 {
			var utf8 unsafe.Pointer
			cmd := bridge.selUTF8String
			_, _ = ffi.CallFunction(&bridge.cifMsg0, bridge.objcMsgSend, unsafe.Pointer(&utf8), []unsafe.Pointer{unsafe.Pointer(&ns), unsafe.Pointer(&cmd)})
			s.Name = cString(utf8)
		}
		ls := bridge.send0(gp, bridge.selLeftStick)
		rs := bridge.send0(gp, bridge.selRightStick)
		s.LeftX = clamp(bridge.sendFloat(bridge.send0(ls, bridge.selXAxis), bridge.selValue), -1, 1)
		s.LeftY = clamp(bridge.sendFloat(bridge.send0(ls, bridge.selYAxis), bridge.selValue), -1, 1)
		s.RightX = clamp(bridge.sendFloat(bridge.send0(rs, bridge.selXAxis), bridge.selValue), -1, 1)
		s.RightY = clamp(bridge.sendFloat(bridge.send0(rs, bridge.selYAxis), bridge.selValue), -1, 1)

		lt := bridge.send0(gp, bridge.selLeftTrigger)
		rt := bridge.send0(gp, bridge.selRightTrigger)
		s.LeftTrigger = clamp(bridge.sendFloat(lt, bridge.selValue), 0, 1)
		s.RightTrigger = clamp(bridge.sendFloat(rt, bridge.selValue), 0, 1)

		dpad := bridge.send0(gp, bridge.selDPad)
		btns := []struct {
			in  objcID
			bit Button
		}{
			{bridge.send0(gp, bridge.selButtonA), ButtonSouth},
			{bridge.send0(gp, bridge.selButtonB), ButtonEast},
			{bridge.send0(gp, bridge.selButtonX), ButtonWest},
			{bridge.send0(gp, bridge.selButtonY), ButtonNorth},
			{bridge.send0(gp, bridge.selLeftShoulder), ButtonL1},
			{bridge.send0(gp, bridge.selRightShould), ButtonR1},
			{lt, ButtonL2},
			{rt, ButtonR2},
			{bridge.send0(gp, bridge.selButtonOpts), ButtonSelect},
			{bridge.send0(gp, bridge.selButtonMenu), ButtonStart},
			{bridge.send0(gp, bridge.selLeftThumbBtn), ButtonL3},
			{bridge.send0(gp, bridge.selRightThmbBtn), ButtonR3},
			{bridge.send0(dpad, bridge.selUp), ButtonDPadUp},
			{bridge.send0(dpad, bridge.selDown), ButtonDPadDown},
			{bridge.send0(dpad, bridge.selLeft), ButtonDPadLeft},
			{bridge.send0(dpad, bridge.selRight), ButtonDPadRight},
		}
		for _, b := range btns {
			if bridge.sendBool(b.in, bridge.selIsPressed) {
				s.Buttons |= b.bit
			}
		}
		return s
	}
	return State{}
}
