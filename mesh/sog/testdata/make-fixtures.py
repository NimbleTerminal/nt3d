# Writes the SOG and compressed-PLY fixtures from the demos' CC0
# gaussian-ring.ply with PlayCanvas's reference converter,
# @playcanvas/splat-transform 3.10.3 (run through npx, so Node must be installed):
#
#   uv run --with pillow --with numpy mesh/sog/testdata/make-fixtures.py \
#       examples/demos/internal/demo/models/gaussian-ring.ply mesh/sog/testdata mesh/testdata
#
# Step 1: splat-transform writes gaussian-ring.sog (SOG v2) and
# gaussian-ring.compressed.ply from the ring, then gaussian-ring.sog.ref.ply,
# its own decode of that bundle back to uncompressed PLY. It reads binary PLY,
# so the ASCII ring is rewritten first.
# Step 2: splat-transform no longer writes SOG v1, so gaussian-ring.v1.sog is
# built from the v2 bundle: means and quats are kept byte for byte; scales, sh0
# and shN trade their codebooks for linear ranges (mins/maxs) with the values
# re-quantized to a byte over them, and sh0's opacity becomes a logit range.
import io, json, math, os, shutil, struct, subprocess, sys, tempfile, zipfile
import numpy as np
from PIL import Image

ring, sog_dir, ply_dir = sys.argv[1:4]
TOOL = ["npx", "-y", "@playcanvas/splat-transform@3.10.3"]

lines = open(ring).read().split("\n")
end = lines.index("end_header")
props = [l.split()[2] for l in lines[:end] if l.startswith("property")]
count = int([l for l in lines[:end] if l.startswith("element vertex")][0].split()[2])
header = "ply\nformat binary_little_endian 1.0\nelement vertex %d\n%s\nend_header\n" % (count, "\n".join("property float " + p for p in props))
body = b"".join(struct.pack("<%df" % len(props), *map(float, l.split())) for l in lines[end+1:end+1+count])
work = tempfile.mkdtemp()
binary = os.path.join(work, "ring.ply")
open(binary, "wb").write(header.encode() + body)

def convert(src, dst):
    subprocess.run(TOOL + [src, dst], check=True, cwd=work)
    assert os.path.exists(dst), dst

sog = os.path.join(work, "gaussian-ring.sog")
ref = os.path.join(work, "gaussian-ring.sog.ref.ply")
compressed = os.path.join(work, "gaussian-ring.compressed.ply")
convert(binary, sog)
convert(sog, ref)
convert(binary, compressed)
os.makedirs(sog_dir, exist_ok=True)
os.makedirs(ply_dir, exist_ok=True)
shutil.copy(sog, sog_dir)
shutil.copy(ref, sog_dir)
shutil.copy(compressed, ply_dir)

# Build the v1 bundle from the v2 one.
with zipfile.ZipFile(sog) as z:
    names = z.namelist()
    files = {n: z.read(n) for n in names}
meta = json.loads(files["meta.json"])
assert meta["version"] == 2 and meta["count"] == count and meta["shN"]["bands"] == 1, meta

def pixels(name):
    return np.asarray(Image.open(io.BytesIO(files[name])).convert("RGBA"))

def webp(array):
    out = io.BytesIO()
    # exact keeps the color of fully transparent pixels, which are data here.
    Image.fromarray(array).save(out, "WEBP", lossless=True, exact=True)
    return out.getvalue()

def to_bytes(values, lo, hi):
    span = hi - lo if hi > lo else 1.0
    return np.clip(np.floor(255 * (values - lo) / span + .5), 0, 255).astype(np.uint8)

out = dict(files)
v1 = {"means": {"shape": [count, 3], "mins": meta["means"]["mins"], "maxs": meta["means"]["maxs"], "files": meta["means"]["files"]},
      "quats": {"files": meta["quats"]["files"]}}

# scales: one codebook index per channel per splat, in the first `count` pixels.
img = pixels("scales.webp").copy()
values = np.asarray(meta["scales"]["codebook"], np.float64)[img[..., :3]]
used = values.reshape(-1, 3)[:count]
lo, hi = used.min(axis=0), used.max(axis=0)
img[..., :3] = np.stack([to_bytes(values[..., c], lo[c], hi[c]) for c in range(3)], axis=-1)
out["scales.webp"] = webp(img)
v1["scales"] = {"mins": lo.tolist(), "maxs": hi.tolist(), "files": meta["scales"]["files"]}

# sh0: codebook indices for R, G, B; A is the opacity byte, which becomes a logit range.
img = pixels("sh0.webp").copy()
values = np.asarray(meta["sh0"]["codebook"], np.float64)[img[..., :3]]
used = values.reshape(-1, 3)[:count]
lo, hi = used.min(axis=0), used.max(axis=0)
alpha = np.clip(img[..., 3] / 255.0, 1e-6, 1 - 1e-6)
logit = np.log(alpha / (1 - alpha))
alo, ahi = logit.reshape(-1)[:count].min(), logit.reshape(-1)[:count].max()
img[..., :3] = np.stack([to_bytes(values[..., c], lo[c], hi[c]) for c in range(3)], axis=-1)
img[..., 3] = to_bytes(logit, alo, ahi)
out["sh0.webp"] = webp(img)
v1["sh0"] = {"mins": lo.tolist() + [float(alo)], "maxs": hi.tolist() + [float(ahi)], "files": meta["sh0"]["files"]}

# shN: the centroid image holds 64 palette entries per row, `coefficients`
# pixels each; one scalar range covers every value of every used entry.
shn = meta["shN"]
coefficients = {1: 3, 2: 8, 3: 15}[shn["bands"]]
img = pixels("shN_centroids.webp").copy()
values = np.asarray(shn["codebook"], np.float64)[img[..., :3]]
entry = np.arange(img.shape[0])[:, None] * 64 + np.arange(img.shape[1])[None, :] // coefficients
used = values[entry < shn["count"]]
lo, hi = float(used.min()), float(used.max())
img[..., :3] = to_bytes(values, lo, hi)
out["shN_centroids.webp"] = webp(img)
v1["shN"] = {"count": shn["count"], "bands": shn["bands"], "mins": lo, "maxs": hi, "files": shn["files"]}

out["meta.json"] = json.dumps(v1, indent=2).encode()
with zipfile.ZipFile(os.path.join(sog_dir, "gaussian-ring.v1.sog"), "w", zipfile.ZIP_STORED) as z:
    for n in names:
        z.writestr(n, out[n])
print("wrote", count, "splats;", shn["count"], "SH palette entries")
shutil.rmtree(work)
