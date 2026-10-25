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

// Decode reads a SOG bundle (a zip holding meta.json and its images), or
// an unbundled meta.json whose images it reads through o.Open. The files
// and decoded images together may hold no more than o.ByteLimit() bytes.
func Decode(data []byte, o *nt3dmesh.Options) (*nt3dmesh.Model, error) {
	budget := o.ByteLimit()
	var open func(name string) ([]byte, error)
	if bytes.HasPrefix(data, []byte(zipMagic)) {
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, metaErr("bundle: %w", err)
		}
		open = func(name string) ([]byte, error) { return entry(z, name, budget) }
		if data, err = open("meta.json"); err != nil {
			return nil, metaErr("bundle: %w", err)
		}
	} else {
		if o == nil || o.Open == nil {
			return nil, errors.New("nt3dmesh: SOG images must be opened: use ReadFile or Options.Open")
		}
		open = o.Open
	}
	m, err := parseMeta(data)
	if err != nil {
		return nil, err
	}
	im, err := loadImages(m, open, budget)
	if err != nil {
		return nil, err
	}
	g, err := unpack(m, im)
	if err != nil {
		return nil, err
	}
	model := &nt3dmesh.Model{Format: SOG, Objects: nt3dmesh.PointPieces(nt3dmesh.Object{Geometry: g})}
	for _, piece := range model.Objects {
		if err := piece.Geometry.Validate(); err != nil {
			return nil, err
		}
	}
	if o != nil && o.Up == nt3dmesh.YUp {
		model.OrientYUp()
	}
	return model, nil
}

// entry reads the zip entry named exactly name, no more than limit bytes.
func entry(z *zip.Reader, name string, limit int64) ([]byte, error) {
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		if f.UncompressedSize64 > uint64(limit) {
			return nil, fmt.Errorf("%s: %w", name, nt3dmesh.ErrTooLarge)
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, limit+1))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if int64(len(data)) > limit {
			return nil, fmt.Errorf("%s: %w", name, nt3dmesh.ErrTooLarge)
		}
		return data, nil
	}
	return nil, fmt.Errorf("%s is not in the bundle", name)
}
