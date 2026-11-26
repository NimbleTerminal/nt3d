// Copyright (c) 2026 Neomantra Corp.

package nt3dsog

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"

	"nimbleterminal.dev/nt3d/mesh"
)

// SOG is the format's name, as nt3dmesh.FormatOf reports it.
const SOG nt3dmesh.Format = "sog"

// zipMagic starts a zip's first local file header.
const zipMagic = "PK\x03\x04"

func init() { nt3dmesh.RegisterFormatSniff(SOG, []string{".sog"}, sniff, Decode) }

// sniffWindow is how much of an unbundled meta.json sniff looks through for
// its "means" and "quats" keys. A version 2 file puts the 256-entry scales
// codebook between them, so "quats" can begin past 5000 bytes in.
const sniffWindow = 64 << 10

// sniff recognizes a zip holding a top-level meta.json, or an unbundled
// meta.json: JSON whose opening names both "means" and "quats".
func sniff(data []byte) bool {
	if bytes.HasPrefix(data, []byte(zipMagic)) {
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return false
		}
		for _, f := range z.File {
			if f.Name == "meta.json" {
				return true
			}
		}
		return false
	}
	head := bytes.TrimLeft(data, " \t\r\n")
	if len(head) == 0 || head[0] != '{' {
		return false
	}
	head = head[:min(len(head), sniffWindow)]
	return bytes.Contains(head, []byte(`"means"`)) && bytes.Contains(head, []byte(`"quats"`))
}

// Decode reads a SOG bundle (a zip holding meta.json and its images), or
// an unbundled meta.json whose images it reads through o.Open. The files
// and decoded images together may hold no more than o.ByteLimit() bytes.
func Decode(data []byte, o *nt3dmesh.Options) (*nt3dmesh.Model, error) {
	b := o.ByteBudget()
	var open func(name string) ([]byte, error)
	if bytes.HasPrefix(data, []byte(zipMagic)) {
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, metaErr("bundle: %w", err)
		}
		open = func(name string) ([]byte, error) { return entry(z, name, b) }
		if data, err = open("meta.json"); err != nil {
			return nil, metaErr("bundle: %w", err)
		}
	} else {
		if o == nil || o.Open == nil {
			return nil, errors.New("nt3dmesh: SOG images must be opened: use ReadFile or Options.Open")
		}
		if err := b.Take(int64(len(data)), "meta.json"); err != nil {
			return nil, metaErr("%w", err)
		}
		open = func(name string) ([]byte, error) {
			data, err := o.Open(name)
			if err != nil {
				return nil, err
			}
			if err := b.Take(int64(len(data)), "file"); err != nil {
				return nil, err
			}
			return data, nil
		}
	}
	m, err := parseMeta(data)
	if err != nil {
		return nil, err
	}
	im, err := loadImages(m, open, b)
	if err != nil {
		return nil, err
	}
	mode := nt3dmesh.SHPaletteKeep
	if o != nil {
		mode = o.SHPalette
	}
	g, err := unpack(m, im, mode)
	if err != nil {
		return nil, err
	}
	model, err := nt3dmesh.GaussianModel(SOG, g, o)
	if err != nil {
		return nil, err
	}
	if o != nil && o.Up == nt3dmesh.YUp {
		model.OrientYUp()
	}
	return model, nil
}

// entry reads the zip entry named exactly name and charges its length to b.
// It refuses one that declares more than b has left.
func entry(z *zip.Reader, name string, b *nt3dmesh.ByteBudget) ([]byte, error) {
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		if f.UncompressedSize64 > uint64(b.Left()) {
			return nil, fmt.Errorf("%w: %s", nt3dmesh.ErrTooLarge, name)
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, b.Left()+1))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if err := b.Take(int64(len(data)), name); err != nil {
			return nil, err
		}
		return data, nil
	}
	return nil, fmt.Errorf("%s is not in the bundle", name)
}
