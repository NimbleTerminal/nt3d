@vertex fn vs_box(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return boxOutput(vi,ii);}
struct ShadowOut { @builtin(position) clip:vec4<f32>, @location(0) uv:vec2<f32>, @location(1) cut:f32, @location(2) @interpolate(flat) repeat:f32, @location(3) world:vec3<f32>, @location(4) normal:vec3<f32>, @location(5) color:vec3<f32> };
@vertex fn vs_shadow_mesh(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->ShadowOut {
 var o:ShadowOut;o.clip=u.pointShadows[vi>>28u]*instances[ii].world*vec4<f32>(items[vi&0x0fffffffu].a.xyz,1.0);return o;
}
@vertex fn vs_shadow_box(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->ShadowOut {
 let b=boxOutput(vi&0x0fffffffu,ii);var o:ShadowOut;o.clip=u.pointShadows[vi>>28u]*vec4<f32>(b.world,1.0);return o;
}
@vertex fn vs_shadow_texture(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->ShadowOut {
 let p=items[vi&0x0fffffffu];let inst=instances[ii];let uv=vec2<f32>(p.a.w,p.b.w);
 var pos=p.a.xyz;var norm=p.b.xyz;
 if nt3dHasDisplace {
  let d=nt3d_displace(pos,norm,(inst.world*vec4<f32>(pos,1.0)).xyz,uv);
  pos=d.position;norm=d.normal;
 }
 let world=(inst.world*vec4<f32>(pos,1.0)).xyz;
 var o:ShadowOut;o.clip=u.pointShadows[vi>>28u]*vec4<f32>(world,1.0);
 o.uv=uv;o.cut=p.color.x;o.world=world;
 let flags=u32(p.color.y);
 o.repeat=select(0.0,1.0,(flags&1u)!=0u && inst.normal[3].z>0.5);
 if nt3dHasMaterial {
  if dot(norm,norm)>0.0 {o.normal=normalize((inst.normal*vec4<f32>(norm,0.0)).xyz);}
  var tint=vec3<f32>(1.0);
  if (flags&4u)!=0u {let c=u32(p.color.z);tint=vec3<f32>(f32((c>>16u)&255u),f32((c>>8u)&255u),f32(c&255u))/255.0;}
  var linTint=srgbDecode3(tint);
  if inst.coat.w>=0.5 {let rgb=u32(inst.coat.z);linTint*=srgbDecode3(vec3<f32>(f32((rgb>>16u)&255u),f32((rgb>>8u)&255u),f32(rgb&255u))/255.0);}
  o.color=linTint;
 }
 return o;
}
// A cutout's shadow is depth alone, but the pass carries a scratch color
// target, and a browser refuses a target the fragment stage does not write
// unless its write mask is zero, which the browser binding cannot say (it
// omits a zero mask, and the default is every channel). So it is written.
@fragment fn fs_shadow_cutout(o:ShadowOut)->@location(0) vec4<f32> {
 let t=sampleMap(o.uv,vec2<f32>(0.0),vec2<f32>(0.0),o.repeat);
 var a=t.a;
 if nt3dHasMaterial {
  var n=vec3<f32>(0.0);if dot(o.normal,o.normal)>0.0 {n=normalize(o.normal);}
  a=nt3d_material(o.world,n,viewAt(o.world),o.uv,vec4<f32>(t.rgb*o.color,t.a)).a;
 }
 if a<o.cut {discard;}
 return vec4<f32>(0.0);
}

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
 let corner=corners[vi];let pos=vec3<f32>((lo.xy+hi.xy)*0.5+corner*radius,select(lo.z,item.color.x,item.color.y>0.5));
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
 let half=0.5/f32(textureDimensions(reflectionMap).y);
 let uv=vec2<f32>(atan2(d.y,d.x)/6.28318530718+0.5,clamp(acos(clamp(d.z,-1.0,1.0))/3.14159265359,half,1.0-half));
 let c=textureSampleLevel(reflectionMap,reflectionSampler,uv,0.0).rgb;
 if u.viewport.z<0.5 {let v=clamp(c,vec3<f32>(0.0),vec3<f32>(1.0));return vec4<f32>(select(1.055*pow(v,vec3<f32>(1.0/2.4))-0.055,v*12.92,v<=vec3<f32>(0.0031308)),1.0);}
 return vec4<f32>(c,1.0);
}

