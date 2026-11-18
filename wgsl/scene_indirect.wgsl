// AO is evaluated at output resolution, independent of MSAA/supersampling.
fn surfaceAO(p:vec3<f32>)->f32 {
 if u.skyRight.w<0.5 {return 1.0;}
 let clip=u.matrix*vec4<f32>(p,1.0);
 let uv=clip.xy/clip.w*vec2<f32>(0.5,-0.5)+vec2<f32>(0.5);
 let dim=vec2<i32>(textureDimensions(screenAO));
 let sample=textureLoad(screenAO,clamp(vec2<i32>(uv*vec2<f32>(dim)),vec2<i32>(0),dim-vec2<i32>(1)),0);
 // Transparent/transmissive surfaces in front of the opaque depth must not
 // inherit its occlusion. The one-pixel white prepass input has no depth.
 if abs(eyeDistance(p)-sample.y)>max(0.001,sample.z*0.05) {return 1.0;}
 return sample.x;
}
// Incoming light, premultiplied by coverage. Diffuse is irradiance/pi;
// specular is roughness-filtered radiance. No material response is baked in.
// Keep these operations in sync with indirect.go. Occlusion affects radiance,
// never coverage; apply it to fallback sources before composing screen hits.
struct IndirectSample { radiance:vec3<f32>, coverage:f32 };
fn indirectCovered(radiance:vec3<f32>,coverage:f32)->IndirectSample {
 return IndirectSample(radiance*coverage,coverage);
}
fn indirectOver(front:IndirectSample,fallback:IndirectSample)->IndirectSample {
 let remaining=1.0-front.coverage;
 return IndirectSample(front.radiance+remaining*fallback.radiance,front.coverage+remaining*fallback.coverage);
}
fn indirectOccluded(source:IndirectSample,visibility:f32)->IndirectSample {
 return IndirectSample(source.radiance*visibility,source.coverage);
}
fn indirectDiffuse(n:vec3<f32>,visibility:f32,baked:vec4<f32>)->IndirectSample {
 if baked.a>0.0 {return indirectOccluded(indirectCovered(baked.rgb,baked.a),visibility);}
 var c=mix(u.ground.rgb,u.sky.rgb,n.z*0.5+0.5);
 if u.viewport.w>0.5 {
  var irradiance=vec3<f32>(0.0);
  for(var k=0u;k<9u;k++){irradiance+=u.environment[k].rgb*nt3dSHBasis(k,n);}
  c+=max(vec3<f32>(0.0),irradiance);
 }
 return indirectOccluded(indirectCovered(c,1.0),visibility);
}
fn indirectToonDiffuse(visibility:f32,baked:vec4<f32>)->IndirectSample {
 if baked.a>0.0 {return indirectOccluded(indirectCovered(baked.rgb,baked.a),visibility);}
 var c=mix(u.ground.rgb,u.sky.rgb,0.5);
 if u.viewport.w>0.5 {c+=max(vec3<f32>(0.0),u.environment[0].rgb*0.2820948);}
 return indirectOccluded(indirectCovered(c,1.0),visibility);
}
// Scalar axis tests avoid zero division and dynamic vector indexing.
fn probeExitDistance(point:f32,direction:f32,minimum:f32,maximum:f32)->f32 {
 if direction>0.0 {return (maximum-point)/direction;}
 if direction<0.0 {return (minimum-point)/direction;}
 return 3.402823e38;
}
// Box projection is only defined inside the influence volume. Pick the first
// containing probe, preserving authored priority without overlap blending.
fn localReflection(point:vec3<f32>,direction:vec3<f32>,roughness:f32)->IndirectSample {
 if dot(direction,direction)==0.0 {return IndirectSample(vec3<f32>(0.0),0.0);}
 for(var i=0u;i<min(maxReflectionProbes,u32(u.skyUp.w));i++) {
  let probe=u.probes[i];
  if probe.position.w<=0.0 || any(point<probe.minimum.xyz) || any(point>probe.maximum.xyz) {continue;}
  let dx=probeExitDistance(point.x,direction.x,probe.minimum.x,probe.maximum.x);
  let dy=probeExitDistance(point.y,direction.y,probe.minimum.y,probe.maximum.y);
  let dz=probeExitDistance(point.z,direction.z,probe.minimum.z,probe.maximum.z);
  let distance=min(dx,min(dy,dz));
  let corrected=normalize(point+direction*distance-probe.position.xyz);
  let uv=vec2<f32>(atan2(corrected.y,corrected.x)/6.28318530718+0.5,acos(clamp(corrected.z,-1.0,1.0))/3.14159265359);
  let level=roughness*probe.minimum.w;
  var sky=vec3<f32>(0.0);
  switch i {
   case 0u: {sky=textureSampleLevel(probeMap0,reflectionSampler,uv,level).rgb;}
   case 1u: {sky=textureSampleLevel(probeMap1,reflectionSampler,uv,level).rgb;}
   case 2u: {sky=textureSampleLevel(probeMap2,reflectionSampler,uv,level).rgb;}
   default: {sky=textureSampleLevel(probeMap3,reflectionSampler,uv,level).rgb;}
  }
  if u.viewport.z<0.5 {sky=select(1.055*pow(sky,vec3<f32>(1.0/2.4))-0.055,sky*12.92,sky<=vec3<f32>(0.0031308));}
  return indirectCovered(sky*probe.position.w,1.0);
 }
 return IndirectSample(vec3<f32>(0.0),0.0);
}
fn indirectSpecular(point:vec3<f32>,direction:vec3<f32>,roughness:f32,envRefl:bool,visibility:f32)->IndirectSample {
 let local=indirectOccluded(localReflection(point,direction,roughness),visibility);
 if local.coverage>=1.0 {return local;}
 let t=mix(direction.z*0.5+0.5,0.5,roughness*roughness);
 var c=mix(u.ground.rgb,u.sky.rgb,t);
 if envRefl {
  let uv=vec2<f32>(atan2(direction.y,direction.x)/6.28318530718+0.5,acos(clamp(direction.z,-1.0,1.0))/3.14159265359);
  var sky=textureSampleLevel(reflectionMap,reflectionSampler,uv,roughness*u.forward.w).rgb;
  if u.viewport.z<0.5 {sky=select(1.055*pow(sky,vec3<f32>(1.0/2.4))-0.055,sky*12.92,sky<=vec3<f32>(0.0031308));}
  c+=sky*u.ground.w;
 }
 return indirectOver(local,indirectOccluded(indirectCovered(c,1.0),visibility));
}
// The classic reflection model uses sharp level-zero radiance, no hemisphere.
fn indirectReflection(point:vec3<f32>,direction:vec3<f32>,envRefl:bool,visibility:f32)->IndirectSample {
 let local=indirectOccluded(localReflection(point,direction,0.0),visibility);
 if local.coverage>=1.0 || !envRefl {return local;}
 let half=0.5/f32(textureDimensions(reflectionMap).y);
 let uv=vec2<f32>(atan2(direction.y,direction.x)/6.28318530718+0.5,clamp(acos(clamp(direction.z,-1.0,1.0))/3.14159265359,half,1.0-half));
 var sky=textureSampleLevel(reflectionMap,reflectionSampler,uv,0.0).rgb;
 if u.viewport.z<0.5 {sky=select(1.055*pow(sky,vec3<f32>(1.0/2.4))-0.055,sky*12.92,sky<=vec3<f32>(0.0031308));}
 return indirectOver(local,indirectOccluded(indirectCovered(sky*u.ground.w,1.0),visibility));
}
