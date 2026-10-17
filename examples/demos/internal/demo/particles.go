// Copyright (c) 2026 Neomantra Corp.
package demo

import (
	"fmt"
	"image/color"
	"math"

	"nimbleterminal.dev/nt3d"
	"nimbleterminal.dev/nt3d/math"
)

// ParticlesMain shows a fire, its smoke, and drifting dust: points moved
// every frame, glowing additively or blended by alpha.
func ParticlesMain() { mainDemo(true, sceneParticles) }

// particleCounts are the counts n cycles through, of all three kinds.
var particleCounts = []int{1_000, 10_000, 100_000}

// particleBlends are the blendings b cycles through: the fire's, the
// smoke's, and the dust's.
var particleBlends = []struct {
	name              string
	fire, smoke, dust nt3d.PointBlend
}{
	{"fire additive, smoke alpha", nt3d.AdditiveBlend, nt3d.AlphaBlend, nt3d.AlphaBlend},
	{"all alpha", nt3d.AlphaBlend, nt3d.AlphaBlend, nt3d.AlphaBlend},
	{"all additive", nt3d.AdditiveBlend, nt3d.AdditiveBlend, nt3d.AdditiveBlend},
	{"opaque", nt3d.OpaqueBlend, nt3d.OpaqueBlend, nt3d.OpaqueBlend},
}

type particleScene struct {
	blend, count      int  // Indexes into particleBlends and particleCounts.
	hard              bool // Hard discs rather than soft.
	t, phase          float64
	fire, smoke, dust nt3d.NodeID
	buffer            []nt3d.Point // Reused each move: the scene copies what it is given.
}

func (d *demo) setupParticles() error {
	d.particles = &particleScene{count: 1}
	// A dark floor of two triangles, for the smoke to show against.
	const s = 3
	up := nt3dmath.Vec3{Z: 1}
	floor := nt3d.Geometry{Indices: []uint32{0, 1, 2, 0, 2, 3}}
	for _, p := range [][2]float32{{-s, -s}, {s, -s}, {s, s}, {-s, s}} {
		floor.Vertices = append(floor.Vertices, nt3d.Vertex{Position: nt3dmath.Vec3{X: p[0], Y: p[1]}, Normal: up, Color: gray(52)})
	}
	if _, err := d.scene.SetGeometry("floor", floor); err != nil {
		return err
	}
	ps := d.particles
	for _, node := range []struct {
		name string
		id   *nt3d.NodeID
	}{{"fire", &ps.fire}, {"smoke", &ps.smoke}, {"dust", &ps.dust}} {
		var err error
		if *node.id, err = d.scene.Add(nt3d.Root, node.name); err != nil {
			return err
		}
	}
	return d.moveParticles()
}

// particleRandom is a fixed pseudo-random number in [0, 1), particle i's
// k-th: particles need no state, only their index and the time.
func particleRandom(i, k int) float64 {
	h := uint32(i)*0x9e3779b1 ^ uint32(k)*0x85ebca77
	h ^= h >> 15
	h *= 0x2c1b3c6d
	h ^= h >> 12
	h *= 0x297a2d39
	h ^= h >> 15
	return float64(h) / (1 << 32)
}

// particleAge is how far through its life of the given seconds particle i
// is at time t, 0 to 1; each starts at its own moment.
func particleAge(i int, t, life float64) float64 {
	return math.Mod(t/life+particleRandom(i, 0), 1)
}

// mixRGBA is a to b by f, 0 to 1.
func mixRGBA(a, b color.RGBA, f float64) color.RGBA {
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*f) }
	return color.RGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), m(a.A, b.A)}
}

func vec(x, y, z float64) nt3dmath.Vec3 {
	return nt3dmath.Vec3{X: float32(x), Y: float32(y), Z: float32(z)}
}

// fireParticle is the fire's particle i at time t: rising from a disc,
// narrowing and flickering, cooling from yellow to deep red, and fading.
func fireParticle(i int, t float64) nt3d.Point {
	a := particleAge(i, t, .7+.6*particleRandom(i, 1))
	r, angle := .3*math.Sqrt(particleRandom(i, 2)), 2*math.Pi*particleRandom(i, 3)
	spread := 1 - .75*a
	sway := .04 * math.Sin(t*6+float64(i))
	c := mixRGBA(color.RGBA{255, 220, 110, 255}, color.RGBA{230, 50, 10, 255}, a)
	c.A = uint8(220 * (1 - a))
	return nt3d.Point{Position: vec(r*math.Cos(angle)*spread+sway, r*math.Sin(angle)*spread, 1.2*a*(.7+.3*particleRandom(i, 4))), Radius: float32(.07 * (1 - .5*a)), Color: c}
}

