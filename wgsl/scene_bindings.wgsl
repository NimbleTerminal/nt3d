@group(1) @binding(0) var shadowMap: texture_depth_2d_array;
@group(1) @binding(1) var reflectionMap: texture_2d<f32>;
@group(1) @binding(2) var reflectionSampler: sampler;
@group(1) @binding(3) var sceneColor: texture_2d<f32>;
@group(1) @binding(4) var sceneDepth: texture_depth_2d;
@group(1) @binding(5) var screenAO: texture_2d<f32>;
@group(1) @binding(6) var probeMap0: texture_2d<f32>;
@group(1) @binding(7) var probeMap1: texture_2d<f32>;
@group(1) @binding(8) var probeMap2: texture_2d<f32>;
@group(1) @binding(9) var probeMap3: texture_2d<f32>;
struct ReflectionProbe { position:vec4<f32>, minimum:vec4<f32>, maximum:vec4<f32> };
struct Light { a: vec4<f32>, b: vec4<f32>, c: vec4<f32> };
struct Uniforms { matrix: mat4x4<f32>, viewport: vec4<f32>, sky: vec4<f32>, eye: vec4<f32>, forward: vec4<f32>, fog: vec4<f32>, fogColor: vec4<f32>, ground: vec4<f32>, lights: array<Light, maxLights>, shadow: mat4x4<f32>, shadowParams: vec4<f32>, environment: array<vec4<f32>,9>, skyRight: vec4<f32>, skyUp: vec4<f32>, skyForward: vec4<f32>, pointShadows: array<mat4x4<f32>,6>, inverse:mat4x4<f32>, probes:array<ReflectionProbe,maxReflectionProbes> };
struct Item { a: vec4<f32>, b: vec4<f32>, color: vec4<f32> };
@group(0) @binding(0) var<uniform> u: Uniforms;
@group(0) @binding(1) var<storage, read> items: array<Item>;
struct Instance { world:mat4x4<f32>, normal:mat4x4<f32>, outline:vec4<f32>, absorption:vec4<f32>, optics:vec4<f32>, toon:vec4<f32>, toonStyle:vec4<f32>, pbr:vec4<f32>, emissive:vec4<f32>, coat:vec4<f32>, sheen:vec4<f32> };
@group(0) @binding(2) var<storage,read> instances:array<Instance>;
// BEGIN_POINT_ONLY
// Points have records of their own, five words each: position, radius
// (negative in world units), and color as RGBA8. Alpha points are drawn in
// sort order, farthest first; the order of others is a placeholder.
struct PointRecord { x:f32, y:f32, z:f32, radius:f32, color:u32 };
@group(0) @binding(3) var<storage,read> points:array<PointRecord>;
@group(0) @binding(4) var<storage,read> order:array<u32>;
// A sorted draw's run among the frame's sorted points: where it starts in
// order, which holds every alpha geometry's points by slice and slot.
struct Run { start:u32, count:u32, pad:vec2<u32> };
@group(0) @binding(6) var<uniform> run:Run;
// END_POINT_ONLY
// decode turns sRGB-encoded color into linear light, when the frame works in
// linear light (viewport.z); the resolve pass encodes the result.
fn decode(c:vec3<f32>)->vec3<f32> {
 if u.viewport.z<0.5 {return c;}
 return select(pow((c+0.055)/1.055,vec3<f32>(2.4)),c/12.92,c<=vec3<f32>(0.04045));
}
fn clipPosition(pos:vec3<f32>,index:f32)->vec4<f32> {
 let instance=instances[u32(index)];
 if instance.normal[3][3]==0.0 {return vec4<f32>(0.0,0.0,2.0,1.0);}
 return u.matrix*instance.world*vec4<f32>(pos,1.0);
}
// Fragments carry, besides color, what highlights and fog need: the world
// position (whose distance from the camera, not affine, is found per
// fragment) and normal, and the instance's specular strength and exponent
// (its normal matrix's spare column).
struct Out { @builtin(position) clip: vec4<f32>, @location(0) color: vec4<f32>, @location(1) uv: vec2<f32>, @location(2) @interpolate(flat) point: f32, @location(3) @interpolate(flat) repeat: f32, @location(4) cut: f32, @location(5) world: vec3<f32>, @location(6) normal: vec3<f32>, @location(7) @interpolate(flat) spec: vec2<f32>, @location(8) @interpolate(flat) normalStrength:f32, @location(9) @interpolate(flat) rect:vec4<f32>, @location(10) normalUV:vec4<f32>, @location(11) @interpolate(flat) full:f32, @location(12) tangent:vec4<f32>, @location(13) flowFrame:vec3<f32>, @location(14) @interpolate(flat) absorption:vec4<f32>, @location(15) @interpolate(flat) optics:vec4<f32> };
// eyeDistance is a world point's distance from the camera, as fog measures
// it: straight, or along the view for orthographic cameras.
fn eyeDistance(world:vec3<f32>)->f32 {
 if u.eye.w>0.5 {return length(world-u.eye.xyz);}
 return dot(world-u.eye.xyz,u.forward.xyz);
}
fn worldPosition(pos:vec3<f32>,index:f32)->vec3<f32> {return (instances[u32(index)].world*vec4<f32>(pos,1.0)).xyz;}
fn output(pos: vec3<f32>, normal: vec3<f32>, color: vec4<f32>) -> Out {
 var o: Out; o.clip=clipPosition(pos,color.w);
 let inst=instances[u32(color.w)];
 o.world=worldPosition(pos,color.w);o.absorption=vec4<f32>(inst.absorption.xyz,0.0);o.optics=vec4<f32>(inst.absorption.w,inst.optics.xy,0.0);
 if dot(normal,normal)>0.0 {
  o.normal=normalize((inst.normal*vec4<f32>(normal,0.0)).xyz);o.spec=inst.normal[3].xy;o.absorption.w=inst.normal[2].w;
 }
 o.color=vec4<f32>(decode(color.rgb),1.0);o.uv=vec2<f32>(0.0);o.point=0.0;return o;
}
fn instanceBaseColor(ii:u32)->vec4<f32> {
 let c=instances[ii].coat;
 if c.w<0.5 {return vec4<f32>(1.0);}
 let rgb=u32(c.z);
 return vec4<f32>(decode(vec3<f32>(f32((rgb>>16u)&255u),f32((rgb>>8u)&255u),f32(rgb&255u))/255.0),c.w-1.0);
}
