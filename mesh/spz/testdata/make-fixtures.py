# Writes gaussian-ring.v{2,3,4}.spz from the demos' CC0 gaussian-ring.ply with
# Niantic's reference encoder (pip install git+https://github.com/nianticlabs/spz,
# commit affd0ec, spz 1.1.0), storing the PLY's coordinates as they are
# (from_coord UNSPECIFIED), so decoding them must match decoding the PLY.
# The reference loader reads binary PLY, so the ASCII ring is rewritten first.
import spz, struct, sys, tempfile, os
ring, out = sys.argv[1], sys.argv[2]
lines = open(ring).read().split("\n")
end = lines.index("end_header")
props = [l.split()[2] for l in lines[:end] if l.startswith("property")]
count = int([l for l in lines[:end] if l.startswith("element vertex")][0].split()[2])
header = "ply\nformat binary_little_endian 1.0\nelement vertex %d\n%s\nend_header\n" % (count, "\n".join("property float " + p for p in props))
body = b"".join(struct.pack("<%df" % len(props), *map(float, l.split())) for l in lines[end+1:end+1+count])
binary = os.path.join(tempfile.mkdtemp(), "ring.ply")
open(binary, "wb").write(header.encode() + body)
g = spz.load_splat_from_ply(binary, spz.UnpackOptions())
assert g.num_points == count, g.num_points
for v in (2, 3, 4):
    o = spz.PackOptions(); o.version = v; o.from_coord = spz.UNSPECIFIED
    assert spz.save_spz(g, o, os.path.join(out, "gaussian-ring.v%d.spz" % v))
print("wrote", count, "splats, sh degree", g.sh_degree)

# Beside each fixture, what the reference decoder reads back from it:
# gaussian-ring.v{N}.ref holds, per splat, little-endian float32s:
# position xyz, log scales xyz, rotation xyzw, opacity logit, DC rgb, then
# the SH coefficients as the reference lays them out (coefficient-major,
# RGB interleaved). A decoder must agree with these to float precision.
import numpy as np
for v in (2, 3, 4):
    c = spz.load_spz(os.path.join(out, "gaussian-ring.v%d.spz" % v), spz.UnpackOptions())
    n = c.num_points
    rows = np.concatenate([np.asarray(c.positions, np.float32).reshape(n, 3), np.asarray(c.scales, np.float32).reshape(n, 3),
        np.asarray(c.rotations, np.float32).reshape(n, 4), np.asarray(c.alphas, np.float32).reshape(n, 1),
        np.asarray(c.colors, np.float32).reshape(n, 3), np.asarray(c.sh, np.float32).reshape(n, -1)], axis=1)
    rows.astype("<f4").tofile(os.path.join(out, "gaussian-ring.v%d.ref" % v))
    print("v%d reference: %d splats x %d floats; first: %s" % (v, n, rows.shape[1], np.array2string(rows[0], precision=4, max_line_width=200)))
