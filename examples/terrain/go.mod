module nimbleterminal.dev/nt3d/examples/terrain

go 1.26.8

require (
	charm.land/bubbles/v2 v2.2.1
	charm.land/bubbletea/v2 v2.1.0
	github.com/NimbleMarkets/go-booba v0.9.2
	github.com/lrstanley/bubblezone/v2 v2.0.0
	github.com/spf13/pflag v1.0.10
	nimbleterminal.dev/nt3d v0.0.0
	nimbleterminal.dev/ntpicture v0.0.0-20261014022756-acfa6954d2d0
)

require (
	charm.land/lipgloss/v2 v2.0.6 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20261008173134-6b8d4baf91b4 // indirect
	github.com/charmbracelet/x/ansi v0.11.9 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/go-webgpu/goffi v0.6.3 // indirect
	github.com/go-webgpu/webgpu v0.5.5 // indirect
	github.com/gogpu/gpucontext v0.31.3 // indirect
	github.com/gogpu/gputypes v0.8.0 // indirect
	github.com/gogpu/naga v0.19.0 // indirect
	github.com/gogpu/wgpu v0.34.5 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/qmuntal/gltf v0.29.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/xo/terminfo v1.2.0 // indirect
	golang.org/x/image v0.47.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	nimbleterminal.dev/ntcanvas v0.1.0 // indirect
	nimbleterminal.dev/ntgpuimage v0.2.0 // indirect
)

replace nimbleterminal.dev/nt3d => ../..

replace charm.land/bubbletea/v2 => github.com/neomantra/bubbletea/v2 v2.0.0-20261009201241-c397c32a5074

tool github.com/NimbleMarkets/go-booba/cmd/booba-assets
