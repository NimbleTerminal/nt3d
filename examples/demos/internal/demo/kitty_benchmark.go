package demo

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/ntpicture"
)

// This driver measures component preparation, encoding and protocol bytes. It
// deliberately does not run a terminal or pretend completion means display.
type kittyBenchDriver struct {
	picture *ntpicture.Picture
	session *ntpicture.TerminalSession
}

type kittyBenchSample struct {
	Bytes        int     `json:"protocol_bytes"`
	Pixels       int     `json:"uploaded_pixels"`
	FullUploads  int     `json:"full_uploads"`
	FrameEdits   int     `json:"frame_edits"`
	Milliseconds float64 `json:"prepare_encode_ms"`
}

type kittyBenchPair struct {
	Frame    int              `json:"frame"`
	RenderMS float64          `json:"render_ms"`
	Off      kittyBenchSample `json:"off"`
	On       kittyBenchSample `json:"on"`
}

type kittyBenchSummary struct {
	MeanBytes   float64 `json:"mean_protocol_bytes"`
	MeanPixels  float64 `json:"mean_uploaded_pixels"`
	MeanMS      float64 `json:"mean_prepare_encode_ms"`
	P50MS       float64 `json:"p50_prepare_encode_ms"`
	P95MS       float64 `json:"p95_prepare_encode_ms"`
	FullUploads int     `json:"full_uploads"`
	FrameEdits  int     `json:"frame_edits"`
}

func newKittyBenchDriver(edits bool, w, h int) (*kittyBenchDriver, error) {
	cap := ntpicture.KittyCapabilityUnsupported
	if edits {
		cap = ntpicture.KittyCapabilitySupported
	}
	tmux := false
	session := ntpicture.NewTerminalSession(ntpicture.TerminalOptions{
		Kitty: ntpicture.KittyCapabilitySupported, SharedMemory: ntpicture.KittyCapabilityUnsupported,
		FrameEdits: cap, FrameAppend: ntpicture.KittyCapabilityUnsupported, FrameSelect: ntpicture.KittyCapabilityUnsupported,
		VirtualPlacement: ntpicture.KittyCapabilitySupported, CellPixelWidth: 8, CellPixelHeight: 16, TmuxPassthrough: &tmux,
	})
	p := ntpicture.New(ntpicture.Options{Session: session, StableID: true, HostGeometry: true, RetireDelay: -1,
		Render: ntpicture.Config{Fit: ntpicture.FitFill, KittyFormat: ntpicture.KittyFormatZlib, KittyMedium: ntpicture.KittyMediumDirect, CellPixelWidth: 8, CellPixelHeight: 16}})
	d := &kittyBenchDriver{p, session}
	var ignored kittyBenchSample
	if err := d.drive(p.SetSize(w/8, h/16), &ignored); err != nil {
		d.close()
		return nil, err
	}
	if err := d.drive(p.SetMode(ntpicture.PictureKitty), &ignored); err != nil {
		d.close()
		return nil, err
	}
	return d, nil
}

func (d *kittyBenchDriver) close() {
	var ignored kittyBenchSample
	_ = d.drive(d.picture.Close(), &ignored)
	d.session.Close()
}

func (d *kittyBenchDriver) drive(cmd tea.Cmd, sample *kittyBenchSample) error {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	// Bubble Tea represents BatchMsg and its private sequence as []tea.Cmd.
	value := reflect.ValueOf(msg)
	if value.Kind() == reflect.Slice {
		for i := 0; i < value.Len(); i++ {
			child, ok := value.Index(i).Interface().(tea.Cmd)
			if !ok {
				return fmt.Errorf("unexpected command message %T", msg)
			}
			if err := d.drive(child, sample); err != nil {
				return err
			}
		}
		return nil
	}
	switch m := msg.(type) {
	case tea.RawMsg:
		raw, ok := m.Msg.(string)
		if !ok {
			return fmt.Errorf("unexpected raw payload %T", m.Msg)
		}
		sample.Bytes += len(raw)
		countKittyUploads(raw, sample)
	case ntpicture.ErrorMsg:
		return m.Err
	}
	return d.drive(d.picture.Update(msg), sample)
}

func countKittyUploads(raw string, sample *kittyBenchSample) {
	for _, chunk := range strings.Split(raw, "\x1b_G")[1:] {
		header, _, ok := strings.Cut(chunk, ";")
		if !ok {
			continue
		}
		var action string
		var w, h int
		for _, field := range strings.Split(header, ",") {
			key, value, _ := strings.Cut(field, "=")
			switch key {
			case "a":
				action = value
			case "s":
				w, _ = strconv.Atoi(value)
			case "v":
				h, _ = strconv.Atoi(value)
			}
		}
		// Only initial upload chunks carry dimensions; continuations aren't frames.
		if w <= 0 || h <= 0 {
			continue
		}
		switch action {
		case "T":
			sample.FullUploads++
		case "f":
			sample.FrameEdits++
		default:
			continue
		}
		sample.Pixels += w * h
	}
}

