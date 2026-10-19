struct Item { a:vec4<f32>, b:vec4<f32>, color:vec4<f32> };
struct Influence { joints:vec4<u32>, weights:vec4<f32> };
@group(0) @binding(0) var<storage,read> source:array<Item>;
@group(0) @binding(1) var<storage,read> influences:array<Influence>;
@group(0) @binding(2) var<storage,read> palette:array<mat4x4<f32>>;
@group(0) @binding(3) var<storage,read_write> posed:array<Item>;
// Records are one per index: the mesh's own, then, for a normal-mapped
// mesh with an extension record each, those, whose tangent (b.xyz) turns
// with the same joints as a direction and keeps its handedness (b.w).
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) id:vec3<u32>) {
 let i=id.x;let n=arrayLength(&influences);
 if i>=arrayLength(&source) || i>=2u*n {return;}
 let v=source[i];let influence=influences[i%n];
 if i>=n {
  var t=vec3<f32>(0.0);var sum=0.0;
  for(var k=0u;k<4u;k++) {
   let w=influence.weights[k];
   if w>0.0 {t+=(palette[influence.joints[k]]*vec4<f32>(v.b.xyz,0.0)).xyz*w;sum+=w;}
  }
  if sum==0.0 {t=v.b.xyz;}
  if dot(t,t)>0.0 {t=normalize(t);}
  posed[i]=Item(v.a,vec4<f32>(t,v.b.w),v.color);return;
 }
 var p=vec3<f32>(0.0);var nrm=vec3<f32>(0.0);var sum=0.0;
 for(var k=0u;k<4u;k++) {
  let w=influence.weights[k];
  if w>0.0 {
   let m=palette[influence.joints[k]];
   p+=(m*vec4<f32>(v.a.xyz,1.0)).xyz*w;
   nrm+=(m*vec4<f32>(v.b.xyz,0.0)).xyz*w;sum+=w;
  }
 }
 if sum==0.0 {p=v.a.xyz;nrm=v.b.xyz;}
 if dot(nrm,nrm)>0.0 {nrm=normalize(nrm);}
 posed[i]=Item(vec4<f32>(p,v.a.w),vec4<f32>(nrm,v.b.w),v.color);
}
