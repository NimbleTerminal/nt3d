// BEGIN_POINT_ONLY
// Sprite atlas cells sample level zero and clamp to texel centers, so bilinear
// filtering cannot leak neighboring frames into a sprite.
fn spriteOutput(vi:u32,packed:u32)->Out {
 var o=pointOutput(vi,packed);o.rect=items[packed & 0xFFFFFFu].a;
 o.uv=vec2<f32>((o.uv.x+1.0)*0.5,(1.0-o.uv.y)*0.5);return o;
}
@vertex fn vs_sprite(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return spriteOutput(vi,ii);}
@vertex fn vs_sprite_sorted(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return spriteOutput(vi,order[run.start+ii]);}
fn spriteSample(o:Out)->vec4<f32> {
 let halfTexel=0.5/vec2<f32>(textureDimensions(mapTexture));
 let uv=clamp(o.rect.xy+o.uv*o.rect.zw,o.rect.xy+halfTexel,o.rect.xy+o.rect.zw-halfTexel);
 return textureSampleLevel(mapTexture,mapSampler,uv,0.0);
}
@fragment fn fs_sprite(o:Out)->@location(0) vec4<f32> {
 let t=spriteSample(o);if t.a<0.5 {discard;}
 return vec4<f32>(finish(o,mapColor(t.rgb)*o.color.rgb),1.0);
}
@fragment fn fs_sprite_alpha(o:Out)->@location(0) vec4<f32> {
 let t=spriteSample(o);return vec4<f32>(finish(o,mapColor(t.rgb)*o.color.rgb),t.a*(o.cut*softIntersection(o)));
}
@fragment fn fs_sprite_add(o:Out)->@location(0) vec4<f32> {
 let t=spriteSample(o);return vec4<f32>(mapColor(t.rgb)*o.color.rgb,t.a*(o.cut*softIntersection(o))*(1.0-fogAmount(o.world)));
}

