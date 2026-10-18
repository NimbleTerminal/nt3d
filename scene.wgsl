@group(1) @binding(0) var shadowMap: texture_depth_2d;
struct Light { a: vec4<f32>, b: vec4<f32> };
struct Uniforms { matrix: mat4x4<f32>, viewport: vec4<f32>, sky: vec4<f32>, eye: vec4<f32>, forward: vec4<f32>, fog: vec4<f32>, fogColor: vec4<f32>, ground: vec4<f32>, lights: array<Light, maxLights>, shadow: mat4x4<f32>, shadowParams: vec4<f32>, environment: array<vec4<f32>,4>, skyRight: vec4<f32>, skyUp: vec4<f32>, skyForward: vec4<f32> };
struct Item { a: vec4<f32>, b: vec4<f32>, color: vec4<f32> };
@group(0) @binding(0) var<uniform> u: Uniforms;
@group(0) @binding(1) var<storage, read> items: array<Item>;
struct Instance { world:mat4x4<f32>, normal:mat4x4<f32> };
@group(0) @binding(2) var<storage,read> instances:array<Instance>;
// Points have records of their own, five words each: position, radius
// (negative in world units), and color as RGBA8. Alpha points are drawn in
// order, farthest first; the order of others is a placeholder.
struct PointRecord { x:f32, y:f32, z:f32, radius:f32, color:u32 };
@group(0) @binding(3) var<storage,read> points:array<PointRecord>;
@group(0) @binding(4) var<storage,read> order:array<u32>;
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
struct Out { @builtin(position) clip: vec4<f32>, @location(0) color: vec4<f32>, @location(1) uv: vec2<f32>, @location(2) @interpolate(flat) point: f32, @location(3) @interpolate(flat) repeat: f32, @location(4) cut: f32, @location(5) world: vec3<f32>, @location(6) normal: vec3<f32>, @location(7) @interpolate(flat) spec: vec2<f32> };
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
 o.world=worldPosition(pos,color.w);
 if dot(normal,normal)>0.0 {
  o.normal=normalize((inst.normal*vec4<f32>(normal,0.0)).xyz);o.spec=inst.normal[3].xy;
 }
 o.color=vec4<f32>(decode(color.rgb),1.0);o.uv=vec2<f32>(0.0);o.point=0.0;return o;
}
// toward is the unit direction from world point p toward a light (xyz) and
// how much of its light reaches p (w): all of a directional light's; a
// point light's fading to nothing at its range, or as 1/(1+d²) without one,
// and none from a light at p.
fn toward(l:Light,p:vec3<f32>)->vec4<f32> {
 if l.a.w<0.5 {return vec4<f32>(l.a.xyz,1.0);}
 let v=l.a.xyz-p;let d2=dot(v,v);
 if d2==0.0 {return vec4<f32>(0.0);}
 var fade=1.0/(1.0+d2);
 if l.b.w>0.0 {let k=max(0.0,1.0-d2/(l.b.w*l.b.w));fade=k*k;}
 return vec4<f32>(v*inverseSqrt(d2),fade);
}
// Manual depth lookup uses only core textureLoad, with clamped PCF taps.
fn shadowVisibility(p:vec3<f32>,n:vec3<f32>,l:vec3<f32>)->f32 {
 let q=u.shadow*vec4<f32>(p+n*(1.5*u.shadowParams.z),1.0);
 let xy=vec2<f32>(q.x*0.5+0.5,0.5-q.y*0.5)*u.shadowParams.w;
 if any(xy<vec2<f32>(0.0)) || any(xy>=vec2<f32>(u.shadowParams.w)) || q.z<0.0 || q.z>1.0 {return 1.0;}
 let center=vec2<i32>(floor(xy));let size=i32(u.shadowParams.w);
 let bias=u.shadowParams.y*(1.0+2.0*(1.0-max(0.0,dot(n,l))));var lit=0.0;
 for(var y=-1;y<=1;y=y+1) {for(var x=-1;x<=1;x=x+1) {
 let texel=clamp(center+vec2<i32>(x,y),vec2<i32>(0),vec2<i32>(size-1));
 if textureLoad(shadowMap,texel,0)>=q.z-bias {lit+=1.0;}
 }}return lit/9.0;
}
fn lightVisibility(i:u32,p:vec3<f32>,n:vec3<f32>,l:vec3<f32>)->f32 {
 if f32(i)==u.shadowParams.x {return shadowVisibility(p,n,l);}return 1.0;
}
// illuminate is the light reaching a surface with unit normal n at p:
// ambient, from ground to sky as n turns up, and each light's diffuse.
fn illuminate(n:vec3<f32>,p:vec3<f32>)->vec3<f32> {
 var c=mix(u.ground.rgb,u.sky.rgb,n.z*0.5+0.5);
 c+=max(vec3<f32>(0.0),u.environment[0].rgb+u.environment[1].rgb*n.x+u.environment[2].rgb*n.y+u.environment[3].rgb*n.z);
 for(var i=0u;i<u32(u.sky.w);i=i+1u) {let l=u.lights[i];let t=toward(l,p);c+=l.b.rgb*(t.w*max(0.0,dot(n,t.xyz))*lightVisibility(i,p,n,t.xyz));}
 return c;
}
// fogAmount is how far toward the fog's color a world point fades.
fn fogAmount(world:vec3<f32>)->f32 {
 if u.fog.y<=u.fog.x {return 0.0;}
 return clamp((eyeDistance(world)-u.fog.x)/(u.fog.y-u.fog.x),0.0,1.0);
}
// finish lights a surface, adds each light's specular highlight, and fades
// into the fog. Fragments without a normal are unlit.
fn finish(o:Out,rgb:vec3<f32>)->vec3<f32> {
 var c=rgb;
 if dot(o.normal,o.normal)>0.0 {
  let n=normalize(o.normal);
  c*=illuminate(n,o.world);
  if o.spec.x>0.0 {
   var view=-u.forward.xyz;
   if u.eye.w>0.5 {view=normalize(u.eye.xyz-o.world);}
   for(var i=0u;i<u32(u.sky.w);i=i+1u) {
    let l=u.lights[i];let t=toward(l,o.world);let h=t.xyz+view;
    if t.w>0.0 && dot(n,t.xyz)>0.0 && dot(h,h)>0.0 {c+=l.b.rgb*(lightVisibility(i,o.world,n,t.xyz)*t.w*o.spec.x*pow(max(0.0,dot(n,normalize(h))),o.spec.y));}
   }
  }
 }
 c=mix(c,u.fogColor.rgb,fogAmount(o.world));
 return c;
}
// A point is a quad about its center, of its radius on screen: a pixel
// radius as it is, a world radius by the projection's vertical scale at the
// point's depth (the matrix's clip-y row over w), clamped to half a pixel
// and maxPointPixels. Blended points carry their alpha in cut.
fn pointOutput(vi:u32,index:u32)->Out {
 var corners=array<vec2<f32>,6>(vec2<f32>(-1.,-1.),vec2<f32>(1.,-1.),vec2<f32>(1.,1.),vec2<f32>(-1.,-1.),vec2<f32>(1.,1.),vec2<f32>(-1.,1.));
 let p=points[index];let corner=corners[vi];
 let c=vec4<f32>(f32(p.color&255u),f32((p.color>>8u)&255u),f32((p.color>>16u)&255u),f32(p.color>>24u))/255.0;
 var o=output(vec3<f32>(p.x,p.y,p.z),vec3<f32>(0.0),vec4<f32>(c.rgb,0.0));
 var r=p.radius;
 if r<0.0 {
  let row=vec3<f32>(u.matrix[0][1],u.matrix[1][1],u.matrix[2][1]);
  r=clamp(-r*length(row)*u.viewport.y*0.5/o.clip.w,0.5,maxPointPixels);
 }
 o.clip=vec4<f32>(o.clip.xy+corner*r*2.0/u.viewport.xy*o.clip.w,o.clip.zw);o.uv=corner;o.point=1.0;o.cut=c.a;return o;
}
@vertex fn vs_point(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return pointOutput(vi,ii);}
@vertex fn vs_point_sorted(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return pointOutput(vi,order[ii]);}
// Mesh records carry opacity in a.w: one, unless the mesh is transparent.
// Transparent meshes also draw their back faces, lit as seen from behind.
// Meshes are drawn instanced: each instance's transform is its own.
fn meshOutput(vi:u32,ii:u32,side:f32)->Out {let p=items[vi];var o=output(p.a.xyz,p.b.xyz*side,vec4<f32>(p.color.rgb,f32(ii)));o.color.a=p.a.w;return o;}
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
@fragment fn fs_point_alpha(o:Out)->@location(0) vec4<f32> {if dot(o.uv,o.uv)>1.0 {discard;} return vec4<f32>(finish(o,o.color.rgb),o.cut);}
@fragment fn fs_point_alpha_soft(o:Out)->@location(0) vec4<f32> {if dot(o.uv,o.uv)>1.0 {discard;} return vec4<f32>(finish(o,o.color.rgb),o.cut*softness(o));}
@fragment fn fs_point_add(o:Out)->@location(0) vec4<f32> {if dot(o.uv,o.uv)>1.0 {discard;} return vec4<f32>(o.color.rgb,o.cut*(1.0-fogAmount(o.world)));}
@fragment fn fs_point_add_soft(o:Out)->@location(0) vec4<f32> {if dot(o.uv,o.uv)>1.0 {discard;} return vec4<f32>(o.color.rgb,o.cut*softness(o)*(1.0-fogAmount(o.world)));}

