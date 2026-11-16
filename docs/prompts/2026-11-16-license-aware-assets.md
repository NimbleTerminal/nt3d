# Prompt: license-aware assets

Notes for a future nt3d feature. It is not designed yet; this collects the
requirements as they came up while importing commercial marketplace kits
into a game built on nt3d.

## Why

Purchased 3D assets come with license terms that differ per asset, and
sometimes per kit within one project. A common shape (Fab Standard
License, for example):

- **Use:** commercial use is allowed. Distribution is allowed only as part
  of a project, and the end user must be prevented from extracting the
  asset.
- **No redistribution:** no standalone redistribution, and no editor or
  level tools that let others export the asset.
- **No copyleft:** no combining with copyleft content (GPL, LGPL, CC
  BY-SA).
- **"NoAI":** some assets are tagged NoAI. They may not be used in datasets
  for, the development of, or as training inputs to generative AI programs.
  The definition excludes programs that only arrange existing content, so
  an AI placing assets is fine. Showing renders of NoAI assets to an AI is
  a grey zone, and cautious projects avoid it.

Today the policy lives outside the engine, in a project's import manifest.
The scene has no idea which nodes are restricted. So every consumer that
sends pixels or geometry somewhere has to re-derive the policy:

- an AI companion's snapshot tool;
- a screenshot-sharing feature;
- an export command;
- a web build that serves files.

That is easy to get wrong.

## What to add (sketch)

1. **Provenance on assets and nodes.**
   - A small, extensible policy record on `SceneAsset` / `AssetNode`, or as
     scene node metadata. It holds an opaque source id (kit or vendor),
     license name, attribution text, and boolean capabilities such as
     `AIViewable`, `Exportable` and `Redistributable`.
   - Inherited by instantiated subtrees and preserved through
     `SharedGeometry`, LOD tiers and material variants.
   - Absent means unrestricted: today's behaviour stays exactly as is.
2. **Capture policies.** A capture or frame option, e.g.
   `Frame.Purpose` or a per-render `RestrictionPolicy`, that says who the
   pixels are for. Restricted nodes are then:
   - **Omitted**, i.e. drawn as if hidden; or
   - **Stand-ins**, drawn with a proxy. Possible proxies:
     - the node's bounds box;
     - an author-supplied placeholder mesh (a "downgrade" model attached to
       the asset);
     - the real mesh with a neutral, untextured material.

     The real mesh with a neutral material keeps layout readable without
     showing the artwork. Whether silhouettes alone count as showing the
     asset is a policy question, so the choice of proxy is the
     application's.

   Picking, physics and stats are unaffected: the policy only changes what
   the capture draws. One frame can serve a human (full) and a model
   (stand-ins) with two renders. That needs to be cheap: share the culling
   and swap materials and geometry per node.
3. **Reports.** `FrameStats` (or the capture result) says how many nodes
   were omitted or stood in, so a tool can tell its user that some items
   are hidden by license.
4. **Export guards.** `nt3d/mesh` writers (glTF, OBJ, STL and so on) and any
   scene-to-file exporter refuse nodes whose policy forbids export, with a
   clear error naming the source.
5. **Packing helpers (maybe separate).** A simple packed asset container,
   compressed and lightly scrambled, plus a loader. Web builds can then ship
   restricted assets inside the project instead of as raw glTF files a user
   could save. This is not DRM, just "not trivially extractable".

## Out of scope / open questions

- Legal interpretation: the engine enforces flags it is given, nothing more.
- Whether policy also travels into glTF `extras` on import and export, so
  marketplace metadata survives round trips.
- Audit: a log of what was captured, under which policy, for which consumer.
- Splats and point clouds: the same policy applies to Gaussian splat assets.
