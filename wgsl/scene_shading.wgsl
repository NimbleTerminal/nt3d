// illuminate is the light reaching a surface with unit normal n at p:
// ambient, from ground to sky as n turns up, and each light's diffuse.
fn illuminate(n:vec3<f32>,p:vec3<f32>,geometric:vec3<f32>,baked:vec4<f32>)->vec3<f32> {
 var c=indirectDiffuse(n,surfaceAO(p),baked).radiance;
 for(var i=0u;i<u32(u.sky.w);i=i+1u) {let l=u.lights[i];let t=toward(l,p);c+=l.b.rgb*(t.w*max(0.0,dot(n,t.xyz))*lightVisibility(i,p,geometric,t.xyz));}
 return c;
}
// fogAmount is how far toward the fog's color a world point fades.
fn fogAmount(world:vec3<f32>)->f32 {
 if u.fog.y<=u.fog.x {return 0.0;}
 return clamp((eyeDistance(world)-u.fog.x)/(u.fog.y-u.fog.x),0.0,1.0);
}
// toonStep is the smooth step from 0 below edge-w/2 to 1 above edge+w/2, and
// toonBand snaps x in 0..1 to bands even levels, stepping up at each k/bands
// across a blend w wide: as the software renderer's.
fn toonStep(edge:f32,w:f32,x:f32)->f32 {let t=clamp((x-edge+w*0.5)/w,0.0,1.0);return t*t*(3.0-2.0*t);}
fn toonBand(x:f32,bands:f32,w:f32)->f32 {
 var sum=0.0;
 for(var k=1.0;k<bands-0.5;k=k+1.0) {sum+=toonStep(k/bands,w,x);}
 return sum/(bands-1.0);
}
// viewAt is the unit direction from world point p toward the viewer.
fn viewAt(p:vec3<f32>)->vec3<f32> {
 if u.eye.w>0.5 {return normalize(u.eye.xyz-p);}
 return -u.forward.xyz;
}
// toonCosine is the cosine between unit normal n at p and light l, and
// toonLobe its specular lobe there: what the toon bands step on.
fn toonCosine(n:vec3<f32>,p:vec3<f32>,l:Light)->f32 {return dot(n,toward(l,p).xyz);}
fn toonLobe(n:vec3<f32>,p:vec3<f32>,l:Light,shininess:f32)->f32 {
 let h=toward(l,p).xyz+viewAt(p);
 if dot(h,h)<=0.0 {return 0.0;}
 return pow(max(0.0,dot(n,normalize(h))),shininess);
}
fn pbrPow5(x:f32)->f32 {
 let c=clamp(x,0.0,1.0);let c2=c*c;return c2*c2*c;
}
fn envBRDF(f0:vec3<f32>,f90:f32,roughness:f32,nv:f32)->vec3<f32> {
 let r0=-roughness+1.0;
 let r1=-0.0275*roughness+0.0425;
 let r2=-0.572*roughness+1.04;
 let r3=0.022*roughness-0.04;
 let a004=min(r0*r0,exp2(-9.28*nv))*r0+r1;
 let abX=-1.04*a004+r2;
 let abY=1.04*a004+r3;
 return f0*abX+vec3<f32>(f90*abY);
}
fn pbrSpecularAO(nv:f32,ao:f32,alpha:f32)->f32 {
 if ao>=1.0 {return 1.0;}
 let e=exp2(-16.0*alpha-1.0);
 return clamp(pow(max(1e-5,nv+ao),e)-1.0+ao,0.0,1.0);
}
fn pbrToonLobe(n:vec3<f32>,p:vec3<f32>,l:Light,a2:f32)->f32 {
 let h=toward(l,p).xyz+viewAt(p);
 if dot(h,h)<=0.0 {return 0.0;}
 let nh=clamp(dot(n,normalize(h)),0.0,1.0);
 let denom=nh*nh*(a2-1.0)+1.0;
 return a2/max(1e-7,denom*denom)*0.25;
}
fn pbrSheenAlbedo(cosTheta:f32,roughness:f32)->f32 {
 let mu=clamp(cosTheta,0.0,1.0);let om=1.0-mu;
 return clamp(0.16*roughness*roughness+pbrPow5(om)*(0.35-0.15*roughness)+om*om*0.12*roughness,0.0,1.0);
}
fn pbrSheenBRDF(nh:f32,nl:f32,nv:f32,invAlpha:f32)->f32 {
 let sin2h=max(0.0,1.0-nh*nh);
 var piD=0.0;
 if sin2h>1e-6 {piD=0.5*(2.0+invAlpha)*pow(sin2h,0.5*invAlpha);}
 return piD/max(1e-4,4.0*(nl+nv-nl*nv));
}
fn pbrToonSheen(n:vec3<f32>,p:vec3<f32>,l:Light,invAlpha:f32)->f32 {
 let dir=toward(l,p).xyz;let nl=dot(n,dir);
 if nl<=0.0 {return 0.0;}
 let v=viewAt(p);let h=dir+v;
 if dot(h,h)<=0.0 {return 0.0;}
 let nh=clamp(dot(n,normalize(h)),0.0,1.0);
 let nv=clamp(abs(dot(n,v)),1e-4,1.0);
 return 2.0*nl*pbrSheenBRDF(nh,nl,nv,invAlpha);
}
// finish lights a surface, adds each light's specular highlight, and fades
// into the fog. Fragments without a normal are unlit. A mesh's fragments
// carry their instance (optics.w, one more than its index; zero otherwise)
// to read its toon, PBR, and emissive parameters.
// Band edges blend across about a pixel: each stepped term is found again a
// pixel over in x and in y, from the screen derivatives of the normal and
// the world position, so a flat face under a point light or a perspective
// view blends as a curved one does. The derivatives are taken here, before
// any branch on a varying.
fn finish(o:Out,rgb:vec3<f32>)->vec3<f32> {return shadeSurface(o,rgb,o.normal,vec3<f32>(1.0),vec3<f32>(1.0),vec4<f32>(0.0));}
fn finishNormal(o:Out,rgb:vec3<f32>,shadeNormal:vec3<f32>)->vec3<f32> {return shadeSurface(o,rgb,shadeNormal,vec3<f32>(1.0),vec3<f32>(1.0),vec4<f32>(0.0));}
fn shadeSurface(o:Out,rgb:vec3<f32>,shadeNormal:vec3<f32>,orm:vec3<f32>,emSample:vec3<f32>,baked:vec4<f32>)->vec3<f32> {
 var c=rgb;
 let nx=dpdx(shadeNormal);let ny=dpdy(shadeNormal);
 let gnx=dpdx(o.normal);let gny=dpdy(o.normal);
 let wx=dpdx(o.world);let wy=dpdy(o.world);
 var toon=vec4<f32>(0.0);var style=vec4<f32>(0.0);var pbr=vec4<f32>(0.0);var coat=vec2<f32>(0.0);var sheen=vec4<f32>(0.0);var emissive=vec3<f32>(0.0);var specPacked=0.0;var unlit=false;
 if o.optics.w>0.5 {let inst=instances[u32(o.optics.w)-1u];toon=inst.toon;style=inst.toonStyle;pbr=inst.pbr;coat=inst.coat.xy;sheen=inst.sheen;emissive=inst.emissive.rgb;specPacked=inst.emissive.w;unlit=inst.normal[0].w>0.5;}
 if !unlit && dot(o.normal,o.normal)>0.0 {
  let n=normalize(shadeNormal);
  let view=viewAt(o.world);
  // The normal and position a pixel over, for the toon band edges.
  let nX=normalize(shadeNormal+nx);let nY=normalize(shadeNormal+ny);
  let pX=o.world+wx;let pY=o.world+wy;
  let bands=style.x;let soft=style.y/max(bands,1.0);let shadowSoft=style.w;
  let envRefl=u.viewport.w>0.5 || (u.viewport.w>0.1 && style.z>0.5);
  if pbr.z>=0.5 {
   let roughness=clamp(pbr.y*orm.y,0.045,1.0);
   let metallic=clamp(pbr.x*orm.z,0.0,1.0);
   let ao=min(1.0+(orm.x-1.0)*pbr.w,surfaceAO(o.world));
   let alpha=roughness*roughness;let a2=alpha*alpha;
   var df0=vec3<f32>(pbr.z-1.0);var specFactor=1.0;
   if specPacked>0.5 {
    let bits=u32(specPacked)-1u;
    let sq=vec3<f32>(f32((bits>>16u)&255u),f32((bits>>8u)&255u),f32(bits&255u))*(1.0/255.0);
    df0=sq*sq;
    specFactor=pbr.z-1.0;
   }
   let f0=mix(df0,c,metallic);
   let f90Base=mix(specFactor,1.0,metallic);
   let diffColor=c*(1.0-metallic);
   let geometric=normalize(o.normal);
   var rn=n;var facing=dot(n,view);
   if facing<0.0 {rn=-n;facing=-facing;}
   let nv=clamp(facing,1e-4,1.0);
   let reflected=normalize(rn*(2.0*facing)-view);
   let cc=coat.x;let ccRough=max(0.045,coat.y);
   let ccAlpha=ccRough*ccRough;let ccA2=ccAlpha*ccAlpha;
   var ccN=geometric;var ccFacing=dot(geometric,view);var ccSign=1.0;
   if ccFacing<0.0 {ccN=-geometric;ccFacing=-ccFacing;ccSign=-1.0;}
   let ccNV=clamp(ccFacing,1e-4,1.0);
   let ccReflected=normalize(ccN*(2.0*ccFacing)-view);
   var ccViewScale=1.0;
   if cc>0.0 {ccViewScale=1.0-cc*(0.04+0.96*pbrPow5(1.0-ccNV));}
   let sheenCol=sheen.rgb;let sRough=max(0.07,sheen.w);
   let maxSheen=max(sheenCol.r,max(sheenCol.g,sheenCol.b));
   var sInvAlpha=0.0;var sheenEnvV=0.0;var sheenViewScale=1.0;
   if maxSheen>0.0 {
    sInvAlpha=1.0/(sRough*sRough);
    sheenEnvV=pbrSheenAlbedo(nv,sRough);
    sheenViewScale=1.0-maxSheen*sheenEnvV;
   }
   var diffOut=vec3<f32>(0.0);var specOut=vec3<f32>(0.0);
   if bands>0.5 {
    let ccNX=normalize((o.normal+gnx)*ccSign);let ccNY=normalize((o.normal+gny)*ccSign);
    let ambient=indirectToonDiffuse(ao,baked).radiance;
    var direct=vec3<f32>(0.0);var directDiff=vec3<f32>(0.0);
    for(var i=0u;i<u32(u.sky.w);i=i+1u) {
     let l=u.lights[i];let t=toward(l,o.world);let nl=dot(n,t.xyz);let ccNL=dot(ccN,t.xyz);
     if t.w>0.0 && (nl>0.0 || (cc>0.0 && ccNL>0.0)) {
      let sh=toonShadowFactor(i,o.world,pX,pY,geometric,t.xyz,shadowSoft);
      let h=t.xyz+view;let hasH=dot(h,h)>0.0;
      var hn=vec3<f32>(0.0);var vh=0.0;
      if hasH {hn=normalize(h);vh=clamp(dot(view,hn),0.0,1.0);}
      let fvh5=pbrPow5(1.0-vh);
      let fc=0.04+0.96*fvh5;
      let ccAtten=select(1.0,1.0-cc*fc,cc>0.0);
      if nl>0.0 {
       var sheenScale=sheenViewScale;
       if maxSheen>0.0 {sheenScale=1.0-maxSheen*max(sheenEnvV,pbrSheenAlbedo(nl,sRough));}
       let w=max(0.0001,abs(toonCosine(nX,pX,l)-nl)+abs(toonCosine(nY,pY,l)-nl)+soft);
       let b=t.w*toonBand(nl,bands,w)*sh;
       direct+=l.b.rgb*b;
       directDiff+=l.b.rgb*(b*ccAtten*sheenScale);
       if hasH {
        let nh=clamp(dot(n,hn),0.0,1.0);
        let denom=nh*nh*(a2-1.0)+1.0;
        let lobe=a2/max(1e-7,denom*denom)*0.25;
        let sw=max(0.0001,abs(pbrToonLobe(nX,pX,l,a2)-lobe)+abs(pbrToonLobe(nY,pY,l,a2)-lobe)+soft);
        let spot=toonStep(0.5,sw,lobe)*t.w*sh*ccAtten*sheenScale;
        let fk=f0+(vec3<f32>(f90Base)-f0)*fvh5;
        specOut+=l.b.rgb*spot*fk;
        if maxSheen>0.0 {
         let shLobe=2.0*nl*pbrSheenBRDF(nh,nl,nv,sInvAlpha);
         let shW=max(0.0001,abs(pbrToonSheen(nX,pX,l,sInvAlpha)-shLobe)+abs(pbrToonSheen(nY,pY,l,sInvAlpha)-shLobe)+soft);
         let shSpot=toonStep(0.5,shW,shLobe)*t.w*sh*ccAtten;
         specOut+=l.b.rgb*(shSpot*sheenCol);
        }
       }
      }
      if cc>0.0 && ccNL>0.0 && hasH {
       let ccNH=clamp(dot(ccN,hn),0.0,1.0);
       let ccDenom=ccNH*ccNH*(ccA2-1.0)+1.0;
       let ccLobe=ccA2/max(1e-7,ccDenom*ccDenom)*0.25;
       let ccSW=max(0.0001,abs(pbrToonLobe(ccNX,pX,l,ccA2)-ccLobe)+abs(pbrToonLobe(ccNY,pY,l,ccA2)-ccLobe)+soft);
       let ccSpot=toonStep(0.5,ccSW,ccLobe)*t.w*sh*cc*fc;
       specOut+=l.b.rgb*ccSpot;
      }
     }
    }
    let ambShaded=ambient*toon.rgb*ccViewScale;
    diffOut=diffColor*(ambShaded*sheenViewScale+directDiff);
    if maxSheen>0.0 {specOut+=sheenCol*sheenEnvV*ambShaded;}
    if toon.w>0.0 {
     let rimFacing=max(0.0,dot(n,view));
     let rw=max(0.0001,abs(max(0.0,dot(nX,viewAt(pX)))-rimFacing)+abs(max(0.0,dot(nY,viewAt(pY)))-rimFacing)+soft);
     specOut+=direct*(toon.w*toonStep(0.7,rw,1.0-rimFacing));
    }
   } else {
    let indDiff=indirectDiffuse(n,ao,baked).radiance;
    let f90=max(vec3<f32>(f90Base*(1.0-roughness)),f0);
    let ks=f0+(f90-f0)*pbrPow5(1.0-nv);
    let baseIndScale=sheenViewScale*ccViewScale;
    diffOut=diffColor*(vec3<f32>(1.0)-ks)*indDiff*baseIndScale;
    if maxSheen>0.0 {specOut+=sheenCol*sheenEnvV*indDiff*ccViewScale;}
    let envF90=select(clamp(50.0*max(f0.r,max(f0.g,f0.b)),0.0,1.0),clamp(f90Base,0.0,1.0),specPacked>0.5);
    let brdf=envBRDF(f0,envF90,roughness,nv);
    let sAO=pbrSpecularAO(nv,ao,alpha);
    let specEnv=indirectSpecular(o.world,reflected,roughness,envRefl,sAO).radiance;
    specOut+=specEnv*brdf*baseIndScale;
    if cc>0.0 {
     let ccBRDF=envBRDF(vec3<f32>(0.04),1.0,ccRough,ccNV);
     let ccSAO=pbrSpecularAO(ccNV,ao,ccAlpha);
     let ccSpecEnv=indirectSpecular(o.world,ccReflected,ccRough,envRefl,ccSAO).radiance;
     specOut+=ccSpecEnv*ccBRDF*cc;
    }
    let oneMinusA2=1.0-a2;let ccOneMinusA2=1.0-ccA2;
    for(var i=0u;i<u32(u.sky.w);i=i+1u) {
     let l=u.lights[i];let t=toward(l,o.world);let nl=dot(n,t.xyz);let ccNL=dot(ccN,t.xyz);
     if t.w>0.0 && (nl>0.0 || (cc>0.0 && ccNL>0.0)) {
      let vis=t.w*lightVisibility(i,o.world,geometric,t.xyz);
      if vis>0.0 {
       let h=t.xyz+view;let hasH=dot(h,h)>0.0;
       var hn=vec3<f32>(0.0);var vh=0.0;
       if hasH {hn=normalize(h);vh=clamp(dot(view,hn),0.0,1.0);}
       let fvh5=pbrPow5(1.0-vh);
       let fc=0.04+0.96*fvh5;
       let ccAtten=select(1.0,1.0-cc*fc,cc>0.0);
       if nl>0.0 {
        var sheenScale=sheenViewScale;
        if maxSheen>0.0 {sheenScale=1.0-maxSheen*max(sheenEnvV,pbrSheenAlbedo(nl,sRough));}
        var dTerm=0.0;var shTerm=0.0;
        if hasH {
         let nh=clamp(dot(n,hn),0.0,1.0);
         let denom=nh*nh*(a2-1.0)+1.0;
         let piD=a2/max(1e-7,denom*denom);
         let ggxV=nl*sqrt(nv*nv*oneMinusA2+a2);
         let ggxL=nv*sqrt(nl*nl*oneMinusA2+a2);
         dTerm=piD*(0.5/max(1e-5,ggxV+ggxL));
         if maxSheen>0.0 {shTerm=pbrSheenBRDF(nh,nl,nv,sInvAlpha);}
        }
        let fk=f0+(vec3<f32>(f90Base)-f0)*fvh5;
        let w=vis*nl*ccAtten;
        diffOut+=l.b.rgb*(w*sheenScale)*(vec3<f32>(1.0)-fk)*diffColor;
        specOut+=l.b.rgb*w*(sheenScale*dTerm*fk+shTerm*sheenCol);
       }
       if cc>0.0 && ccNL>0.0 && hasH {
        let ccNH=clamp(dot(ccN,hn),0.0,1.0);
        let ccDenom=ccNH*ccNH*(ccA2-1.0)+1.0;
        let ccPiD=ccA2/max(1e-7,ccDenom*ccDenom);
        let ccGGXV=ccNL*sqrt(ccNV*ccNV*ccOneMinusA2+ccA2);
        let ccGGXL=ccNV*sqrt(ccNL*ccNL*ccOneMinusA2+ccA2);
        let ccDTerm=ccPiD*(0.5/max(1e-5,ccGGXV+ccGGXL));
        specOut+=l.b.rgb*(vis*ccNL*cc*ccDTerm*fc);
       }
      }
     }
    }
   }
   if o.optics.x>0.0 {diffOut=transmit(o,diffOut,n);}
   c=diffOut+specOut;
  } else {
   var direct=vec3<f32>(0.0);
   if bands>0.5 {
    let ambient=indirectToonDiffuse(surfaceAO(o.world),baked).radiance;
    let geometric=normalize(o.normal);
    for(var i=0u;i<u32(u.sky.w);i=i+1u) {
     let l=u.lights[i];let t=toward(l,o.world);let s=dot(n,t.xyz);
     if t.w>0.0 && s>0.0 {
      let sh=toonShadowFactor(i,o.world,pX,pY,geometric,t.xyz,shadowSoft);
      let w=max(0.0001,abs(toonCosine(nX,pX,l)-s)+abs(toonCosine(nY,pY,l)-s)+soft);
      direct+=l.b.rgb*(t.w*toonBand(s,bands,w)*sh);
     }
    }
    c*=ambient*toon.rgb+direct;
   } else {
    c*=illuminate(n,o.world,normalize(o.normal),baked);
   }
   if o.optics.x>0.0 {c=transmit(o,c,n);}
   if o.absorption.w>0.0 {
    let rn=select(n,-n,dot(n,view)<0.0);
    let reflected=reflect(-view,rn);
    let sample=indirectReflection(o.world,reflected,envRefl,surfaceAO(o.world));
    let fresnel=o.absorption.w+(1.0-o.absorption.w)*pow(1.0-clamp(dot(rn,view),0.0,1.0),5.0);
    c=c*(1.0-fresnel*sample.coverage)+sample.radiance*fresnel;
   }
   if o.spec.x>0.0 {
    let specGeom=normalize(o.normal);
    for(var i=0u;i<u32(u.sky.w);i=i+1u) {
     let l=u.lights[i];let t=toward(l,o.world);let h=t.xyz+view;
     if t.w>0.0 && dot(n,t.xyz)>0.0 && dot(h,h)>0.0 {
      let hn=normalize(h);let nh=max(0.0,dot(n,hn));
      var lobe=pow(nh,o.spec.y);
      var vis=1.0;
      if bands>0.5 {
       let w=max(0.0001,abs(toonLobe(nX,pX,l,o.spec.y)-lobe)+abs(toonLobe(nY,pY,l,o.spec.y)-lobe)+soft);
       lobe=toonStep(0.5,w,lobe);
       vis=toonShadowFactor(i,o.world,pX,pY,specGeom,t.xyz,shadowSoft);
      } else {
       vis=lightVisibility(i,o.world,specGeom,t.xyz);
      }
      c+=l.b.rgb*(vis*t.w*o.spec.x*lobe);
     }
    }
   }
   if bands>0.5 && toon.w>0.0 {
    let facing=max(0.0,dot(n,view));
    let w=max(0.0001,abs(max(0.0,dot(nX,viewAt(pX)))-facing)+abs(max(0.0,dot(nY,viewAt(pY)))-facing)+soft);
    c+=direct*(toon.w*toonStep(0.7,w,1.0-facing));
   }
  }
 }
 c+=emissive*emSample;
 c=mix(c,u.fogColor.rgb,fogAmount(o.world));
 return c;
}
