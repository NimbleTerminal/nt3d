# flatiron.obj

A block of extruded buildings, a ground quad and road centerlines near
Flatiron, Manhattan, baked from OpenStreetMap data for the demos.

Data © OpenStreetMap contributors, available under the Open Database License
(ODbL): https://www.openstreetmap.org/copyright

This is geographic data, distinct from the repository's software license. A
demo that shows it must display "© OpenStreetMap contributors".

- Bounds: south 40.7400, west -73.9910, north 40.7430, east -73.9870
- Downloaded: 2026-10-18, from
  https://api.openstreetmap.org/api/0.6/map.json?bbox=-73.9910,40.7400,-73.9870,40.7430
- Baked: 2026-11-09 from the offline extract ntmaps bundles
  (`examples/internal/cityview/neighborhood.json`; see
  `examples/cityview3d/DATA.md` there)

## Contents

- `ground`: one quad just below z = 0.
- `building <osm id>`: one object per building or building part (142), so
  each can be recolored on its own.
- `roads`: two-point polylines (1,001 segments) clipped to the bounds.

Coordinates are meters, +X east, +Y north, +Z up, with the origin on the
ground at the center of the bounds. Vertices carry ntmaps' viewer colors
(orange for a tagged height, blue for a floor-count estimate, gray for the
9 m default) in the r g b extension. Faces have no normals; they are flat and
wind counterclockwise seen from outside. Heights are estimates from tags, with
flat roofs and no terrain; courtyard relations are omitted.

## Regenerating

From a checkout of ntmaps that has `cityview3d -export`:

    go -C examples run ./cityview3d -export /path/to/nt3d/examples/demos/internal/demo/models/flatiron.obj

The output is byte-identical for the same bundled extract.
