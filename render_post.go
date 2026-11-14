// Copyright (c) 2026 Neomantra Corp.

package nt3d

import (
	"encoding/binary"
	"math"

	"github.com/gogpu/gputypes"
	"github.com/gogpu/wgpu"
)

const maxBloomLevels = 5

const ssaoDepth1xHeader = `
@group(1) @binding(0) var ssaoDepthTex: texture_depth_2d;
fn loadSceneDepth(coord: vec2<i32>, factor: i32, dim: vec2<i32>) -> f32 {
	let base = clamp(coord * factor, vec2<i32>(0), dim - vec2<i32>(1));
	if factor <= 1 {
		return textureLoad(ssaoDepthTex, base, 0);
	}
	var best = 2.0;
	for (var dy = 0; dy < factor; dy++) {
		for (var dx = 0; dx < factor; dx++) {
			let c = clamp(base + vec2<i32>(dx, dy), vec2<i32>(0), dim - vec2<i32>(1));
			best = min(best, textureLoad(ssaoDepthTex, c, 0));
		}
	}
	return best;
}
`

const ssaoDepthMSAAHeader = `
@group(1) @binding(0) var ssaoDepthTex: texture_depth_multisampled_2d;
fn loadSampleMin(c: vec2<i32>) -> f32 {
	let d0 = textureLoad(ssaoDepthTex, c, 0);
	let d1 = textureLoad(ssaoDepthTex, c, 1);
	let d2 = textureLoad(ssaoDepthTex, c, 2);
	let d3 = textureLoad(ssaoDepthTex, c, 3);
	return min(min(d0, d1), min(d2, d3));
}
fn loadSceneDepth(coord: vec2<i32>, factor: i32, dim: vec2<i32>) -> f32 {
	let base = clamp(coord * factor, vec2<i32>(0), dim - vec2<i32>(1));
	if factor <= 1 {
		return loadSampleMin(base);
	}
	var best = 2.0;
	for (var dy = 0; dy < factor; dy++) {
		for (var dx = 0; dx < factor; dx++) {
			let c = clamp(base + vec2<i32>(dx, dy), vec2<i32>(0), dim - vec2<i32>(1));
			best = min(best, loadSampleMin(c));
		}
	}
	return best;
}
`

const ssaoShaderBody = `
struct SSAOUniform {
	invMatrix: mat4x4<f32>,
	eyePersp: vec4<f32>,
	forward: vec4<f32>,
	// x: width, y: height, z: supersampleFactor, w: strength
	params: vec4<f32>,
	// x: explicitWorldRadius, y: bias, z: defaultRadiusPx, w: 0
	tuning: vec4<f32>,
};

@group(0) @binding(0) var<uniform> su: SSAOUniform;

@vertex fn vs_ssao(@builtin(vertex_index) i: u32) -> @builtin(position) vec4<f32> {
	let p = vec2<f32>(f32((i << 1u) & 2u), f32(i & 2u));
	return vec4<f32>(p * 2.0 - 1.0, 0.0, 1.0);
}

fn unprojectSSAO(px: vec2<f32>, depth: f32) -> vec3<f32> {
	let ndc = vec2<f32>(px.x / su.params.x * 2.0 - 1.0, 1.0 - px.y / su.params.y * 2.0);
	let w = su.invMatrix * vec4<f32>(ndc, depth, 1.0);
	if abs(w.w) < 1e-8 {
		return vec3<f32>(0.0);
	}
	return w.xyz / w.w;
}

fn ssaoEyeDist(world: vec3<f32>) -> f32 {
	let d = world - su.eyePersp.xyz;
	if su.eyePersp.w > 0.5 {
		return length(d);
	}
	return dot(d, su.forward.xyz);
}

fn ssaoViewDir(world: vec3<f32>) -> vec3<f32> {
	if su.eyePersp.w > 0.5 {
		return normalize(su.eyePersp.xyz - world);
	}
	return -su.forward.xyz;
}

@fragment fn fs_ssao(@builtin(position) fragPos: vec4<f32>) -> @location(0) vec4<f32> {
	let w = i32(su.params.x);
	let h = i32(su.params.y);
	let factor = max(1, i32(su.params.z));
	let depthDim = vec2<i32>(textureDimensions(ssaoDepthTex));
	let coord = clamp(vec2<i32>(floor(fragPos.xy)), vec2<i32>(0), vec2<i32>(w - 1, h - 1));
	let d = loadSceneDepth(coord, factor, depthDim);
	if d < 0.0 || d >= 1.0 {
		return vec4<f32>(1.0, 0.0, 1.0, 0.0);
	}
	let centerPx = vec2<f32>(coord) + vec2<f32>(0.5);
	let pos = unprojectSSAO(centerPx, d);
	let viewDist = ssaoEyeDist(pos);

	let dL = select(2.0, loadSceneDepth(coord + vec2<i32>(-1, 0), factor, depthDim), coord.x > 0);
	let dR = select(2.0, loadSceneDepth(coord + vec2<i32>(1, 0), factor, depthDim), coord.x + 1 < w);
	let hasL = dL >= 0.0 && dL < 1.0;
	let hasR = dR >= 0.0 && dR < 1.0;
	var dxVec = vec3<f32>(0.0);
	var haveX = false;
	if hasL && hasR {
		let pL = unprojectSSAO(centerPx + vec2<f32>(-1.0, 0.0), dL);
		let pR = unprojectSSAO(centerPx + vec2<f32>(1.0, 0.0), dR);
		dxVec = select(pos - pL, pR - pos, abs(dR - d) <= abs(d - dL));
		haveX = true;
	} else if hasR {
		dxVec = unprojectSSAO(centerPx + vec2<f32>(1.0, 0.0), dR) - pos;
		haveX = true;
	} else if hasL {
		dxVec = pos - unprojectSSAO(centerPx + vec2<f32>(-1.0, 0.0), dL);
		haveX = true;
	}

	let dU = select(2.0, loadSceneDepth(coord + vec2<i32>(0, -1), factor, depthDim), coord.y > 0);
	let dD = select(2.0, loadSceneDepth(coord + vec2<i32>(0, 1), factor, depthDim), coord.y + 1 < h);
	let hasU = dU >= 0.0 && dU < 1.0;
	let hasD = dD >= 0.0 && dD < 1.0;
	var dyVec = vec3<f32>(0.0);
	var haveY = false;
	if hasU && hasD {
		let pU = unprojectSSAO(centerPx + vec2<f32>(0.0, -1.0), dU);
		let pD = unprojectSSAO(centerPx + vec2<f32>(0.0, 1.0), dD);
		dyVec = select(pos - pU, pD - pos, abs(dD - d) <= abs(d - dU));
		haveY = true;
	} else if hasD {
		dyVec = unprojectSSAO(centerPx + vec2<f32>(0.0, 1.0), dD) - pos;
		haveY = true;
	} else if hasU {
		dyVec = pos - unprojectSSAO(centerPx + vec2<f32>(0.0, -1.0), dU);
		haveY = true;
	}
	if !haveX || !haveY {
		return vec4<f32>(1.0, viewDist, 1.0, 1.0);
	}

	var n = cross(dyVec, dxVec);
	let nLen2 = dot(n, n);
	if nLen2 <= 1e-16 {
		return vec4<f32>(1.0, viewDist, 1.0, 1.0);
	}
	n = n * inverseSqrt(nLen2);
	if dot(n, ssaoViewDir(pos)) < 0.0 {
		n = -n;
	}

	let pxWorld = max(1e-5, max(length(dxVec), length(dyVec)));
	var radiusPx = su.tuning.z;
	var radiusWorld = radiusPx * pxWorld;
	if su.tuning.x > 0.0 {
		radiusWorld = su.tuning.x;
		radiusPx = clamp(radiusWorld / pxWorld, 3.0, 40.0);
	}

	let ign = fract(52.9829189 * fract(0.06711056 * centerPx.x + 0.00583715 * centerPx.y));
	let ang = 6.2831853 * ign;
	let cs = cos(ang);
	let sn = sin(ang);

	let bias = su.tuning.y;
	let invR2 = 1.0 / max(1e-8, radiusWorld * radiusWorld);
	var occ = 0.0;
	for (var k = 0; k < 16; k++) {
		let r = sqrt((f32(k) + 0.5) / 16.0);
		let a = f32(k) * 2.39996323;
		let sp = vec2<f32>(r * cos(a), r * sin(a));
		let off = vec2<f32>(sp.x * cs - sp.y * sn, sp.x * sn + sp.y * cs) * radiusPx;
		let sc = vec2<i32>(round(vec2<f32>(coord) + off));
		if sc.x < 0 || sc.x >= w || sc.y < 0 || sc.y >= h {
			continue;
		}
		if sc.x == coord.x && sc.y == coord.y {
			continue;
		}
		let sd = loadSceneDepth(sc, factor, depthDim);
		if sd < 0.0 || sd >= 1.0 {
			continue;
		}
		let sPos = unprojectSSAO(vec2<f32>(sc) + vec2<f32>(0.5), sd);
		let v = sPos - pos;
		let v2 = dot(v, v);
		if v2 <= 1e-12 || v2 >= radiusWorld * radiusWorld {
			continue;
		}
		let ndotv = dot(n, v * inverseSqrt(v2));
		if ndotv > bias {
			occ += (ndotv - bias) * (1.0 - v2 * invR2);
		}
	}
	let ao = clamp(1.0 - su.params.w * (occ * (2.2 / 16.0)), 0.0, 1.0);
	return vec4<f32>(ao, viewDist, radiusWorld, 1.0);
}
`

