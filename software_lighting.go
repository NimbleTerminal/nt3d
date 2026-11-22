// Copyright (c) 2026 Neomantra Corp.

package nt3d

// light composes diffuse light, transmission, then highlights. The sampled
// light map and ambient occlusion belong to this fragment alone.
func (tri *softwareTriangle) light(s *surfaceSample, x, y int) {
	pass, m := tri.pass, tri.material
	f := pass.frame
	rig := pass.rig
	if (m != nil && m.LightMap != nil) || pass.ao != nil {
		local := *rig
		rig = &local
		if m != nil && m.LightMap != nil {
			baked := tri.sampleMap(s, m.LightMap.texture, false, tri.a.lightMapUV, tri.b.lightMapUV, tri.c.lightMapUV)
			rig.bakedDiffuse = lightMapDiffuse(baked, f.Linear)
		}
		if pass.ao != nil && !tri.blended && (m == nil || m.Transmission.Amount == 0) {
			rig.screenOcclusion = 1 - pass.ao[y*f.Width+x]
		}
	}
	if tri.pbr {
		orm := [3]float32{1, 1, 1}
		if m.PBR.ORMMap != nil {
			channels := tri.sampleMap(s, m.PBR.ORMMap, m.PBR.ORMRepeat, tri.a.uv, tri.b.uv, tri.c.uv)
			orm = [3]float32{channels[0], channels[1], channels[2]}
		}
		base := [3]float32{s.rgb[0] / 255, s.rgb[1] / 255, s.rgb[2] / 255}
		diffuse, specular := pbrShade(base, rig, f, m, orm, s.normal, s.world, s.geometric, f.viewAt(s.world))
		s.rgb = [3]float32{diffuse[0] * 255, diffuse[1] * 255, diffuse[2] * 255}
		tri.transmit(s)
		for k := range s.rgb {
			s.rgb[k] += specular[k] * 255
		}
		return
	}
	var rim [3]float32
	if tri.toon {
		rim = rig.toonLight(&s.rgb, m.Toon, s.normal, s.world, s.geometric, f.viewAt(s.world))
	} else {
		rig.lightNormal(&s.rgb, s.normal, s.world, s.geometric)
	}
	tri.transmit(s)
	if tri.reflective {
		reflectSceneEnvironment(&s.rgb, f, s.world, m.Phong.Reflectivity, s.normal, f.viewAt(s.world), 1-rig.screenOcclusion)
	}
	if tri.specular {
		var highlight [3]float32
		if tri.toon {
			highlight = rig.toonHighlight(m, s.normal, s.world, f.viewAt(s.world), s.geometric)
		} else {
			highlight = rig.highlightNormal(m, s.normal, s.world, f.viewAt(s.world), s.geometric)
		}
		for k := range s.rgb {
			s.rgb[k] += highlight[k] * 255
		}
	}
	if tri.toon && m.Toon.Rim > 0 {
		for k := range s.rgb {
			s.rgb[k] += rim[k] * 255
		}
	}
}

func (tri *softwareTriangle) transmit(s *surfaceSample) {
	pass, m := tri.pass, tri.material
	if m != nil && m.Transmission.Amount > 0 && pass.opaqueColor != nil {
		transmitSoftware(&s.rgb, m, s.normal, pass.right, pass.up, s.px, s.py, s.depth, pass.frame.Width, pass.frame.Height, pass.inverseScene, pass.opaqueColor, pass.opaqueDepth, pass.dst.fromPixel)
	}
}

func (tri *softwareTriangle) emit(s *surfaceSample) {
	em := [3]float32{1, 1, 1}
	if m := tri.material; m.Emission.Map != nil {
		channels := tri.sampleMap(s, m.Emission.Map, m.Emission.Repeat, tri.a.uv, tri.b.uv, tri.c.uv)
		for k := range em {
			em[k] = channels[k]
			if !tri.pass.frame.Linear {
				em[k] = float32(encodeSRGB(float64(em[k])))
			}
		}
	}
	for k := range s.rgb {
		s.rgb[k] += tri.emissiveMul[k] * em[k] * 255
	}
}