// smokeParticle is the smoke's particle i at time t: rising above the fire,
// spreading and growing, carried by a breeze, fading in and out.
func smokeParticle(i int, t float64) nt3d.Point {
	a := particleAge(i, t, 3.5+1.5*particleRandom(i, 1))
	r, angle := (.08+.5*a)*math.Sqrt(particleRandom(i, 2)), 2*math.Pi*particleRandom(i, 3)
	v := uint8(70 + 50*particleRandom(i, 4))
	fade := min(1, a*6) * (1 - a)
	return nt3d.Point{Position: vec(r*math.Cos(angle)+.35*a*a, r*math.Sin(angle), .9+2*a), Radius: float32(.1 + .25*a), Color: color.RGBA{v, v, v, uint8(110 * fade)}}
}

// dustParticle is mote i at time t, drifting slowly through the air.
func dustParticle(i int, t float64) nt3d.Point {
	x := 4*particleRandom(i, 1) - 2 + .2*math.Sin(t*.3+6*particleRandom(i, 2))
	y := 4*particleRandom(i, 3) - 2 + .2*math.Cos(t*.25+6*particleRandom(i, 4))
	z := math.Mod(1.6*particleRandom(i, 5)+t*.05*(.5+particleRandom(i, 6)), 1.6)
	return nt3d.Point{Position: vec(x, y, z), Radius: 1.5, Color: color.RGBA{220, 200, 160, 150}}
}

// moveParticles sets each kind's points as they are at the scene's time,
// in the blending, shape, and count chosen: half fire, a third smoke, the
// rest dust. Fire and smoke are sized in world units, dust in pixels.
func (d *demo) moveParticles() error {
	ps := d.particles
	n, b := particleCounts[ps.count], particleBlends[ps.blend]
	shape := nt3d.SoftPoint
	if ps.hard {
		shape = nt3d.HardPoint
	}
	for _, k := range []struct {
		id    nt3d.NodeID
		n     int
		style nt3d.PointStyle
		at    func(int, float64) nt3d.Point
	}{
		{ps.fire, n / 2, nt3d.PointStyle{Shape: shape, Units: nt3d.WorldUnits, Blend: b.fire}, fireParticle},
		{ps.smoke, n / 3, nt3d.PointStyle{Shape: shape, Units: nt3d.WorldUnits, Blend: b.smoke}, smokeParticle},
		{ps.dust, n - n/2 - n/3, nt3d.PointStyle{Shape: shape, Blend: b.dust}, dustParticle},
	} {
		ps.buffer = ps.buffer[:0]
		for i := range k.n {
			ps.buffer = append(ps.buffer, k.at(i, ps.t))
		}
		if err := d.scene.SetNodeGeometry(k.id, nt3d.Geometry{PointStyle: k.style, Points: ps.buffer}); err != nil {
			return err
		}
	}
	return nil
}

// stepParticles moves the particles on by the time since the last step.
func (d *demo) stepParticles() error {
	// The demo's phase wraps at 2π; the particles keep their own clock.
	step := math.Mod(d.phase-d.particles.phase+2*math.Pi, 2*math.Pi)
	d.particles.phase = d.phase
	d.particles.t += step
	return d.moveParticles()
}

func (d *demo) cycleParticleBlend() error {
	d.particles.blend = (d.particles.blend + 1) % len(particleBlends)
	return d.moveParticles()
}

func (d *demo) toggleParticleShape() error {
	d.particles.hard = !d.particles.hard
	return d.moveParticles()
}

func (d *demo) cycleParticleCount() error {
	d.particles.count = (d.particles.count + 1) % len(particleCounts)
	return d.moveParticles()
}

func (d *demo) particlesView(content string) string {
	ps := d.particles
	shape := "soft"
	if ps.hard {
		shape = "hard"
	}
	return fmt.Sprintf("nt3d particles · b blend · s shape · n count · a pause · d fog · 1/2/3 backend · q quit\n%s\n%s · %s · %s · %dk particles · drag orbit · wheel zoom · r rotate · %s",
		content, d.rendering(), particleBlends[ps.blend].name, shape, particleCounts[ps.count]/1000, d.status)
}