const ssaoBlurShader = `
@group(0) @binding(0) var aoSrc: texture_2d<f32>;

@vertex fn vs_blur(@builtin(vertex_index) i: u32) -> @builtin(position) vec4<f32> {
	let p = vec2<f32>(f32((i << 1u) & 2u), f32(i & 2u));
	return vec4<f32>(p * 2.0 - 1.0, 0.0, 1.0);
}

fn bilateralBlur(coord: vec2<i32>, dir: vec2<i32>) -> vec4<f32> {
	let dim = vec2<i32>(textureDimensions(aoSrc));
	let c = clamp(coord, vec2<i32>(0), dim - vec2<i32>(1));
	let center = textureLoad(aoSrc, c, 0);
	if center.w < 0.5 {
		return vec4<f32>(1.0, 0.0, 1.0, 0.0);
	}
	let sigma = max(1e-4, center.z * 0.35);
	var weights = array<f32, 5>(1.0, 4.0, 6.0, 4.0, 1.0);
	var sum = 0.0;
	var wsum = 0.0;
	for (var k = -2; k <= 2; k++) {
		let nc = clamp(c + dir * k, vec2<i32>(0), dim - vec2<i32>(1));
		let s = textureLoad(aoSrc, nc, 0);
		if s.w > 0.5 {
			let dd = abs(s.y - center.y) / sigma;
			let w = weights[k + 2] * exp(-dd * dd);
			sum += s.x * w;
			wsum += w;
		}
	}
	var ao = center.x;
	if wsum > 0.0 {
		ao = sum / wsum;
	}
	return vec4<f32>(ao, center.y, center.z, 1.0);
}

@fragment fn fs_blur_x(@builtin(position) p: vec4<f32>) -> @location(0) vec4<f32> {
	return bilateralBlur(vec2<i32>(floor(p.xy)), vec2<i32>(1, 0));
}

@fragment fn fs_blur_y(@builtin(position) p: vec4<f32>) -> @location(0) vec4<f32> {
	return bilateralBlur(vec2<i32>(floor(p.xy)), vec2<i32>(0, 1));
}
`

const bloomShader = `
struct BloomParams {
	// x: threshold, y: radius, z: sceneLinear (1 if scene is linear), w: 0
	v: vec4<f32>,
};

@group(0) @binding(0) var<uniform> bp: BloomParams;
@group(1) @binding(0) var bloomSrc: texture_2d<f32>;
@group(1) @binding(1) var bloomSamp: sampler;

struct BloomVSOut {
	@builtin(position) pos: vec4<f32>,
	@location(0) uv: vec2<f32>,
};

@vertex fn vs_bloom(@builtin(vertex_index) i: u32) -> BloomVSOut {
	let p = vec2<f32>(f32((i << 1u) & 2u), f32(i & 2u));
	return BloomVSOut(vec4<f32>(p * 2.0 - 1.0, 0.0, 1.0), vec2<f32>(p.x, 1.0 - p.y));
}

fn bloomDecodeSRGB(c: vec3<f32>) -> vec3<f32> {
	let v = max(c, vec3<f32>(0.0));
	let lo = v / 12.92;
	let hi = pow((v + vec3<f32>(0.055)) / 1.055, vec3<f32>(2.4));
	return select(hi, lo, v <= vec3<f32>(0.04045));
}

fn softKneeWeight(rgb: vec3<f32>, threshold: f32) -> f32 {
	let br = max(rgb.r, max(rgb.g, rgb.b));
	if br <= 1e-5 {
		return 0.0;
	}
	let knee = threshold * 0.5 + 1e-4;
	let soft = clamp(br - threshold + knee, 0.0, 2.0 * knee);
	let contrib = max(br - threshold, (soft * soft) / (4.0 * knee));
	return max(0.0, contrib / br);
}

fn prefilterBox(uv: vec2<f32>) -> vec4<f32> {
	var c = max(textureSampleLevel(bloomSrc, bloomSamp, uv, 0.0).rgb, vec3<f32>(0.0));
	if bp.v.z < 0.5 {
		c = bloomDecodeSRGB(c);
	}
	let b = c * softKneeWeight(c, bp.v.x);
	let kw = 1.0 / (1.0 + max(b.r, max(b.g, b.b)));
	return vec4<f32>(b * kw, kw);
}

@fragment fn fs_prefilter(o: BloomVSOut) -> @location(0) vec4<f32> {
	let t = 1.0 / vec2<f32>(textureDimensions(bloomSrc));
	let t0 = prefilterBox(o.uv) * 0.5;
	let t1 = prefilterBox(o.uv + vec2<f32>(-t.x, -t.y)) * 0.125;
	let t2 = prefilterBox(o.uv + vec2<f32>(t.x, -t.y)) * 0.125;
	let t3 = prefilterBox(o.uv + vec2<f32>(-t.x, t.y)) * 0.125;
	let t4 = prefilterBox(o.uv + vec2<f32>(t.x, t.y)) * 0.125;
	let sum = t0 + t1 + t2 + t3 + t4;
	if sum.w <= 1e-6 {
		return vec4<f32>(0.0, 0.0, 0.0, 1.0);
	}
	return vec4<f32>(sum.rgb / sum.w, 1.0);
}

@fragment fn fs_downsample(o: BloomVSOut) -> @location(0) vec4<f32> {
	let t = 1.0 / vec2<f32>(textureDimensions(bloomSrc));
	let uv = o.uv;
	let a = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(-2.0 * t.x, -2.0 * t.y), 0.0).rgb;
	let b = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(0.0, -2.0 * t.y), 0.0).rgb;
	let c = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(2.0 * t.x, -2.0 * t.y), 0.0).rgb;
	let d = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(-t.x, -t.y), 0.0).rgb;
	let e = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(t.x, -t.y), 0.0).rgb;
	let f = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(-2.0 * t.x, 0.0), 0.0).rgb;
	let g = textureSampleLevel(bloomSrc, bloomSamp, uv, 0.0).rgb;
	let h = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(2.0 * t.x, 0.0), 0.0).rgb;
	let j = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(-t.x, t.y), 0.0).rgb;
	let k = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(t.x, t.y), 0.0).rgb;
	let l = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(-2.0 * t.x, 2.0 * t.y), 0.0).rgb;
	let m = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(0.0, 2.0 * t.y), 0.0).rgb;
	let n = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(2.0 * t.x, 2.0 * t.y), 0.0).rgb;

	let center = (d + e + j + k) * (0.5 * 0.25);
	let corners = ((a + b + f + g) + (b + c + g + h) + (f + g + l + m) + (g + h + m + n)) * (0.125 * 0.25);
	return vec4<f32>(max(center + corners, vec3<f32>(0.0)), 1.0);
}

@fragment fn fs_upsample(o: BloomVSOut) -> @location(0) vec4<f32> {
	let t = 1.0 / vec2<f32>(textureDimensions(bloomSrc));
	let uv = o.uv;
	let s0 = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(-t.x, -t.y), 0.0).rgb;
	let s1 = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(0.0, -t.y), 0.0).rgb * 2.0;
	let s2 = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(t.x, -t.y), 0.0).rgb;
	let s3 = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(-t.x, 0.0), 0.0).rgb * 2.0;
	let s4 = textureSampleLevel(bloomSrc, bloomSamp, uv, 0.0).rgb * 4.0;
	let s5 = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(t.x, 0.0), 0.0).rgb * 2.0;
	let s6 = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(-t.x, t.y), 0.0).rgb;
	let s7 = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(0.0, t.y), 0.0).rgb * 2.0;
	let s8 = textureSampleLevel(bloomSrc, bloomSamp, uv + vec2<f32>(t.x, t.y), 0.0).rgb;
	let tent = (s0 + s1 + s2 + s3 + s4 + s5 + s6 + s7 + s8) * (1.0 / 16.0);
	return vec4<f32>(tent * bp.v.y, 1.0);
}
`

