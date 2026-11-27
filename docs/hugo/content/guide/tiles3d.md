---
title: "3D Tiles"
weight: 84
---

# 3D Tiles

`nt3d/tiles3d` (package `nt3dtiles3d`) reads [OGC 3D Tiles 1.1](https://docs.ogc.org/cs/22-025r4/22-025r4.html): a tree of tiles, coarse at the root and finer toward the leaves, that lets a city too large for one file be loaded a piece at a time. Each tile has bounds, a geometric error that says how wrong it is to stop there, and content, which is glTF.

The package reads and describes. It draws nothing, and fetches only through a `Fetcher` you give it. It imports `nt3d/math`, `nt3d/geo`, and the standard library, so a tool that checks tilesets, or a server that writes them, can use it without a renderer.

Positions are Earth-centered meters in `float64`; [Geographic scenes](../geo/) covers that frame and how to draw in a local one.

## Reading a tileset

```go
fetch := nt3dtiles3d.FS(os.DirFS("city"))          // or &nt3dtiles3d.HTTP{}
data, err := fetch.Fetch(ctx, "tileset.json")
tileset, err := nt3dtiles3d.ParseTileset(data, nil)
```

A `Tileset` is plain data: `Asset`, `GeometricError`, `Root`, and the metadata. A `Tile` has:

| Field | Meaning |
| --- | --- |
| `Transform` | Carries the tile, its content and its children into its parent's space; the identity when the file has none. The product down from the root reaches Earth-centered meters. |
| `Bounds` | A `Volume` holding the tile's content and its children's: a box, a region, or a sphere |
| `GeometricError` | The error, in meters, of drawing this tile and not its children |
| `Refine` | `RefineReplace`, `RefineAdd`, or `RefineInherit` for a tile that does not say: it refines as its parent does, and a root that does not say replaces |
| `Contents` | What the tile draws, each with a `URI`; none for a tile that only groups others |
| `Children` | The finer tiles |
| `Implicit` | Set on the root of an [implicit tree](#implicit-tiling) |
| `Metadata` | The tile's metadata, or nil |

What the specification requires is required: an asset of version 1.0 or 1.1, a geometric error and a bounding volume on every tile, finite numbers, and a transform that is affine and can be inverted. A tileset that lists an extension under `extensionsRequired` that the package does not read is refused. The 1.0 spellings are read too: `content.url`, lower-case `refine`, and the `3DTILES_implicit_tiling` and `3DTILES_multiple_contents` extensions that became part of 1.1.

A content's URI is relative to the tileset that names it. `Resolve(base, ref)` makes it absolute, for URLs and for plain paths alike:

```go
uri, err := nt3dtiles3d.Resolve("https://tiles.example/city/tileset.json", content.URI)
```

## Bounds

`Volume.World(toECEF)` returns a tile's bounds as an oriented box (`nt3dmath.OBBd`) in Earth-centered meters, given the product of the tile's transform and its ancestors'. It is what a selection measures its distance to.

| Kind | In the file | `World` |
| --- | --- | --- |
| `VolumeBox` | a center and three half axes, in the tile's space | carried through the transform |
| `VolumeRegion` | west, south, east, north in radians, and two heights | already on the Earth: the transform is ignored, as the specification says, and the box holds the curved surface ([`Region.Box`](../geo/#bounding-regions)) |
| `VolumeSphere` | a center and a radius, in the tile's space | the cube around the sphere, so a distance to it is never more than the sphere's |

## Implicit tiling

A tree of a million tiles is not written out. An implicit root says how it divides (`Quadtree` in four, `Octree` in eight) and every tile below follows from its coordinates, a `Coord` of level, X, Y, and Z:

```go
implicit := tileset.Root.Implicit
c := nt3dtiles3d.Coord{Level: 2, X: 3, Y: 0}

implicit.Bounds(c)          // the root's volume, divided
implicit.GeometricError(c)  // the root's, halved each level
implicit.ContentURIs(c)     // the root's content templates, with {level} {x} {y} {z} filled in
implicit.Children(c)        // the four or eight coordinates below
```

Which tiles exist is kept in subtree files, each covering `SubtreeLevels` levels:

```go
root := implicit.SubtreeRoot(c)
data, err := fetch.Fetch(ctx, resolve(implicit.SubtreeURI(root)))
subtree, err := nt3dtiles3d.ParseSubtree(data, implicit, root, nil, nil)

subtree.TileAvailable(c)          // does the tile exist
subtree.ContentAvailable(c, 0)    // and has it its first content
subtree.ChildSubtreeAvailable(d)  // is there a subtree file starting at d, one level below this one's last
```

`ParseSubtree` reads the binary form and the JSON form. A subtree that keeps its availability in a third file names it by URI; pass a function that fetches it, relative to the subtree file, as the fourth argument.

## Metadata

`Tileset.Schema` describes the classes of metadata and their properties, and `Entity` values on the tileset, its groups, tiles, contents, and subtrees carry them, with properties as the JSON gives them. A schema kept in a file of its own is named by `Tileset.SchemaURI` and read with `ParseSchema`. Per-feature metadata inside glTF (`EXT_structural_metadata`, `EXT_mesh_features`) and the binary property tables of subtrees are not read yet.

## Content

Tile content is glTF, as a `.glb` or as JSON, which [`nt3d/mesh/gltf`](../meshes/) decodes, [Gaussian splats](../rendering/#gaussian-splats-in-gltf) included. `SniffContent` tells what a fetched content is: `ContentGLB`, `ContentGLTF`, `ContentB3DM`, or `ContentTileset` for a tileset whose root continues the tile.

`UnwrapB3DM` returns the glb inside a legacy batched model, and its `RTC_CENTER` when it has one: a center the glb's positions are relative to. Add it to the tile's transform in `float64`, and the positions stay small enough for `float32`. The other legacy formats (`i3dm`, `pnts`, `cmpt`) are not read.

## Fetching

A `Fetcher` returns the bytes a URI names. Everything outside the program is reached through one, so a cache, an archive, or a test is a `Fetcher` too.

- `FS(fsys)` reads paths from an `fs.FS`: a directory, or files embedded in the program.
- `HTTP` asks a web server, natively and in a browser build. `Prepare` sees each request before it is sent; `Bearer(token, origin)` is a `Prepare` that sends a token only to `https` URLs on the tileset's own host, so a tile that links to another server is fetched without it. A redirect is treated as a new request: the `Authorization` and `Cookie` headers of the request before are taken off and `Prepare` decides again, so a token never follows a redirect to plain `http` or to another host. `MaxBytes` limits a response.
- `FetcherFunc` makes a function a `Fetcher`.

## Limits and untrusted files

Every document is untrusted. `Options` limits what a parse accepts (`MaxBytes`, `MaxTiles`, `MaxDepth`; zero means the default), and a document over a limit returns an error that wraps `ErrTooLarge`. No input panics: the tileset, subtree, and b3dm readers each have a fuzz target.

Credits are text from a file, shown on screen. `Asset.Credits()` returns the tileset's `copyright` and its `extras.attribution` (a string or a list of them), each cut to one line of printable characters.
