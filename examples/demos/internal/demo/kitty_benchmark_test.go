package demo

import (
	"image"
	"image/color"
	"testing"
)

func TestKittyBenchmarkPairedUpdates(t *testing.T) {
	off, err := newKittyBenchDriver(false, 160, 160)
	if err != nil {
		t.Fatal(err)
	}
	defer off.close()
	on, err := newKittyBenchDriver(true, 160, 160)
	if err != nil {
		t.Fatal(err)
	}
	defer on.close()
	first := image.NewNRGBA(image.Rect(0, 0, 160, 160))
	for y := 0; y < 160; y++ {
		for x := 0; x < 160; x++ {
			first.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), A: 255})
		}
	}
	for _, d := range []*kittyBenchDriver{off, on} {
		s, e := d.step(first)
		if e != nil || s.FullUploads != 1 || s.Pixels != 25600 {
			t.Fatalf("initial upload: %+v %v", s, e)
		}
	}
	second := image.NewNRGBA(first.Bounds())
	copy(second.Pix, first.Pix)
	second.SetNRGBA(40, 40, color.NRGBA{B: 255, A: 255})
	full, e := off.step(second)
	if e != nil {
		t.Fatal(e)
	}
	edit, e := on.step(second)
	if e != nil {
		t.Fatal(e)
	}
	if full.FullUploads != 1 || full.FrameEdits != 0 || full.Pixels != 25600 {
		t.Fatalf("full: %+v", full)
	}
	if edit.FullUploads != 0 || edit.FrameEdits != 1 || edit.Pixels != 1 || edit.Bytes >= full.Bytes {
		t.Fatalf("edit: %+v full: %+v", edit, full)
	}
	same, e := on.step(second)
	if e != nil || same.Bytes != 0 || same.Pixels != 0 {
		t.Fatalf("unchanged: %+v %v", same, e)
	}
}

func TestKittyBenchmarkCountsChunksOnce(t *testing.T) {
	var sample kittyBenchSample
	countKittyUploads("\x1b[?2026h\x1b_Ga=f,s=10,v=20,m=1;AAAA\x1b\\\x1b_Ga=f,m=0;AAAA\x1b\\\x1b[?2026l", &sample)
	if sample.FrameEdits != 1 || sample.Pixels != 200 {
		t.Fatalf("%+v", sample)
	}
}