const postResolveShader = `
struct PostResolve {
	// x: supersampleFactor, y: transfer (1 encode, 0 linear), z: load, w: sceneLinear
	resolve: vec4<f32>,
	// x: bloomGain, y: exposureScale, z: toneMap (0/1/2), w: vignette
	grading: vec4<f32>,
	// x: originX, y: originY, z: viewportW, w: viewportH
	viewport: vec4<i32>,
};

@group(0) @binding(0) var<uniform> pr: PostResolve;
@group(1) @binding(0) var sceneSrc: texture_2d<f32>;
@group(1) @binding(1) var aoSrc: texture_2d<f32>;
@group(1) @binding(2) var bloomSrc: texture_2d<f32>;
@group(1) @binding(3) var postSampler: sampler;

@vertex fn vs_post_resolve(@builtin(vertex_index) i: u32) -> @builtin(position) vec4<f32> {
	let p = vec2<f32>(f32((i << 1u) & 2u), f32(i & 2u));
	return vec4<f32>(p * 2.0 - 1.0, 0.0, 1.0);
}

fn resolveEncodeSRGB(c: vec3<f32>) -> vec3<f32> {
	let v = clamp(c, vec3<f32>(0.0), vec3<f32>(1.0));
	return select(1.055 * pow(v, vec3<f32>(1.0 / 2.4)) - 0.055, v * 12.92, v <= vec3<f32>(0.0031308));
}

fn resolveDecodeSRGB(c: vec3<f32>) -> vec3<f32> {
	let v = max(c, vec3<f32>(0.0));
	let lo = v / 12.92;
	let hi = pow((v + vec3<f32>(0.055)) / 1.055, vec3<f32>(2.4));
	return select(hi, lo, v <= vec3<f32>(0.04045));
}

fn resolveToneMap(rgb: vec3<f32>, mode: i32) -> vec3<f32> {
	let x = max(rgb, vec3<f32>(0.0));
	if mode == 1 {
		return x / (vec3<f32>(1.0) + x);
	}
	if mode == 2 {
		return clamp((x * (2.51 * x + vec3<f32>(0.03))) / (x * (2.43 * x + vec3<f32>(0.59)) + vec3<f32>(0.14)), vec3<f32>(0.0), vec3<f32>(1.0));
	}
	return x;
}

@fragment fn fs_post_resolve(@builtin(position) p: vec4<f32>) -> @location(0) vec4<f32> {
	let s = max(1, i32(pr.resolve.x));
	let n = f32(s * s);
	let local = vec2<i32>(floor(p.xy)) - pr.viewport.xy;
	let base = local * s;
	let sceneDim = vec2<i32>(textureDimensions(sceneSrc));

	var sum = vec4<f32>(0.0);
	for (var y = 0; y < s; y++) {
		for (var x = 0; x < s; x++) {
			let c = clamp(base + vec2<i32>(x, y), vec2<i32>(0), sceneDim - vec2<i32>(1));
			var sample = textureLoad(sceneSrc, c, 0);
			if pr.resolve.w < 0.5 {
				sample = vec4<f32>(resolveDecodeSRGB(sample.rgb), sample.a);
			}
			sum += sample;
		}
	}

	var rgb = sum.rgb / n;
	var a = 1.0;
	let load = pr.resolve.z > 0.5;
	if load {
		a = clamp(sum.a / n, 0.0, 1.0);
		if sum.a > 1e-6 {
			rgb = sum.rgb / sum.a;
		} else {
			rgb = vec3<f32>(0.0);
		}
	}

	let aoDim = vec2<i32>(textureDimensions(aoSrc));
	let ao = textureLoad(aoSrc, clamp(local, vec2<i32>(0), aoDim - vec2<i32>(1)), 0).x;
	rgb *= ao;

	let vpSize = max(vec2<f32>(1.0), vec2<f32>(pr.viewport.zw));
	let uv = (vec2<f32>(local) + vec2<f32>(0.5)) / vpSize;

	if pr.grading.x > 0.0 {
		let glow = max(textureSampleLevel(bloomSrc, postSampler, uv, 0.0).rgb, vec3<f32>(0.0)) * pr.grading.x;
		if load {
			let premul = rgb * a + glow;
			let glowA = clamp(max(glow.r, max(glow.g, glow.b)), 0.0, 1.0);
			a = clamp(a + glowA * (1.0 - a), 0.0, 1.0);
			if a > 1e-6 {
				rgb = premul / a;
			} else {
				rgb = vec3<f32>(0.0);
			}
		} else {
			rgb += glow;
		}
	}

	rgb = resolveToneMap(rgb * pr.grading.y, i32(round(pr.grading.z)));

	if pr.grading.w > 0.0 {
		let d = uv - vec2<f32>(0.5);
		let r2 = dot(d, d);
		let t = clamp((r2 - 0.05) / (0.55 - 0.05), 0.0, 1.0);
		let smooth = t * t * (3.0 - 2.0 * t);
		rgb *= max(0.0, 1.0 - pr.grading.w * smooth);
	}

	if pr.resolve.y > 0.5 {
		rgb = resolveEncodeSRGB(rgb);
	}
	if pr.resolve.z > 1.5 {
		rgb *= a;
	}
	return vec4<f32>(rgb, a);
}
`

