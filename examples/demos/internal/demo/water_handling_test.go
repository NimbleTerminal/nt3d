package demo

import (
	"math"
	"nimbleterminal.dev/nt3d"
	"strings"
	"testing"
)

func TestWaterHandlingResponse(t *testing.T) {
	flat := &nt3d.WaveSurface{}
	for preset := range waterHandlingPresets {
		c := waterCraft{bound: 1000, throttle: .65, preset: preset}
		if err := c.advance(flat, 1.0/120); err != nil {
			t.Fatal(err)
		}
		if c.power <= 0 || c.power >= c.throttle {
			t.Fatal("throttle was not progressive", c.power)
		}
		if err := c.advance(flat, 5); err != nil {
			t.Fatal(err)
		}
		straight := c
		c.turn = .7
		if err := c.advance(flat, 7); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s speed %.2f yaw %.2f bank %.1f slip %.1f", c.handling().name, c.speed, c.telemetry.yawRate, c.telemetry.bank, c.telemetry.slip)
		if c.rudder != .7 || c.telemetry.yawRate <= .1 || c.telemetry.bank >= 0 || c.pose()[10] < .8 {
			t.Fatal("turn did not produce stable left lean", c)
		}
		// Trim must affect pitch through torque, while preserving forward travel.
		trimmed := straight
		trimmed.trim = .15
		if err := trimmed.advance(flat, 7); err != nil {
			t.Fatal(err)
		}
		if err := straight.advance(flat, 7); err != nil {
			t.Fatal(err)
		}
		if trimmed.telemetry.pitch <= straight.telemetry.pitch || trimmed.speed <= 1 {
			t.Fatal("bow-up trim ineffective", trimmed.telemetry.pitch, straight.telemetry.pitch)
		}
		c.throttle = 0
		c.turn = 0
		before := c.speed
		if err := c.advance(flat, 10); err != nil {
			t.Fatal(err)
		}
		if c.power != 0 || c.rudder != 0 || c.speed >= before || math.IsNaN(c.z) {
			t.Fatal("coasting failed", c)
		}
	}
}

func TestWaterDebugAndTuningPreserveState(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	d.animationTime = 3
	if err := d.stepWater(); err != nil {
		t.Fatal(err)
	}
	before := d.water.craft
	for i := range 3 {
		if _, err := d.waterKey("b"); err != nil {
			t.Fatal(err)
		}
		if d.water.craft != before {
			t.Fatal("debug changed physics")
		}
		n, ok := d.scene.Node(d.water.debugNode)
		if !ok {
			t.Fatal("missing debug node")
		}
		if n.Visible != (i != 2) {
			t.Fatal("debug visibility", n.Visible)
		}
		if i < 2 {
			f, err := d.scene.Frame(nt3d.DefaultCamera(), 80, 60)
			if err != nil {
				t.Fatal(err)
			}
			renderer := nt3d.NewSoftwareRenderer()
			_, err = renderer.Render(f)
			renderer.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		if i < 2 && !strings.Contains(d.waterDebugText(), "touring") {
			t.Fatal("missing telemetry")
		}
	}
	for _, key := range []string{"u", "]", "j", "k"} {
		if _, err := d.waterKey(key); err != nil {
			t.Fatal(err)
		}
	}
	c := d.water.craft
	if c.time != before.time || c.pose() != before.pose() || c.vx != before.vx || c.preset != 1 || c.turn != 0 || c.autopilot || c.trim <= 0 {
		t.Fatal("tuning altered body state or controls failed")
	}
	for range 30 {
		d.waterKey("]")
	}
	if d.water.craft.trim != .15 {
		t.Fatal("trim is not bounded")
	}
}

func TestWaterProbeTelemetry(t *testing.T) {
	c := waterCraft{}
	if err := c.advance(&nt3d.WaveSurface{}, 1); err != nil {
		t.Fatal(err)
	}
	total := 0.0
	for _, p := range c.telemetry.probes {
		total += p.wet
		if p.surface.Z != 0 {
			t.Fatal("wrong probe surface")
		}
	}
	if math.Abs(total/4-float64(c.telemetry.water.WetFraction)) > 1e-5 || math.Abs(float64(c.telemetry.net.Z)) > .001 {
		t.Fatal("telemetry does not match forces", c.telemetry)
	}
}

func TestWaterHandlingWaveCircuit(t *testing.T) {
	for preset := range waterHandlingPresets {
		d := &demo{scene: nt3d.NewScene()}
		if err := d.setupWater(); err != nil {
			t.Fatal(err)
		}
		c := d.water.craft
		c.preset = preset
		maxRadius, bumpers := 0.0, 0
		for i := 1; i <= 120*30; i++ {
			if err := c.advance(d.water.surface, float64(i)/120); err != nil {
				t.Fatal(err)
			}
			maxRadius = math.Max(maxRadius, math.Hypot(c.x, c.y))
			if math.Abs(c.x) >= c.bound || math.Abs(c.y) >= c.bound {
				bumpers++
			}
			if c.pose()[10] < .7 || math.IsNaN(c.z) || math.Abs(c.z) > 3 {
				t.Fatalf("unstable %s at %.2f: %+v", c.handling().name, c.time, c)
			}
		}
		if bumpers > 120 {
			t.Fatal("autopilot spent too long on bumpers", bumpers)
		}
		t.Logf("%s radius %.2f bumper steps %d speed %.2f launches %d landings %d", c.handling().name, maxRadius, bumpers, c.speed, c.launches, c.landings)
	}
}

func TestWaterDebugStepAndReset(t *testing.T) {
	d := &demo{scene: nt3d.NewScene()}
	if err := d.setupWater(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.waterKey("."); err != nil {
		t.Fatal(err)
	}
	if math.Abs(d.water.craft.time-1.0/120) > 1e-9 || d.animationTime != d.water.craft.time {
		t.Fatal("wrong paused step")
	}
	before := d.water.craft
	d.animate = true
	if _, err := d.waterKey("."); err != nil {
		t.Fatal(err)
	}
	if before != d.water.craft {
		t.Fatal("single step advanced running simulation")
	}
	d.water.craft.telemetry.peakAcceleration = 5
	d.water.craft.telemetry.lastImpact = 2
	d.water.craft.launches, d.water.craft.landings = 2, 2
	if _, err := d.waterKey("x"); err != nil {
		t.Fatal(err)
	}
	c := d.water.craft
	if c.pose() != before.pose() || c.time != before.time || c.telemetry.peakAcceleration != 0 || c.telemetry.lastImpact != 0 || c.landings != 0 {
		t.Fatal("reset changed body or retained measurements")
	}
}

func TestWaterHandlingFiniteTopSpeed(t *testing.T) {
	for preset := range waterHandlingPresets {
		c := waterCraft{bound: 10000, preset: preset, throttle: 1}
		if err := c.advance(&nt3d.WaveSurface{}, 50); err != nil {
			t.Fatal(err)
		}
		before := c.speed
		if err := c.advance(&nt3d.WaveSurface{}, 60); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s cruise %.2f m/s", c.handling().name, c.speed)
		if c.speed < 3 || c.speed > 15 || math.Abs(c.speed-before) > .3 {
			t.Fatal("no bounded cruising speed", before, c.speed)
		}
	}
}
