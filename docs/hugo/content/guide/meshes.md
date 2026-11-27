---
title: "Mesh files"
weight: 60
---

# Mesh files

`nt3dmesh` reads and writes the common 3D file formats. Reading gives you a `Model`, a list of objects, which you can add to a scene as one node each:

```go
model, err := nt3dmesh.ReadFile("part.obj", &nt3dmesh.Options{Color: grey})
group, err := model.AddTo(scene, nt3d.Root, "part") // one child node per object
fit, _, _ := model.Bounds().Normalization()
err = scene.SetTransform(group, fit)                 // scale it to fit the default camera
```

<img class="poster" src="../../posters/models.png" alt="A striped robot with a sky-blue head, orange arms, and a smiling screen">

## Formats

| Format | Read | Write |
| --- | --- | --- |
| STL | binary and ASCII, with face colors | binary, with colors |
| OBJ | with materials, textures, and lines | triangles and lines |
| 3MF | with colors, textures, painting, and parts | one object, with face colors |
| PLY | ASCII and binary, including raw scans | binary |
| glTF 2.0 | `.gltf` and `.glb` | — |
| SOG | `.sog` bundles and `meta.json` folders | — |

STL, OBJ, 3MF, and PLY are built in. glTF is in its own package, `nt3d/mesh/gltf`, [SPZ Gaussian splats](../rendering/#spz) in `nt3d/mesh/spz`, and [SOG Gaussian splats](../rendering/#sog-and-compressed-ply) in `nt3d/mesh/sog`; each registers itself when imported, the way image formats do with `image`. To get every optional format, and WebP textures, import them all at once:

```go
import _ "nimbleterminal.dev/nt3d/mesh/formats"
```

That keeps programs that read no files small: glTF alone adds about 0.6 MB to a WebAssembly build.

The Gaussian decoders (PLY, SPZ, SOG) also group each cloud into clusters at load, which is what lets the renderers draw a budgeted subset: see [Gaussian clouds](../clustering/#gaussian-clouds). That costs about 160 ms per million splats (406 ms for 2.5 million on an M5 Max); `Options.NoClusters` skips it, so loading is faster and every splat is drawn, taking its share off the budget first. `Options.SHPalette` chooses whether the spherical harmonics are shared through a palette: a SOG's palette is kept by default, and `SHPaletteBuild` makes one for PLY, compressed PLY, and SPZ clouds at load (see [SH palettes](../rendering/#sh-palettes)).

## Which way is up

Files don't say which way is up, and nt3d is Z-up. Many OBJ files are Y-up; set `Options.Up = nt3dmesh.YUp` to turn them. glTF is always Y-up by its specification, so it is always turned. Units are the file's own, and nothing is rescaled unless you ask, as `Normalization` does above.

## Staying safe with big or hostile files

Reading is bounded, so a file can't exhaust your memory:

- `Options.MaxTriangles` caps the whole model (`DefaultMaxTriangles`, 4 million, unless you raise it).
- Coordinates must be within ±10¹⁵, and textures at most 8192 pixels a side, checked before decoding.
- 3MF packages are limited to 4096 entries and `Options.MaxBytes` unpacked (256 MiB by default).
- A texture's decoded pixels count against `Options.MaxBytes` too, at four bytes a pixel, in every format. An OBJ, PLY or 3MF texture that does not fit is left out with a warning; a glTF image that does not fit fails the decode with `ErrTooLarge`. Raise `MaxBytes` for a model with more than 256 MiB of textures.
- A decode that stops at a limit says which, so a program knows what would help: its error wraps `ErrTooManyTriangles` (raise `MaxTriangles`, or choose fewer 3MF parts), `ErrTooLarge` (raise `MaxBytes`), or `ErrTooComplex`, for limits no option raises: more points or line ends than nt3d draws, more glTF nodes or variants than the decoder walks, or a hierarchy over 1024 levels deep.
- Side files, like an OBJ's materials or a glTF's buffers, are read only through `Options.Open`. `ReadFile` confines them to the model's directory; `Decode` without `Open` skips them. Missing ones become `Model.Warnings` rather than errors.

An object bigger than one geometry can hold (`nt3d.MeshTriangleLimit()`, `MaxMeshTriangles` at the default limit) is split into consecutive objects that share its name, material, and part. `AddTo` gives each its own node. A point cloud or Gaussian splat larger than one geometry holds (`nt3d.PointLimit()`, or `nt3d.MaxGaussians`, or `nt3d.MaxPreciseGaussians` with `PointStyle.Precise`, for Gaussian splats) is split the same way, into slabs along its longest axis, by `nt3dmesh.PointPieces`; blended slabs sort together on the GPU, so they blend in order to within a depth slice where they meet. The decoders split at the limit in force when they run, so raise it with `nt3d.SetBindingLimit` or `nt3d.AdoptBindingLimit` before loading on a device that holds more than the 128 MiB baseline (see [Limits](../rendering/#limits)). The decoders split at the compact limit; to draw a decoded cloud `Precise`, set `PointStyle.Precise` on each object's geometry and pass it through `nt3dmesh.PointPieces` again, which re-splits at the precise limit.

A decoder of your own, registered with `RegisterFormat`, keeps the same limits with the same pieces the built-in ones use: `Options.ByteBudget()` and `Options.TriangleBudget()` count what it reads and refuse the rest with the errors above, `ValidCoordinate` checks what it parsed, and `nt3d.MaxTextureSide` bounds a texture before it is decoded. A splat decoder builds each splat with `GaussianPoint` and hands the whole cloud to `GaussianModel`, which applies the SH palette mode, splits the cloud into pieces, clusters them, and validates them.

`Object.Plain` marks vertices the file left uncolored, so a viewer can paint them its own way.

## Format details

### STL

Binary and ASCII, including files with several ASCII solids, and VisCAM/SolidView or Magics face colors. Binary files are recognized by their exact length, since their headers may begin with "solid". Facet normals are recomputed.

### OBJ

Positions (with the `r g b` vertex-color extension), texture coordinates, normals, polygons (concave ones are split properly), polylines, objects and groups, and material libraries: `Kd` colors and `map_Kd` PNG or JPEG textures. Transparent texels are flattened over `Kd`. Each object or group becomes one `Object` per material. Faces without normals get flat ones.

### 3MF

The core specification and more:

- the production extension's objects in other model parts
- base materials, and color groups with a color at each corner
- textures (each textured part becomes an object with UVs)
- faces painted in Bambu Studio or PrusaSlicer, each taking the filament covering most of it
- filament colors and names from Bambu Studio, OrcaSlicer, and their relatives

Each build item is a `Model.Parts` entry with its triangle count, and `Options.Parts` chooses which to assemble. `Model.Unit`, `Metadata`, and `Thumbnail` (the slicer's plate picture first) are filled in too.

Over the triangle or byte limit, `Decode3MF` returns what it learned along with an error wrapping `ErrTooManyTriangles` or `ErrTooLarge`. `Model.Summary` measures the whole build without assembling it, even past the limit: total triangles, distinct meshes, and world-space bounds and surface area, with each `Part` carrying its own. Together they let a viewer describe a model too big to load, then load only the parts you choose.

### PLY

ASCII or binary, in either byte order: positions, normals, colors, and texture coordinates under their common names; polygons with face colors and MeshLab's per-corner `texcoord` lists; and the texture a `comment TextureFile` names. Unknown elements are skipped.

PLY is how the [Stanford scanning repository](https://graphics.stanford.edu/data/3Dscanrep/) shares its models, and nt3d reads them: the bunny's 69,451 triangles take about 25 ms. A raw scan's range grid becomes a surface joining neighboring samples, without bridging jumps in depth, and vertices with neither faces nor grid become a point cloud.

`DecodeConf` reads a Stanford `.conf` file, which places a set of range scans together: the bunny's ten raw scans assemble into one model of 668,686 triangles.

### glTF

`nt3dgltf.Decode` reads `.gltf` files with their buffers and images, and binary `.glb` files, using [qmuntal/gltf](https://github.com/qmuntal/gltf). The default scene's nodes are walked, and each mesh primitive becomes an object placed where its node puts it and named for it.

- Base colors and vertex colors become sRGB vertex colors. Base color textures keep their alpha, and repeat unless clamped. `KHR_texture_basisu` decodes KTX 2.0 BasisLZ/ETC1S, UASTC (all 19 block modes), and uncompressed textures (including Zstandard and zlib supercompression, and automatic tangent `Z` reconstruction for two-channel `RRRG` / `RRR+GGG` normal maps) in pure Go with zero CGo, bounded by `Options.MaxBytes` and accepted in `extensionsRequired`. `EXT_texture_webp` is also decoded directly in `nt3d/mesh/gltf` (accepted in `extensionsRequired`), with graceful fallback from `KHR_texture_basisu` to `EXT_texture_webp` and `Texture.Source` when an external side file is missing.
- `MASK` becomes a cutout, `BLEND` transparency, `KHR_materials_unlit` unlit, and double-sided materials get back faces.
- Metallic and roughness factors, `metallicRoughnessTexture`, and `occlusionTexture` decode directly to Cook-Torrance GGX `Material.PBR` (`PBR.Metallic`, `PBR.Roughness`, `PBR.ORMMap`, and `PBR.OcclusionStrength`), merging separate occlusion and metallic-roughness images into one linear ORM map when needed.
- `emissiveFactor`, `emissiveTexture`, and `KHR_materials_emissive_strength` map to `Emission.Color`, `Emission.Map`, and HDR `Emission.Intensity`. `KHR_materials_ior`, `KHR_materials_specular` (`specularFactor`, `specularColorFactor`), `KHR_materials_transmission`, and `KHR_materials_volume` (`thicknessFactor`, `attenuationColor`, `attenuationDistance`) map to `PBR.IOR`, `PBR.Specular` (`Strength` or `Off` from `specularFactor`, `Color` and `Intensity` from `specularColorFactor`), `Transmission.Amount`, `Transmission.Refraction`, and Beer-Lambert `Transmission.Absorption`, and are accepted when listed in `extensionsRequired`.
- `KHR_materials_clearcoat` (`clearcoatFactor`, `clearcoatRoughnessFactor`) and `KHR_materials_sheen` (`sheenColorFactor`, `sheenRoughnessFactor`) map to `PBR.Clearcoat.Amount`, `PBR.Clearcoat.Roughness`, `PBR.Sheen.Color`, and `PBR.Sheen.Roughness`, and are accepted when listed in `extensionsRequired`.
- `KHR_materials_variants` decodes document-level variant names (`Model.Variants()` and `SceneAsset.Variants`) and per-primitive material mappings (`AssetNode.VariantMaterials`), accepted when listed in `extensionsRequired`. After `root, _ := model.AddTo(scene, nt3d.Root, "car")` (or `model.Asset.Instantiate`), call `scene.MaterialVariants(root)` to list colorways, `scene.SetMaterialVariant(root, "Taxi")` (or `""` for the default look) to switch a placed instance's materials, and `scene.MaterialVariant(root)` to inspect the active variant—preserving `SharedGeometry` GPU vertex buffers and BVH pick indexes across instances via `Material.BaseColor`.
- `KHR_mesh_quantization` decodes quantized `BYTE`, `UNSIGNED_BYTE`, `SHORT`, and `UNSIGNED_SHORT` accessors for `POSITION`, `TEXCOORD_0` / `TEXCOORD_n` (normalized or unnormalized, pairing cleanly with node/matrix dequantization transforms and `KHR_texture_transform`), normalized `BYTE` / `SHORT` `NORMAL` and `TANGENT` frames, and quantized morph target deltas (`POSITION`, `NORMAL`, `TANGENT`), accepted when listed in `extensionsRequired`.
- `EXT_meshopt_compression` decompresses `ATTRIBUTES`, `TRIANGLES`, and `INDICES` `bufferView` streams in pure Go (no CGo, identical in native and `GOOS=js GOARCH=wasm` builds), including `OCTAHEDRAL`, `QUATERNION`, and `EXPONENTIAL` post-filters and `fallback` buffers, bounded by `Options.MaxBytes` and accepted when listed in `extensionsRequired`.
- `EXT_mesh_gpu_instancing` expands per-instance `TRANSLATION` (`VEC3` float), `ROTATION` (`VEC4` float or normalized `BYTE`/`SHORT` quaternions), and `SCALE` (`VEC3` float) accessors onto the node's mesh (leaving child nodes uninstanced, per the Khronos specification), accepted when listed in `extensionsRequired`.
- `KHR_gaussian_splatting` reads a `POINTS` primitive as a [Gaussian splat cloud](../rendering/#gaussian-splats-in-gltf): rotations, scales, opacity, and spherical harmonics of degree 0 through 3, turned to Z-up with the rest of the scene, from plain accessors or from one SPZ payload (the draft compression extension, under its three names). All are accepted in `extensionsRequired`.
- Strips, fans, points, and lines are read. Missing normals are computed flat.
- **Content placed on the Earth keeps its precision.** Vertices are `float32`, which steps by half a meter at the six million meters of Earth-centered coordinates. When a root node is translated more than 100 km from zero, or the document carries `CESIUM_RTC` (the center 3D Tiles 1.0 content is relative to, accepted in `extensionsRequired`), the geometry is built near zero and the placement comes back as `Model.Origin`, a `float64` point in the model's Z-up space. `Model.AddTo` and `AddLODTo` set it as the node's origin (`Scene.SetNodeOrigin`), so the model is drawn where the file put it, exactly, by a camera whose `Origin` is nearby. If you instantiate `Model.Asset` yourself, set the origin yourself. Only the root nodes of the scene being read decide this, and a translation track on a recentered root has its keys moved with the node (a cubic spline's tangents stay as written), so its clips play in the recentered space. For other models `Origin` is zero and nothing changes.

A glTF model comes back in two views: `Model.Objects`, every primitive placed in the scene and flattened, and `Model.Asset`, the node hierarchy with each mesh in its own space, ready for `Instantiate`. Building both reads every primitive twice. If you only instantiate, ask for the asset alone:

```go
model, err := nt3dgltf.Decode(data, &nt3dmesh.Options{AssetOnly: true})
inst, err := model.Asset.Instantiate(scene, nt3d.Root, "kit")
```

`Model.Objects` is then empty, so `Triangles`, `Bounds`, `Geometry`, and `AddTo` see nothing, and `MaxTriangles` bounds what the asset holds, counting a mesh many nodes share once. On a 5 MB textured model this took a seventh off the decode time and nearly a third off its memory.

Files that require an extension nt3d can't honor, such as Draco compression, are refused. Animation, skins, and morph targets use the [generic animation API](../animation/). All glTF models retain their hierarchy in `Model.Asset`, including empty transform nodes. Undeformed static triangle primitives in `Model.Asset` automatically populate `AssetNode.Shared` (`*nt3d.SharedGeometry`), so repeated mesh nodes inside one `.glb`, `EXT_mesh_gpu_instancing` instances, and repeated `model.AddTo` / `SceneAsset.Instantiate` calls across a scene share a single GPU mesh upload, instanced draw batch, and BVH pick index (with mirrored static nodes sharing a reflected `*nt3d.SharedGeometry`). `AddTo` instantiates `Model.Asset` and `scene.AnimationClips(root)` exposes its clips. `Model.Objects`, `Bounds`, and `Geometry` keep the flattened rest-pose view for existing callers. Static mirrored nodes retain their ancestry, with reflections moved into primitive vertices; animated negative or singular scales remain unsupported by the scene graph.

Normal maps use the [generic normal-map path](../normal-maps/). `KHR_texture_transform` applies scale, rotation, then offset to each supported texture slot's UVs, including its `texCoord` override. Base color and normal maps can use different UV sets and transforms (`Geometry.UV` and optional `Geometry.NormalUV`), while ORM and emissive maps share `Geometry.UV` (warning if a model authors distinct transforms on them). The extension is accepted when required. Authored `TANGENT` frames are used; without them the loader generates MikkTSpace-compatible ones (see [tangent frames](../normal-maps/#tangent-frames)).

### Authored cameras

Perspective and orthographic cameras belong to their nodes, including animated parent transforms. Camera-only scenes are supported. No imported camera automatically replaces the viewer's orbit camera:

```go
root, err := model.AddTo(scene, nt3d.Root, "model")
if err != nil { return err }
cameras := scene.Cameras(root)
if len(cameras) > 0 {
    camera, err := scene.NodeCamera(cameras[0])
    if err != nil { return err }
    frame, err := scene.Frame(camera, width, height)
    // Render frame, or pass camera to the widget's SetCamera method.
    _, _ = frame, err
}
```

Call `NodeCamera` again after sampling animation to follow its motion. The returned `Camera` is a detached snapshot, with `Pose` holding its orientation and position (-Z forward, +Y up). It preserves roll, vertical `FOV` (in degrees; the file's radians are converted), optional fixed `AspectRatio`, clipping planes, and orthographic `Xmag`/`Ymag` half extents. An omitted perspective far plane stays infinite. Node scale does not scale the lens. Normal viewport aspect is used when the file omits perspective aspect.

These are generic APIs: procedural sources can call `SetNodeCamera`, or set `AssetNode.Camera`. The ordinary orbit camera retains its defaults. Orbit/automatic rotation returns an authored view to orbit orientation; pan and zoom retain its roll. Picking rays and perspective skies use the same lens and orientation as rendering.

See the [glTF camera specification](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#cameras) and [KHR_texture_transform](https://github.com/KhronosGroup/glTF/tree/main/extensions/2.0/Khronos/KHR_texture_transform) for the source conventions.

### Authored punctual lights (`KHR_lights_punctual`)

`nt3dgltf.Decode` also decodes `KHR_lights_punctual` (`directional`, `point`, and `spot` lights, including `color`, `intensity`, `range`, and `spot.innerConeAngle` / `spot.outerConeAngle`, accepted when listed in `extensionsRequired`) onto `AssetNode.Light`. Instantiating the scene via `AddTo` or `SceneAsset.Instantiate` attaches each light with `scene.SetNodeLight(id, light)`:

- `scene.Lights(root)` lists punctual light nodes in hierarchy order, `scene.NodeLight(id)` resolves one light's current world position and `-Z` forward orientation (including animated parent nodes), and `scene.SceneLights(root)` resolves up to `nt3d.MaxLights` visible lights.
- `Scene.Frame` populates `Frame.Lighting.Lights` automatically from `scene.SceneLights(nt3d.Root)` whenever visible light nodes exist, and `nt3dwidget` preserves those scene lights unless `SetLighting` is called explicitly.

### Multi-LOD assets

Asset packs commonly ship distance-tiered meshes either as sibling subtrees in one `.glb` (`SM_Bench_01_LOD0`, `SM_Bench_01_LOD1`, `SM_Bench_01_LOD2`, `bench-lod0`, `Bench.LOD1`, or `LOD0` / `LOD1`) or as separate per-tier files. `Model.LODAsset`, `Model.AddLODTo`, `CombineLOD`, and `AddLODModels` compile them directly onto `Scene.SetLOD` while preserving `*nt3d.SharedGeometry` instancing and `KHR_materials_variants` colorways across every tier:

```go
// Single .glb with sibling *_LOD0, *_LOD1, *_LOD2 subtrees:
// pass K-1 transition distances (unbounded final tier) or all K distances (finite cull cutoff).
lodAsset, err := propModel.LODAsset(35, 100, 250)
if err != nil { return err }
for i, pos := range positions {
    inst, err := lodAsset.Instantiate(scene, nt3d.Root, fmt.Sprintf("lamp-%d", i))
    if err != nil { return err }
    _ = scene.SetTransform(inst.Root, nt3dmath.Translation(pos))
    _ = scene.SetMaterialVariant(inst.Root, "MunicipalGreen")
}

// Or combine separate per-tier files (.glb, .obj, .stl, .3mf, or .ply):
root, err := nt3dmesh.AddLODModels(scene, nt3d.Root, "sedan", []nt3dmesh.ModelLOD{
    {MaxDistance: 30, Model: sedanLOD0},
    {MaxDistance: 90, Model: sedanLOD1},
    {MaxDistance: 0, Model: sedanLOD2},
})
```

Multi-material props whose coarser tiers drop detail primitives (such as glass or trim) automatically cap dropped slots at their last tier's `MaxDistance` and synchronize switching across all parts of the prop via `AssetNode.LODBounds`. Showcase `.glb` files that park sibling `*_LOD0`, `*_LOD1`, `*_LOD2` nodes side-by-side keep `LOD0`'s placement pivot without teleporting when tiers switch. Call `model.LODTiers()` (or `asset.LODTiers()`, `asset.Bounds()`, `asset.Triangles()`) to inspect an arbitrary asset before compiling tiers, or open any `.glb` directly in `task meshes -- pack.glb` (`v` variant, `l` LOD auto/raw, `[`/`]` bias) or `task gogpu -- -file pack.glb` (`V` variant, `D` LOD auto/raw, `-`/`=`/`0` bias).

## Writing

`EncodeSTL`, `EncodeOBJ`, `Encode3MF`, and `EncodePLY` write triangles; OBJ writes lines too. Nothing is normalized on the way out. `nt3dsog.Encode` writes a Gaussian cloud whose bands above DC are a palette (see [Preprocessing splats](#preprocessing-splats)) as a SOG bundle.

## Preprocessing splats

Loading a big PLY or SPZ with `SHPaletteBuild` pays the k-means every time, about 2.5 s for a million degree-3 splats. `nt3d-palette`, built to `bin/` by `task build`, does it once and writes a file that loads paletted:

```
nt3d-palette [flags] input.ply|.spz|.sog ...
```

It reads each input (PLY, compressed PLY, SPZ, or SOG), builds a palette with `BuildSHPalette`, and writes a SOG v2 bundle, which `nt3dmesh` loads with the default `SHPaletteKeep`: the palette is the bundle's `shN` centroid image and 16-bit labels, nothing new to decode. A SOG input that already holds a palette is copied through unchanged, and a cloud with no bands above DC is written without a palette, with a warning. Inputs are converted in order, and one that fails is reported on stderr while the rest continue. The exit status is 1 when any failed, 2 for a usage error.

| Flag | Default | Effect |
| --- | --- | --- |
| `-centroids` | 65536 | Palette centroids, at most `MaxSHPaletteCentroids`. |
| `-iterations` | 10 | K-means rounds at each level. |
| `-max-rms` | 0 | Fail, writing nothing, when the palette's RMS error exceeds this; 0 ignores it. |
| `-workers` | 0 | Goroutines for the build; 0 is `GOMAXPROCS`. |
| `-out` | | The output file (one input only) or directory; a directory is one that exists, or a name ending in a separator, which is created. |
| `-i` | | Write `<stem>.sog` beside the input and remove the input. A SOG input that is copied through is left untouched. |
| `-rebuild` | | Build a new palette even when the input holds one. |
| `-progress` | | Report the build's progress on stderr. |
| `-quiet` | | Print no reports, warnings, or progress; errors still go to stderr. |
| `-fail-fast` | | Stop at the first input that fails. |

Without `-out` or `-i`, the output is `<stem>.paletted.sog` beside the input; with `-out` naming a directory it is `<dir>/<stem>.sog`. The output is written through a temporary file beside it and renamed into place, so an interrupted or failed run leaves nothing. Each input gets one report line on stdout: the output path, the centroids used and the RMS error (or `kept N centroids` for a copied SOG), whether `-max-rms` was met, the SH bytes as per-splat coefficients and as a palette with labels, and the wall time:

```
scan.sog: 65536 centroids, RMS 0.0412, SH 92.0 MB -> 4.9 MB, 3.1s
```

The bundle's images are lossless PNG rather than WebP, since Go has no lossless WebP encoder: `nt3dmesh` and any reader that decodes images by content open it, but a SOG reader that insists on WebP may not. Positions keep 16 bits in SOG's log domain and scales, DC, and palette coefficients 8 bits over each attribute's range, as SOG v1's linear ranges did, so a reload matches within that quantization. The file keeps the input's coordinates; `Options.Up` applies at load as it does for any SOG.
