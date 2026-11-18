module nimbleterminal.dev/nt3d/examples/gogpu

go 1.26.8

require (
	github.com/gogpu/gogpu v0.54.0
	github.com/gogpu/gpucontext v0.32.0
	nimbleterminal.dev/nt3d v0.11.1
	nimbleterminal.dev/nt3d/examples/demos v0.0.0
)

require (
	charm.land/bubbles/v2 v2.2.1 // indirect
	charm.land/bubbletea/v2 v2.1.1 // indirect
	charm.land/lipgloss/v2 v2.1.1 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20261023060817-a45b7c86333f // indirect
	github.com/charmbracelet/x/ansi v0.11.9 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/go-webgpu/goffi v0.6.8 // indirect
	github.com/go-webgpu/webgpu v0.5.5 // indirect
	github.com/gogpu/gputypes v0.8.2 // indirect
	github.com/gogpu/naga v0.20.4 // indirect
	github.com/gogpu/wgpu v0.34.5 // indirect
	github.com/klauspost/compress v1.20.4 // indirect
	github.com/lrstanley/bubblezone/v2 v2.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/qmuntal/gltf v0.29.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/spf13/pflag v1.1.0 // indirect
	github.com/xo/terminfo v1.3.1 // indirect
	golang.org/x/image v0.48.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	nimbleterminal.dev/booba v0.13.0 // indirect
	nimbleterminal.dev/ntcanvas v0.2.0 // indirect
	nimbleterminal.dev/ntgpuimage v0.6.0 // indirect
	nimbleterminal.dev/ntpicture v0.2.0 // indirect
)

replace nimbleterminal.dev/nt3d => ../..

replace nimbleterminal.dev/nt3d/examples/demos => ../demos

replace charm.land/bubbletea/v2 => github.com/neomantra/bubbletea/v2 v2.0.0-20261020122444-536722b0bdb6
