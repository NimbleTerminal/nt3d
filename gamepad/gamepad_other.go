// Copyright (c) 2026 Neomantra Corp.

//go:build !js && !darwin && !linux

package nt3dgamepad

type noopReader struct{}

func openPlatform() Reader { return noopReader{} }

func (noopReader) Poll() State  { return State{} }
func (noopReader) Close() error { return nil }
