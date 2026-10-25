# SOG and compressed PLY fixtures

All four are the demos' CC0 `gaussian-ring.ply` (576 splats, degree-1 SH)
written by PlayCanvas's reference converter, `@playcanvas/splat-transform`
3.10.3 (through `npx`; Node, `uv`, Pillow, and numpy are needed to regenerate):

- `gaussian-ring.sog`: SOG v2 bundle (`splat-transform ring.ply gaussian-ring.sog`).
- `gaussian-ring.sog.ref.ply`: `splat-transform`'s own decode of that bundle
  back to uncompressed PLY. Decoding `gaussian-ring.sog` must match it tightly.
- `gaussian-ring.v1.sog`: the same content in SOG v1 layout (no `version`,
  linear `mins`/`maxs` instead of codebooks, opacity as a logit range). The
  converter no longer writes v1, so `make-fixtures.py` rebuilds it from the v2
  bundle, re-quantizing scales, SH0, and SH-N to bytes over their ranges.
- `../../testdata/gaussian-ring.compressed.ply`: the ring through the
  converter's compressed PLY writer (`splat-transform ring.ply
  gaussian-ring.compressed.ply`), used by the `mesh` package.

Decoding the SOG fixtures must also match the source `gaussian-ring.ply`
within quantization (the converter reorders the splats).

Regenerate from the repository root:

    uv run --with pillow --with numpy mesh/sog/testdata/make-fixtures.py \
        examples/demos/internal/demo/models/gaussian-ring.ply mesh/sog/testdata mesh/testdata
