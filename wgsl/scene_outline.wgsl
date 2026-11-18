// Inverted hull: expand back faces in output pixels along the projected normal.
// Use the same records (and GPU skin output) as the surface; no mesh reupload.
fn unpackOutlineNormal(code:u32)->vec3<f32> {
 return normalize((vec3<f32>(f32(code&63u),f32((code>>6u)&63u),f32((code>>12u)&63u))-32.0)/31.0);
}
fn outlineOutput(input:Out,vi:u32,ii:u32,code:u32)->Out {
 var o=input; let inst=instances[ii];
 var n=o.normal;
 if !nt3dHasDisplace {
  var localN=items[vi].b.xyz;
  if code!=0u {localN=unpackOutlineNormal(code);}
  n=(inst.normal*vec4<f32>(localN,0.0)).xyz;
 }
 let d=u.matrix*vec4<f32>(n,0.0);
 let direction=(d.xy*o.clip.w-o.clip.xy*d.w)*u.viewport.xy;
 var width=inst.outline.w;
 if width<0.0 {
  let row=vec3<f32>(u.matrix[0][1],u.matrix[1][1],u.matrix[2][1]);
  width=clamp(-width*length(row)*u.viewport.y*0.5/max(o.clip.w,0.00001),0.0,32.0);
 }
 if o.clip.w>0.00001 && length(direction)>0.000001 {
  o.clip=vec4<f32>(o.clip.xy+normalize(direction)*width*2.0/u.viewport.xy*o.clip.w,o.clip.zw);
 }
 o.color=vec4<f32>(decode(inst.outline.rgb),o.color.a);
 o.normal=vec3<f32>(0.0);o.normalStrength=0.0;o.optics=vec4<f32>(0.0);
 if width<=0.0 {o.clip=vec4<f32>(0.0,0.0,2.0,1.0);}
 return o;
}
@vertex fn vs_outline(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return outlineOutput(meshOutput(vi,ii,1.0),vi,ii,u32(items[vi].color.w));}
@vertex fn vs_outline_map(@builtin(vertex_index) vi:u32,@builtin(instance_index) ii:u32)->Out {return outlineOutput(mapOutput(vi,ii,1.0),vi,ii,u32(items[vi].color.y)>>6u);}
@fragment fn fs_outline_cutout(o:Out)->@location(0) vec4<f32> {
 var a=sampleMap(o.uv,vec2<f32>(0.0),vec2<f32>(0.0),o.repeat).a;
 if nt3dHasMaterial {a=evalMaterialHook(o,vec3<f32>(0.0),vec3<f32>(1.0),a).a;}
 if a<o.cut {discard;}
 return vec4<f32>(finish(o,o.color.rgb),1.0);
}
@fragment fn fs_outline_blend(o:Out)->@location(0) vec4<f32> {
 var alpha=sampleMap(o.uv,dpdx(o.uv),dpdy(o.uv),o.repeat).a*o.cut;
 if nt3dHasMaterial {alpha=evalMaterialHook(o,vec3<f32>(0.0),vec3<f32>(1.0),alpha).a;}
 return vec4<f32>(finish(o,o.color.rgb),alpha);
}

