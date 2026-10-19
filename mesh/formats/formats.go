// Copyright (c) 2026 Neomantra Corp.

// Package formats is a batteries-included, import-for-side-effect helper
// that registers every optional mesh format with nt3dmesh, and the image
// formats their textures may use:
//
//	import _ "nimbleterminal.dev/nt3d/mesh/formats"
//
// nt3dmesh reads STL, OBJ, 3MF, PLY, and Stanford .conf itself, with PNG and
// JPEG textures. This adds glTF (.gltf, .glb) and WebP textures, as glTF's
// EXT_texture_webp uses, and SPZ Gaussian splats (.spz). Import individual
// formats instead when only some are needed.
package formats

import (
	_ "golang.org/x/image/webp" // WebP textures.

	_ "nimbleterminal.dev/nt3d/mesh/gltf" // glTF.
	_ "nimbleterminal.dev/nt3d/mesh/spz"  // SPZ Gaussian splats.
)