type gpuPostShared struct {
	ready   bool
	sampler *wgpu.Sampler
	white   *wgpu.Texture
	black   *wgpu.Texture
	whiteV  *wgpu.TextureView
	blackV  *wgpu.TextureView

	ssaoUniformGroup *wgpu.BindGroupLayout
	ssaoDepthGroups  [2]*wgpu.BindGroupLayout
	ssaoLayouts      [2]*wgpu.PipelineLayout
	ssaoShaders      [2]*wgpu.ShaderModule
	ssaoPipelines    [2]*wgpu.RenderPipeline

	blurShader *wgpu.ShaderModule
	blurGroup  *wgpu.BindGroupLayout
	blurLayout *wgpu.PipelineLayout
	blurX      *wgpu.RenderPipeline
	blurY      *wgpu.RenderPipeline

	bloomShader       *wgpu.ShaderModule
	bloomUniformGroup *wgpu.BindGroupLayout
	bloomTexGroup     *wgpu.BindGroupLayout
	bloomLayout       *wgpu.PipelineLayout
	bloomPrefilter    *wgpu.RenderPipeline
	bloomDownsample   *wgpu.RenderPipeline
	bloomUpsample     *wgpu.RenderPipeline

	resolveShader       *wgpu.ShaderModule
	resolveUniformGroup *wgpu.BindGroupLayout
	resolveTexGroup     *wgpu.BindGroupLayout
	resolveLayout       *wgpu.PipelineLayout
	resolvePipelines    map[resolveKey]*wgpu.RenderPipeline

	overlayPipeline *wgpu.RenderPipeline
}

func (ps *gpuPostShared) release() {
	for _, p := range []*wgpu.RenderPipeline{ps.ssaoPipelines[0], ps.ssaoPipelines[1], ps.blurX, ps.blurY, ps.bloomPrefilter, ps.bloomDownsample, ps.bloomUpsample, ps.overlayPipeline} {
		if p != nil {
			p.Release()
		}
	}
	for k, p := range ps.resolvePipelines {
		if p != nil {
			p.Release()
		}
		delete(ps.resolvePipelines, k)
	}
	for _, l := range []*wgpu.PipelineLayout{ps.ssaoLayouts[0], ps.ssaoLayouts[1], ps.blurLayout, ps.bloomLayout, ps.resolveLayout} {
		if l != nil {
			l.Release()
		}
	}
	for _, g := range []*wgpu.BindGroupLayout{ps.ssaoUniformGroup, ps.ssaoDepthGroups[0], ps.ssaoDepthGroups[1], ps.blurGroup, ps.bloomUniformGroup, ps.bloomTexGroup, ps.resolveUniformGroup, ps.resolveTexGroup} {
		if g != nil {
			g.Release()
		}
	}
	for _, s := range []*wgpu.ShaderModule{ps.ssaoShaders[0], ps.ssaoShaders[1], ps.blurShader, ps.bloomShader, ps.resolveShader} {
		if s != nil {
			s.Release()
		}
	}
	for _, v := range []*wgpu.TextureView{ps.whiteV, ps.blackV} {
		if v != nil {
			v.Release()
		}
	}
	for _, t := range []*wgpu.Texture{ps.white, ps.black} {
		if t != nil {
			t.Release()
		}
	}
	if ps.sampler != nil {
		ps.sampler.Release()
	}
	*ps = gpuPostShared{}
}

func (ps *gpuPostShared) init(dev *wgpu.Device) (err error) {
	if ps.ready {
		return nil
	}
	defer func() {
		if err != nil {
			ps.release()
		}
	}()

	ps.sampler, err = dev.CreateSampler(&wgpu.SamplerDescriptor{
		Label:        "nt3d post sampler",
		AddressModeU: gputypes.AddressModeClampToEdge,
		AddressModeV: gputypes.AddressModeClampToEdge,
		AddressModeW: gputypes.AddressModeClampToEdge,
		MagFilter:    gputypes.FilterModeLinear,
		MinFilter:    gputypes.FilterModeLinear,
		MipmapFilter: gputypes.FilterModeNearest,
		Anisotropy:   1,
	})
	if err != nil {
		return err
	}

	makeConstTex := func(label string, r, g, b, a uint16) (*wgpu.Texture, *wgpu.TextureView, error) {
		tex, e := dev.CreateTexture(&wgpu.TextureDescriptor{
			Label:         label,
			Size:          wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1},
			MipLevelCount: 1,
			SampleCount:   1,
			Dimension:     wgpu.TextureDimension2D,
			Format:        sceneFormat,
			Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopyDst,
		})
		if e != nil {
			return nil, nil, e
		}
		var data [256]byte
		binary.LittleEndian.PutUint16(data[0:], r)
		binary.LittleEndian.PutUint16(data[2:], g)
		binary.LittleEndian.PutUint16(data[4:], b)
		binary.LittleEndian.PutUint16(data[6:], a)
		if e = dev.Queue().WriteTexture(
			&wgpu.ImageCopyTexture{Texture: tex, MipLevel: 0},
			data[:],
			&wgpu.ImageDataLayout{Offset: 0, BytesPerRow: 256, RowsPerImage: 1},
			&wgpu.Extent3D{Width: 1, Height: 1, DepthOrArrayLayers: 1},
		); e != nil {
			tex.Release()
			return nil, nil, e
		}
		view, e := dev.CreateTextureView(tex, nil)
		if e != nil {
			tex.Release()
			return nil, nil, e
		}
		return tex, view, nil
	}

	const f16One = 0x3C00
	if ps.white, ps.whiteV, err = makeConstTex("nt3d post white", f16One, f16One, f16One, f16One); err != nil {
		return err
	}
	if ps.black, ps.blackV, err = makeConstTex("nt3d post black", 0, 0, 0, 0); err != nil {
		return err
	}

	// SSAO uniform layout (shared by 1x and MSAA variants; pipelines created lazily per variant).
	if ps.ssaoUniformGroup, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "nt3d ssao uniform",
		Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}},
	}); err != nil {
		return err
	}

	// SSAO bilateral blur pipelines.
	if ps.blurShader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d ssao blur", WGSL: ssaoBlurShader}); err != nil {
		return err
	}
	if ps.blurGroup, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "nt3d ssao blur src",
		Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeUnfilterableFloat, ViewDimension: gputypes.TextureViewDimension2D}}},
	}); err != nil {
		return err
	}
	if ps.blurLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{ps.blurGroup}}); err != nil {
		return err
	}
	for i, ep := range [2]string{"fs_blur_x", "fs_blur_y"} {
		p, e := dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
			Label:       "nt3d " + ep,
			Layout:      ps.blurLayout,
			Vertex:      wgpu.VertexState{Module: ps.blurShader, EntryPoint: "vs_blur"},
			Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
			Multisample: gputypes.DefaultMultisampleState(),
			Fragment:    &wgpu.FragmentState{Module: ps.blurShader, EntryPoint: ep, Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}},
		})
		if e != nil {
			return e
		}
		if i == 0 {
			ps.blurX = p
		} else {
			ps.blurY = p
		}
	}

	// Bloom pipelines.
	if ps.bloomShader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d bloom", WGSL: bloomShader}); err != nil {
		return err
	}
	if ps.bloomUniformGroup, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "nt3d bloom uniform",
		Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}},
	}); err != nil {
		return err
	}
	if ps.bloomTexGroup, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "nt3d bloom tex",
		Entries: []gputypes.BindGroupLayoutEntry{
			{Binding: 0, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D}},
			{Binding: 1, Visibility: wgpu.ShaderStageFragment, Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}},
		},
	}); err != nil {
		return err
	}
	if ps.bloomLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{ps.bloomUniformGroup, ps.bloomTexGroup}}); err != nil {
		return err
	}
	makeBloomPipe := func(ep string, additive bool) (*wgpu.RenderPipeline, error) {
		ct := gputypes.ColorTargetState{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}
		if additive {
			ct.Blend = &gputypes.BlendState{
				Color: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorOne, DstFactor: gputypes.BlendFactorOne, Operation: gputypes.BlendOperationAdd},
				Alpha: gputypes.BlendComponent{SrcFactor: gputypes.BlendFactorZero, DstFactor: gputypes.BlendFactorOne, Operation: gputypes.BlendOperationAdd},
			}
		}
		return dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
			Label:       "nt3d bloom " + ep,
			Layout:      ps.bloomLayout,
			Vertex:      wgpu.VertexState{Module: ps.bloomShader, EntryPoint: "vs_bloom"},
			Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
			Multisample: gputypes.DefaultMultisampleState(),
			Fragment:    &wgpu.FragmentState{Module: ps.bloomShader, EntryPoint: ep, Targets: []gputypes.ColorTargetState{ct}},
		})
	}
	if ps.bloomPrefilter, err = makeBloomPipe("fs_prefilter", false); err != nil {
		return err
	}
	if ps.bloomDownsample, err = makeBloomPipe("fs_downsample", false); err != nil {
		return err
	}
	if ps.bloomUpsample, err = makeBloomPipe("fs_upsample", true); err != nil {
		return err
	}

	// Post-resolve pipelines.
	if ps.resolveShader, err = dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d post resolve", WGSL: postResolveShader}); err != nil {
		return err
	}
	if ps.resolveUniformGroup, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label:   "nt3d post resolve uniform",
		Entries: []gputypes.BindGroupLayoutEntry{{Binding: 0, Visibility: wgpu.ShaderStageFragment, Buffer: &gputypes.BufferBindingLayout{Type: gputypes.BufferBindingTypeUniform}}},
	}); err != nil {
		return err
	}
	if ps.resolveTexGroup, err = dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "nt3d post resolve textures",
		Entries: []gputypes.BindGroupLayoutEntry{
			{Binding: 0, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeUnfilterableFloat, ViewDimension: gputypes.TextureViewDimension2D}},
			{Binding: 1, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeUnfilterableFloat, ViewDimension: gputypes.TextureViewDimension2D}},
			{Binding: 2, Visibility: wgpu.ShaderStageFragment, Texture: &gputypes.TextureBindingLayout{SampleType: gputypes.TextureSampleTypeFloat, ViewDimension: gputypes.TextureViewDimension2D}},
			{Binding: 3, Visibility: wgpu.ShaderStageFragment, Sampler: &gputypes.SamplerBindingLayout{Type: gputypes.SamplerBindingTypeFiltering}},
		},
	}); err != nil {
		return err
	}
	if ps.resolveLayout, err = dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{ps.resolveUniformGroup, ps.resolveTexGroup}}); err != nil {
		return err
	}
	ps.resolvePipelines = make(map[resolveKey]*wgpu.RenderPipeline)
	ps.ready = true
	return nil
}

