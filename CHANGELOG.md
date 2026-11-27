# Changelog

## Unreleased

### Added

- **Positions on the Earth (`nt3d/geo`)**: package `nt3dgeo` converts between WGS84 latitude, longitude and ellipsoidal height (`Geodetic`), Earth-centered meters (`Geodetic.ECEF`, `FromECEF`), and a local east-north-up frame (`ENU`), which is nt3d's Z-up world. `ENU.Place` splits a transform into Earth-centered meters, such as a 3D Tiles tile's, into a `float64` origin for `Scene.SetNodeOrigin` and a `float32` matrix with no translation, so a model 14 km from the scene's origin lands within a tenth of a millimeter. `Region` is a 3D Tiles bounding region, and `Region.Box` an oriented box that holds its curved surface and not only its corners. The package depends only on `nt3d/math`, and is tested against PROJ 9.9.0 reference values in `geo/testdata/geo-vectors.json`.
- **Gaussian splats in glTF (`KHR_gaussian_splatting`)**: `nt3d/mesh/gltf` reads a `POINTS` primitive carrying the ratified extension as a Gaussian cloud: x, y, z, w rotations, linear scales and opacity (float or the integer types the extension allows), and spherical harmonics of degree 0 through 3; an attribute whose accessor has no buffer view reads as zeros, as glTF defines it, and a primitive's indices choose which records are splats. The cloud turns from glTF's Y-up to Z-up once, harmonics included, follows its node's transform, and is finished like the other Gaussian decoders' (`Options.SHPalette`, pieces, clusters), in `Model.Objects` and `Model.Asset` alike. A primitive may instead keep its data in one SPZ payload, under the draft compression extension's names (`KHR_gaussian_splatting_compression_spz_2`, `KHR_gaussian_splatting_compression_spz`, and the earlier `KHR_spz_gaussian_splats_compression`): it decodes to the cloud the same bytes give as an `.spz` file, with the stored values read as glTF's coordinates and nothing flipped, as CesiumJS reads them and against the draft's disputed left-up-front wording. `nt3dspz.Unpack` is the SPZ decoder's reading step on its own, a cloud in the file's axes, for containers that hold SPZ. A kernel other than `ellipse` falls back to plain points with a warning; any other compression is refused by name. `geo/testdata/geo-vectors.json` gains one Gaussian with its axes and color worked by hand, for readers and writers of splat tiles to agree on.
- **Streaming 3D Tiles into a scene (`nt3d/tiles3d/scene`)**: package `nt3dtiles3dscene` keeps a scene showing what the camera needs of a tileset. `Open` binds a tileset to a scene drawn in an east-north-up frame, or with `Options.AtCenter` around the tileset's own middle (`nt3dtiles3d.Tileset.Center`); `Update`, each frame, chooses tiles by their error on screen measured to the nearest point of their bounds, fetches and decodes the missing ones on background goroutines, places each to the millimeter (tile transform, b3dm `RTC_CENTER` and `Model.Origin` composed in `float64`), and shows and hides nodes; `Settle` waits for a still picture, and leaves the same scene however the loads raced. Replacing tiles hold the view until every child in view has arrived, and go on holding it for a child or an implicit branch that failed, adding tiles draw with their children, and contentless tiles (whatever their error), nested tilesets and implicit trees with their subtrees are walked through. A tile's geometric error scales with its transform in a 1.1 tileset and is meters in a 1.0 one, each nested tileset read by its own version. A tileset under an `Options.Parent` that is moved, turned or scaled is chosen where that node puts it. A content URI that cannot be read counts as a failed content, and a tile missing one of several contents is shown only where no replaced tile can stand in for it. Unused tiles are evicted past `Options.MaxBytes`, `Options.SplatBudget` coarsens the selection across splat tiles, by error and, inside a tile, by level, `SetMaxScreenSpaceError` changes the threshold while running, every node of a tile carries a `*Tile` record with its metadata and credits, its contents' metadata and groups index for index with its URIs, and `OnTileLoaded` and `OnTileUnloaded` let a caller restyle tiles or take their triangles as they stream.
- **A streaming 3D Tiles demo**: `task tiles` streams a town generated in memory, three levels of detail and a park of Gaussian splats, or a tileset named by URL or path, with a reference picture in each renderer and a page in the web gallery.
- **Reading OGC 3D Tiles 1.1 (`nt3d/tiles3d`)**: package `nt3dtiles3d` parses a tileset into plain data (`ParseTileset`: tiles with transforms, box, region and sphere bounds, `ADD` and `REPLACE` refinement, one or several contents, and schema, tileset, group, tile and content metadata), implicit quadtrees and octrees (`Implicit`, `Coord`, and `ParseSubtree` for binary and JSON subtree files with bitstream or constant availability), and the legacy b3dm wrapper (`UnwrapB3DM`, with its `RTC_CENTER`). `Volume.World` gives a tile's bounds as a box in Earth-centered meters. Bytes are asked for through a `Fetcher`: `FS` for a directory or embedded files, and `HTTP` for a server, natively and in the browser, with `Bearer` sending a token only to `https` on the tileset's own host, redirects included. Documents are limited by `Options` (`ErrTooLarge`), credits come back as printable text (`Asset.Credits`), and the three readers have fuzz targets. It depends only on `nt3d/math` and `nt3d/geo`.
- **glTF content placed on the Earth keeps its precision**: `nt3dmesh.Model.Origin` is a `float64` point, in the model's Z-up space, that a model placed too far from zero for `float32` is relative to. The glTF decoder sets it from `CESIUM_RTC` (now read, and accepted in `extensionsRequired`) and from a root node translated more than 100 km out, and builds the geometry near zero, in `Objects` and `Asset` alike, moving the keys of a translation track on such a root with it. `Model.AddTo`, `AddLODTo` and `AddLODModels` set it as the node's origin; `AddLODModels` refuses tiers placed at different origins.
- **A float64 oriented box and affine inverse in `nt3d/math`**: `OBBd` (`Distance`, `Nearest`, `Transformed`, `Corners`, `Radius`) bounds things placed where `float32` does not reach, and `Mat4d.InverseAffine` is `Mat4.InverseAffine` in `float64`.

### Changed

- **A glTF whose root node is more than 100 km from zero decodes near zero**, with the placement in `Model.Origin`. Before, its vertices were rounded to `float32` out there, in half-meter steps at Earth-centered sizes. A model added with `Model.AddTo` or `AddLODTo` is drawn in the same place as before, now exactly; code that reads `Model.Objects` or instantiates `Model.Asset` itself must add `Origin`.

## v0.13.0 (2026-11-26)

### Added

- **Toon posterization for points and Gaussian splats**: `PointStyle.ToonBands` (`2..8`) quantizes point and 3DGS brightness into cel bands while preserving hue and chroma across WebGPU and software renderers. `Scene.ApplyToon` sets it, from the style's `Bands`, on the points and splats of a subtree along with the toon style of its meshes, so one call toons a scene of both.
- **Depth-only holdout proxy meshes with ink outlines**: `AlphaHoldout` and `Material.Holdout` (a `HoldoutStyle`, with `DepthOffset`) write scene depth without drawing surface color (using zero-alpha blending on WebGPU so browser color-mask defaults stay untouched), while drawing `OutlineStyle` silhouettes and `Crease` ink lines over blended splats.
- **Projected mesh decals and runtime Gaussian patches**: `BuildDecal` clips target triangles to an oriented box with planar `[0, 1]²` UVs and normal lift, while `GaussianCloud` and `BuildFlatGaussians` insert and remove surface-aligned 3DGS patches inside a single depth-sorted cloud (preserving SH palettes). Every length is in the scene's units: a patch's splats are sized from its grid, and `FlatGaussians.Thickness` sets their sigma along the normal. A mark that lands on nothing (a decal that misses, an image with no opaque texel, a patch of no splats) is an empty result, not an error.
- **Kinematic capsule collision (`nt3d/collide`)**: Z-up `float64` ray casts and `MoveCapsule` collide-and-slide against a static triangle mesh, with slope limits, curb step-up, and downward ground snapping. The package depends only on `nt3d/math`; `nt3d/collide/scene` builds a mesh from an `nt3d.Geometry`. `MoveCapsule` moves in substeps of at most half the capsule's radius, so speed and step length cannot carry it through a surface, and it returns an error for wrong input (a capsule or `MoveConfig` that fails `Validate`, a position, velocity or `dt` that is not finite) with the pose unchanged. `Mesh.RayCast` returns an error, not a miss, for a ray that cannot be cast.
- **The 3DGS color convention in one place**: `nt3dsh.Y00` is the constant spherical-harmonic basis function, and `SHDCFromColor` and `ColorFromSHDC` convert a color channel to and from the degree-0 coefficient that draws as it, so a tool that writes splats need not copy the constant.
- **The pieces of a mesh decoder, for decoders of your own**: `Options.ByteBudget()` and `Options.TriangleBudget()` count what a decode reads against `MaxBytes` and `MaxTriangles` and refuse the rest with `ErrTooLarge` and `ErrTooManyTriangles`; `ValidCoordinate` and `nt3d.MaxTextureSide` are the coordinate and texture-side checks; and `GaussianModel` finishes a decoded splat cloud (SH palette mode, pieces, clusters, validation). Every built-in decoder now uses these, where the four splat decoders each repeated the finish and the byte budget was written five ways.

### Changed

- **A decode that stops at a limit names the right one.** `ErrTooComplex` is new, for limits no option raises. Errors that told a caller to raise the wrong option, or wrapped nothing, changed:

  | Stopped by | Before | Now |
  | --- | --- | --- |
  | Gaussian PLY or compressed PLY over `MaxBytes` | an unwrapped error | wraps `ErrTooLarge` |
  | glTF with more points or line ends than nt3d draws | `ErrTooManyTriangles` | `ErrTooComplex` |
  | glTF hierarchy over 1024 levels, or more nodes or variants than the decoder walks | `ErrTooLarge`, or an unwrapped error | `ErrTooComplex` |

  `ErrTooLarge` now always carries what did not fit, where glTF returned it bare, and its text is "model takes more bytes than MaxBytes allows".
