// toward is the unit direction from world point p toward a light (xyz) and
// how much of its light reaches p (w): all of a directional light's; a
// point or spot light's fading to nothing at its range, or as 1/(1+d²)
// without one (plus cone attenuation for spotlights), and none at p.
fn toward(l:Light,p:vec3<f32>)->vec4<f32> {
 if abs(l.a.w)<0.5 {return vec4<f32>(l.a.xyz,1.0);}
 let v=l.a.xyz-p;let d2=dot(v,v);
 if d2==0.0 {return vec4<f32>(0.0);}
 let dir=v*inverseSqrt(d2);
 var fade=1.0/(1.0+d2);
 if l.b.w>0.0 {let k=max(0.0,1.0-d2/(l.b.w*l.b.w));fade=k*k;}
 if l.a.w<0.0 {
  let cone=clamp((-dot(dir,l.c.xyz)-l.c.w)*(-l.a.w),0.0,1.0);
  fade*=cone*cone;
 }
 return vec4<f32>(dir,fade);
}
// Manual depth lookup uses only core textureLoad, with clamped PCF taps.
fn pointShadowFace(v:vec3<f32>)->u32 {
 let a=abs(v);
 if a.x>=a.y && a.x>=a.z {return select(0u,1u,v.x<0.0);}
 if a.y>=a.z {return select(2u,3u,v.y<0.0);}
 return select(4u,5u,v.z<0.0);
}
fn shadowVisibility(p:vec3<f32>,n:vec3<f32>,l:vec3<f32>)->f32 {
 var face=0u;var pos=p+n*(1.5*u.shadowParams.z);var matrix=u.shadow;
 let light=u.lights[u32(u.shadowParams.x)];let point=light.a.w>0.5;let spot=light.a.w<-0.5;
 if point {
  let d=abs(p-light.a.xyz);let texel=2.0*max(d.x,max(d.y,d.z))/u.shadowParams.w;
  pos=p+n*(1.5*texel);face=pointShadowFace(pos-light.a.xyz);matrix=u.pointShadows[face];
 } else if spot {
  let w0=(u.shadow*vec4<f32>(p,1.0)).w;
  if w0<=0.0 {return 1.0;}
  pos=p+n*(1.5*w0*u.shadowParams.z);
 }
 let clip=matrix*vec4<f32>(pos,1.0);
 if clip.w<=0.0{return 1.0;}
 let q=clip.xyz/clip.w;
 var xy=vec2<f32>(q.x*0.5+0.5,0.5-q.y*0.5)*u.shadowParams.w;
 if point {xy=clamp(xy,vec2<f32>(0.0),vec2<f32>(u.shadowParams.w-0.001));}
 if any(xy<vec2<f32>(0.0)) || any(xy>=vec2<f32>(u.shadowParams.w)) || q.z<0.0 || q.z>1.0 {return 1.0;}
 let center=vec2<i32>(floor(xy));let size=i32(u.shadowParams.w);
 var bias=u.shadowParams.y*(1.0+2.0*(1.0-max(0.0,dot(n,l))));var lit=0.0;
 if point || spot {bias*=max(1.0-q.z,0.000001);}
 for(var y=-1;y<=1;y=y+1) {for(var x=-1;x<=1;x=x+1) {
 let texel=clamp(center+vec2<i32>(x,y),vec2<i32>(0),vec2<i32>(size-1));
 if textureLoad(shadowMap,texel,i32(face),0)>=q.z-bias {lit+=1.0;}
 }}return lit/9.0;
}
fn lightVisibility(i:u32,p:vec3<f32>,n:vec3<f32>,l:vec3<f32>)->f32 {
 if f32(i)==u.shadowParams.x {return shadowVisibility(p,n,l);}return 1.0;
}
fn shadowTexelXY(matrix:mat4x4<f32>,pos:vec3<f32>,size:f32)->vec3<f32> {
 let clip=matrix*vec4<f32>(pos,1.0);
 if clip.w<=0.0 {return vec3<f32>(-1.0);}
 let q=clip.xyz/clip.w;
 return vec3<f32>((q.x*0.5+0.5)*size,(0.5-q.y*0.5)*size,1.0);
}
fn shadowVisibilityToon(p:vec3<f32>,pX:vec3<f32>,pY:vec3<f32>,n:vec3<f32>,l:vec3<f32>,shadowSoft:f32)->vec2<f32> {
 let radius=1.5+2.5*shadowSoft;
 var face=0u;var offset=n*(radius*u.shadowParams.z);var matrix=u.shadow;
 let light=u.lights[u32(u.shadowParams.x)];let point=light.a.w>0.5;let spot=light.a.w<-0.5;
 if point {
  let d=abs(p-light.a.xyz);let texel=2.0*max(d.x,max(d.y,d.z))/u.shadowParams.w;
  offset=n*(radius*texel);face=pointShadowFace(p+offset-light.a.xyz);matrix=u.pointShadows[face];
 } else if spot {
  let w0=(u.shadow*vec4<f32>(p,1.0)).w;
  if w0<=0.0 {return vec2<f32>(1.0,0.01);}
  offset=n*(radius*w0*u.shadowParams.z);
 }
 let pos=p+offset;
 let clip=matrix*vec4<f32>(pos,1.0);
 if clip.w<=0.0 {return vec2<f32>(1.0,0.01);}
 let q=clip.xyz/clip.w;
 var xy=vec2<f32>(q.x*0.5+0.5,0.5-q.y*0.5)*u.shadowParams.w;
 if point {xy=clamp(xy,vec2<f32>(0.0),vec2<f32>(u.shadowParams.w-0.001));}
 if any(xy<vec2<f32>(0.0)) || any(xy>=vec2<f32>(u.shadowParams.w)) || q.z<0.0 || q.z>1.0 {return vec2<f32>(1.0,0.01);}
 var pixelStep=0.01;
 let cX=shadowTexelXY(matrix,pX+offset,u.shadowParams.w);
 let cY=shadowTexelXY(matrix,pY+offset,u.shadowParams.w);
 if cX.z>0.0 && cY.z>0.0 {pixelStep=clamp((length(cX.xy-xy)+length(cY.xy-xy))/(2.0*radius),0.0001,1.0);}
 let size=i32(u.shadowParams.w);
 var bias=u.shadowParams.y*(1.0+2.0*(1.0-max(0.0,dot(n,l))))*(radius/1.5);
 if point || spot {bias*=max(1.0-q.z,0.000001);}
 let lo=vec2<i32>(floor(xy-vec2<f32>(radius-0.5)));
 let hi=vec2<i32>(floor(xy+vec2<f32>(radius-0.5)));
 var lit=0.0;var total=0.0;
 for(var iy=lo.y;iy<=hi.y;iy=iy+1) {
  let wy=max(0.0,radius-abs(f32(iy)+0.5-xy.y));
  if wy>0.0 {
   let sy=clamp(iy,0,size-1);
   for(var ix=lo.x;ix<=hi.x;ix=ix+1) {
    let wx=max(0.0,radius-abs(f32(ix)+0.5-xy.x));
    if wx>0.0 {
     let wt=wx*wy;total+=wt;
     let sx=clamp(ix,0,size-1);
     if textureLoad(shadowMap,vec2<i32>(sx,sy),i32(face),0)>=q.z-bias {lit+=wt;}
    }
   }
  }
 }
 if total<=0.0 {return vec2<f32>(1.0,pixelStep);}
 return vec2<f32>(lit/total,pixelStep);
}
fn toonShadowFactor(i:u32,p:vec3<f32>,pX:vec3<f32>,pY:vec3<f32>,n:vec3<f32>,l:vec3<f32>,shadowSoft:f32)->f32 {
 if f32(i)!=u.shadowParams.x {return 1.0;}
 let sv=shadowVisibilityToon(p,pX,pY,n,l,shadowSoft);
 return toonStep(0.5,clamp(max(sv.y,shadowSoft),0.0001,1.0),sv.x);
}