func (ps *gpuPostShared) ensureSSAO(dev *wgpu.Device, msaa bool) (*wgpu.BindGroupLayout, *wgpu.RenderPipeline, error) {
	idx := 0
	src := ssaoDepth1xHeader + ssaoShaderBody
	if msaa {
		idx = 1
		src = ssaoDepthMSAAHeader + ssaoShaderBody
	}
	if ps.ssaoPipelines[idx] != nil {
		return ps.ssaoDepthGroups[idx], ps.ssaoPipelines[idx], nil
	}
	dg, err := dev.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "nt3d ssao depth",
		Entries: []gputypes.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: wgpu.ShaderStageFragment,
			Texture: &gputypes.TextureBindingLayout{
				SampleType:    gputypes.TextureSampleTypeDepth,
				ViewDimension: gputypes.TextureViewDimension2D,
				Multisampled:  msaa,
			},
		}},
	})
	if err != nil {
		return nil, nil, err
	}
	pl, err := dev.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{ps.ssaoUniformGroup, dg}})
	if err != nil {
		dg.Release()
		return nil, nil, err
	}
	sm, err := dev.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "nt3d ssao", WGSL: src})
	if err != nil {
		pl.Release()
		dg.Release()
		return nil, nil, err
	}
	pipe, err := dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:       "nt3d ssao",
		Layout:      pl,
		Vertex:      wgpu.VertexState{Module: sm, EntryPoint: "vs_ssao"},
		Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
		Multisample: gputypes.DefaultMultisampleState(),
		Fragment:    &wgpu.FragmentState{Module: sm, EntryPoint: "fs_ssao", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}},
	})
	if err != nil {
		sm.Release()
		pl.Release()
		dg.Release()
		return nil, nil, err
	}
	ps.ssaoDepthGroups[idx] = dg
	ps.ssaoLayouts[idx] = pl
	ps.ssaoShaders[idx] = sm
	ps.ssaoPipelines[idx] = pipe
	return dg, pipe, nil
}

func (ps *gpuPostShared) resolvePipeline(dev *wgpu.Device, format gputypes.TextureFormat, load bool) (*wgpu.RenderPipeline, error) {
	key := resolveKey{format: format, load: load}
	if p := ps.resolvePipelines[key]; p != nil {
		return p, nil
	}
	target := gputypes.ColorTargetState{Format: format, WriteMask: gputypes.ColorWriteMaskAll}
	if load {
		blend := gputypes.BlendStateAlpha()
		target.Blend = &blend
	}
	p, err := dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:       "nt3d post resolve",
		Layout:      ps.resolveLayout,
		Vertex:      wgpu.VertexState{Module: ps.resolveShader, EntryPoint: "vs_post_resolve"},
		Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
		Multisample: gputypes.DefaultMultisampleState(),
		Fragment:    &wgpu.FragmentState{Module: ps.resolveShader, EntryPoint: "fs_post_resolve", Targets: []gputypes.ColorTargetState{target}},
	})
	if err != nil {
		return nil, err
	}
	ps.resolvePipelines[key] = p
	return p, nil
}

func (ps *gpuPostShared) ensureOverlayPipeline(dev *wgpu.Device, layout *wgpu.PipelineLayout, shader *wgpu.ShaderModule) (*wgpu.RenderPipeline, error) {
	if ps.overlayPipeline != nil {
		return ps.overlayPipeline, nil
	}
	p, err := dev.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:       "nt3d post overlay",
		Layout:      layout,
		Vertex:      wgpu.VertexState{Module: shader, EntryPoint: "vs_ui"},
		Primitive:   gputypes.PrimitiveState{Topology: gputypes.PrimitiveTopologyTriangleList, CullMode: gputypes.CullModeNone},
		Multisample: gputypes.DefaultMultisampleState(),
		Fragment:    &wgpu.FragmentState{Module: shader, EntryPoint: "fs_overlay", Targets: []gputypes.ColorTargetState{{Format: sceneFormat, WriteMask: gputypes.ColorWriteMaskAll}}},
	})
	if err != nil {
		return nil, err
	}
	ps.overlayPipeline = p
	return p, nil
}

type bloomMip struct {
	w, h    int
	tex     *wgpu.Texture
	view    *wgpu.TextureView
	sampled *wgpu.BindGroup
}

