// Copyright (c) 2026 Neomantra Corp.

package demo

import (
	"embed"
	"fmt"
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d"
	nt3dmath "nimbleterminal.dev/nt3d/math"
)

//go:embed roomassets/*.hdr
var roomAssets embed.FS

// RoomMain compares local probe reflections and supplied irradiance lightmaps.
func RoomMain() { mainDemo(false, sceneRoom) }

type roomSurface struct {
	id       nt3d.NodeID
	material nt3d.Material
}
type roomScene struct {
	probes, lightmaps bool
	probe             nt3d.ReflectionProbe
	surfaces          []roomSurface
	sphere            nt3d.NodeID
}

func roomCamera(c *nt3d.Camera) {
	c.Alpha, c.Beta, c.Distance, c.Projection = 20, -75, 7.5, nt3d.Perspective
	c.Target = nt3dmath.Vec3{Z: .8}
}

// roomQuad has a dedicated atlas UV channel even though it needs no color UVs.
func roomQuad(origin, u, v nt3dmath.Vec3, c color.RGBA, m *nt3d.Material) nt3d.Geometry {
	n := u.Cross(v).Normalize()
	return nt3d.Geometry{
		Vertices: []nt3d.Vertex{{Position: origin, Normal: n, Color: c}, {Position: origin.Add(u), Normal: n, Color: c}, {Position: origin.Add(u).Add(v), Normal: n, Color: c}, {Position: origin.Add(v), Normal: n, Color: c}},
		Indices:  []uint32{0, 1, 2, 0, 2, 3}, LightMapUV: []nt3d.UV{{U: 0, V: 1}, {U: 1, V: 1}, {U: 1, V: 0}, {U: 0, V: 0}}, Material: m,
	}
}

func (d *demo) setupRoom() error {
	read := func(name string) []byte { b, _ := roomAssets.ReadFile("roomassets/" + name); return b }
	floor, err := nt3d.NewHDRLightMap(read("floor.hdr"))
	if err != nil {
		return err
	}
	wall, err := nt3d.NewHDRLightMap(read("wall.hdr"))
	if err != nil {
		return err
	}
	env, err := nt3d.NewHDREnvironment(read("room.hdr"))
	if err != nil {
		return err
	}
	r := &roomScene{probes: true, lightmaps: true}
	d.room = r
	var bounds nt3dmath.AABB
	bounds.Include(nt3dmath.Vec3{X: -2, Y: -1.5})
	bounds.Include(nt3dmath.Vec3{X: 2, Y: 1.5, Z: 2.4})
	r.probe = nt3d.ReflectionProbe{Environment: env, Position: nt3dmath.Vec3{Z: 1}, Bounds: bounds}
	// A neutral global source makes the local-vs-distant comparison easy to see.
	d.scene.SetEnvironment(nt3d.GradientEnvironment(gray(90), gray(120), gray(40)))
	if err := d.scene.SetReflectionProbes([]nt3d.ReflectionProbe{r.probe}); err != nil {
		return err
	}
	for _, s := range []struct {
		name    string
		p, u, v nt3dmath.Vec3
		c       color.RGBA
		lm      *nt3d.LightMap
	}{
		{"floor", nt3dmath.Vec3{X: -2, Y: -1.5}, nt3dmath.Vec3{X: 4}, nt3dmath.Vec3{Y: 3}, gray(220), floor},
		{"back wall", nt3dmath.Vec3{X: -2, Y: 1.5}, nt3dmath.Vec3{X: 4}, nt3dmath.Vec3{Z: 2.4}, gray(220), wall},
		{"warm wall", nt3dmath.Vec3{X: -2, Y: -1.5}, nt3dmath.Vec3{Y: 3}, nt3dmath.Vec3{Z: 2.4}, color.RGBA{210, 90, 55, 255}, wall},
		{"cool wall", nt3dmath.Vec3{X: 2, Y: 1.5}, nt3dmath.Vec3{Y: -3}, nt3dmath.Vec3{Z: 2.4}, color.RGBA{60, 120, 210, 255}, wall},
	} {
		m := nt3d.Material{PBR: true, Roughness: .85, LightMap: s.lm}
		id, err := d.scene.SetGeometry(s.name, roomQuad(s.p, s.u, s.v, s.c, &m))
		if err != nil {
			return err
		}
		r.surfaces = append(r.surfaces, roomSurface{id, m})
	}
	// Visible panels match the authored panorama, but are not runtime area lights.
	for i, c := range []color.RGBA{{255, 183, 92, 255}, {100, 190, 255, 255}} {
		x := float32(-1.65)
		if i == 1 {
			x = .55
		}
		m := &nt3d.Material{Unlit: true, Emissive: c, EmissiveIntensity: 2}
		_, err = d.scene.SetGeometry(fmt.Sprintf("panel %d", i), roomQuad(nt3dmath.Vec3{X: x, Y: 1.49, Z: 1.35}, nt3dmath.Vec3{X: 1.1}, nt3dmath.Vec3{Z: .7}, c, m))
		if err != nil {
			return err
		}
	}
	g, err := nt3d.BuildSphere(nt3d.Sphere{Radius: .42, Color: gray(235), Material: &nt3d.Material{PBR: true, Metallic: 1, Roughness: .12}})
	if err != nil {
		return err
	}
	r.sphere, err = d.scene.SetGeometry("moving polished sphere", g)
	if err != nil {
		return err
	}
	g, err = nt3d.BuildCuboid(nt3d.Cuboid{Size: nt3dmath.Vec3{X: .65, Y: .65, Z: .65}, Color: gray(190), Material: &nt3d.Material{PBR: true, Metallic: .85, Roughness: .4}})
	if err != nil {
		return err
	}
	id, err := d.scene.SetGeometry("rough metal pedestal", g)
	if err != nil {
		return err
	}
	if err = d.scene.SetTransform(id, nt3dmath.Translation(nt3dmath.Vec3{Y: .6, Z: .325})); err != nil {
		return err
	}
	return d.stepRoom()
}