func (d *kittyBenchDriver) step(img image.Image) (kittyBenchSample, error) {
	var sample kittyBenchSample
	start := time.Now()
	err := d.drive(d.picture.SetImageImmutable(img), &sample)
	sample.Milliseconds = float64(time.Since(start)) / float64(time.Millisecond)
	return sample, err
}

func summarizeKitty(samples []kittyBenchPair, edits bool) kittyBenchSummary {
	var result kittyBenchSummary
	times := make([]float64, 0, len(samples))
	for _, pair := range samples {
		s := pair.Off
		if edits {
			s = pair.On
		}
		result.MeanBytes += float64(s.Bytes)
		result.MeanPixels += float64(s.Pixels)
		result.MeanMS += s.Milliseconds
		result.FullUploads += s.FullUploads
		result.FrameEdits += s.FrameEdits
		times = append(times, s.Milliseconds)
	}
	if len(times) == 0 {
		return result
	}
	n := float64(len(times))
	result.MeanBytes /= n
	result.MeanPixels /= n
	result.MeanMS /= n
	slices.Sort(times)
	result.P50MS = times[int(math.Ceil(n*.5))-1]
	result.P95MS = times[int(math.Ceil(n*.95))-1]
	return result
}

func (d *demo) benchmarkKitty(path string, frames int, cameraScale float64) error {
	if frames < 1 || frames > 3600 || cameraScale <= 0 || math.IsNaN(cameraScale) || math.IsInf(cameraScale, 0) {
		return fmt.Errorf("benchmark requires 1..3600 frames and a finite positive camera scale")
	}
	if d.animation == nil && d.water == nil {
		return fmt.Errorf("Kitty benchmark currently supports animation and water demos")
	}
	const width, height, warmup = 960, 640, 10
	camera := d.view.Camera()
	camera.Distance *= cameraScale
	camera.AutoRotate = false
	d.view.SetCamera(camera)
	off, err := newKittyBenchDriver(false, width, height)
	if err != nil {
		return err
	}
	defer off.close()
	on, err := newKittyBenchDriver(true, width, height)
	if err != nil {
		return err
	}
	defer on.close()
	samples := make([]kittyBenchPair, 0, frames)
	backend := ""
	for i := 0; i < frames+warmup; i++ {
		d.animationTime = 1 + float64(i)/30
		d.phase = math.Mod(float64(i)/30, 2*math.Pi)
		if d.animation != nil {
			err = d.animation.Sample(d.animationTime)
		} else {
			err = d.stepWater()
		}
		if err != nil {
			return err
		}
		start := time.Now()
		img, used, err := d.view.Snapshot(width, height)
		renderMS := float64(time.Since(start)) / float64(time.Millisecond)
		if err != nil {
			return err
		}
		if backend != "" && backend != used.String() {
			return fmt.Errorf("renderer changed during benchmark")
		}
		backend = used.String()
		pair := kittyBenchPair{Frame: i - warmup, RenderMS: renderMS}
		// Alternate order to reduce systematic cache/thermal bias. Both paths receive
		// the identical immutable image, and retain independent previous frames.
		if i%2 == 0 {
			pair.Off, err = off.step(img)
			if err == nil {
				pair.On, err = on.step(img)
			}
		} else {
			pair.On, err = on.step(img)
			if err == nil {
				pair.Off, err = off.step(img)
			}
		}
		if err != nil {
			return err
		}
		if i >= warmup {
			samples = append(samples, pair)
		}
	}
	scene := "animation"
	if d.water != nil {
		scene = "water"
	}
	report := struct {
		Schema      int               `json:"schema"`
		Scope       string            `json:"scope"`
		Scene       string            `json:"scene"`
		Backend     string            `json:"backend"`
		Platform    string            `json:"platform"`
		GoVersion   string            `json:"go_version"`
		Width       int               `json:"width"`
		Height      int               `json:"height"`
		CameraScale float64           `json:"camera_scale"`
		Warmup      int               `json:"warmup_frames"`
		StepSeconds float64           `json:"simulation_step_seconds"`
		Off         kittyBenchSummary `json:"off"`
		On          kittyBenchSummary `json:"on"`
		Samples     []kittyBenchPair  `json:"samples"`
	}{1, "Headless paired component benchmark: zlib/direct, stable ID, identical rendered frames, alternating order. Includes image preparation, diffing, encoding, command execution and byte counting. Excludes terminal IO, Bubble Tea rendering/scheduling and terminal painting. Render time is measured separately.", scene, backend, runtime.GOOS + "/" + runtime.GOARCH, runtime.Version(), width, height, cameraScale, warmup, 1.0 / 30, summarizeKitty(samples, false), summarizeKitty(samples, true), samples}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		return err
	}
	fmt.Printf("%s %dx%d (%s), %d paired frames: off %.0f bytes/frame %.3f ms; on %.0f bytes/frame %.3f ms. Report: %s\n", scene, width, height, backend, frames, report.Off.MeanBytes, report.Off.MeanMS, report.On.MeanBytes, report.On.MeanMS, path)
	return nil
}
