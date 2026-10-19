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

STL, OBJ, 3MF, and PLY are built in. glTF is in its own package, `nt3d/mesh/gltf`, and registers itself when imported, the way image formats do with `image`. To get every optional format, and WebP textures, import them all at once:

```go
import _ "nimbleterminal.dev/nt3d/mesh/formats"
```

That keeps programs that read no files small: glTF alone adds about 0.6 MB to a WebAssembly build.

## Which way is up

Files don't say which way is up, and nt3d is Z-up. Many OBJ files are Y-up; set `Options.Up = nt3dmesh.YUp` to turn them. glTF is always Y-up by its specification, so it is always turned. Units are the file's own, and nothing is rescaled unless you ask, as `Normalization` does above.

## Staying safe with big or hostile files

Reading is bounded, so a file can't exhaust your memory:

- `Options.MaxTriangles` caps the whole model (`DefaultMaxTriangles`, 4 million, unless you raise it).
- Coordinates must be within ±10¹⁵, and textures at most 8192 pixels a side, checked before decoding.
- 3MF packages are limited to 4096 entries and `Options.MaxBytes` unpacked (256 MiB by default).
- Side files, like an OBJ's materials or a glTF's buffers, are read only through `Options.Open`. `ReadFile` confines them to the model's directory; `Decode` without `Open` skips them. Missing ones become `Model.Warnings` rather than errors.

An object bigger than one geometry can hold (`nt3d.MaxMeshTriangles`) is split into consecutive objects that share its name, material, and part. `AddTo` gives each its own node. A point cloud or Gaussian splat larger than one geometry holds (`nt3d.MaxPoints`, or `nt3d.MaxGaussians` for splats with SH) is split the same way, into slabs along its longest axis, by `nt3dmesh.PointPieces`; blended slabs draw farthest first by their centers, so a very large cloud can still blend out of order where slabs meet.

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

- Base colors and vertex colors become sRGB vertex colors. Base color textures keep their alpha, and repeat unless clamped.
- `MASK` becomes a cutout, `BLEND` transparency, `KHR_materials_unlit` unlit, and double-sided materials get back faces.
- Roughness and metalness become an approximate highlight. Metals, which glTF colors by their reflected surroundings, show their base color.
- Strips, fans, points, and lines are read. Missing normals are computed flat.

Files that require an extension nt3d can't honor, such as Draco or meshopt compression, are refused. Animation, skins, and morph targets use the [generic animation API](../animation/). All glTF models retain their hierarchy in `Model.Asset`, including empty transform nodes. `AddTo` instantiates it and `scene.AnimationClips(root)` exposes its clips. `Model.Objects`, `Bounds`, and `Geometry` keep the flattened rest-pose view for existing callers. Static mirrored nodes retain their ancestry, with reflections moved into primitive vertices; animated negative or singular scales remain unsupported by the scene graph.

Normal maps use the [generic normal-map path](../normal-maps/). `KHR_texture_transform` applies scale, rotation, then offset to each supported texture slot's UVs, including its `texCoord` override. Base color and normal maps can use different UV sets and transforms: decoded `Geometry.UV` and optional `Geometry.NormalUV` contain the results, so software, GPU, picking and cutout shadows share them. The extension is accepted when required. Emissive/occlusion/metallic-roughness textures remain outside the renderer's material model; this extension does not add those maps. Authored tangents still use the generated triangle basis.

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

Call `NodeCamera` again after sampling animation to follow its motion. The returned `Camera` is a detached snapshot, with `Pose` holding its orientation and position (-Z forward, +Y up). It preserves roll, vertical `YFOV` in radians, optional fixed `AspectRatio`, clipping planes, and orthographic `Xmag`/`Ymag` half extents. An omitted perspective far plane stays infinite. Node scale does not scale the lens. Normal viewport aspect is used when the file omits perspective aspect.

These are generic APIs: procedural sources can call `SetNodeCamera`, or set `AssetNode.Camera`. The ordinary orbit camera retains its defaults. Orbit/automatic rotation returns an authored view to orbit orientation; pan and zoom retain its roll. Picking rays and perspective skies use the same lens and orientation as rendering.

See the [glTF camera specification](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#cameras) and [KHR_texture_transform](https://github.com/KhronosGroup/glTF/tree/main/extensions/2.0/Khronos/KHR_texture_transform) for the source conventions.

## Writing

`EncodeSTL`, `EncodeOBJ`, `Encode3MF`, and `EncodePLY` write triangles; OBJ writes lines too. Nothing is normalized on the way out.