@group(2) @binding(0) var mapTexture: texture_2d<f32>;
@group(2) @binding(1) var mapSampler: sampler;
// Textured records carry the cutout threshold, or a transparent vertex's
// opacity, where color would be; it reaches the fragment in cut.
// Flags: 1 repeat, 2 edge-on, 4 tint, whose color is a 24-bit integer.
// (Helpers named texture... are taken for WGSL builtins by the shader
// translator, hence mapOutput.)
fn mapOutput(vi:u32,ii:u32,side:f32)->Out {
 let p=items[vi];let flags=u32(p.color.y);var tint=vec3<f32>(1.0);
 if (flags&4u)!=0u {let c=u32(p.color.z);tint=vec3<f32>(f32((c>>16u)&255u),f32((c>>8u)&255u),f32(c&255u))/255.0;}
 var o=output(p.a.xyz,p.b.xyz*side,vec4<f32>(tint,f32(ii)));o.uv=vec2<f32>(p.a.w,p.b.w);o.cut=p.color.x;
 // Image textures repeat through their samplers; procedural ones (flagged in
 // the instance record) are drawn each frame, clamped, and wrapped by hand.
 o.repeat=select(0.0,1.0,(flags&1u)!=0u && instances[ii].normal[3].z>0.5);
 // Edge-on cutouts fade out as the surface turns to face the viewer.
 if (flags&2u)!=0u && dot(p.b.xyz,p.b.xyz)>0.0 {
  let inst=instances[ii];
  let world=(inst.world*vec4<f32>(p.a.xyz,1.0)).xyz;
  let n=normalize((inst.normal*vec4<f32>(p.b.xyz,0.0)).xyz);
  var view=-u.forward.xyz;
  if u.eye.w>0.5 {view=normalize(u.eye.xyz-world);}
  if dot(view,view)>0.0 {o.cut=mix(p.color.x,1.01,smoothstep(0.15,0.5,abs(dot(n,normalize(view)))));}
 }
 return o;
}
@vertex fn vs_texture(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return mapOutput(vi,ii,1.0);}
@vertex fn vs_texture_back(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return mapOutput(vi,ii,-1.0);}
// sampleMap samples a texture, in linear light (textures are sRGB): through
// its sampler, filtering across mip levels by the coordinates' change from
// pixel to pixel (gx, gy; zero takes full size, as cutouts do). A procedural
// texture that repeats is wrapped by hand, at full size: its sampler clamps,
// and wrapping the coordinate alone would blend tiles' edges with clamped
// texels, showing a seam. Explicit derivatives are allowed where materials
// differ by primitive, outside uniform control flow.
fn sampleMap(uv:vec2<f32>,gx:vec2<f32>,gy:vec2<f32>,manual:f32)->vec4<f32> {
 if manual<0.5 {return textureSampleGrad(mapTexture,mapSampler,uv,gx,gy);}
 let size=vec2<i32>(textureDimensions(mapTexture,0));
 let p=uv*vec2<f32>(size)-0.5;let f=floor(p);let t=p-f;let i=vec2<i32>(f);
 let i0=((i%size)+size)%size;let i1=(((i+1)%size)+size)%size;
 let a=textureLoad(mapTexture,i0,0);let b=textureLoad(mapTexture,vec2<i32>(i1.x,i0.y),0);
 let c=textureLoad(mapTexture,vec2<i32>(i0.x,i1.y),0);let d=textureLoad(mapTexture,i1,0);
 return mix(mix(a,b,t.x),mix(c,d,t.x),t.y);
}
// mapColor is a linear texture sample in the frame's color space.
fn mapColor(c:vec3<f32>)->vec3<f32> {
 if u.viewport.z>0.5 {return c;}
 let v=clamp(c,vec3<f32>(0.0),vec3<f32>(1.0));
 return select(1.055*pow(v,vec3<f32>(1.0/2.4))-0.055,v*12.92,v<=vec3<f32>(0.0031308));
}
@fragment fn fs_texture(o:Out)->@location(0) vec4<f32> {
 let gx=dpdx(o.uv);let gy=dpdy(o.uv);
 return vec4<f32>(finish(o,mapColor(sampleMap(o.uv,gx,gy,o.repeat).rgb)*o.color.rgb),1.0);
}
// Transparent materials blend by the texture's alpha times the vertex's.
@fragment fn fs_blend_texture(o:Out)->@location(0) vec4<f32> {
 let gx=dpdx(o.uv);let gy=dpdy(o.uv);
 let t=sampleMap(o.uv,gx,gy,o.repeat);
 return vec4<f32>(finish(o,mapColor(t.rgb)*o.color.rgb),t.a*o.cut);
}
// Cutout materials skip texels under their threshold, sampled full size so
// distance does not average the cut away; what remains is opaque.
@fragment fn fs_cutout(o:Out)->@location(0) vec4<f32> {
 let t=sampleMap(o.uv,vec2<f32>(0.0),vec2<f32>(0.0),o.repeat);
 if t.a<o.cut {discard;}
 return vec4<f32>(finish(o,mapColor(t.rgb)*o.color.rgb),1.0);
}