// Covariance axes share the sprite auxiliary buffer, or for compact records
// (the SH header's flag) are nine halves, two a word, six words a splat.
// Project each axis by the homogeneous Jacobian, then use a Cholesky factor
// for the ellipse.
@group(0) @binding(7) var<storage,read> compactAxes:array<u32>;
@vertex fn vs_gaussian(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {
 // The order's top byte is the splat's cluster fraction drawn, in 1/255ths:
 // a thinned cluster's splats grow by 1/sqrt of it to keep its coverage.
 let packed=order[run.start+ii];let index=packed & 0xFFFFFFu;let p=points[index];
 var lodScale=1.0; // Exactly, at full size.
 if (packed>>24u)<255u {lodScale=inverseSqrt(max(f32(packed>>24u),1.0)/255.0);}
 let compact=gaussianSH[11]>0.5;
 var vectors:array<vec3<f32>,3>;
 if compact {
  let at=index*6u;
  let h0=unpack2x16float(compactAxes[at]);let h1=unpack2x16float(compactAxes[at+1u]);let h2=unpack2x16float(compactAxes[at+2u]);
  let h3=unpack2x16float(compactAxes[at+3u]);let h4=unpack2x16float(compactAxes[at+4u]);
  vectors=array<vec3<f32>,3>(vec3<f32>(h0.x,h0.y,h1.x),vec3<f32>(h1.y,h2.x,h2.y),vec3<f32>(h3.x,h3.y,h4.x));
 } else {
  let axes=items[index];
  vectors=array<vec3<f32>,3>(axes.a.xyz,axes.b.xyz,axes.color.xyz);
 }
 for(var k=0u;k<3u;k++) {vectors[k]*=lodScale;}
 var o=pointOutput(vi,index);
 if gaussianSH[9]>0.0 {o.color=vec4<f32>(gaussianSHColor(index,o.world),1.0);}
 let m=u.matrix*instances[0].world;
 let center=m*vec4<f32>(p.x,p.y,p.z,1.0);
 if center.w<=0.0 || center.z<0.0 || center.z>center.w {o.clip=vec4<f32>(2.0,2.0,2.0,1.0);return o;}
 var cov=vec3<f32>(0.3,0.0,0.3);
 for(var k=0u;k<3u;k++) {
  let d=m*vec4<f32>(vectors[k],0.0);
  let s=(d.xy-center.xy/center.w*d.w)/center.w*u.viewport.xy*0.5;
  cov+=vec3<f32>(s.x*s.x,s.x*s.y,s.y*s.y);
 }
 let a=sqrt(cov.x);let b=cov.y/a;let c=sqrt(max(cov.z-b*b,0.000001));
 let scale=min(1.0,maxPointPixels/(3.0*sqrt(max(cov.x,cov.z))));
 let q=o.uv*3.0;
 let offset=vec2<f32>(a*q.x,b*q.x+c*q.y)*scale;
 o.clip=vec4<f32>(center.xy+offset*2.0/u.viewport.xy*center.w,center.zw);
 return o;
}
@fragment fn fs_gaussian(o:Out)->@location(0) vec4<f32> {
 let r2=dot(o.uv,o.uv);if r2>1.0 {discard;}
 return vec4<f32>(finish(o,o.color.rgb),min(0.99,o.cut)*softIntersection(o)*exp(-4.5*r2));
}

// Header, 16 floats: [0..8] the 3x3 SH-from-local transform; [9] RGB
// entries a point at its own (1 with a palette); [10] coefficients evaluated;
// [11] the layout flag (1 compact, 0 precise); [12] palette centroids (0
// without one); [13] the labels' byte offset at binding 8; [14] the
// palette's offset, bytes at binding 8 (compact) or a float index here
// (precise); [15] 0. Precise coefficients follow it as packed RGB floats in
// point-major order, then a precise palette's, centroid-major; compact ones
// are bytes in the same order at binding 8, SPZ-quantized, then 16-bit
// labels and the palette's band bytes. A coefficient k at or past [9] is
// coefficient k-1 of the splat's labelled centroid.
@group(0) @binding(5) var<storage,read> gaussianSH:array<f32>;
@group(0) @binding(8) var<storage,read> compactSH:array<u32>;
fn compactSHByte(at:u32)->f32 {return f32((compactSH[at/4u]>>(8u*(at%4u)))&255u);}
fn gaussianSHColor(index:u32,world:vec3<f32>)->vec3<f32> {
 var ray=u.forward.xyz;if u.eye.w>0.5 {ray=world-u.eye.xyz;}
 let n=instances[0].normal;
 let local=transpose(mat3x3<f32>(n[0].xyz,n[1].xyz,n[2].xyz))*ray;
 let m=mat3x3<f32>(vec3<f32>(gaussianSH[0],gaussianSH[1],gaussianSH[2]),vec3<f32>(gaussianSH[3],gaussianSH[4],gaussianSH[5]),vec3<f32>(gaussianSH[6],gaussianSH[7],gaussianSH[8]));
 let d=m*local;let rayLength=length(d);var direction=vec3<f32>(0.0);
 if rayLength>0.0 {direction=d/rayLength;}
 let perPoint=u32(gaussianSH[9]);let count=u32(gaussianSH[10]);
 var rgb=vec3<f32>(0.5);
 let compact=gaussianSH[11]>0.5;
 let paletteCount=u32(gaussianSH[12]);let paletteAt=u32(gaussianSH[14]);
 var label=0u;
 if paletteCount>0u {
  let labelsAt=u32(gaussianSH[13])+index*2u;
  label=u32(compactSHByte(labelsAt))|(u32(compactSHByte(labelsAt+1u))<<8u);
 }
 let stride=count-1u; // A palette's bands: all but the DC.
 for(var k=0u;k<count;k++) {
  if k>=perPoint {
   if paletteCount==0u {break;}
   let at=paletteAt+(label*stride+k-1u)*3u;
   if compact {
    let b=vec3<f32>(compactSHByte(at),compactSHByte(at+1u),compactSHByte(at+2u));
    rgb+=nt3dSHBasis(k,direction)*(b-128.0)/128.0;
   } else {
    rgb+=nt3dSHBasis(k,direction)*vec3<f32>(gaussianSH[at],gaussianSH[at+1u],gaussianSH[at+2u]);
   }
   continue;
  }
  if compact {
   let at=(index*perPoint+k)*3u;
   let b=vec3<f32>(compactSHByte(at),compactSHByte(at+1u),compactSHByte(at+2u));
   var c=(b-128.0)/128.0;
   if k==0u {c=(b/255.0-0.5)/0.15;}
   rgb+=nt3dSHBasis(k,direction)*c;
   continue;
  }
  let at=16u+(index*perPoint+k)*3u;rgb+=nt3dSHBasis(k,direction)*vec3<f32>(gaussianSH[at],gaussianSH[at+1u],gaussianSH[at+2u]);
 }
 return decode(max(rgb,vec3<f32>(0.0)));
}
// END_POINT_ONLY