func (m *bloomMip) release() {
	if m.sampled != nil {
		m.sampled.Release()
	}
	if m.view != nil {
		m.view.Release()
	}
	if m.tex != nil {
		m.tex.Release()
	}
	*m = bloomMip{}
}

type gpuPost struct {
	ownShared gpuPostShared

	ssaoUniform     *wgpu.Buffer
	ssaoUniformBind *wgpu.BindGroup
	depthView       *wgpu.TextureView
	depthMSAA       bool
	depthBind       *wgpu.BindGroup

	aoW, aoH           int
	aoRaw, aoTemp      *wgpu.Texture
	aoRawView, aoTempV *wgpu.TextureView
	blurXBind          *wgpu.BindGroup
	blurYBind          *wgpu.BindGroup

	bloomUniform      *wgpu.Buffer
	bloomUniformBind  *wgpu.BindGroup
	bloomW, bloomH    int
	bloomLevels       int
	bloomSceneView    *wgpu.TextureView
	bloomPrefilterBnd *wgpu.BindGroup
	mips              [maxBloomLevels]bloomMip

	resolveUniform     *wgpu.Buffer
	resolveUniformBind *wgpu.BindGroup
	resolveScene       *wgpu.TextureView
	resolveAO          *wgpu.TextureView
	resolveBloom       *wgpu.TextureView
	resolveTexBind     *wgpu.BindGroup

	overlayW, overlayH int
	overlayTex         *wgpu.Texture
	overlayView        *wgpu.TextureView
}

func (p *gpuPost) releaseSSAO() {
	for _, b := range []*wgpu.BindGroup{p.blurXBind, p.blurYBind} {
		if b != nil {
			b.Release()
		}
	}
	for _, v := range []*wgpu.TextureView{p.aoRawView, p.aoTempV} {
		if v != nil {
			v.Release()
		}
	}
	for _, t := range []*wgpu.Texture{p.aoRaw, p.aoTemp} {
		if t != nil {
			t.Release()
		}
	}
	p.aoW, p.aoH, p.aoRaw, p.aoTemp, p.aoRawView, p.aoTempV, p.blurXBind, p.blurYBind = 0, 0, nil, nil, nil, nil, nil, nil
}

func (p *gpuPost) releaseBloom() {
	if p.bloomPrefilterBnd != nil {
		p.bloomPrefilterBnd.Release()
		p.bloomPrefilterBnd = nil
		p.bloomSceneView = nil
	}
	for i := range p.mips {
		p.mips[i].release()
	}
	p.bloomW, p.bloomH, p.bloomLevels = 0, 0, 0
}

func (p *gpuPost) releaseOverlay() {
	if p.overlayView != nil {
		p.overlayView.Release()
	}
	if p.overlayTex != nil {
		p.overlayTex.Release()
	}
	p.overlayW, p.overlayH, p.overlayTex, p.overlayView = 0, 0, nil, nil
}

func (p *gpuPost) release(shared bool) {
	p.releaseSSAO()
	p.releaseBloom()
	p.releaseOverlay()
	for _, b := range []*wgpu.BindGroup{p.ssaoUniformBind, p.depthBind, p.bloomUniformBind, p.resolveUniformBind, p.resolveTexBind} {
		if b != nil {
			b.Release()
		}
	}
	for _, u := range []*wgpu.Buffer{p.ssaoUniform, p.bloomUniform, p.resolveUniform} {
		if u != nil {
			u.Release()
		}
	}
	if !shared {
		p.ownShared.release()
	}
	*p = gpuPost{}
}

func (g *gpuRenderer) postShared(dev *wgpu.Device) (*gpuPostShared, error) {
	if g.shared != nil {
		g.shared.mu.Lock()
		defer g.shared.mu.Unlock()
		if err := g.shared.post.init(dev); err != nil {
			return nil, err
		}
		return &g.shared.post, nil
	}
	if err := g.post.ownShared.init(dev); err != nil {
		return nil, err
	}
	return &g.post.ownShared, nil
}

func (g *gpuRenderer) ensurePostUniforms(dev *wgpu.Device, ps *gpuPostShared) error {
	p := &g.post
	if p.resolveUniform != nil {
		return nil
	}
	makeUB := func(label string, size uint64, layout *wgpu.BindGroupLayout) (*wgpu.Buffer, *wgpu.BindGroup, error) {
		buf, err := dev.CreateBuffer(&wgpu.BufferDescriptor{Label: label, Size: size, Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst})
		if err != nil {
			return nil, nil, err
		}
		bg, err := dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: layout, Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: buf, Size: size}}})
		if err != nil {
			buf.Release()
			return nil, nil, err
		}
		return buf, bg, nil
	}
	var err error
	if p.ssaoUniform, p.ssaoUniformBind, err = makeUB("nt3d ssao uniform", 128, ps.ssaoUniformGroup); err != nil {
		return err
	}
	if p.bloomUniform, p.bloomUniformBind, err = makeUB("nt3d bloom uniform", 16, ps.bloomUniformGroup); err != nil {
		return err
	}
	if p.resolveUniform, p.resolveUniformBind, err = makeUB("nt3d post resolve uniform", 48, ps.resolveUniformGroup); err != nil {
		return err
	}
	return nil
}

func createPostTexture(dev *wgpu.Device, label string, w, h int) (*wgpu.Texture, *wgpu.TextureView, error) {
	tex, err := dev.CreateTexture(&wgpu.TextureDescriptor{
		Label:         label,
		Size:          wgpu.Extent3D{Width: uint32(w), Height: uint32(h), DepthOrArrayLayers: 1},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        sceneFormat,
		Usage:         wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding,
	})
	if err != nil {
		return nil, nil, err
	}
	view, err := dev.CreateTextureView(tex, nil)
	if err != nil {
		tex.Release()
		return nil, nil, err
	}
	return tex, view, nil
}