- **Decoded texture pixels count against `Options.MaxBytes`**: glTF PNG, JPEG and WebP images, and OBJ, PLY and 3MF textures, are charged four bytes a pixel before they are decoded, as KTX2 and SOG images already were. Before, only the 8192-pixel side limit applied, so a small file of highly compressible images could allocate far past the budget. A glTF image that does not fit fails the decode with `ErrTooLarge`; an OBJ, PLY or 3MF texture that does not fit is left out with a warning. A model with more than 256 MiB of decoded textures (four 4096² images) now needs a larger `MaxBytes`.
- **A glTF image, or an OBJ texture file, is decoded once** however many textures or materials name it, and they share its pixels.

### Fixed

- **A normal map is refused as an ORM map, and the reverse**: `NewORMMap` and `NewNormalMap` produced the same kind of texture, so `Material.Validate`, `SetNodeORMMap` and `SetNodeNormalMap` accepted either in the other's place and drew it wrongly.
- **Widget keys move a camera placed by a `Pose`**: the arrow, `h`/`j`/`k`/`l` and `+`/`-` keys changed the orbit fields directly, which a posed camera ignores; they now go through `Camera.Orbit` and `Camera.Zoom`, as the mouse does.
- `task fmt-check` covers every Go file; it skipped `collide`.

## v0.12.0 (2026-11-18)

This release reshapes the API so that it is hard to misuse, and it breaks
most programs that use nt3d. A frame is one list of draws; a material says
what kind of surface it is instead of being inferred from which fields are
set; scene methods address nodes one way; every angle on a camera or light is
in degrees; and a camera or material that cannot mean what it says is an
error where it used to be corrected or ignored. Alongside that, a scene can
now say its own draw order, look, and per-node data; the GPU draws in the
frame's order as software does; baked lightmaps and local reflection probes
are new; and the engine no longer links a terminal UI.

### Breaking changes

**These will not compile until updated.** Each has a before-and-after table
under *Migrating* below.

| What | Before | After |
| --- | --- | --- |
| A frame's contents | `Frame.Geometry`, `Instances`, `GeometryRevisions`, `Shared`; `Instance` | `Frame.Draws`, a slice of `Draw`; `Hidden` in place of `Visible` |
| A material's fields | 43 flat fields; `PBR`, `Unlit`, `Transparent`, `Cutout`, `EdgeOn` flags | `Shading` and `Alpha`, and the groups `Phong`, `PBR`, `Normal`, `Emission`, `Transmission` |
| Per-node texture setters | `SetTexture(name, …)` and three more, by name | `SetNodeTexture(id, …)` and three more, by `NodeID` |
| Methods that change a subtree | `SetMaterial`, `SetToon`, `SetOutline`, `SetDisplacement`, `SetMaterialShader` | `ApplyMaterial`, `ApplyToon`, `ApplyOutline`, `ApplyDisplacement`, `ApplyMaterialShader` |
| Camera angles | `Alpha`, `Beta` in degrees; `YFOV` in radians | `Elevation`, `Azimuth`, `FOV`, all in degrees |
| Spot light cones | `InnerCone`, `OuterCone` in radians | `InnerAngle`, `OuterAngle` in degrees |
| Camera-path bridge | `nt3d.CameraFromState` and four more | the same names in `nimbleterminal.dev/nt3d/cam/scene` |
| `TRS` | `nt3d.TRS(t, [4]float32, s)` | `nt3dmath.TRS(t, nt3dmath.Quat, s)` |
| Widget options | `WithEffects(pp)`; `WithPostProcess(fn)` | `WithPostProcess(pp)`; `WithImageFilter(fn)` |
| `Material.EnvironmentReflect` | a field | removed; use a reflection probe |
| `MaxVectors` | a constant | `MaxArrows` |

**These compile, and behave differently.** They are the ones to read.

- **A material is PBR only when it says `Shading: ShadePBR`.** Setting
  `Roughness`, `Metallic`, or another PBR field no longer switches it. A
  group that belongs to a lighting model the material does not use (`PBR` on
  a `ShadePhong` material, `Phong` on a `ShadePBR` one) is now an error when
  the geometry reaches a scene; it used to be ignored.
- **A normal map with `Strength` left zero is at full strength.** Zero used
  to mean off, so a map set alone did nothing. To have none, leave the map
  out.
- **A bad camera is an error.** `Scene.Frame`, `StillFrame`, and
  `SetNodeCamera` return one for a `FOV` of 180 or more, a `Far` at or before
  `Near`, only one of `Xmag` and `Ymag`, a mirrored `Pose`, a NaN, or a
  `Target` too far out for float32. All of these used to be corrected
  silently and draw some other view.
- **The GPU draws in the frame's order.** Of two surfaces at one depth the
  later draw shows, as in software. A picture that relied on textured meshes
  always winning, or on instances of a shared mesh drawing together, changes.
- **The PBR highlight's strength is no longer in `SpecularColor`'s alpha.**
  It is `PBR.Specular.Strength`, with `Off` for none; the color's alpha is
  ignored.
- **A geometry copied out of a frame carries no hidden state.** Code that
  edits a frame by assigning to a scene-made draw's `Geometry` should replace
  the whole `Draw`.
- **A shader hook that declares a name the scene shader uses is refused when
  it is made,** in software too. It never compiled on a GPU, but software
  accepted it.
- **glTF reading.** A light of zero intensity is read as `Off` with its
  color, where it was a black light. A document with `null` in an array of
  objects is refused, including a null nothing refers to. A normal texture
  of zero scale is left out.

### Added

- **A scene says more about its frames**: `SetNodeDrawOrder` (layer, then
  key, then the order added), `SetNodeData` (your own value on a node, back
  from picks and draws), `StillFrame` (a frame that depends on the scene and
  camera alone), and `SetLook` (background, lighting, fog, environment, and
  post-processing, set where the scene is built).
- **Validation you can call**: `Material.Validate` and `Camera.Validate`,
  the rules a scene applies, beside `Geometry.Validate`.
- **Flags that say "none"** where a zero means the default: `Light.Off`,
  `Ambient.Off`, `ReflectionProbe.Off`, `Bloom.NoThreshold`,
  `PBR.NoOcclusion`, `PBR.Specular.Off`.
- **Baked and local lighting**: `NewLightMap` and `Material.LightMap` for
  baked irradiance, `ReflectionProbe` for local reflections, and SSAO applied
  to indirect light only.
- **`nt3dmath` holds all the math**, in float32 and now float64: `Vec3d`,
  `Quatd`, `Mat4d`, `Quat`, `TRS`, `Mat4.Inverse`, and the ray and culling
  tests.
- `Geometry.Clone`, `Scene.SetNodeLabel`, and
  `nt3dmesh.Options.AssetOnly`, which decodes a glTF into its asset alone.

### Fixed

- The GPU ignored the frame's draw order between vertex-colored and textured
  meshes, and between instances of a shared mesh and a mesh drawn between
  them.
- An orthographic camera with only `Near` set drew nothing once `Near`
  reached its automatic far plane.
- A reflection probe or a light far from the camera's `Origin` kept the
  whole frame from drawing.
- In browsers, post-processing never drew and cutout shadows raised a
  pipeline error; both work now.
- Two decoder panics on malformed glTF and PLY files.
- `nt3dwidget` closed its GPU renderer when a software or wireframe frame
  was refused, and discarded errors from `Scene.Frame`.
- `nt3dwater.WaveTile.RestBounds` returned bounds that were treated as empty.

### Faster and smaller

- A 5 MB glTF with WebP textures decodes in 211 ms instead of 300, with 38
  thousand allocations instead of 13.9 million.
- `Scene.Frame` makes three allocations however many nodes it draws, where
  it made two per node.
- A program that does not use `nt3dwidget` no longer builds Bubble Tea, Lip
  Gloss, or bubblezone, and one that does not use camera paths no longer
  builds `nt3d/cam`.
- GPU shader-hook programs that go unused are released.

<details>
<summary><b>Migrating</b>: every breaking change, with before and after</summary>

#### A frame is one list of draws

`Frame.Geometry`, `Instances`, `GeometryRevisions`, and `Shared`, four slices
kept in step by index, are one slice, `Frame.Draws`, and `Instance` is gone.
A `Draw` holds a node's `Geometry`, `World`, `Node`, `Name`, `Data`,
`Revision`, and `Shared`, and is `Hidden` rather than not `Visible`, so that
`Draw{Geometry: g}` alone draws `g` where it lies: a zero `World` is the
identity.

| Before | After |
| --- | --- |
| `f.Geometry[i]` | `f.Draws[i].Geometry` |
| `f.Instances[i].World`, `.Node`, `.Name` | `f.Draws[i].World`, `.Node`, `.Name` |
| `f.Instances[i].Visible = false` | `f.Draws[i].Hidden = true` |
| `f.GeometryRevisions[i]`, `f.Shared[i]` | `f.Draws[i].Revision`, `f.Draws[i].Shared` |
| `Frame{Geometry: []Geometry{g}}` | `Frame{Draws: []Draw{{Geometry: g}}}` |
| reordering all four slices | `Scene.SetNodeDrawOrder`, or reorder `Draws` |

Per-node GPU caching is now per draw: a draw with a nonzero `Revision` needs
a `Node` that is nonzero and its alone, and a frame may mix cached and
uncached draws.

`Geometry` is plain data. Its eight unexported fields (certificates,
partial-update ranges, skin pose, rest bounds, level-of-detail tier) moved
beside it into the `Draw`, so every field of a `Geometry` is the
application's, and a geometry copied out of a frame carries nothing with it.
A frame edited by assigning to a scene-made draw's `Geometry` should replace
the whole `Draw` instead.

#### `Material` is grouped by feature

`Material` was 43 flat fields whose lighting model was chosen by which of
them were non-zero: sliding `Roughness` to zero turned a PBR material into a
Blinn-Phong one, and fields of the model not in use were ignored without a
word.

