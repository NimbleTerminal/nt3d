// Copyright (c) 2026 Neomantra Corp.
package nt3dspz

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// tinySPZ is a one-splat, degree-0 SPZ of version 2 or 3, gzip-compressed
// as those versions are: a 16-byte header, then the packed arrays.
func tinySPZ(f *testing.F, version uint32) []byte {
	raw := make([]byte, 16)
	binary.LittleEndian.PutUint32(raw, magic)
	binary.LittleEndian.PutUint32(raw[4:], version)
	binary.LittleEndian.PutUint32(raw[8:], 1)    // one splat
	raw[13] = 12                                 // fractional bits
	raw = append(raw, 0, 0, 0, 0, 0, 0, 0, 0, 0) // position
	raw = append(raw, 200)                       // alpha
	raw = append(raw, 128, 64, 32)               // color
	raw = append(raw, 100, 100, 100)             // scales
	raw = append(raw, 128, 128, 128)             // rotation
	if version >= 3 {
		raw = append(raw, 255)
	}
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		f.Fatal(err)
	}
	if err := w.Close(); err != nil {
		f.Fatal(err)
	}
	return buf.Bytes()
}

// tinySPZ4 is a one-splat, degree-0 SPZ in the version 4 container: a
// 32-byte header naming a stream table, the table, then a zstd frame a stream.
func tinySPZ4(f *testing.F) []byte {
	data := make([]byte, 32)
	binary.LittleEndian.PutUint32(data, magic)
	binary.LittleEndian.PutUint32(data[4:], 4)
	binary.LittleEndian.PutUint32(data[8:], 1) // one splat
	data[13] = 12                              // fractional bits
	data[15] = 5                               // streams: all but SH
	binary.LittleEndian.PutUint32(data[16:], 32)
	streams := [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 0, 0}, // position
		{200},                       // alpha
		{128, 64, 32},               // color
		{100, 100, 100},             // scales
		{128, 128, 128, 255},        // rotation
	}
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		f.Fatal(err)
	}
	defer enc.Close()
	var frames []byte
	for _, raw := range streams {
		packed := enc.EncodeAll(raw, nil)
		var entry [16]byte
		binary.LittleEndian.PutUint64(entry[:], uint64(len(packed)))
		binary.LittleEndian.PutUint64(entry[8:], uint64(len(raw)))
		data = append(data, entry[:]...)
		frames = append(frames, packed...)
	}
	return append(data, frames...)
}

// FuzzDecode feeds SPZ files to the decoder: a gzip stream or raw streams
// holding a header and six packed attribute arrays, in versions 2 to 4.
// Seeds stay few and small, as the fuzzer re-minimizes each one every round.
func FuzzDecode(f *testing.F) {
	o := fuzzOptions()
	for _, version := range []uint32{2, 3} {
		seed := tinySPZ(f, version)
		if _, err := Decode(seed, o); err != nil {
			f.Fatalf("v%d seed does not decode: %v", version, err)
		}
		f.Add(seed)
	}
	v4 := tinySPZ4(f)
	if _, err := Decode(v4, o); err != nil {
		f.Fatalf("v4 seed does not decode: %v", err)
	}
	f.Add(v4)
	f.Add([]byte("NGSP\x04\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Decode(data, o)
		checkModel(t, m, err, o)
	})
}