func (g *gpuRenderer) encodeSSAO(dev *wgpu.Device, enc *wgpu.CommandEncoder, ps *gpuPostShared, f Frame, s int, msaa bool) (*wgpu.TextureView, error) {
	p := &g.post
	w, h := f.Width, f.Height
	if p.aoW != w || p.aoH != h || p.aoRawView == nil {
		p.releaseSSAO()
		var err error
		if p.aoRaw, p.aoRawView, err = createPostTexture(dev, "nt3d ssao raw", w, h); err != nil {
			return nil, err
		}
		if p.aoTemp, p.aoTempV, err = createPostTexture(dev, "nt3d ssao temp", w, h); err != nil {
			p.releaseSSAO()
			return nil, err
		}
		if p.blurXBind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: ps.blurGroup, Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: p.aoRawView}}}); err != nil {
			p.releaseSSAO()
			return nil, err
		}
		if p.blurYBind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: ps.blurGroup, Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: p.aoTempV}}}); err != nil {
			p.releaseSSAO()
			return nil, err
		}
		p.aoW, p.aoH = w, h
	}

	var depthGroup *wgpu.BindGroupLayout
	var ssaoPipe *wgpu.RenderPipeline
	var err error
	if g.shared != nil {
		g.shared.mu.Lock()
		depthGroup, ssaoPipe, err = ps.ensureSSAO(dev, msaa)
		g.shared.mu.Unlock()
	} else {
		depthGroup, ssaoPipe, err = ps.ensureSSAO(dev, msaa)
	}
	if err != nil {
		return nil, err
	}

	if p.depthView != g.target.depthView || p.depthMSAA != msaa || p.depthBind == nil {
		if p.depthBind != nil {
			p.depthBind.Release()
			p.depthBind = nil
		}
		p.depthBind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Layout:  depthGroup,
			Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: g.target.depthView}},
		})
		if err != nil {
			return nil, err
		}
		p.depthView = g.target.depthView
		p.depthMSAA = msaa
	}

	inv, _ := inverseProjection(f.Matrix)
	persp := float32(0)
	if f.Perspective {
		persp = 1
	}
	defaultPx := min(40, max(3, 0.06*float32(min(w, h))))
	ub := make([]byte, 0, 128)
	ub = appendFloats(ub, inv[:]...)
	ub = appendFloats(ub,
		f.Eye.X, f.Eye.Y, f.Eye.Z, persp,
		f.Forward.X, f.Forward.Y, f.Forward.Z, 0,
		float32(w), float32(h), float32(s), f.PostProcess.SSAO.Strength,
		f.PostProcess.SSAO.Radius, f.PostProcess.SSAO.bias(), defaultPx, 0,
	)
	if err := dev.Queue().WriteBuffer(p.ssaoUniform, 0, ub); err != nil {
		return nil, err
	}

	runPass := func(dst *wgpu.TextureView, pipe *wgpu.RenderPipeline, bg0, bg1 *wgpu.BindGroup) error {
		pass, e := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
			ColorAttachments: []wgpu.RenderPassColorAttachment{{View: dst, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}},
		})
		if e != nil {
			return e
		}
		pass.SetPipeline(pipe)
		pass.SetBindGroup(0, bg0, nil)
		if bg1 != nil {
			pass.SetBindGroup(1, bg1, nil)
		}
		pass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
		return pass.End()
	}

	if err := runPass(p.aoRawView, ssaoPipe, p.ssaoUniformBind, p.depthBind); err != nil {
		return nil, err
	}
	if err := runPass(p.aoTempV, ps.blurX, p.blurXBind, nil); err != nil {
		return nil, err
	}
	if err := runPass(p.aoRawView, ps.blurY, p.blurYBind, nil); err != nil {
		return nil, err
	}
	return p.aoRawView, nil
}

func (g *gpuRenderer) encodeBloom(dev *wgpu.Device, enc *wgpu.CommandEncoder, ps *gpuPostShared, f Frame) (*wgpu.TextureView, float32, error) {
	p := &g.post
	w, h := f.Width, f.Height
	if p.bloomW != w || p.bloomH != h || p.bloomLevels == 0 {
		p.releaseBloom()
		mw, mh := max(1, w/2), max(1, h/2)
		levels := 0
		for i := range maxBloomLevels {
			var err error
			m := &p.mips[i]
			m.w, m.h = mw, mh
			if m.tex, m.view, err = createPostTexture(dev, "nt3d bloom mip", mw, mh); err != nil {
				p.releaseBloom()
				return nil, 0, err
			}
			if m.sampled, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
				Layout:  ps.bloomTexGroup,
				Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: m.view}, {Binding: 1, Sampler: ps.sampler}},
			}); err != nil {
				p.releaseBloom()
				return nil, 0, err
			}
			levels++
			if mw < 4 && mh < 4 {
				break
			}
			mw, mh = max(1, (mw+1)/2), max(1, (mh+1)/2)
		}
		p.bloomW, p.bloomH, p.bloomLevels = w, h, levels
	}

	if p.bloomSceneView != g.target.colorView || p.bloomPrefilterBnd == nil {
		if p.bloomPrefilterBnd != nil {
			p.bloomPrefilterBnd.Release()
			p.bloomPrefilterBnd = nil
		}
		var err error
		p.bloomPrefilterBnd, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Layout:  ps.bloomTexGroup,
			Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: g.target.colorView}, {Binding: 1, Sampler: ps.sampler}},
		})
		if err != nil {
			return nil, 0, err
		}
		p.bloomSceneView = g.target.colorView
	}

	sceneLin := float32(0)
	if f.Linear {
		sceneLin = 1
	}
	radius := f.PostProcess.Bloom.radius()
	var ub [16]byte
	binary.LittleEndian.PutUint32(ub[0:], math.Float32bits(f.PostProcess.Bloom.threshold()))
	binary.LittleEndian.PutUint32(ub[4:], math.Float32bits(radius))
	binary.LittleEndian.PutUint32(ub[8:], math.Float32bits(sceneLin))
	if err := dev.Queue().WriteBuffer(p.bloomUniform, 0, ub[:]); err != nil {
		return nil, 0, err
	}

	runBloom := func(dst *wgpu.TextureView, load gputypes.LoadOp, pipe *wgpu.RenderPipeline, srcBind *wgpu.BindGroup) error {
		pass, e := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
			ColorAttachments: []wgpu.RenderPassColorAttachment{{View: dst, LoadOp: load, StoreOp: gputypes.StoreOpStore}},
		})
		if e != nil {
			return e
		}
		pass.SetPipeline(pipe)
		pass.SetBindGroup(0, p.bloomUniformBind, nil)
		pass.SetBindGroup(1, srcBind, nil)
		pass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
		return pass.End()
	}

	if err := runBloom(p.mips[0].view, gputypes.LoadOpClear, ps.bloomPrefilter, p.bloomPrefilterBnd); err != nil {
		return nil, 0, err
	}
	for i := 1; i < p.bloomLevels; i++ {
		if err := runBloom(p.mips[i].view, gputypes.LoadOpClear, ps.bloomDownsample, p.mips[i-1].sampled); err != nil {
			return nil, 0, err
		}
	}
	for i := p.bloomLevels - 1; i >= 1; i-- {
		if err := runBloom(p.mips[i-1].view, gputypes.LoadOpLoad, ps.bloomUpsample, p.mips[i].sampled); err != nil {
			return nil, 0, err
		}
	}

	gain := f.PostProcess.Bloom.Strength * bloomMipGain(p.bloomLevels, radius)
	return p.mips[0].view, gain, nil
}