`Shading` is the lighting model (`ShadePhong`, the zero value; `ShadePBR`;
`ShadeUnlit`) and `Alpha` is what alpha does (`AlphaOpaque`, the zero value;
`AlphaCutout`; `AlphaEdgeOn`; `AlphaTransparent`). The rest are groups, each
off while it is zero, and each of type field-name-plus-`Style`.

| Before | After |
| --- | --- |
| `PBR: true`, or any PBR field non-zero | `Shading: ShadePBR` |
| `Unlit: true` | `Shading: ShadeUnlit` |
| `Transparent: true` | `Alpha: AlphaTransparent` |
| `Cutout: true` | `Alpha: AlphaCutout` |
| `Cutout: true, EdgeOn: true` | `Alpha: AlphaEdgeOn` |
| `Specular`, `Shininess`, `Reflectivity` | `Phong: PhongStyle{Specular, Shininess, Reflectivity}` |
| `Metallic`, `Roughness`, `IOR`, `ORMMap`, `ORMRepeat`, `OcclusionStrength` | `PBR: PBRStyle{…}`, same names |
| `Clearcoat`, `ClearcoatRoughness` | `PBR.Clearcoat: ClearcoatStyle{Amount, Roughness}` |
| `SheenColor`, `SheenRoughness` | `PBR.Sheen: SheenStyle{Color, Roughness}` |
| `SpecularColor`, `SpecularIntensity` | `PBR.Specular: SpecularStyle{Color, Intensity, Strength, Off}` |
| `NormalMap`, `NormalStrength`, `NormalRepeat` | `Normal: NormalStyle{Map, Strength, Repeat}` |
| `FlowMap`, `FlowPhase`, `FlowStrength`, `FlowRepeat` | `Normal.Flow: FlowStyle{Map, Phase, Strength, Repeat}` |
| `Emissive`, `EmissiveIntensity`, `EmissiveMap`, `EmissiveRepeat` | `Emission: EmissionStyle{Color, Intensity, Map, Repeat}` |
| `Transmission`, `Refraction`, `Absorption` | `Transmission: TransmissionStyle{Amount, Refraction, Absorption}` |

`Texture`, `Repeat`, `FullSize`, `Tint`, `BaseColor`, `Threshold`, `Toon`,
`LightMap`, and `Shader` are unchanged.

What changes besides the names:

- **Nothing switches the model by being set.** A material is PBR when its
  `Shading` is `ShadePBR` and not otherwise. Code that relied on a PBR field
  to make a material PBR must say `Shading: ShadePBR`.
- **A group for another model is an error.** `PBR` on a material that is not
  `ShadePBR`, or `Phong` on one that is not `ShadePhong`, is refused when the
  geometry reaches a scene, where it used to be ignored.
- **`Normal.Strength` left zero means 1,** like every other strength; it used
  to mean off, so a normal map set alone did nothing. To have none, leave the
  map out.
- **The PBR highlight's strength is `PBR.Specular.Strength`, and `Off`
  removes it.** It used to be carried in `SpecularColor`'s alpha, which is
  now ignored.
- **`PBR.NoOcclusion`** ignores the ORM map's occlusion channel, which a zero
  `OcclusionStrength` cannot ask for.

`Material.EnvironmentReflect` is removed. It let one material reflect the
environment when `EnvironmentBackgroundOnly` made it a backdrop. A reflection
probe holding the same panorama does that for every reflective surface inside
its box, and is independent of the setting:
`scene.SetReflectionProbes([]nt3d.ReflectionProbe{{Environment: sky,
Position: p, Bounds: box}})`. Where the environment lights the frame the
field did nothing and can simply be deleted.

#### Scene methods address nodes by `NodeID`

The texture setters found their node by name, alone among per-node setters,
and five methods named `Set…` changed every mesh under a node rather than the
node.

| Before | After |
| --- | --- |
| `SetTexture(name, t)` | `SetNodeTexture(id, t)` |
| `SetNormalMap(name, t)` | `SetNodeNormalMap(id, t)` |
| `SetORMMap(name, t)` | `SetNodeORMMap(id, t)` |
| `SetEmissiveMap(name, t)` | `SetNodeEmissiveMap(id, t)` |
| `SetMaterial(root, m)` | `ApplyMaterial(root, m)` |
| `SetToon(root, style)` | `ApplyToon(root, style)` |
| `SetOutline(root, style)` | `ApplyOutline(root, style)` |
| `SetDisplacement(root, d)` | `ApplyDisplacement(root, d)` |
| `SetMaterialShader(root, shader)` | `ApplyMaterialShader(root, shader)` |

Behaviour is unchanged. Use the ID a setter returned, or `Find(name)`. An
`Apply` method changes the meshes under `root` when it is called and
remembers nothing, so nodes added later are untouched. `SetGeometry`,
`SetShared`, and `SetLabel` still take a name, because they create the node
when it is missing; each returns the ID.

#### Angles are in degrees

Orbit angles were in degrees under Greek letters while the field of view and
spot cones were in radians.

| Before | After |
| --- | --- |
| `Camera.Alpha` (degrees) | `Camera.Elevation` (degrees) |
| `Camera.Beta` (degrees) | `Camera.Azimuth` (degrees) |
| `Camera.YFOV` (radians, zero is π/4) | `Camera.FOV` (degrees, zero is 45) |
| `Light.InnerCone`, `OuterCone` (radians) | `Light.InnerAngle`, `OuterAngle` (degrees, zero outer is 45) |

The renames are what make this safe: code that still sets the old fields
fails to compile instead of meaning another angle. Multiply a radian value by
`180 / math.Pi`. `nt3dmath`'s functions keep radians, as Go's `math` does,
and glTF cameras and lights, which store radians, are converted when read.

#### A camera that cannot mean what it says is an error

A camera used to be corrected silently, so a mistake drew some other view.
`Camera.Validate` now reports these, and `Scene.Frame`, `Scene.StillFrame`,
and `Scene.SetNodeCamera` return its error, as they do for a geometry.

| A camera with | Before | After |
| --- | --- | --- |
| `FOV` negative or 180 and over | drew at 45 degrees | error |
| `Far` at or before `Near`, or either negative | automatic clipping | error |
| only one of `Xmag`, `Ymag` | both ignored | error |
| a flattened or mirrored `Pose` | the orbit camera | error |
| NaN or infinity anywhere | the default | error |
| negative `Distance`, `AspectRatio`, `ResumeAfter` | the default | error |
| numbers too large or small for float32 | an empty or broken frame | error |
| zero `Distance`, `FOV`, `Near`, `Far`, extents | the default | the default |
| `Elevation` past ±89, `Distance` out of range | held to the limit | held to the limit |

"Too large or small for float32" covers a camera whose numbers are finite
but more than its view and projection can hold: a `Far` of 1e300, an
`AspectRatio` of 1e-300, a `Far` that equals `Near` once both are float32, or
a `Target` so far out that the orbit distance vanishes into it and the camera
has no direction left to look in. For coordinates that large, use `Origin`.

Methods that cannot return an error (`Matrix`, `Eye`, `Orbit`, `Pick`, and
`Valid`) still compute with a corrected camera, so nothing panics, and
`Valid` always returns a camera `Validate` accepts. `nt3dwidget` keeps an
invalid camera as given and draws nothing with it: `Err` reports why until a
sound camera is set.

#### Smaller moves and renames

- **The camera-path bridge has its own package.** `CameraFromState`,
  `StateFromCamera`, `SceneResolver`, `AppendCameraPathOverlay`, and
  `CameraPathOverlayOptions` move from package `nt3d` to
  `nimbleterminal.dev/nt3d/cam/scene` (package `nt3dcamscene`), under the same
  names.
- **`nt3d.TRS` moves to `nt3dmath.TRS`,** and takes its rotation as an
  `nt3dmath.Quat` rather than a `[4]float32`.
- **`MaxVectors` is renamed `MaxArrows`,** which is what it limits.
- **`nt3dwidget` options are named for what they take.**

  | Before | After |
  | --- | --- |
  | `WithEffects(pp)`, `SetEffects(pp)`, `Effects()` | `WithPostProcess(pp)`, `SetPostProcess(pp)`, `PostProcess()` |
  | `WithPostProcess(fn)`, `SetPostProcess(fn)` | `WithImageFilter(fn)`, `SetImageFilter(fn)` |

  The two old meanings of `WithPostProcess` take different types, so code
  that still means the function fails to compile rather than misbehaving.
- In the camera packages, `nt3dcam.Vec3d`, `nt3dcam.Quatd`,
  `nt3dcurve.Vec3`, and `nt3dframes.Quat` are now aliases of
  `nt3dmath.Vec3d` and `nt3dmath.Quatd`. Code using them is unchanged, though
  `go vet` now asks for field names in their struct literals.

</details>

<details>
<summary><b>What's new</b>, in detail</summary>

#### What a scene can say about its frames

Applications had been editing frames after they were made to get these.

- **Draw order.** `Scene.SetNodeDrawOrder(id, DrawOrder{Layer, Key})` places
  a node, and the nodes under it, among those a frame draws: by layer, then
  key, then the order they were added. Blended geometry can be put after what
  shows through it, and a scene that loads in the background can draw the
  same way every time. The sorted order is kept between frames.
- **Node data.** `Scene.SetNodeData(id, v)` attaches an application's own
  value to a node. It comes back from `NodeData`, `Node.Data`, `Draw.Data`,
  and `Hit.Data`, so nothing has to be encoded into node names.
  `SceneAsset.Instantiate` now documents how it names its nodes.
- **Stills.** `Scene.StillFrame` takes a frame that depends on the scene and
  camera alone: levels of detail are chosen by distance, and the hysteresis
  memory `Scene.Frame` keeps is neither read nor written. A capture no longer
  needs `SetLODHysteresis(0)` around it. `nt3dwidget`'s `Snapshot` uses it.
- **Look.** `Scene.SetLook(Look{...})` holds the background, lighting, fog,
  environment settings, and post-processing, and every frame starts out with
  them, so a scene is lit where it is built instead of on each frame.
  `SetEnvironment` sets the look's `Environment`. Light positions are world
  space and follow `Camera.Origin`. `nt3dwidget` draws the scene's look
  unless it was given a setting of its own (`WithBackground`, `SetFog`,
  `SetLighting`, `WithPostProcess`, and the rest).

