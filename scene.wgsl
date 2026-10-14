struct Uniforms { matrix: mat4x4<f32>, viewport: vec4<f32>, light: vec4<f32>, eye: vec4<f32>, forward: vec4<f32> };
struct Item { a: vec4<f32>, b: vec4<f32>, color: vec4<f32> };
@group(0) @binding(0) var<uniform> u: Uniforms;
@group(0) @binding(1) var<storage, read> items: array<Item>;
struct Instance { world:mat4x4<f32>, normal:mat4x4<f32> };
@group(0) @binding(2) var<storage,read> instances:array<Instance>;
fn clipPosition(pos:vec3<f32>,index:f32)->vec4<f32> {
 let instance=instances[u32(index)];
 if instance.normal[3][3]==0.0 {return vec4<f32>(0.0,0.0,2.0,1.0);}
 return u.matrix*instance.world*vec4<f32>(pos,1.0);
}
struct Out { @builtin(position) clip: vec4<f32>, @location(0) color: vec4<f32>, @location(1) uv: vec2<f32>, @location(2) @interpolate(flat) point: f32, @location(3) @interpolate(flat) repeat: f32, @location(4) cut: f32 };
fn output(pos: vec3<f32>, normal: vec3<f32>, color: vec4<f32>) -> Out {
 var o: Out; o.clip=clipPosition(pos,color.w); var light=1.0;
 if dot(normal,normal)>0.0 { light=u.light.w+(1.0-u.light.w)*max(0.0,dot(normalize((instances[u32(color.w)].normal*vec4<f32>(normal,0.0)).xyz),normalize(u.light.xyz))); }
 o.color=vec4<f32>(color.rgb*light,1.0);o.uv=vec2<f32>(0.0);o.point=0.0;return o;
}
@vertex fn vs_point(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {
 var corners=array<vec2<f32>,6>(vec2<f32>(-1.,-1.),vec2<f32>(1.,-1.),vec2<f32>(1.,1.),vec2<f32>(-1.,-1.),vec2<f32>(1.,1.),vec2<f32>(-1.,1.));
 let p=items[ii];let corner=corners[vi];var o=output(p.a.xyz,vec3<f32>(0.0),p.color);
 o.clip=vec4<f32>(o.clip.xy+corner*p.a.w*2.0/u.viewport.xy*o.clip.w,o.clip.zw);o.uv=corner;o.point=1.0;return o;
}
// Mesh records carry opacity in a.w: one, unless the mesh is transparent.
// Transparent meshes also draw their back faces, lit as seen from behind.
fn meshOutput(vi:u32,side:f32)->Out {let p=items[vi];var o=output(p.a.xyz,p.b.xyz*side,p.color);o.color.a=p.a.w;return o;}
@vertex fn vs_mesh(@builtin(vertex_index) vi:u32)->Out {return meshOutput(vi,1.0);}
@vertex fn vs_mesh_back(@builtin(vertex_index) vi:u32)->Out {return meshOutput(vi,-1.0);}
// Depth-clip the centerline before expanding it in pixel space.
struct StrokeClip { a:vec4<f32>, b:vec4<f32>, t0:f32, t1:f32, visible:u32 };
fn clipStroke(a:vec4<f32>,b:vec4<f32>)->StrokeClip {
 var s=StrokeClip(a,b,0.0,1.0,1u);
 for(var plane=0u;plane<3u;plane=plane+1u) {
  var da=s.a.z;var db=s.b.z;
  if plane==1u {da=s.a.w-s.a.z;db=s.b.w-s.b.z;}
  if plane==2u {da=s.a.w-0.00001;db=s.b.w-0.00001;}
  if da<0.0 && db<0.0 {s.visible=0u;return s;}
  if (da<0.0)!=(db<0.0) {
   let t=da/(da-db);let p=mix(s.a,s.b,t);let param=mix(s.t0,s.t1,t);
   if da<0.0 {s.a=p;s.t0=param;}else{s.b=p;s.t1=param;}
  }
 }
 return s;
}
fn strokeOutput(vi:u32,a:vec4<f32>,b:vec4<f32>,ca:vec4<f32>,cb:vec4<f32>,width:f32,head:f32,bias:f32)->Out {
 let s=clipStroke(a,b);
 var o:Out;o.uv=vec2<f32>(0.0);o.point=0.0;o.color=ca;
 o.clip=vec4<f32>(0.0,0.0,2.0,1.0);
 if s.visible==0u {return o;}
 let pa=s.a.xy/s.a.w*u.viewport.xy*0.5;
 let pb=s.b.xy/s.b.w*u.viewport.xy*0.5;
 let delta=pb-pa;let length=length(delta);
 let atEnd=vi==2u || vi==4u || vi==5u;
 let side=select(-1.0,1.0,vi==1u || vi==2u || vi==4u);
 var offset=vec2<f32>(0.0);var fraction=0.0;
 if length<0.00001 {
  if vi<6u {offset=vec2<f32>(select(-0.5,0.5,atEnd),side*0.5)*width;}
 }else{
  let direction=delta/length;let perpendicular=vec2<f32>(-direction.y,direction.x);
  var hl=0.0;if s.t1>=1.0 {hl=min(head,length*0.45);}
  if vi<6u {
   fraction=select(0.0,1.0-hl/length,atEnd);
   offset=perpendicular*side*width*0.5;
  }else{
   fraction=1.0-hl/length;
   let hw=max(width*0.75,hl*0.5);
   if vi==6u {offset=perpendicular*hw;}
   if vi==7u {fraction=1.0;}
   if vi==8u {offset=-perpendicular*hw;}
   if hl==0.0 {fraction=1.0;offset=vec2<f32>(0.0);}
  }
 }
 let inverseW=mix(1.0/s.a.w,1.0/s.b.w,fraction);let w=1.0/inverseW;
 let param=mix(s.t0,s.t1,(fraction/s.b.w)/inverseW);
 let position=mix(pa,pb,fraction)+offset;
 let depth=mix(s.a.z/s.a.w,s.b.z/s.b.w,fraction)+bias;
 o.clip=vec4<f32>(position*2.0/u.viewport.xy*w,depth*w,w);
 o.color=vec4<f32>(mix(ca.rgb,cb.rgb,param),1.0);
 return o;
}
fn lineOutput(vi:u32,ii:u32,bias:f32)->Out {
 let a=items[ii*2u];let b=items[ii*2u+1u];
 return strokeOutput(vi,clipPosition(a.a.xyz,a.color.w),clipPosition(b.a.xyz,b.color.w),a.color,b.color,a.a.w,0.0,bias);
}
@vertex fn vs_line(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out { return lineOutput(vi,ii,-0.00001); }
@vertex fn vs_dynamic(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out { return lineOutput(vi,ii,0.00002); }
@vertex fn vs_arrow(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {
 let a=items[ii];return strokeOutput(vi,clipPosition(a.a.xyz,a.color.w),clipPosition(a.b.xyz,a.color.w),a.color,a.color,a.a.w,a.b.w,0.0);
}
@vertex fn vs_ui(@builtin(vertex_index) vi:u32)->Out {let p=items[vi];var o:Out;let x=p.a.x/u.viewport.x*2.0-1.0;let y=1.0-p.a.y/u.viewport.y*2.0;o.clip=vec4<f32>(x,y,0.0,1.0);o.color=p.color;o.uv=vec2<f32>(0.0);o.point=0.0;return o;}
@vertex fn vs_box(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {
var corners=array<vec3<f32>,36>(vec3<f32>(0.0,0.0,0.0),vec3<f32>(0.0,1.0,0.0),vec3<f32>(1.0,1.0,0.0),vec3<f32>(0.0,0.0,0.0),vec3<f32>(1.0,1.0,0.0),vec3<f32>(1.0,0.0,0.0),vec3<f32>(0.0,0.0,1.0),vec3<f32>(1.0,0.0,1.0),vec3<f32>(1.0,1.0,1.0),vec3<f32>(0.0,0.0,1.0),vec3<f32>(1.0,1.0,1.0),vec3<f32>(0.0,1.0,1.0),vec3<f32>(0.0,0.0,0.0),vec3<f32>(1.0,0.0,0.0),vec3<f32>(1.0,0.0,1.0),vec3<f32>(0.0,0.0,0.0),vec3<f32>(1.0,0.0,1.0),vec3<f32>(0.0,0.0,1.0),vec3<f32>(0.0,1.0,0.0),vec3<f32>(0.0,1.0,1.0),vec3<f32>(1.0,1.0,1.0),vec3<f32>(0.0,1.0,0.0),vec3<f32>(1.0,1.0,1.0),vec3<f32>(1.0,1.0,0.0),vec3<f32>(0.0,0.0,0.0),vec3<f32>(0.0,0.0,1.0),vec3<f32>(0.0,1.0,1.0),vec3<f32>(0.0,0.0,0.0),vec3<f32>(0.0,1.0,1.0),vec3<f32>(0.0,1.0,0.0),vec3<f32>(1.0,0.0,0.0),vec3<f32>(1.0,1.0,0.0),vec3<f32>(1.0,1.0,1.0),vec3<f32>(1.0,0.0,0.0),vec3<f32>(1.0,1.0,1.0),vec3<f32>(1.0,0.0,1.0));
var normals=array<vec3<f32>,6>(vec3<f32>(0.0,0.0,-1.0),vec3<f32>(0.0,0.0,1.0),vec3<f32>(0.0,-1.0,0.0),vec3<f32>(0.0,1.0,0.0),vec3<f32>(-1.0,0.0,0.0),vec3<f32>(1.0,0.0,0.0));
let p=items[ii];return output(p.a.xyz+corners[vi]*p.b.xyz,normals[vi/6u],p.color);}
@fragment fn fs_main(o:Out)->@location(0) vec4<f32> {if o.point>0.0 && dot(o.uv,o.uv)>1.0 {discard;} return o.color;}

@group(1) @binding(0) var mapTexture: texture_2d<f32>;
@group(1) @binding(1) var mapSampler: sampler;
// Textured records carry the cutout threshold, or a transparent vertex's
// opacity, where color would be; it reaches the fragment in cut.
// Flags: 1 repeat, 2 edge-on, 4 tint, whose color is a 24-bit integer.
// (Helpers named texture... are taken for WGSL builtins by the shader
// translator, hence mapOutput.)
fn mapOutput(vi:u32,side:f32)->Out {
 let p=items[vi];let flags=u32(p.color.y);var tint=vec3<f32>(1.0);
 if (flags&4u)!=0u {let c=u32(p.color.z);tint=vec3<f32>(f32((c>>16u)&255u),f32((c>>8u)&255u),f32(c&255u))/255.0;}
 var o=output(p.a.xyz,p.b.xyz*side,vec4<f32>(tint,p.color.w));o.uv=vec2<f32>(p.a.w,p.b.w);o.repeat=f32(flags&1u);o.cut=p.color.x;
 // Edge-on cutouts fade out as the surface turns to face the viewer.
 if (flags&2u)!=0u && dot(p.b.xyz,p.b.xyz)>0.0 {
  let inst=instances[u32(p.color.w)];
  let world=(inst.world*vec4<f32>(p.a.xyz,1.0)).xyz;
  let n=normalize((inst.normal*vec4<f32>(p.b.xyz,0.0)).xyz);
  var view=-u.forward.xyz;
  if u.eye.w>0.5 {view=normalize(u.eye.xyz-world);}
  if dot(view,view)>0.0 {o.cut=mix(p.color.x,1.01,smoothstep(0.15,0.5,abs(dot(n,normalize(view)))));}
 }
 return o;
}
@vertex fn vs_texture(@builtin(vertex_index) vi:u32)->Out {return mapOutput(vi,1.0);}
@vertex fn vs_texture_back(@builtin(vertex_index) vi:u32)->Out {return mapOutput(vi,-1.0);}
// sampleMap filters the texture bilinearly. The sampler clamps to the edge,
// so a repeating material filters by hand, wrapping the four texels it
// blends; wrapping the coordinate alone would blend tiles' edges with clamped
// texels and show a seam between them. Textures have no mipmaps, so sampling
// at level zero is exact, and allowed where materials differ by primitive.
fn sampleMap(uv:vec2<f32>,repeat:f32)->vec4<f32> {
 if repeat<0.5 {return textureSampleLevel(mapTexture,mapSampler,uv,0.0);}
 let size=vec2<i32>(textureDimensions(mapTexture,0));
 let p=uv*vec2<f32>(size)-0.5;let f=floor(p);let t=p-f;let i=vec2<i32>(f);
 let i0=((i%size)+size)%size;let i1=(((i+1)%size)+size)%size;
 let a=textureLoad(mapTexture,i0,0);let b=textureLoad(mapTexture,vec2<i32>(i1.x,i0.y),0);
 let c=textureLoad(mapTexture,vec2<i32>(i0.x,i1.y),0);let d=textureLoad(mapTexture,i1,0);
 return mix(mix(a,b,t.x),mix(c,d,t.x),t.y);
}
@fragment fn fs_texture(o:Out)->@location(0) vec4<f32> {
 return vec4<f32>(sampleMap(o.uv,o.repeat).rgb*o.color.rgb,1.0);
}
// Transparent materials blend by the texture's alpha times the vertex's.
@fragment fn fs_blend_texture(o:Out)->@location(0) vec4<f32> {
 let t=sampleMap(o.uv,o.repeat);
 return vec4<f32>(t.rgb*o.color.rgb,t.a*o.cut);
}
// Cutout materials skip texels under their threshold; what remains is opaque.
@fragment fn fs_cutout(o:Out)->@location(0) vec4<f32> {
 let t=sampleMap(o.uv,o.repeat);
 if t.a<o.cut {discard;}
 return vec4<f32>(t.rgb*o.color.rgb,1.0);
}