@vertex fn vs_box(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return boxOutput(vi,ii);}
struct ShadowOut { @builtin(position) clip:vec4<f32>, @location(0) uv:vec2<f32>, @location(1) cut:f32, @location(2) @interpolate(flat) repeat:f32 };
@vertex fn vs_shadow_mesh(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->ShadowOut {
 var o:ShadowOut;o.clip=u.shadow*instances[ii].world*vec4<f32>(items[vi].a.xyz,1.0);return o;
}
@vertex fn vs_shadow_box(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->ShadowOut {
 let b=boxOutput(vi,ii);var o:ShadowOut;o.clip=u.shadow*vec4<f32>(b.world,1.0);return o;
}
@vertex fn vs_shadow_texture(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->ShadowOut {
 let p=items[vi];var o:ShadowOut;o.clip=u.shadow*instances[ii].world*vec4<f32>(p.a.xyz,1.0);
 o.uv=vec2<f32>(p.a.w,p.b.w);o.cut=p.color.x;
 o.repeat=select(0.0,1.0,(u32(p.color.y)&1u)!=0u && instances[ii].normal[3].z>0.5);return o;
}
@fragment fn fs_shadow_cutout(o:ShadowOut) {if sampleMap(o.uv,vec2<f32>(0.0),vec2<f32>(0.0),o.repeat).a<o.cut {discard;}}

// Drop bounds are local; transform every corner before fitting the world disc.
@vertex fn vs_drop(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {
 let item=items[0]; let world=instances[ii].world;
 var lo=vec3<f32>(1e30);var hi=vec3<f32>(-1e30);
 for(var i=0u;i<8u;i=i+1u) {
  let p=vec3<f32>(select(item.a.x,item.b.x,(i&1u)!=0u),select(item.a.y,item.b.y,(i&2u)!=0u),select(item.a.z,item.b.z,(i&4u)!=0u));
  let q=(world*vec4<f32>(p,1.0)).xyz;lo=min(lo,q);hi=max(hi,q);
 }
 var radius=item.b.w;
 if radius==0.0 {radius=max(hi.x-lo.x,hi.y-lo.y)*0.5;}
 var corners=array<vec2<f32>,6>(vec2<f32>(-1.,-1.),vec2<f32>(1.,-1.),vec2<f32>(1.,1.),vec2<f32>(-1.,-1.),vec2<f32>(1.,1.),vec2<f32>(-1.,1.));
 let corner=corners[vi];let pos=vec3<f32>((lo.xy+hi.xy)*0.5+corner*radius,lo.z);
 var o:Out;o.clip=u.matrix*vec4<f32>(pos,1.0);o.clip.z+=-0.00001*o.clip.w;
 o.uv=corner;o.point=1.0;o.cut=item.a.w;o.color=vec4<f32>(0.0);o.normal=vec3<f32>(0.0);o.world=pos;return o;
}

// A fullscreen distant sky, independent of camera position and scene depth.
struct SkyOut { @builtin(position) clip:vec4<f32>, @location(0) ray:vec3<f32> };
@vertex fn vs_environment(@builtin(vertex_index) index:u32)->SkyOut {
 var xy=vec2<f32>(-1.0,-1.0);
 if index==1u {xy=vec2<f32>(3.0,-1.0);}
 if index==2u {xy=vec2<f32>(-1.0,3.0);}
 var o:SkyOut;o.clip=vec4<f32>(xy,1.0,1.0);
 o.ray=u.skyForward.xyz+xy.x*u.skyRight.xyz+xy.y*u.skyUp.xyz;return o;
}
@fragment fn fs_environment(o:SkyOut)->@location(0) vec4<f32> {
 let d=normalize(o.ray);
 let half=0.5/f32(textureDimensions(mapTexture).y);
 let uv=vec2<f32>(atan2(d.y,d.x)/6.28318530718+0.5,clamp(acos(clamp(d.z,-1.0,1.0))/3.14159265359,half,1.0-half));
 let c=textureSampleLevel(mapTexture,mapSampler,uv,0.0).rgb;
 if u.viewport.z<0.5 {return vec4<f32>(select(1.055*pow(c,vec3<f32>(1.0/2.4))-0.055,c*12.92,c<=vec3<f32>(0.0031308)),1.0);}
 return vec4<f32>(c,1.0);
}