#### Saying "none" where zero means the default

`Light.Intensity`, `Ambient.Intensity`, and `ReflectionProbe.Strength` treat
zero as 1, so none could not be asked for, and a value faded to exactly zero
came back at full strength. Flags say it: `Light.Off` (no light and no
shadow; the light keeps its place and settings), `Ambient.Off`, and
`ReflectionProbe.Off` (the probe is passed over). `Bloom.NoThreshold` lets
every pixel glow, which a zero `Threshold` could not ask for. The sky already
had `EnvironmentBackgroundOnly`. Nothing existing changes meaning, and the
caller still has to set the flag: a strength animated down to exactly zero
without it comes back at full.

#### Lighting

- **Baked lightmaps.** `NewLightMap` and `NewHDRLightMap`,
  `Material.LightMap`, and an independent `Geometry.LightMapUV` consume
  externally baked linear HDR irradiance. GPU and software replace indirect
  diffuse, preserve direct, specular, and emissive lighting, and share
  half-float mip filtering. UVs survive cloning, clipping, merging, and
  normal rebuilding.
- **Reflection probes.** `ReflectionProbe` is an authored local panorama
  with a capture position and an influence box, giving box-projected PBR,
  clearcoat, and classic reflections on GPU and software.
  `Scene.SetReflectionProbes` holds up to four, in order; surfaces no probe
  covers keep the environment's lighting.
- **SSAO darkens indirect light only,** on WebGPU and complete software
  frames, leaving direct lights, emission, and unlit surfaces alone. It
  combines with a material's own occlusion by taking the lesser visibility,
  and adds an opaque geometry pass before shading; with SSAO off there is
  still one scene pass.
- `task room` is an interactive cutaway-room demo with separate probe and
  lightmap toggles, in the terminal, the native gallery, and the web.

#### `nt3dmath`

Math written two and three times over now lives once in `nt3dmath`, which
gains a float64 family beside the float32 one:

- `Vec3d`, `Quatd`, and `Mat4d`, with `TRSd`, for transforms that carry
  geographic origins, matrices read from files, and camera paths. `D` and
  `Vec3`/`Quat`/`Mat4` convert between the families.
- `Quat`, a float32 quaternion with `Normalize` and `Slerp`, `TRS`, and
  `Mat4.Rotation`.
- `Mat4.Inverse`, for any invertible matrix, projections included.
- `RayProbe` and `AABB.Hits`, the ray-against-box test, and `Mat4.Outside`,
  the culling test.

The scene, the glTF decoder, the BVH, `nt3daabbtree`, `nt3dclod`, picking,
culling, and the camera packages use them in place of their own copies.

#### Smaller additions

- `Material.Validate` and `Camera.Validate` report what a scene would
  refuse, before there is a scene.
- `Geometry.Clone` returns a copy that can be edited without touching the
  original: every slice, the material, and the billboard are copied, and
  what a geometry only points at and never changes is shared.
- `Scene.SetNodeLabel(id, label)` labels a node you already hold, as
  `SetNodeGeometry` and `SetNodeShared` do for geometry.
- `nt3dmesh.Options.AssetOnly` decodes a glTF into `Model.Asset` alone,
  leaving `Objects` empty. A glTF decode otherwise reads every primitive
  twice, placed for the flat view and again in its own space for the asset;
  a caller that only instantiates needs the second. `MaxTriangles` then
  bounds what the asset holds, counting a shared mesh once.

</details>

<details>
<summary><b>Fixes, performance, and internals</b>, in detail</summary>

#### Fixes

- **The GPU keeps the frame's draw order, as software does.** Of two surfaces
  at one depth the later draw shows, which `DrawOrder` relies on, but the GPU
  broke it in two ways. It drew every vertex-colored mesh before any textured
  one, so a textured mesh always won; it now draws geometry by geometry. And
  it drew every instance of a shared mesh where the first was, so of red,
  blue, red at one place it showed blue; an instance now joins the earlier
  ones only when nothing drawn since overlaps it, which keeps instancing
  wherever order cannot show.
- **An orthographic camera with only `Near` set keeps its far plane beyond
  `Near`.** `Near` at or past the automatic far plane (200 units at the
  default distance) used to leave no depth, and a view matrix that was not
  finite, so nothing was drawn.
- **A reflection probe far from the camera's `Origin` no longer keeps the
  whole frame from drawing.** Rebased into the frame's float32 coordinates
  its box collapsed, and the frame was refused; such a probe is now left out.
  A light farther from the origin than float32 reaches did the same, its
  position coming out infinite: a directional light, which has no use for a
  position, keeps shining, and a point or spot light that far is off in the
  frame.
- **Two faults only a browser showed.** The post-processing resolve shader
  named a variable `smooth`, which WGSL reserves: browsers refused the
  module, so post-processing never drew there, while the native translator
  accepted it. And the cutout shadow pipeline asked for a color write mask
  of zero, which the browser binding omits, leaving the browser's default of
  every channel on a target the shader did not write; the shader now writes
  it. Both were in v0.11.1.
- **Two decoder panics on malformed files.** A glTF/GLB document with `null`
  in any array of objects (materials, scenes, cameras, mesh primitives,
  animation channels and samplers, and the rest) is refused by name, such as
  `materials[0] is null`, including a null nothing refers to, which was
  accepted before. A PLY range grid whose `num_cols` × `num_rows` overflows
  is skipped like any other size that does not match the element.
- **Shader hooks.** A hook whose WGSL declares a name the scene shader uses
  is refused by `NewVertexDisplacement` and `NewMaterialShader` with an error
  naming it. Hooks are compiled into the scene shader's module and share its
  names, so such a hook never compiled on a GPU; it now fails when it is
  made, in software too, instead of on the first GPU frame with the shader
  translator's message. A displacement and a material shader on one mesh
  that both declare a name fail that frame the same way.
- **`nt3dwidget`.** A frame that software or wireframe mode was asked for
  and refused no longer closes the GPU renderer held for WebGPU mode; only a
  GPU fallback lets it go, as documented. An error from `Scene.Frame` is
  reported through `Err`, where it was discarded.
- **`nt3dwater.WaveTile.RestBounds`** returns a valid `AABB`; it returned one
  that `IsValid`, `Transformed`, and culling treated as empty.
- **glTF.** A light with zero intensity is read as `Off` with its color
  kept, where it was a black light of intensity 1. `specularFactor` maps to
  `PBR.Specular.Strength` or `Off`, and a normal texture whose scale is zero
  is left out.

#### Performance

- **Textures convert without allocating per pixel.** `NewTexture`, and the
  glTF occlusion-roughness-metallic packer, read JPEG and WebP images
  (`YCbCr`, `NYCbCrA`), premultiplied `RGBA`, and `Gray` through their own
  accessors instead of boxing each pixel's color; the pixels are identical. A
  5 MB glTF with WebP textures decodes in 211 ms instead of 300, with 38
  thousand allocations instead of 13.9 million, and in 181 ms and 126 MB with
  `AssetOnly`, from 239 MB.
- **`Scene.Frame` makes three allocations however many nodes it draws.** It
  made two per node: 10,000 nodes cost 20,078 allocations and 43.7 MB, which
  the first step of this work cut to 8.9 MB in a little over half the time.
- **GPU hook programs no frame has drawn for 600 frames are released,** and
  compiled again if their source returns. They were kept until the renderer
  closed, so an application that generates shader sources grew without bound.

#### Dependencies

- **The engine no longer links the terminal UI stack.** `RenderWireframe`
  was the root package's only use of ntcanvas, whose single package brought
  Bubble Tea, Lip Gloss, and bubblezone to every importer, `water` and `fur`
  included. It now uses `nimbleterminal.dev/ntcanvas/grid` (ntcanvas v0.2.0),
  which imports none of them, and wireframe output is unchanged. Only
  `nt3dwidget` imports Bubble Tea.
- **The engine no longer imports `nt3d/cam`,** so a program that does not
  use camera paths does not build them.

#### Internals and tooling

- **Orientation for people and agents.** The package documentation describes
  the Scene, Frame, Renderer pipeline and where things are; the guide gains
  an Architecture page mapping packages and files and the rules that keep
  the renderers in step; `AGENTS.md` states the repository's invariants and
  workflow; and the root package and widget have compiled examples for the
  pipeline, draw order, node data, the look, materials, cameras, lights,
  editing a frame, and building one by hand.
- **Conventions held by tests.** Tests fail if the engine links a terminal
  UI or camera paths, if the byte counts Go writes for the scene uniform,
  instance, mesh, and point records differ from the structs the shader
  declares, if a scene method finds its node by name, if a `Material` group
  is not named for its field, or if `Geometry.Clone` misses a field.
- **Reference pictures.** Every demo is drawn at 240 by 160 and compared
  with a recorded picture, in software as part of `task ci` and on the GPU
  as part of `task test-gpu`; a picture fails when more than half a percent
  of its pixels move by more than eight levels. Until now rendering tests
  asked about a few pixels of small scenes or compared one renderer with the
  other, so nothing noticed a whole picture change. Against v0.11.1, every
  demo of this release draws the same picture to within 140 of 614,400
  pixels, in both renderers. The toon demo added its ground and roads in the
  order a map ranged over them, so its picture varied from run to run; the
  order is fixed.
- **Fuzzing.** `FuzzCamera` holds that `Camera.Valid` always satisfies
  `Camera.Validate`, and that a camera `Validate` accepts gives a frame a
  renderer accepts; it found the orthographic and float32-range camera
  faults above. `FuzzSceneFrame` holds the same for a whole scene: whatever
  material, lights, fog, reflection probe, and origins a scene accepts, the
  frame validates and software draws it; it found the far light.
  `FuzzNewHDRLightMap` covers the lightmap reader. The glTF fuzz target gains
  seeds aimed at the places where
  the reader has to translate a file into nt3d's stricter rules.
