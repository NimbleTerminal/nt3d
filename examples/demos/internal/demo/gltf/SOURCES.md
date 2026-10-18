# Khronos animation samples

Unmodified files from [KhronosGroup/glTF-Sample-Assets](https://github.com/KhronosGroup/glTF-Sample-Assets), pinned to commit
[`edc7c9e67c639d230715049ee31f9a96a6babbbe`](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/edc7c9e67c639d230715049ee31f9a96a6babbbe/Models).

- [AnimatedMorphCube](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/edc7c9e67c639d230715049ee31f9a96a6babbbe/Models/AnimatedMorphCube): Microsoft, 2017; binary GLB with two morph targets.
- [SimpleSkin](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/edc7c9e67c639d230715049ee31f9a96a6babbbe/Models/SimpleSkin): Marco Hutter, 2017; glTF plus four external buffers demonstrating two-joint skinning.
- [AnimatedTriangle](https://github.com/KhronosGroup/glTF-Sample-Assets/tree/edc7c9e67c639d230715049ee31f9a96a6babbbe/Models/AnimatedTriangle): Marco Hutter, 2017; glTF plus two external buffers demonstrating quaternion rotation.

Model assets are **CC0-1.0**. Upstream licenses identify metadata, including the LICENSE files, as **CC-BY-4.0**. Each model's original README.md and LICENSE.md is retained unchanged with the files. The upstream README links to screenshots and variants that are not vendored here; follow the source links above to view them.

Only `cmd/animate` imports this embed package. No runtime download is needed, and these files are in the separate `examples/demos` module, outside the main library module's distribution.

## Original assembly

`Assembly/assembly.glb` is original generated CC0 data, not a Khronos sample.
Its reproducible generator is `generate/main.go`; see `Assembly/README.md` and
`Assembly/LICENSE.md`. The generator is not embedded or imported by demo binaries.
