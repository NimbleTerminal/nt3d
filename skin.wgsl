struct Item { a:vec4<f32>, b:vec4<f32>, color:vec4<f32> };
struct Influence { joints:vec4<u32>, weights:vec4<f32> };
@group(0) @binding(0) var<storage,read> source:array<Item>;
@group(0) @binding(1) var<storage,read> influences:array<Influence>;
@group(0) @binding(2) var<storage,read> palette:array<mat4x4<f32>>;
@group(0) @binding(3) var<storage,read_write> posed:array<Item>;
@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) id:vec3<u32>) {
 let i=id.x;
 if i>=arrayLength(&source) {return;}
 let v=source[i];let influence=influences[i];
 var p=vec3<f32>(0.0);var n=vec3<f32>(0.0);var sum=0.0;
 for(var k=0u;k<4u;k++) {
  let w=influence.weights[k];
  if w>0.0 {
   let m=palette[influence.joints[k]];
   p+=(m*vec4<f32>(v.a.xyz,1.0)).xyz*w;
   n+=(m*vec4<f32>(v.b.xyz,0.0)).xyz*w;sum+=w;
  }
 }
 if sum==0.0 {p=v.a.xyz;n=v.b.xyz;}
 if dot(n,n)>0.0 {n=normalize(n);}
 posed[i]=Item(vec4<f32>(p,v.a.w),vec4<f32>(n,v.b.w),v.color);
}
