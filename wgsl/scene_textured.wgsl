@group(2) @binding(0) var mapTexture: texture_2d<f32>;
@group(2) @binding(1) var mapSampler: sampler;
// Textured records carry the cutout threshold, or a transparent vertex's
// opacity, where color would be; it reaches the fragment in cut.
// Flags: 1 repeat, 2 edge-on, 4 tint, whose color is a 24-bit integer, 8
// an extension record follows, 16 it carries a tangent frame.
// (Helpers named texture... are taken for WGSL builtins by the shader
// translator, hence mapOutput.)
fn mapOutput(vi:u32,ii:u32,side:f32)->Out {
 let p=items[vi];let flags=u32(p.color.y);var tint=vec3<f32>(1.0);
 if (flags&4u)!=0u {let c=u32(p.color.z);tint=vec3<f32>(f32((c>>16u)&255u),f32((c>>8u)&255u),f32(c&255u))/255.0;}
 let uv=vec2<f32>(p.a.w,p.b.w);
 var pos=p.a.xyz;var norm=p.b.xyz;
 if nt3dHasDisplace {
  let d=nt3d_displace(pos,norm,(instances[ii].world*vec4<f32>(pos,1.0)).xyz,uv);
  pos=d.position;norm=d.normal;
 }
 let bc=instanceBaseColor(ii);
 var o=output(pos,norm*side,vec4<f32>(tint,f32(ii)));o.color=vec4<f32>(o.color.rgb*bc.rgb,o.color.a);o.uv=uv;o.optics.w=f32(ii)+1.0;o.cut=p.color.x*bc.a;o.normalStrength=p.color.w;o.normalUV=vec4<f32>(o.uv,vec2<f32>(0.0));o.rect.x=select(0.0,1.0,(flags&32u)!=0u);
 // The extension record carries the normal map's coordinates and the
 // vertex's tangent frame: turned by the world matrix (not the normal
 // matrix: tangents turn as positions do), its handedness in w, negated
 // for a back face, whose normal is.
 o.rect=vec4<f32>(o.rect.x,instances[ii].optics.zw,o.rect.w);
 o.tangent=vec4<f32>(0.0);o.flowFrame.x=select(0.0,1.0,(flags&16u)!=0u);
 if (flags&8u)!=0u {let e=items[u32(instances[ii].normal[1].w)+vi];o.normalUV=vec4<f32>(e.a.xy,e.color.xy);o.flowFrame=vec3<f32>(o.flowFrame.x,e.a.zw);o.tangent=vec4<f32>((instances[ii].world*vec4<f32>(e.b.xyz,0.0)).xyz,e.b.w*side);}
 if instances[ii].normal[0].w>0.5 && !nt3dHasMaterial && !nt3dHasDisplace {o.normal=vec3<f32>(0.0);}
 if instances[ii].normal[0].w>0.5 {o.spec=vec2<f32>(0.0);}
 // Image textures repeat through their samplers; procedural ones (flagged in
 // the instance record) are drawn each frame, clamped, and wrapped by hand.
 o.repeat=select(0.0,1.0,(flags&1u)!=0u && instances[ii].normal[3].z>0.5);
 // A procedural cutout samples full size: its device chain averages alpha.
 o.full=select(0.0,1.0,instances[ii].normal[3].z>0.5);
 // Edge-on cutouts fade out as the surface turns to face the viewer.
 if (flags&2u)!=0u && dot(norm,norm)>0.0 {
  let inst=instances[ii];
  let world=(inst.world*vec4<f32>(pos,1.0)).xyz;
  let n=normalize((inst.normal*vec4<f32>(norm*side,0.0)).xyz);
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
// pixel to pixel (gx, gy; zero takes full size, as shadows do). A procedural
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
@group(2) @binding(2) var normalTexture: texture_2d<f32>;
@group(2) @binding(3) var normalSampler: sampler;
@group(2) @binding(4) var ormTexture: texture_2d<f32>;
@group(2) @binding(5) var ormSampler: sampler;
@group(2) @binding(6) var emissiveTexture: texture_2d<f32>;
@group(2) @binding(7) var emissiveSampler: sampler;
fn sampleORM(uv:vec2<f32>,gx:vec2<f32>,gy:vec2<f32>)->vec3<f32> {
 return textureSampleGrad(ormTexture,ormSampler,uv,gx,gy).rgb;
}
fn sampleEmissive(uv:vec2<f32>,gx:vec2<f32>,gy:vec2<f32>)->vec3<f32> {
 return textureSampleGrad(emissiveTexture,emissiveSampler,uv,gx,gy).rgb;
}
// surfaceNormal is the shading normal: the interpolated normal, tilted by
// the normal map's sample in the vertices' tangent frame (W × (N × T) its
// bitangent), or, for a mesh without one, in a per-triangle basis from the
// screen derivatives of position and texture coordinate.
fn surfaceNormal(o:Out,gx:vec2<f32>,gy:vec2<f32>,px:vec3<f32>,py:vec3<f32>)->vec3<f32> {
 if o.normalStrength==0.0 || dot(o.normal,o.normal)==0.0 {return o.normal;}
 let n=normalize(o.normal);
 // (A select inside the product miscompiles to Metal; the sign is a branch.)
 var sign=1.0;if o.tangent.w<0.0 {sign=-1.0;}
 var raw=o.tangent.xyz;var b=cross(n,raw)*sign;
 if o.flowFrame.x<0.5 {
  let det=gx.x*gy.y-gx.y*gy.x;
  if abs(det)<=1e-8*max(max(abs(gx.x*gy.y),abs(gx.y*gy.x)),1e-30) {return o.normal;}
  raw=(px*gy.y-py*gx.y)/det;
  b=(py*gx.x-px*gy.x)/det;
 }
 let projected=raw-n*dot(n,raw);
 if dot(projected,projected)<1e-12 || dot(b,b)<1e-12 {return o.normal;}
 let tangent=normalize(projected);var bitangent=cross(n,tangent);
 if dot(bitangent,b)<0.0 {bitangent=-bitangent;}
 var sample=vec3<f32>(0.0);
 let flow=o.flowFrame.yz*o.rect.z;
 if dot(flow,flow)>0.0 {
  let a=fract(o.rect.y);let b=fract(a+0.5);let weight=abs(2.0*a-1.0);
  sample=mix(normalFlowTap(o.normalUV.xy-flow*a,gx,gy,o.rect.x),normalFlowTap(o.normalUV.xy-flow*b,gx,gy,o.rect.x),weight);
 }else{sample=normalFlowTap(o.normalUV.xy,gx,gy,o.rect.x);}
 sample=sample*2.0-1.0;
 let result=tangent*sample.x*o.normalStrength+bitangent*sample.y*o.normalStrength+n*sample.z;
 if dot(result,result)<1e-12 {return o.normal;}
 return normalize(result);
}
fn srgbDecode3(c:vec3<f32>)->vec3<f32> {
 let v=max(c,vec3<f32>(0.0));
 return select(pow((v+0.055)/1.055,vec3<f32>(2.4)),v/12.92,v<=vec3<f32>(0.04045));
}
fn workingToLinear(c:vec3<f32>)->vec3<f32> {
 if u.viewport.z>0.5 {return c;}
 return srgbDecode3(c);
}
fn evalMaterialHook(o:Out,n:vec3<f32>,workRGB:vec3<f32>,alpha:f32)->vec4<f32> {
 var unitN=vec3<f32>(0.0);
 if dot(n,n)>0.0 {unitN=normalize(n);}
 let m=nt3d_material(o.world,unitN,viewAt(o.world),o.uv,vec4<f32>(workingToLinear(workRGB),alpha));
 return vec4<f32>(mapColor(m.rgb),m.a);
}
@fragment fn fs_texture(o:Out)->@location(0) vec4<f32> {
 let gx=dpdx(o.uv);let gy=dpdy(o.uv);
 let n=surfaceNormal(o,dpdx(o.normalUV.xy),dpdy(o.normalUV.xy),dpdx(o.world),dpdy(o.world));
 let t=sampleMap(o.uv,gx,gy,o.repeat);
 var base=mapColor(t.rgb)*o.color.rgb;
 if nt3dHasMaterial {base=evalMaterialHook(o,n,base,t.a).rgb;}
 let orm=sampleORM(o.uv,gx,gy);
 let em=mapColor(sampleEmissive(o.uv,gx,gy));
 return vec4<f32>(shadeSurface(o,base,n,orm,em,sampleLightMap(o.normalUV.zw)),1.0);
}
// Transparent materials blend by the texture's alpha times the vertex's.
@fragment fn fs_blend_texture(o:Out)->@location(0) vec4<f32> {
 let gx=dpdx(o.uv);let gy=dpdy(o.uv);
 let t=sampleMap(o.uv,gx,gy,o.repeat);
 let n=surfaceNormal(o,dpdx(o.normalUV.xy),dpdy(o.normalUV.xy),dpdx(o.world),dpdy(o.world));
 var base=mapColor(t.rgb)*o.color.rgb;
 var alpha=t.a*o.cut;
 if nt3dHasMaterial {let m=evalMaterialHook(o,n,base,alpha);base=m.rgb;alpha=m.a;}
 let orm=sampleORM(o.uv,gx,gy);
 let em=mapColor(sampleEmissive(o.uv,gx,gy));
 return vec4<f32>(shadeSurface(o,base,n,orm,em,sampleLightMap(o.normalUV.zw)),alpha);
}
// Cutout materials skip texels under their threshold; what remains is
// opaque. An image cutout is uploaded with its coverage-preserving chain,
// sampled trilinearly (its sampler has no anisotropy), so distance does
// not average the cut away; a procedural one samples full size.
@fragment fn fs_cutout(o:Out)->@location(0) vec4<f32> {
 let gx=dpdx(o.uv);let gy=dpdy(o.uv);
 let n=surfaceNormal(o,dpdx(o.normalUV.xy),dpdy(o.normalUV.xy),dpdx(o.world),dpdy(o.world));
 let k=1.0-o.full;
 let t=sampleMap(o.uv,gx*k,gy*k,o.repeat);
 var base=mapColor(t.rgb)*o.color.rgb;
 var alpha=t.a;
 if nt3dHasMaterial {let m=evalMaterialHook(o,n,base,alpha);base=m.rgb;alpha=m.a;}
 if alpha<o.cut {discard;}
 let orm=sampleORM(o.uv,gx,gy);
 let em=mapColor(sampleEmissive(o.uv,gx,gy));
 return vec4<f32>(shadeSurface(o,base,n,orm,em,sampleLightMap(o.normalUV.zw)),1.0);
}