// resolveWithPost runs SSAO, Bloom, Exposure, ToneMap, and Vignette on the
// scene target, draws any screen-space overlays on top without post-grading,
// and resolves into target.
func (g *gpuRenderer) resolveWithPost(dev *wgpu.Device, enc *wgpu.CommandEncoder, f Frame, target GPUTarget) error {
	ps, err := g.postShared(dev)
	if err != nil {
		return err
	}
	if err := g.ensurePostUniforms(dev, ps); err != nil {
		return err
	}

	s := supersampleFactor(f)
	aoView := ps.whiteV
	if f.PostProcess.SSAO.on() {
		if aoView, err = g.encodeSSAO(dev, enc, ps, f, s, g.target.samples > 1); err != nil {
			return err
		}
	}

	bloomView := ps.blackV
	bloomGain := float32(0)
	if f.PostProcess.Bloom.on() {
		if bloomView, bloomGain, err = g.encodeBloom(dev, enc, ps, f); err != nil {
			return err
		}
	}

	p := &g.post
	if p.resolveScene != g.target.colorView || p.resolveAO != aoView || p.resolveBloom != bloomView || p.resolveTexBind == nil {
		if p.resolveTexBind != nil {
			p.resolveTexBind.Release()
			p.resolveTexBind = nil
		}
		p.resolveTexBind, err = dev.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Layout: ps.resolveTexGroup,
			Entries: []wgpu.BindGroupEntry{
				{Binding: 0, TextureView: g.target.colorView},
				{Binding: 1, TextureView: aoView},
				{Binding: 2, TextureView: bloomView},
				{Binding: 3, Sampler: ps.sampler},
			},
		})
		if err != nil {
			return err
		}
		p.resolveScene, p.resolveAO, p.resolveBloom = g.target.colorView, aoView, bloomView
	}

	hasOverlays := g.batches[4].count > 0
	if hasOverlays {
		if err := g.resolvePostWithOverlays(dev, enc, ps, f, target, s, bloomGain); err != nil {
			return err
		}
		return nil
	}

	var pipe *wgpu.RenderPipeline
	if g.shared != nil {
		g.shared.mu.Lock()
		pipe, err = ps.resolvePipeline(dev, target.Format, target.Load)
		g.shared.mu.Unlock()
	} else {
		pipe, err = ps.resolvePipeline(dev, target.Format, target.Load)
	}
	if err != nil {
		return err
	}

	vp := targetViewport(target)
	fullTarget := vp.Min.X == 0 && vp.Min.Y == 0 && vp.Max.X == target.Width && vp.Max.Y == target.Height
	loadFlag := float32(0)
	if target.Load {
		loadFlag = 1
	}
	sceneLin := float32(0)
	if f.Linear {
		sceneLin = 1
	}
	// Post-processing works in linear light; encode to sRGB when the target
	// format stores non-sRGB bytes (or RGBA16Float when !f.Linear).
	transfer := resolvePostTransfer(f.Linear, target.Format)

	var ub [48]byte
	binary.LittleEndian.PutUint32(ub[0:], math.Float32bits(float32(s)))
	binary.LittleEndian.PutUint32(ub[4:], math.Float32bits(transfer))
	binary.LittleEndian.PutUint32(ub[8:], math.Float32bits(loadFlag))
	binary.LittleEndian.PutUint32(ub[12:], math.Float32bits(sceneLin))
	binary.LittleEndian.PutUint32(ub[16:], math.Float32bits(bloomGain))
	binary.LittleEndian.PutUint32(ub[20:], math.Float32bits(exposureScale(f.PostProcess.Exposure)))
	binary.LittleEndian.PutUint32(ub[24:], math.Float32bits(float32(f.PostProcess.ToneMap)))
	binary.LittleEndian.PutUint32(ub[28:], math.Float32bits(f.PostProcess.Vignette))
	binary.LittleEndian.PutUint32(ub[32:], uint32(int32(vp.Min.X)))
	binary.LittleEndian.PutUint32(ub[36:], uint32(int32(vp.Min.Y)))
	binary.LittleEndian.PutUint32(ub[40:], uint32(int32(vp.Dx())))
	binary.LittleEndian.PutUint32(ub[44:], uint32(int32(vp.Dy())))
	if err := dev.Queue().WriteBuffer(p.resolveUniform, 0, ub[:]); err != nil {
		return err
	}

	loadOp := gputypes.LoadOpClear
	if target.Load || !fullTarget {
		loadOp = gputypes.LoadOpLoad
	}
	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{View: target.Color, LoadOp: loadOp, StoreOp: gputypes.StoreOpStore}},
	})
	if err != nil {
		return err
	}
	if !fullTarget {
		pass.SetViewport(gputypes.Viewport{
			X:        float32(vp.Min.X),
			Y:        float32(vp.Min.Y),
			Width:    float32(vp.Dx()),
			Height:   float32(vp.Dy()),
			MinDepth: 0,
			MaxDepth: 1,
		})
		pass.SetScissorRect(gputypes.ScissorRect{
			X:      uint32(vp.Min.X),
			Y:      uint32(vp.Min.Y),
			Width:  uint32(vp.Dx()),
			Height: uint32(vp.Dy()),
		})
	}
	pass.SetPipeline(pipe)
	pass.SetBindGroup(0, p.resolveUniformBind, nil)
	pass.SetBindGroup(1, p.resolveTexBind, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})
	return pass.End()
}

func (g *gpuRenderer) resolvePostWithOverlays(dev *wgpu.Device, enc *wgpu.CommandEncoder, ps *gpuPostShared, f Frame, target GPUTarget, s int, bloomGain float32) error {
	p := &g.post
	if p.overlayW != f.Width || p.overlayH != f.Height || p.overlayView == nil {
		p.releaseOverlay()
		var err error
		if p.overlayTex, p.overlayView, err = createPostTexture(dev, "nt3d post overlay", f.Width, f.Height); err != nil {
			return err
		}
		p.overlayW, p.overlayH = f.Width, f.Height
	}

	var postPipe, uiPipe *wgpu.RenderPipeline
	var err error
	if g.shared != nil {
		g.shared.mu.Lock()
		postPipe, err = ps.resolvePipeline(dev, sceneFormat, false)
		if err == nil {
			uiPipe, err = ps.ensureOverlayPipeline(dev, g.layout, g.shader)
		}
		g.shared.mu.Unlock()
	} else {
		postPipe, err = ps.resolvePipeline(dev, sceneFormat, false)
		if err == nil {
			uiPipe, err = ps.ensureOverlayPipeline(dev, g.layout, g.shader)
		}
	}
	if err != nil {
		return err
	}

	sceneLin := float32(0)
	transfer := float32(1)
	if f.Linear {
		sceneLin = 1
		transfer = 0
	}
	loadFlag := float32(0)
	if target.Load {
		loadFlag = 2
	}

	var ub [48]byte
	binary.LittleEndian.PutUint32(ub[0:], math.Float32bits(float32(s)))
	binary.LittleEndian.PutUint32(ub[4:], math.Float32bits(transfer))
	binary.LittleEndian.PutUint32(ub[8:], math.Float32bits(loadFlag))
	binary.LittleEndian.PutUint32(ub[12:], math.Float32bits(sceneLin))
	binary.LittleEndian.PutUint32(ub[16:], math.Float32bits(bloomGain))
	binary.LittleEndian.PutUint32(ub[20:], math.Float32bits(exposureScale(f.PostProcess.Exposure)))
	binary.LittleEndian.PutUint32(ub[24:], math.Float32bits(float32(f.PostProcess.ToneMap)))
	binary.LittleEndian.PutUint32(ub[28:], math.Float32bits(f.PostProcess.Vignette))
	binary.LittleEndian.PutUint32(ub[40:], uint32(int32(f.Width)))
	binary.LittleEndian.PutUint32(ub[44:], uint32(int32(f.Height)))
	if err := dev.Queue().WriteBuffer(p.resolveUniform, 0, ub[:]); err != nil {
		return err
	}

	pass, err := enc.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{{View: p.overlayView, LoadOp: gputypes.LoadOpClear, StoreOp: gputypes.StoreOpStore}},
	})
	if err != nil {
		return err
	}
	pass.SetPipeline(postPipe)
	pass.SetBindGroup(0, p.resolveUniformBind, nil)
	pass.SetBindGroup(1, p.resolveTexBind, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: 3, InstanceCount: 1})

	g.setScenePipeline(pass, uiPipe)
	pass.SetBindGroup(0, g.batches[4].bind, nil)
	pass.Draw(gputypes.DrawArgs{VertexCount: g.batches[4].count, InstanceCount: 1})
	if err := pass.End(); err != nil {
		return err
	}

	// Resolve the 1x overlay-composited target into the caller's target.
	return g.resolve1xView(dev, enc, f, target, p.overlayView)
}

func resolvePostTransfer(linear bool, format gputypes.TextureFormat) float32 {
	switch format {
	case gputypes.TextureFormatRGBA8Unorm, gputypes.TextureFormatBGRA8Unorm:
		return 1
	case gputypes.TextureFormatRGBA8UnormSrgb, gputypes.TextureFormatBGRA8UnormSrgb:
		return 0
	default:
		if linear {
			return 0
		}
		return 1
	}
}
