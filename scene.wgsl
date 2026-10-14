struct Uniforms { matrix: mat4x4<f32>, viewport: vec4<f32>, light: vec4<f32>, reserved: vec4<f32> };
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
struct Out { @builtin(position) clip: vec4<f32>, @location(0) color: vec4<f32>, @location(1) uv: vec2<f32>, @location(2) @interpolate(flat) point: f32 };
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
@vertex fn vs_mesh(@builtin(vertex_index) vi:u32)->Out {let p=items[vi];return output(p.a.xyz,p.b.xyz,p.color);}
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
// Textured records carry the cutout threshold where color would be; it
// reaches the fragment in point, which only points otherwise use.
@vertex fn vs_texture(@builtin(vertex_index) vi:u32)->Out {
 let p=items[vi];var o=output(p.a.xyz,p.b.xyz,vec4<f32>(1.0,1.0,1.0,p.color.w));o.uv=vec2<f32>(p.a.w,p.b.w);o.point=p.color.x;return o;
}
@fragment fn fs_texture(o:Out)->@location(0) vec4<f32> {
 return vec4<f32>(textureSample(mapTexture,mapSampler,o.uv).rgb*o.color.rgb,1.0);
}
// Cutout materials skip texels under their threshold; what remains is opaque.
@fragment fn fs_cutout(o:Out)->@location(0) vec4<f32> {
 let t=textureSample(mapTexture,mapSampler,o.uv);
 if t.a<o.point {discard;}
 return vec4<f32>(t.rgb*o.color.rgb,1.0);
}
