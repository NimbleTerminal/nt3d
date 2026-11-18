// BEGIN_POINT_ONLY
// A point is a quad about its center, of its radius on screen: a pixel
// radius as it is, a world radius by the projection's vertical scale at the
// point's depth (the matrix's clip-y row over w), clamped to half a pixel
// and maxPointPixels. Blended points carry their alpha in cut.
fn pointOutput(vi:u32,packed:u32)->Out {
 var corners=array<vec2<f32>,6>(vec2<f32>(-1.,-1.),vec2<f32>(1.,-1.),vec2<f32>(1.,1.),vec2<f32>(-1.,-1.),vec2<f32>(1.,1.),vec2<f32>(-1.,1.));
 let index=packed & 0xFFFFFFu;
 var lodScale=1.0;
 let f8=packed>>24u;
 if f8>0u && f8<255u {lodScale=inverseSqrt(f32(f8)/255.0);}
 let p=points[index];let corner=corners[vi];
 let c=vec4<f32>(f32(p.color&255u),f32((p.color>>8u)&255u),f32((p.color>>16u)&255u),f32(p.color>>24u))/255.0;
 var o=output(vec3<f32>(p.x,p.y,p.z),vec3<f32>(0.0),vec4<f32>(c.rgb,0.0));
 var r=p.radius*lodScale;
 if r<0.0 {
  let row=vec3<f32>(u.matrix[0][1],u.matrix[1][1],u.matrix[2][1]);
  r=clamp(-r*length(row)*u.viewport.y*0.5/o.clip.w,0.5,maxPointPixels);
 } else if lodScale>1.0 {
  r=min(r,32.0);
 }
 o.clip=vec4<f32>(o.clip.xy+corner*r*2.0/u.viewport.xy*o.clip.w,o.clip.zw);o.uv=corner;o.point=1.0;o.cut=c.a;return o;
}
@vertex fn vs_point(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return pointOutput(vi,ii);}
@vertex fn vs_point_sorted(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return pointOutput(vi,order[run.start+ii]);}
// END_POINT_ONLY
// Mesh records carry opacity in a.w: one, unless the mesh is transparent.
// Transparent meshes also draw their back faces, lit as seen from behind.
// Meshes are drawn instanced: each instance's transform is its own.
fn meshOutput(vi:u32,ii:u32,side:f32)->Out {let p=items[vi];let bc=instanceBaseColor(ii);var o=output(p.a.xyz,p.b.xyz*side,vec4<f32>(p.color.rgb,f32(ii)));o.color=vec4<f32>(o.color.rgb*bc.rgb,p.a.w*bc.a);o.optics.w=f32(ii)+1.0;if instances[ii].normal[0].w>0.5 {o.normal=vec3<f32>(0.0);o.spec=vec2<f32>(0.0);}return o;}
@vertex fn vs_mesh(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return meshOutput(vi,ii,1.0);}
@vertex fn vs_mesh_back(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return meshOutput(vi,ii,-1.0);}
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
// Strokes carry their world points, wa to wb, for fog. Their colors mix in
// the frame's working space, linear light or encoded, as the rasterizer
// mixes between vertices.
fn strokeOutput(vi:u32,a:vec4<f32>,b:vec4<f32>,ca:vec4<f32>,cb:vec4<f32>,width:f32,head:f32,bias:f32,wa:vec3<f32>,wb:vec3<f32>)->Out {
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
 o.color=vec4<f32>(mix(decode(ca.rgb),decode(cb.rgb),param),1.0);o.world=mix(wa,wb,param);
 return o;
}
fn lineOutput(vi:u32,ii:u32,bias:f32)->Out {
 let a=items[ii*2u];let b=items[ii*2u+1u];
 return strokeOutput(vi,clipPosition(a.a.xyz,a.color.w),clipPosition(b.a.xyz,b.color.w),a.color,b.color,a.a.w,0.0,bias,worldPosition(a.a.xyz,a.color.w),worldPosition(b.a.xyz,b.color.w));
}
@vertex fn vs_line(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out { return lineOutput(vi,ii,-0.00001); }
@vertex fn vs_dynamic(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out { return lineOutput(vi,ii,0.00002); }
// Crease lines are a mesh's sharp edges, endpoint pairs in its own space,
// drawn once for each of its instances: six vertices a segment, in the
// instance's outline color and width (negative in world units, as the hull's).
// Endpoints carry UV in (a.w, b.w), threshold or vertex opacity in color.x,
// and flags in color.y (1 repeat, 2 per-instance displacement).
fn creaseOutput(vi:u32,ii:u32)->Out {
 let seg=vi/6u;let corner=vi%6u;
 let a=items[seg*2u];let b=items[seg*2u+1u];
 let inst=instances[ii];let index=f32(ii);
 let flags=u32(a.color.y);
 let uva=vec2<f32>(a.a.w,a.b.w);let uvb=vec2<f32>(b.a.w,b.b.w);
 var pa=a.a.xyz;var pb=b.a.xyz;
 if nt3dHasDisplace && (flags&2u)!=0u {
  pa=nt3d_displace(pa,a.b.xyz,(inst.world*vec4<f32>(pa,1.0)).xyz,uva).position;
  pb=nt3d_displace(pb,b.b.xyz,(inst.world*vec4<f32>(pb,1.0)).xyz,uvb).position;
 }
 let ca=clipPosition(pa,index);let cb=clipPosition(pb,index);
 let s=clipStroke(ca,cb);
 var width=inst.outline.w;
 if width<0.0 {
  let row=vec3<f32>(u.matrix[0][1],u.matrix[1][1],u.matrix[2][1]);
  width=clamp(-width*length(row)*u.viewport.y*0.5/max(0.5*(ca.w+cb.w),0.00001),0.0,32.0);
 }
 let ink=vec4<f32>(inst.outline.rgb,1.0);
 var o=strokeOutput(corner,ca,cb,ink,ink,width,0.0,-0.00001,worldPosition(pa,index),worldPosition(pb,index));
 if s.visible!=0u {
  let atEnd=corner==2u || corner==4u || corner==5u;
  let param=select(s.t0,s.t1,atEnd);
  let alpha=mix(a.color.x,b.color.x,param)*instanceBaseColor(ii).a;
  o.uv=mix(uva,uvb,param);
  o.cut=alpha;
  o.color.a=alpha;
  o.repeat=select(0.0,1.0,(flags&1u)!=0u && inst.normal[3].z>0.5);
 }
 return o;
}
@vertex fn vs_crease(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return creaseOutput(vi,ii);}
@vertex fn vs_arrow(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {
 let a=items[ii];return strokeOutput(vi,clipPosition(a.a.xyz,a.color.w),clipPosition(a.b.xyz,a.color.w),a.color,a.color,a.a.w,a.b.w,0.0,worldPosition(a.a.xyz,a.color.w),worldPosition(a.b.xyz,a.color.w));
}
@vertex fn vs_ui(@builtin(vertex_index) vi:u32)->Out {let p=items[vi];var o:Out;let x=p.a.x/u.viewport.x*2.0-1.0;let y=1.0-p.a.y/u.viewport.y*2.0;o.clip=vec4<f32>(x,y,0.0,1.0);o.color=vec4<f32>(decode(p.color.rgb),p.color.a);o.uv=vec2<f32>(0.0);o.point=0.0;return o;}
fn boxOutput(vi:u32,ii:u32)->Out {
var corners=array<vec3<f32>,36>(vec3<f32>(0.0,0.0,0.0),vec3<f32>(0.0,1.0,0.0),vec3<f32>(1.0,1.0,0.0),vec3<f32>(0.0,0.0,0.0),vec3<f32>(1.0,1.0,0.0),vec3<f32>(1.0,0.0,0.0),vec3<f32>(0.0,0.0,1.0),vec3<f32>(1.0,0.0,1.0),vec3<f32>(1.0,1.0,1.0),vec3<f32>(0.0,0.0,1.0),vec3<f32>(1.0,1.0,1.0),vec3<f32>(0.0,1.0,1.0),vec3<f32>(0.0,0.0,0.0),vec3<f32>(1.0,0.0,0.0),vec3<f32>(1.0,0.0,1.0),vec3<f32>(0.0,0.0,0.0),vec3<f32>(1.0,0.0,1.0),vec3<f32>(0.0,0.0,1.0),vec3<f32>(0.0,1.0,0.0),vec3<f32>(0.0,1.0,1.0),vec3<f32>(1.0,1.0,1.0),vec3<f32>(0.0,1.0,0.0),vec3<f32>(1.0,1.0,1.0),vec3<f32>(1.0,1.0,0.0),vec3<f32>(0.0,0.0,0.0),vec3<f32>(0.0,0.0,1.0),vec3<f32>(0.0,1.0,1.0),vec3<f32>(0.0,0.0,0.0),vec3<f32>(0.0,1.0,1.0),vec3<f32>(0.0,1.0,0.0),vec3<f32>(1.0,0.0,0.0),vec3<f32>(1.0,1.0,0.0),vec3<f32>(1.0,1.0,1.0),vec3<f32>(1.0,0.0,0.0),vec3<f32>(1.0,1.0,1.0),vec3<f32>(1.0,0.0,1.0));
var normals=array<vec3<f32>,6>(vec3<f32>(0.0,0.0,-1.0),vec3<f32>(0.0,0.0,1.0),vec3<f32>(0.0,-1.0,0.0),vec3<f32>(0.0,1.0,0.0),vec3<f32>(-1.0,0.0,0.0),vec3<f32>(1.0,0.0,0.0));
// Materials, and so highlights, apply to meshes only.
let p=items[ii];var o=output(p.a.xyz+corners[vi]*p.b.xyz,normals[vi/6u],p.color);o.spec=vec2<f32>(0.0);return o;}
// Overlays are flat screen rectangles: no highlight, no fog.
@fragment fn fs_overlay(o:Out)->@location(0) vec4<f32> {return o.color;}
@fragment fn fs_main(o:Out)->@location(0) vec4<f32> {if o.point>0.0 && dot(o.uv,o.uv)>1.0 {discard;} return vec4<f32>(finish(o,o.color.rgb),o.color.a);}
// Blended points: alpha (cut) times, for soft points, (1-r²)², r the
// distance from the center over the radius. Alpha points fog as surfaces
// do; additive ones fade out with fog instead, adding less.
fn softness(o:Out)->f32 {let s=max(0.0,1.0-dot(o.uv,o.uv));return s*s;}
@fragment fn fs_point_alpha(o:Out)->@location(0) vec4<f32> {if dot(o.uv,o.uv)>1.0 {discard;} return vec4<f32>(finish(o,o.color.rgb),(o.cut*softIntersection(o)));}
@fragment fn fs_point_alpha_soft(o:Out)->@location(0) vec4<f32> {if dot(o.uv,o.uv)>1.0 {discard;} return vec4<f32>(finish(o,o.color.rgb),(o.cut*softIntersection(o))*softness(o));}
@fragment fn fs_point_add(o:Out)->@location(0) vec4<f32> {if dot(o.uv,o.uv)>1.0 {discard;} return vec4<f32>(o.color.rgb,(o.cut*softIntersection(o))*(1.0-fogAmount(o.world)));}
@fragment fn fs_point_add_soft(o:Out)->@location(0) vec4<f32> {if dot(o.uv,o.uv)>1.0 {discard;} return vec4<f32>(o.color.rgb,(o.cut*softIntersection(o))*softness(o)*(1.0-fogAmount(o.world)));}