- **GPU renderer,** with no change in what is drawn beyond the draw-order
  fix: the unreachable single-renderer code path beside the shared device
  state is removed (some 300 lines); primitive slots, their entry points,
  and the draw order are named rather than numbered; and the scene shader is
  ten files by topic under `wgsl/`, joined into the same module text.
- **Indirect light** is sampled apart from material response in Go and
  WGSL, with premultiplied radiance and coverage, ready for further local
  and screen-space sources.
- **`nt3dgltf`.** Each material extension is a function and a row in one
  table, which also declares it supported, in place of a 330-line function;
  a test holds every extension name the package reads to the tables.
- **Camera.** The rules a camera has to satisfy are stated once, in a table
  that both `Validate` and the correction read.
- **`task ci`** type-checks the `webgpu`-tagged GPU tests in every module
  (`go vet -tags webgpu`), which no CI step compiled before. Running them
  still takes `task test-gpu` and a GPU.

</details>

## v0.11.1 (2026-11-16)

- Add `Scene.SetLODReferenceBounds`, which gives a `Scene.SetLOD` node shared
  reference bounds (in the node's local space) to measure its distance from
  instead of its own tier bounds, so the separate parts of one chunk switch
  tiers together. It resets the node's LOD hysteresis; `SceneAsset` already did
  this internally for multi-part props.

## v0.11.0 (2026-11-16)

This release completes pure-Go, WASM-safe glTF 2.0 and OGC 3D Tiles 1.1 asset
loading, instanced multi-colorway switching, and distance-tiered multi-LOD
compilation across `nt3d` and `nt3d/mesh`: KTX 2.0 (`KHR_texture_basisu`
BasisLZ/ETC1S and 19-mode UASTC), `KHR_mesh_quantization`, and
`EXT_meshopt_compression` decode without CGo; `EXT_texture_webp`,
`EXT_mesh_gpu_instancing`, `KHR_materials_variants`, and
`KHR_materials_specular` work across WebGPU and software renderers while
sharing `*SharedGeometry` GPU buffers; `(*SceneAsset).WithLOD`,
`CombineAssetLOD`, `Scene.SetLODBias`, and `Scene.SetLODHysteresis` scale
thousands of multi-tier props in `O(1)` per frame; and `task meshes` and
`task gogpu` inspect colorways and LOD tiers interactively out of the box.

### Pure-Go glTF and OGC 3D Tiles 1.1 codecs

- Add pure-Go, WASM-safe `KHR_texture_basisu` (KTX 2.0) decoding to
  `nt3d/mesh/gltf` (accepted in `extensionsRequired`, with zero new module
  dependencies):
  - Decodes `KHR_DF_MODEL_ETC1S` (`BasisLZ` global DPCM endpoint and raw/XOR-DPCM
    selector codebooks, canonical LSB-first Huffman + VLC + RLE +
    `approx_move_to_front` selector history slices, `2×2` spatial endpoint
    predictors, single-slice `RGB`/`RRR`, and dual-slice `RGBA` / `RRR+GGG`),
    `KHR_DF_MODEL_UASTC` across all 19 4×4 LDR block modes (`0..18`, including
    BISE trit/quint endpoints, 1/2/3-subset ASTC partitions, and dual-plane
    weights), and uncompressed 8-bit `VkFormat` payloads with `None`, Zstandard,
    or zlib supercompression bounded by `Options.MaxBytes`.
  - Automatically reconstructs unit tangent `+Z` on two-channel (`RRRG`,
    `RRR+GGG`, `R8G8`) normal maps before `NewNormalMap`, and resolves texture
    sources in priority order `KHR_texture_basisu` → `EXT_texture_webp` →
    `Texture.Source` with clean fallback when external side files are absent.
- Add pure-Go, WASM-safe `KHR_mesh_quantization` and `EXT_meshopt_compression`
  support to `nt3d/mesh/gltf` (accepted in `extensionsRequired`):
  - `KHR_mesh_quantization` decodes quantized `BYTE`, `UNSIGNED_BYTE`, `SHORT`,
    and `UNSIGNED_SHORT` `POSITION` and `TEXCOORD_n` accessors, normalized `BYTE` /
    `SHORT` `NORMAL` and `TANGENT` frames, and quantized morph target deltas
    (`POSITION`, `NORMAL`, `TANGENT`).
  - `EXT_meshopt_compression` decompresses `ATTRIBUTES`, `TRIANGLES`, and
    `INDICES` `bufferView` streams with `OCTAHEDRAL`, `QUATERNION`, and
    `EXPONENTIAL` post-filters and dummy `fallback` buffers within
    `Options.MaxBytes`, plus normalized integer `rotation` and `weights`
    animation sampler outputs.
- Add `EXT_texture_webp` and `EXT_mesh_gpu_instancing` support to
  `nt3d/mesh/gltf` (accepted in `extensionsRequired`), expanding per-instance
  `TRANSLATION`, `ROTATION` (`FLOAT` and normalized `BYTE` / `SHORT`
  quaternions), and `SCALE` accessors in both `Model.Objects` and `Model.Asset`
  within triangle, visit, and byte budgets.

### Material colorways, dielectric specular, and shared asset meshes

- Add glTF `KHR_materials_variants` support (`nt3d/mesh/gltf`, accepted in
  `extensionsRequired`), per-instance `Material.BaseColor`, and
  format-independent colorway switching (`SceneAsset.Variants`,
  `AssetNode.VariantMaterials`, `AnimatedInstance.Variants`,
  `nt3dmesh.Model.Variants`, `Scene.MaterialVariants`, `Scene.MaterialVariant`,
  and `Scene.SetMaterialVariant`) so multi-colorway props and vehicles switch
  materials per instantiated subtree while preserving `SharedGeometry` GPU
  vertex buffers and BVH pick indexes.
- Add `KHR_materials_specular` support (`nt3d/mesh/gltf`, accepted in
  `extensionsRequired`, leaving `ignoredExtensions` empty) and per-instance
  `Material.SpecularColor` and `Material.SpecularIntensity` across smooth PBR
  and `PBR` + `Toon` on both WebGPU and software renderers, packing
  `sqrt`-quantized dielectric `F0` RGB and grazing `F90` into existing GPU
  instance uniform slots without increasing `instanceRecordSize`.
- Add `AssetNode.Shared` (`*SharedGeometry`) and `SharedGeometry.Geometry` so
  `SceneAsset.Instantiate` attaches shared triangle meshes via
  `Scene.SetNodeShared`. Update `nt3d/mesh/gltf` to deduplicate undeformed
  static triangle primitives in `Model.Asset` into cached `*SharedGeometry`
  handles (including cached reflected handles for static mirrored nodes),
  sharing one GPU upload, instanced draw batch, and BVH pick index across
  repeated mesh nodes in a `.glb` and repeated `Model.AddTo` /
  `SceneAsset.Instantiate` placements in a scene.

### Multi-LOD compilation, distance scaling, and viewer inspection

- Add multi-LOD asset compilation and `Scene.SetLOD` template support:
  `AssetNode.LOD`, `AssetNode.LODBounds`, and `AssetNode.LODVariantMaterials`
  let `SceneAsset.Instantiate` bind shared `LODLevel` chains directly with
  synchronized multi-part AABB switching and per-tier `Scene.SetMaterialVariant`
  colorway updates; `(*SceneAsset).WithLOD`, `CombineAssetLOD`,
  `(*SceneAsset).LODTiers`, `(*SceneAsset).Bounds`, `(*SceneAsset).Triangles`,
  `nt3dmesh.Model.LODAsset`, `Model.AddLODTo`, `Model.LODTiers`,
  `nt3dmesh.CombineLOD`, and `nt3dmesh.AddLODModels` compile sibling `*_LOD0..N`
  `.glb` subtrees (including kitbash packs, nested containers, and
  showcase-offset previews) or separate per-tier files into instanced
  `SharedGeometry` LOD assets.
- Add `Scene.SetLODBias` / `LODBias` for `O(1)` scene-wide distance scaling
  across `Scene.SetLOD` tiers (including finite hide-beyond cutoffs) without
  rebuilding node geometries or invalidating GPU/BVH caches,
  `Scene.SetLODHysteresis` / `LODHysteresis` (`[0, 0.5]`) for deterministic
  per-node deadband anti-popping across WebGPU and software renderers, and
  `FrameStats.LODTierInstances` (`[8]int`, folding tiers `>= 7` into index `7`).
- Add interactive `KHR_materials_variants` and multi-LOD `.glb` inspection to
  `task meshes` (`v` colorway cycle, `l` `LOD auto` vs raw hierarchy toggle,
  `[`/`]` `SetLODBias` steps) and `task gogpu` (`V`/`Shift+V` colorways,
  `D`/`-`/`=`/`0` LOD controls, and live `LODTierInstances` HUD telemetry), and
  extend `forest` (`[`/`]` bias steps, `b` 5% hysteresis band).

### Fuzzing, dependencies, and validation

- Add `FuzzDecodeKTX2` and `FuzzDecodeMeshopt` in `nt3d/mesh/gltf` and
  `FuzzSceneAssetLOD` in root `nt3d` (registered in `Taskfile.yml`
  `FUZZ_TARGETS`), and expand `mesh/gltf:FuzzDecode`'s oracle to instantiate
  `SceneAsset`, cycle material variants, compile multi-LOD assets, and validate
  `Scene.Frame` outputs.
- Bump the nominal `nimbleterminal.dev/nt3d` requirement in `examples/demos` and
  `examples/gogpu` to `v0.10.0` while adding zero new external module
  dependencies (retaining `wgpu v0.34.5`, `naga v0.20.4`, `gputypes v0.8.2`, and
  `gogpu v0.54.0` for Apple Metal compatibility after verifying `wgpu v0.34.15`
  with `naga v0.22.4` still fails Metal environment pipeline creation).
- Full CI (`task ci`), fuzz smoke (`task fuzz-smoke` across all 17 targets),
  site/guide builds (`task build-site`), and Apple Metal hardware GPU suites
  (`task test-gpu`) pass across all library packages and 20 gallery demos. Linux
  Vulkan GPU validation remains open per [ROADMAP.md](ROADMAP.md).

----

## v0.10.0 (2026-11-15)

This release upgrades `nt3d`'s shading, lighting, camera, and transparency
pipeline on both WebGPU and software: Cook-Torrance GGX PBR with Clearcoat and
Sheen lobes, Radiance `.hdr` environments and roughness-filtered specular IBL,
spotlights and glTF punctual lights, Weighted Blended Order-Independent
Transparency, 4× WebGPU MSAA, HDR post-processing, data-driven vertex/material
shader hooks, in-place dynamic mesh updates, distance-tiered LOD, declarative
`float64` camera paths (`nt3d/cam` and `nt3d/cam/spec`) with jitter-free
camera-relative world origins, and softened toon street shadows with crease ink
on deforming and see-through meshes. It also moves `water` and `fur` into
dedicated subpackages.

### Breaking changes

- **Move water into `nt3d/water`:** Gerstner wave surfaces, wake impulses, tiled
  water meshes, and Archimedes hull buoyancy, drag, and planing evaluation move
  out of the root `nt3d` package into `nimbleterminal.dev/nt3d/water`
  (`package nt3dwater`), renaming `WaterSurface`, `WaterBodyState`, and
  `WaterForces` to `Surface`, `BodyState`, and `Forces`.
- **Move shell fur into `nt3d/fur`:** shell-fur and silhouette-fin builders move
  out of the root `nt3d` package into `nimbleterminal.dev/nt3d/fur`
  (`package nt3dfur`) as `Config`, `Coat`, and `New`. Expose `Texture.NRGBAAt`
  and `Texture.Sample` for straight-alpha texel reads and bilinear UV sampling.

### PBR, Clearcoat, Sheen, and HDR image-based lighting

- Add Cook-Torrance GGX metallic/roughness PBR, packed linear ORM maps,
  roughness-filtered specular IBL, and HDR emissive materials to `Material` on
  both WebGPU and software renderers (`PBR`, `Metallic`, `Roughness`, `IOR`,
  `ORMMap`, `ORMRepeat`, `OcclusionStrength`, `Emissive`, `EmissiveIntensity`,
  `EmissiveMap`, `EmissiveRepeat`, `NewORMMap`, `NewShaderORMMap`,
  `NewShaderORMMapWithParameters`, `Scene.SetORMMap`, and
  `Scene.SetEmissiveMap`).
- Add `Material.Clearcoat`, `Material.ClearcoatRoughness`,
  `Material.SheenColor`, and `Material.SheenRoughness` to both smooth PBR and
  `PBR` + `Toon` across WebGPU and software renderers. Clearcoat adds an
  energy-conserving dielectric top coat (`IOR = 1.5`, `F0 = 0.04`) along the
  unperturbed geometric surface normal with independent prefiltered GGX
  environment IBL, direct GGX specular, and quantized cel lacquer highlights
  over normal-mapped or rough bases; Sheen adds an Estevez & Kulla Charlie
  microfiber grazing/retroreflective lobe with Neubelt visibility,
  directional-albedo base attenuation, ambient cloth sheen, and quantized cel
  cloth bands.
- Update `nt3d/mesh/gltf` to decode glTF 2.0 PBR metallic/roughness, merged
  occlusion/metallic-roughness ORM maps, emissive textures, and
  `KHR_materials_clearcoat`, `KHR_materials_sheen`,
  `KHR_materials_emissive_strength`, `KHR_materials_ior`,
  `KHR_materials_transmission`, and `KHR_materials_volume` (accepted in
  `extensionsRequired`).
- Add `NewHDREnvironment`, a zero-dependency Radiance RGBE (`.hdr` / `.pic`)
  panorama decoder feeding unclamped `RGBA16Float` radiance into GGX specular
  IBL, diffuse SH, HDR skies, and `Bloom`, plus `Material.EnvironmentReflect`
  so reflective (`Reflectivity > 0`) and `PBR` materials can sample
  `Frame.Environment` reflections when `Frame.EnvironmentBackgroundOnly` keeps
  diffuse environment SH lighting off for the rest of the scene.

### Lighting, OIT, MSAA, and post-processing

- Add `SpotLight` (`InnerCone`, `OuterCone`, and single-face perspective shadow
  maps on GPU and software), scene-graph punctual light nodes
  (`Scene.SetNodeLight`, `Lights`, `NodeLight`, `SceneLights`), and glTF
  `KHR_lights_punctual` decoding in `nt3d/mesh/gltf`. Add `RotationX` and
  `RotationY` to `nt3d/math`.
- Add McGuire & Bavoil Weighted Blended Order-Independent Transparency
  (`Frame.OIT` and `nt3dwidget` `WithOIT` / `SetOIT` / `OIT`), compatible with
  1× and 4× WebGPU MSAA and software float accumulation.
- Add `Frame.MSAA` (4× WebGPU multisample anti-aliasing across scene passes,
  transmission captures, and `GPUSession.Pick` depth reads, shared across
  `GPUSession.NewView`) and `Frame.PostProcess` (`SSAO`, HDR `Bloom`,
  `Exposure`, `ReinhardToneMap` / `ACESToneMap`, and `Vignette`) on GPU with
  tiered CPU software fallbacks and `nt3dwidget` `WithMSAA` / `SetMSAA` and
  `WithEffects` / `SetEffects`.
- Showcase PBR, ORM/emissive, `coat+sheen`, `HDR IBL`, `spotlights`, MSAA, and
  post-processing in `lights`, intersecting-glass OIT (`m` and `t`) in `shapes`,
  an animated `KHR_lights_punctual` **Spotlight stage** scene in `animate`, and
  matching controls in `fur`, `mipmaps`, and `examples/gogpu`.

### Shader hooks, dynamic meshes, and level of detail

- Add data-driven shader hooks with pure-Go fallbacks for vertex displacement
  and per-fragment material color: `VertexDisplacement` (`NewVertexDisplacement`,
  `NewVertexDisplacementWithParameters`, `WithParameters`, `WithMaxDisplacement`,
  `Geometry.Displacement`, `Scene.SetDisplacement`) and `MaterialShader`
  (`NewMaterialShader`, `NewMaterialShaderWithParameters`, `WithParameters`,
  `Material.Shader`, `Scene.SetMaterialShader`, `MaxShaderParameters = 16`).
  WebGPU compiles lazily cached hook pipelines shared across
  `GPUSession.NewView` and updates `[16][4]float32` uniform blocks via
  `WriteBuffer` without mesh buffer re-uploads while preserving `SharedGeometry`
  instancing; software rendering, directional/point/spot shadow maps (including
  procedural `Cutout` shadows), silhouette outlines, wireframe, and `Scene.Pick`
  / `GPUSession.Pick` evaluate the Go fallbacks in parity. Wire live wave
  displacement and crest material shader hooks (`w`) into `procedural` and
  `terrain`.
- Add `Scene.SetNodeVertices`, `Scene.UpdateNodeVertices`,
  `Scene.SetNodeVerticesWithTangents`, and `(*nt3dwater.WaveTile).Apply` for
  in-place fixed-topology mesh updates that preserve indices, UVs, and
  materials, skip redundant per-frame `O(V + I)` validation and smooth-grid
  outline-normal maps, and overwrite existing WebGPU vertex buffers via
  `WriteBuffer` without buffer reallocations. Add `Scene.SetMaterial` to replace
  or clear materials atomically across indexed meshes in a subtree while
  preserving GPU mesh buffers when vertex-baked fields are unchanged. Wire these
  into `water`.
- Add `Scene.SetLOD` and `LODLevel` for multi-tier distance-based level of
  detail with shared GPU buffers and BVH pick indexes across instances,
  per-tier outline overrides, and optional far-distance cutoffs, plus
  `Frame.Stats` / `FrameStats` (`forest` `l` toggle) and `Camera.Forward` /
  `Camera.Basis`.

### Stylized toon shading and crease lines

- Add `ToonStyle.ShadowSoftness` (`0..1`) with continuous separable tent PCF
  (`sinc²` frequency response across `1.5..4.0` shadow-map texels on
  directional, spot, and point shadow maps) and decoupled cast-shadow band
  transitions so large toon-lit scenes smooth shadow-texel staircases across
  streets without blurring `N·L` cel bands on curved characters or creating
  mid-band contour rings.
- Extend `OutlineStyle.Crease` to deforming (skinned, morphed, in-place updated,
  and single- or instanced-`VertexDisplacement`) and see-through (`Cutout`,
  `Transmission`, sorted `Transparent`, and Weighted Blended `Frame.OIT`) meshes
  across both WebGPU and software renderers, with in-place `WriteBuffer` crease
  reuse on GPU.
- Expand `toon` with coated rain-slick asphalt, soft street shadows (`s`), a
  refractive/translucent inked transit shelter, and a breeze-animated skinned
  cloth-sheen pennant (`a`), plus crease linework on transparent/OIT glass in
  `shapes` and deforming rigs in `animate` and `characters`.

### Declarative camera paths and float64 world origins

- Add zero-dependency pure-`float64` camera path packages (`nt3d/cam`,
  `nt3d/cam/ease`, `nt3d/cam/curve`, `nt3d/cam/frames`, and `nt3d/cam/spec`
  with JSON Schema `ntcamera.schema.json`): precompiled immutable `Path` values
  evaluate `CameraState` (`Pos`, `Rot`, `FovY`, `OrthoScale`, `FocusDist`,
  `Aperture`, `Near`, `Far`) in ~0.2 µs with zero heap allocations at any
  `time.Duration` (`Eval` / `EvalWith` / `EvalOverrides`, `Blend`, `Switch`,
  `FitRect`), supporting centripetal Catmull-Rom, cubic Bézier, clamped/periodic
  B-splines, Gauss-Legendre arc-length parameterization, `C¹` `Squad` and `Slerp`
  orientation, Hanson & Ma Bishop parallel-transport tangent steering, causal
  look crossfades (`target`, `tangent`, `orient`, `path`), banking `roll`,
  keyframed `fov` / `focal_mm` / `ortho_scale` / depth-of-field hints, timeline
  markers, and YAML 1.2 + JSON parsing and round-trip marshaling.
- Add `Camera.Origin`, `Camera.WorldEye`, `Frame.Origin`, `Scene.SetNodeOrigin`,
  `Scene.NodeOrigin`, `Scene.NodeWorldPosition`, and `Hit.WorldPosition` so
  scenes, `nt3dwidget` viewports, and cameras placed at geographic or
  astronomical `float64` coordinates subtract `Camera.Origin` in `float64`
  before constructing `float32` render, shadow, lighting, and picking
  transforms, eliminating `float32` vertex and depth jitter far from `(0, 0, 0)`
  with zero overhead when origins are unset.
- Add `CameraFromState`, `StateFromCamera`, `SceneResolver` (resolving
  `"node:<name>"` world positions and `"path:<name>"` trajectories),
  `Frame.DynamicLineWidth`, and `AppendCameraPathOverlay`
  (`CameraPathOverlayOptions` / `DefaultCameraPathOverlayOptions`, including
  configurable pixel `LineWidth`) for 3D spline polylines, control points,
  Bézier tangents, marker ticks, target rays, and live camera frustum pyramids.
- Add the `campath` demo (`task campath`, browser gallery #20, and
  `examples/gogpu`) showcasing a banked canyon flythrough, a closed orbit with
  focal-length pull, causal camera switching, bidirectional scrubbing, and an
  interactive director camera with live 3D frustum and spline overlays.

### Reliability, dependencies, and documentation

- Add `FuzzNewHDREnvironment` for Radiance `.hdr` decoding with pre-allocation
  scanline bounds checks and `FuzzParse` for `nt3d/cam/spec` YAML/JSON round-trip
  validation, and include `examples/gogpu` in `task ci` builds, race tests, and
  `go vet`.
- Document remaining exported symbols across all library packages, and separate
  file copyright headers from `package` clauses so `go doc` and `pkg.go.dev`
  render clean package overviews.
- Split the `examples/gogpu` desktop viewer into focused source files,
  consolidate benchmark artifacts under `docs/performance/`, add
  `CODE_OF_CONDUCT.md`, and refresh `README.md` and the Hugo guide.
- Update Lip Gloss to v2.1.1 across the library and both example modules
  (`ntpicture v0.2.0`, `ntgpuimage v0.6.0`, `ntcanvas v0.1.4`, and
  `booba v0.13.0` remain at their latest releases).
- Retain `wgpu v0.34.5`, `naga v0.20.4`, `gputypes v0.8.2`, and `gogpu v0.54.0`
  for Metal compatibility: tested `wgpu v0.34.15` with `naga v0.21.12` and
  `v0.22.1` still fails Metal environment/reflection pipeline creation, and
  `gogpu v0.54.1` requires that newer GPU stack.

### Validation and known limitations

- Full CI (`task ci`), fuzz smoke (`task fuzz-smoke`), Hugo documentation builds
  (`task build-site`), and all Apple Metal hardware GPU suites (`task test-gpu`)
  pass across all library packages and 20 gallery demos.
- **Linux Vulkan GPU validation remains open** per [ROADMAP.md](ROADMAP.md)
  (NVIDIA GB10 rendering, particle-sort, clustered-LOD, and `water` device-loss
  follow-up). `v0.9.0`'s toon cast-shadow stair-stepping and rigid-only crease
  line limitations are resolved in `v0.10.0` via `ToonStyle.ShadowSoftness` and
  deforming/see-through crease support.

----

## v0.9.0 (2026-11-12)

This release adds toon shading and crease outlines to the GPU and software
renderers, with a new demo showing both. It also improves camera depth
precision, adds controls for embedding nt3d in applications with their own
camera and keyboard handling, and fixes a Vulkan binding-order crash.

### Toon shading and outlines

- Add `Material.Toon` and `ToonStyle{Bands, Softness, Shade, Rim}` for banded
  lighting, tinted ambient shading, flat specular highlights, and rim lighting.
  `Scene.SetToon` applies the style to indexed meshes throughout a subtree.
  Toon band edges use antialiasing to reduce jagged transitions on flat faces.
- Add `OutlineStyle.Crease`, an angle in degrees, to draw sharp edges, open
  borders, and edges shared by more than two faces on opaque rigid meshes.
  Crease lines use the outline's color and width, and their geometry caches
  stay bounded as styles change.
- Add the `toon` demo to `task toon`, the browser gallery, and the GoGPU
  viewer. Spot and the robot stand in a Manhattan block baked from
  OpenStreetMap data (`flatiron.obj`, © OpenStreetMap contributors, ODbL).
  The fur demo also gains a `t` toggle for toon bands and inked fur tips.

### Cameras and embedding

- Fix perspective cameras with only `Far` set: `Near == 0` now retains an
  automatic positive near plane instead of collapsing depth values. If Far
  falls inside the automatic near plane, scale Near to Far at the default
  4,000:1 ratio. Preserve infinite perspective far planes (`Near > 0,
  Far == 0`) and explicit zero near planes for orthographic cameras.
- **Clipping behavior change:** increase the automatic near plane from
  `Distance * 0.01 / 3.4` to `Distance * 0.05 / 3.4`. This improves perspective
  depth precision and reduces z-fighting, but clips geometry very close to
  the eye sooner. Set an explicit positive `Near` when closer viewing is
  needed; set `Far` too when a finite perspective far plane is desired.
- Add `Camera.LookDirection(eye, forward)` to position first-person and chase
  cameras while preserving Distance and the automatic clipping scale.
  `LookAt` continues to set Distance from the eye-to-target distance.
- Add `nt3dwidget.WithDefaultKeyBindings(false)` so host applications can own
  keyboard controls while retaining the widget's mouse orbit, zoom, and
  picking. Existing keyboard bindings remain enabled by default.

### Reliability and dependencies

- Fix Vulkan render and compute binding order: select pipelines before
  binding resources, restore scene lighting across pipeline layout changes,
  and defer resumed-pass bindings until a pipeline is selected. This removes
  the `SetBindGroup` nil-pointer crash and prevents dropped particle-sort and
  clustered-LOD compute bindings.
- Add native Go fuzz targets for mesh decoders, polygon triangulation,
  AABB tree operations, spherical-harmonic rotation, and line clipping.
  Decoder tests use bounded inputs and include format round trips where
  supported. Run all targets briefly with `task fuzz-smoke`.
- Update dependencies across the library and both example modules:
  ntpicture v0.2.0, booba v0.13.0, gpucontext v0.32.0, Lip Gloss v2.1.0,
  Bubblezone v2.0.1, compress v1.20.4, pflag v1.1.0, terminfo v1.3.1,
  and golang.org/x/image v0.48.0, x/sync v0.25.0, and x/sys v0.50.0.
- Retain wgpu v0.34.5, naga v0.20.4, and gogpu v0.54.0 for Metal
  compatibility. Tested wgpu v0.34.14 with naga v0.21.10 fails five Metal
  environment/reflection tests; gogpu v0.54.1 requires a newer GPU stack.

### Validation and known limitations

- Full CI passes on macOS and Linux ARM64, including race tests, vet, native
  builds, and browser builds. All Metal hardware GPU suites pass.
- **Linux Vulkan GPU validation is not yet passing.** Testing on an NVIDIA
  GB10 with driver 580.178.04 confirms the binding-order crash is fixed, but
  rendering, particle-sort, and clustered-LOD comparisons still fail, and
  the water GPU demo reports device loss. See [ROADMAP.md](ROADMAP.md) for
  reproduction details and follow-up work.
- Toon cast-shadow boundaries can remain stair-stepped in large scenes.
  Crease lines currently support opaque rigid meshes; skinned, morphed,
  transparent, cutout, and transmissive meshes retain hull outlines only.

----

## v0.8.1 (2026-11-04)

- Update gputypes to v0.8.2 and goffi to v0.6.8 across all three Go modules.
  Retain wgpu v0.34.5 and naga v0.20.4: wgpu v0.34.6/v0.34.7 fail Metal
  hardware tests with their matching naga versions.

- Update ntcanvas to v0.1.4 and ntpicture to v0.1.5 across the library and
  example modules; ntgpuimage remains at v0.6.0.
- Migrate the browser demos and asset tool from `github.com/NimbleMarkets/go-booba`
  v0.10.7 to `nimbleterminal.dev/booba` v0.12.1. Regenerate browser assets with
  `task build-web` to pick up the bundled ntwebterm v0.4.2 runtime.

## v0.8.0 (2026-10-30)

- Add `GPUSession` and `GPUTarget` for rendering into host-owned WebGPU devices,
  command encoders, and texture views without CPU readback. Support sRGB and
  linear RGBA/BGRA targets, RGBA16Float, alpha composition with `GPUTarget.Load`,
  and sub-viewports through `GPUTarget.Viewport`.
- Add `GPUSession.NewView` to share pipelines, geometry, splat, texture, and
  environment/reflection caches across views while keeping per-view state
  independent. Use ntgpuimage's borrowed-device support and accept any
  `BindingLimiter` in `AdoptBindingLimit`.
- Add GPU-depth picking, `ReadDepth`, `RenderToImage`, and `RenderToImageInto`,
  plus asynchronous BVH warming for CPU picking.
- Expand the GoGPU desktop example into an 18-demo gallery and model/splat
  viewer with file dialogs and drag-and-drop, HUD controls, pick annotations,
  translate/rotate/scale gizmos, a camera orientation triad, lighting and shadow
  presets, and PNG/GIF turntable capture. Pin published GoGPU v0.54.0 so no
  sibling GoGPU checkout is required.
- Add world-unit outlines with `OutlineStyle.Units` (`WorldUnits`) and
  position-averaged contour normals for seamless expansion of split-normal
  meshes on both GPU and software renderers.
- Include clustered alpha-blended point clouds in `Frame.SplatBudget` continuous
  LOD selection, scaling retained point radii by 1/√f. Carry leaf entry distances
  through `nt3daabbtree.Tree.RayCast` so each leaf is slab-tested once.
- Add `--kitty-frame-edits=auto|on|off`, active update-mode status, and an FPS
  readout for the animation demo. Add paired headless benchmarks for animation
  and water, with per-frame protocol bytes, uploaded pixel area, preparation/
  encoding time, and scene render time; include the recorded comparison results.
- Update ntcanvas to v0.1.3, ntgpuimage to v0.6.0, ntpicture to v0.1.4, go-booba
  to v0.10.7, terminfo to v1.3.0, and the browser-compatible Bubble Tea fork to
  `536722b0bdb6`. ntpicture brings unknown-terminal capability probing,
  synchronized image uploads to prevent replacement flicker, and cleanup of
  unanswered shared-memory probes.

----

## v0.7.0

- Update dependencies: `ntpicture` v0.1.2, `ntcanvas` v0.1.2, `ntgpuimage` v0.5.0, `go-booba` v0.10.5, Bubble Tea v2.1.1, and Ultraviolet at `a45b7c86333f`; synchronize the library and demo modules.

- `nt3d.SetBindingLimit` and `AdoptBindingLimit(renderer)` raise the per-geometry storage limit above WebGPU's 128 MiB baseline on capable devices, up to 1 GiB, with points still capped at 16,777,216 a geometry; `Validate`, `MaxGaussians`, the new `PointLimit`, `MeshTriangleLimit`, and `MeshIndexLimit`, and the decoders' piece splitting follow it, and the GPU renderer refuses a geometry past its device's own limit with a clear frame error. The demos adopt the device's limit before loading.

- The software renderer blends transparent fragments with headroom above white, as the GPU's 16-bit float target does, clamping once per pixel instead of at every blend, so splats whose spherical harmonics evaluate above 1 match the GPU renderer where they overlap.

- `nt3d-palette` (`cmd/nt3d-palette`, built to `bin/`) reads PLY, compressed PLY, SPZ, or SOG, builds an SH palette with `BuildSHPalette`, and writes a SOG v2 bundle through the new `nt3dsog.Encode`, so later loads keep the palette and skip the k-means; a SOG that already holds one passes through unchanged. The bundle's images are PNG, not WebP.

----

## v0.6.0

- Gaussian splats can share their spherical harmonics through a palette (`Geometry.GaussianSHPalette` and `GaussianSHLabels`), drawn by both renderers: `nt3dmesh.BuildSHPalette` (or `Options.SHPalette = SHPaletteBuild` for PLY, compressed PLY, and SPZ) builds one by two-level k-means, taking a degree-3 splat's SH from 48 bytes on the GPU to 5, plus a palette of at most 2.9 MB per geometry; `w` in the meshes demo builds one in the background. A decoded SOG now keeps its palette, so its `GaussianSH` holds DC only: code that reads a SOG's `GaussianSH` must call `nt3d.ExpandSHPalette` or decode with `Options.SHPalette = SHPaletteExpand`.

----

## v0.5.0

- Gaussian clouds cluster at load and both renderers draw a budgeted subset each frame (`Frame.SplatBudget`, default one million), culled and thinned by projected size with coverage scaling, so the sort and draw cost follow the budget rather than the capture's size.

- Frames no longer re-validate scene-stored geometry every frame: a store-time certificate covers points and clusters, so a 2.5M-splat frame dropped from 67 to 17 ms and a 1M frame from 28 to 7 ms on an M5 Max.

----

## v0.4.0

- Gaussian splats store compactly on the GPU by default: half-float axes and byte spherical harmonics at SPZ's precision, so a geometry holds 2,796,202 degree-3 splats instead of 699,050 and a million-splat cloud takes about 92 MB instead of 260; `PointStyle.Precise` keeps full-precision records. Niantic's SPZ samples now open as one slab each.

- Blended points sort on the GPU every frame, globally across geometries to within one of 64 depth slices, with transparent meshes interleaved; the software renderer orders the same way. Slabs of a big cloud and overlapping clouds blend in order.

- `nt3dmesh` decodes PlayCanvas SOG (`.sog` bundles, unbundled `meta.json`, legacy v1) through `nt3d/mesh/sog`, and SuperSplat compressed PLY through `DecodePLY`, into the same Gaussian geometry as PLY and SPZ.

- Water adds touring/sport handling, progressive throttle and steering, jet trim, rider lean, and predictive autopilot control. Debug pages show hull probe immersion, force/velocity arrows, handling telemetry, and landing measurements, with paused single-step inspection.

- `BuoyantHull` supplies world-space buoyancy, water-relative drag, and empirical planing forces/torques to external rigid-body engines. The water demo integrates these at 120 Hz with stern-jet propulsion, steering, and a `p` planing comparison.

- `WaveSurface.WithWake` adds bounded crest/trough impulses with analytic normals, tangents, and velocities shared by mesh and world queries.
- The water craft now deforms its wake and creates landing disturbances; `z` compares displacement independently of foam and spray.

- `NewFlowMap` and material flow phase/strength controls advect normal maps with matching GPU/software shading and continuous dual-phase blending.
- `BuildWaveTiles` creates bounded, independently culled water tiles with matching shared borders and allocation-free geometry updates.
- Water demonstrates terrain-driven shoreline foam, bank-aligned flow, and 10×10 / 30×30 water extents (`v` / `m` comparisons).

- Scene color/depth capture for `Material.Transmission`, normal-driven `Refraction`, and RGB Beer–Lambert `Absorption`, with GPU/software parity and foreground rejection.
- `PointStyle.SoftDistance` fades blended particles against opaque and transmissive surface depth. Water demonstrates refractive shallows and soft spray with `t` / `h` comparisons.

- Water wake ribbons and ballistic spray reuse transparent meshes and soft point particles, with bounded pools, wave-following foam, turn spray, landing bursts, and an `f` comparison control.
- Wireframe hides zero-alpha blended particles, matching dormant particle pools in the other renderers.

- Water driving prototype: throttle, steering, autopilot, crest launches and ballistic landings with fixed-step physics; animated ripple normals and sky reflections with comparison toggles.
- `Material.Reflectivity`: Schlick Fresnel environment reflections in GPU and software, respecting normal maps and environment intensity/background-only settings.
- `NewShaderNormalMap` / `NewShaderNormalMapWithParameters`: procedural linear-data normal targets with mipmaps and matching software fallbacks.
- Shared Gerstner wave surfaces: analytic normals and velocities, world-coordinate surface queries, and a `water` demo with a four-probe float on the same animated surface in every renderer.

----

## v0.3.1

- Update dependencies: `ntpicture` v0.1.1 (no per-pixel allocations in Kitty NRGBA packing, legacy glyph cache kept across views), `ntcanvas` v0.1.1 (no grid reallocation on unchanged resizes), and `go-booba` v0.10.2 for the browser demos (OSC 52 clipboard from wasm, wheel events as SGR mouse buttons, Ctrl+V / Cmd+V paste).

## v0.3.0

- The widget's `PickMsg` carries the click's world-space `Ray`, for scenes that pick their own way (the AABB tree demo casts it through its tree).
- Add `nt3d/aabbtree`, a generic dynamic AABB tree (`Tree[T]`): insert, remove, and update boxed values, and query them by box overlap or ray, independent of the scene and renderers. [AABB tree demo](https://nimbleterminal.github.io/nt3d/aabb/).
- Tangent frames for normal maps: `Geometry.Tangents` carries authored frames (glTF `TANGENT`), scenes generate MikkTSpace-compatible ones at upload (`GenerateTangents`) for normal-mapped meshes without them, and both renderers interpolate, skin, and morph them (`MorphTarget.Tangents`); the per-triangle basis remains the fallback. glTF models without `TANGENT` now shade as they do with it (green up the image). Also fixes the GPU reading independent normal UVs as zeros.
- Read Niantic SPZ Gaussian splats (versions 2 to 4, zstd included) through `nt3d/mesh/spz` or `nt3d/mesh/formats`, about ten times smaller than PLY. Point limits now derive from the 128 MiB WebGPU binding (`MaxPoints`, `MaxGaussians`), and point clouds larger than one geometry split into slabs along their longest axis (`nt3dmesh.PointPieces`).
- Coverage-preserving mipmaps for cutouts: cutout textures get a mip chain whose alpha is rank-matched to full size, so distant fur and leaves stay dense instead of thinning or shimmering, on the GPU and in software. `Material.FullSize` keeps the old full-size sampling; `m` in the [fur demo](https://nimbleterminal.github.io/nt3d/fur/) compares.
- Mipmap shader textures: the GPU renderer regenerates their chain each frame from the picture the shader draws (`ntgpuimage` v0.4.0 render-target mips), and the software renderer mips the fallback image, so distant procedural floors stop shimmering.
- Add `nt3d/clod`, clustered LOD selection: cluster a large item set once, then cull and thin clusters by distance each frame on the CPU or in WebGPU compute. [Point cloud demo](https://nimbleterminal.github.io/nt3d/cloud/).
- Speed up software texture sampling while preserving interpolation results.
- Avoid expensive color conversion for eligible unlit textured pixels, with byte-identical output.

----

## v0.2.0

- Add reusable animation clips, playback and blending, morph targets, and CPU/GPU skinning, with glTF support. [Animation demo](https://nimbleterminal.github.io/nt3d/animation/).
- Add self-contained animation galleries with embedded CC0 [Khronos samples](https://nimbleterminal.github.io/nt3d/animate/) and [Gobkit characters](https://nimbleterminal.github.io/nt3d/characters/).
- Add gradient and image skies with diffuse image-based lighting. [Lights demo](https://nimbleterminal.github.io/nt3d/lights/).
- Add normal-map rendering in GPU and software renderers, including glTF materials. Toggle with `n` in the [lights demo](https://nimbleterminal.github.io/nt3d/lights/).
- Add directional shadow maps and soft drop shadows. [Lights](https://nimbleterminal.github.io/nt3d/lights/) and [forest](https://nimbleterminal.github.io/nt3d/forest/) demos.
- Add world-anchored labels and billboards. [Terrain demo](https://nimbleterminal.github.io/nt3d/terrain/).
- Avoid per-pixel allocations when copying textures.

----

## v0.1.0

- Initial release of the scene engine, WebGPU/software/wireframe renderers, mesh loaders, terminal widget, and web gallery.