func (d *demo) stepRoom() error {
	return d.scene.SetTransform(d.room.sphere, nt3dmath.Translation(nt3dmath.Vec3{X: float32(math.Sin(d.animationTime*.65)) * 1.2, Y: -.45, Z: .5}))
}

func (d *demo) roomKey(key string) (bool, error) {
	r := d.room
	switch key {
	case "p":
		r.probes = !r.probes
		var probes []nt3d.ReflectionProbe
		if r.probes {
			probes = []nt3d.ReflectionProbe{r.probe}
		}
		return true, d.scene.SetReflectionProbes(probes)
	case "l":
		r.lightmaps = !r.lightmaps
		for _, s := range r.surfaces {
			m := s.material
			if !r.lightmaps {
				m.LightMap = nil
			}
			if err := d.scene.SetMaterial(s.id, &m); err != nil {
				return true, err
			}
		}
		return true, nil
	}
	return false, nil
}

func (d *demo) decorateRoom(f *nt3d.Frame) {
	f.Background = color.RGBA{18, 22, 30, 255}
	f.EnvironmentBackgroundOnly = true
	f.Lighting = nt3d.Lighting{Ambient: nt3d.Ambient{Sky: gray(255), Ground: gray(255), Intensity: .18}, Lights: []nt3d.Light{{Direction: nt3dmath.Vec3{X: 1, Y: -2, Z: 3}, Intensity: .35}}}
	f.PostProcess = nt3d.PostProcess{ToneMap: nt3d.ACESToneMap}
}

func (d *demo) roomView(content string) string {
	return fmt.Sprintf("nt3d room · p local probes · l lightmaps · a pause · 1/2/3 backend · q quit\n%s\n%s · probes %t · lightmaps %t · supplied synthetic HDR fixtures · drag orbit · wheel zoom · c fit · %s", content, d.rendering(), d.room.probes, d.room.lightmaps, d.status)
}
