# SPZ fixtures

`gaussian-ring.v2.spz`, `.v3.spz`, and `.v4.spz` are the demos' CC0
`gaussian-ring.ply` (576 splats, degree-1 SH) written by Niantic's reference
encoder at each format version, with no axis conversion, so decoding them
must match decoding the PLY. Beside each, `gaussian-ring.vN.ref` is what the
reference decoder reads back from it, as little-endian float32s per splat:
position, log scales, rotation x y z w, opacity logit, DC color, then the SH
coefficients coefficient-major with RGB interleaved. `make-fixtures.py`
writes all six, with the `spz` Python package (pip install
git+https://github.com/nianticlabs/spz, commit affd0ec, version 1.1.0).
