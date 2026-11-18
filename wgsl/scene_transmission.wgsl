// Immutable opaque color and depth. Depth is refreshed after transmission for
// particles. Texture loads keep this portable to unfilterable HDR targets.
fn sceneWorld(pixel:vec2<f32>,depth:f32)->vec3<f32> {
 let size=vec2<f32>(textureDimensions(sceneDepth));
 let p=u.inverse*vec4<f32>(pixel/size*vec2<f32>(2.0,-2.0)+vec2<f32>(-1.0,1.0),depth,1.0);
 if abs(p.w)<1e-8 {return vec3<f32>(0.0);}
 return p.xyz/p.w;
}
fn sceneGap(pixel:vec2<f32>,z:f32,back:f32)->f32 {
 if back>=1.0 {return 1000.0;}
 if back<=z {return 0.0;}
 return min(1000.0,length(sceneWorld(pixel,back)-sceneWorld(pixel,z)));
}
fn softIntersection(o:Out)->f32 {
 if o.optics.z<=0.0 {return 1.0;}
 let back=textureLoad(sceneDepth,vec2<i32>(o.clip.xy),0);
 if back>=1.0 {return 1.0;}
 return clamp(sceneGap(o.clip.xy,o.clip.z,back)/o.optics.z,0.0,1.0);
}
fn transmit(o:Out,base:vec3<f32>,n:vec3<f32>)->vec3<f32> {
 let size=vec2<f32>(textureDimensions(sceneDepth));
 let here=vec2<i32>(o.clip.xy);
 let depth=textureLoad(sceneDepth,here,0);
 let thickness=sceneGap(o.clip.xy,o.clip.z,depth);
 let direction=vec2<f32>(dot(n,normalize(u.skyRight.xyz)),-dot(n,normalize(u.skyUp.xyz)));
 let offset=direction*o.optics.y*size.y*min(1.0,thickness);
 var pixel=o.clip.xy+offset;
 if any(pixel<vec2<f32>(0.5)) || any(pixel>size-vec2<f32>(0.5)) {pixel=o.clip.xy;}
 // Reject foreground at every tap of the bilinear footprint.
 let lo=vec2<i32>(floor(pixel-vec2<f32>(0.5)));
 let hi=vec2<i32>(size)-vec2<i32>(1);
 var blocked=false;
 for(var y=0;y<2;y++) {for(var x=0;x<2;x++) {
  if textureLoad(sceneDepth,clamp(lo+vec2<i32>(x,y),vec2<i32>(0),hi),0)<=o.clip.z {blocked=true;}
 }}
 if blocked {pixel=o.clip.xy;}
 let origin=vec2<i32>(floor(pixel-vec2<f32>(0.5)));
 let weight=fract(pixel-vec2<f32>(0.5));
 let a=textureLoad(sceneColor,clamp(origin,vec2<i32>(0),hi),0).rgb;
 let b=textureLoad(sceneColor,clamp(origin+vec2<i32>(1,0),vec2<i32>(0),hi),0).rgb;
 let c=textureLoad(sceneColor,clamp(origin+vec2<i32>(0,1),vec2<i32>(0),hi),0).rgb;
 let d=textureLoad(sceneColor,clamp(origin+vec2<i32>(1,1),vec2<i32>(0),hi),0).rgb;
 let behind=mix(mix(a,b,weight.x),mix(c,d,weight.x),weight.y);
 let back=textureLoad(sceneDepth,vec2<i32>(pixel),0);
 let attenuation=exp(-o.absorption.xyz*sceneGap(pixel,o.clip.z,back));
 return mix(base,behind,o.optics.x*attenuation);
}

fn normalFlowTap(at:vec2<f32>,gx:vec2<f32>,gy:vec2<f32>,repeat:f32)->vec3<f32> {
 var uv=at;
 if repeat<0.5 {let half=vec2<f32>(0.5)/vec2<f32>(textureDimensions(normalTexture));uv=clamp(uv,half,vec2<f32>(1.0)-half);}
 return textureSampleGrad(normalTexture,normalSampler,uv,gx,gy).rgb;
}

struct OITOut { @location(0) accum:vec4<f32>, @location(1) reveal:vec4<f32> };
fn oitWeight(z:f32,alpha:f32)->f32 {
 let d=clamp(1.0-z,0.0,1.0);
 return max(0.03,(0.03+200.0*d*d*d)*alpha);
}
fn oitPack(rgb:vec3<f32>,rawAlpha:f32,z:f32)->OITOut {
 let a=clamp(rawAlpha,0.0,1.0);
 let aw=a*oitWeight(z,a);
 return OITOut(vec4<f32>(rgb*aw,aw),vec4<f32>(0.0,0.0,0.0,a));
}
@fragment fn fs_oit_main(@builtin(front_facing) front:bool,oIn:Out)->OITOut {
 var o=oIn;
 var side=1.0;if !front {side=-1.0;}
 o.normal*=side;
 return oitPack(finish(o,o.color.rgb),o.color.a,o.clip.z);
}
@fragment fn fs_oit_texture(@builtin(front_facing) front:bool,oIn:Out)->OITOut {
 var o=oIn;
 var side=1.0;if !front {side=-1.0;}
 o.normal*=side;o.tangent.w*=side;
 let gx=dpdx(o.uv);let gy=dpdy(o.uv);
 let t=sampleMap(o.uv,gx,gy,o.repeat);
 let n=surfaceNormal(o,dpdx(o.normalUV.xy),dpdy(o.normalUV.xy),dpdx(o.world),dpdy(o.world));
 var base=mapColor(t.rgb)*o.color.rgb;
 var alpha=t.a*o.cut;
 if nt3dHasMaterial {let m=evalMaterialHook(o,n,base,alpha);base=m.rgb;alpha=m.a;}
 let orm=sampleORM(o.uv,gx,gy);
 let em=mapColor(sampleEmissive(o.uv,gx,gy));
 return oitPack(shadeSurface(o,base,n,orm,em,sampleLightMap(o.normalUV.zw)),alpha,o.clip.z);
}
@fragment fn fs_oit_outline_blend(o:Out)->OITOut {
 var alpha=sampleMap(o.uv,dpdx(o.uv),dpdy(o.uv),o.repeat).a*o.cut;
 if nt3dHasMaterial {alpha=evalMaterialHook(o,vec3<f32>(0.0),vec3<f32>(1.0),alpha).a;}
 return oitPack(finish(o,o.color.rgb),alpha,o.clip.z);
}

@group(2) @binding(8) var lightMapTexture: texture_2d<f32>;
@group(2) @binding(9) var lightMapSampler: sampler;
fn sampleLightMap(uv:vec2<f32>)->vec4<f32> {
 let sample=textureSampleGrad(lightMapTexture,lightMapSampler,uv,dpdx(uv),dpdy(uv));
 var c=sample.rgb/3.14159265359;
 if u.viewport.z<0.5 {c=select(1.055*pow(c,vec3<f32>(1.0/2.4))-0.055,c*12.92,c<=vec3<f32>(0.0031308));}
 return vec4<f32>(c,sample.a);
}
