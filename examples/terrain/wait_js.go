//go:build js && wasm

package main

import "syscall/js"

// holdRuntime keeps WASM alive because booba polls callbacks after the UI exits.
func holdRuntime() {
	// Replace released callbacks with a live no-op until the page closes.
	noop := js.FuncOf(func(js.Value, []js.Value) any { return "" })
	for _, name := range []string{"bubbletea_read", "bubbletea_write", "bubbletea_resize"} {
		js.Global().Set(name, noop)
	}
	select {}
}
