// Clustered LOD selection: pick culls and levels each cluster, scan lays
// their items out, and scatter copies the item indices. The expressions
// repeat select.go's, so both paths choose alike.
struct Params {
 matrix:mat4x4<f32>,
 eye:vec4<f32>,
 pad:vec4<f32>, // x, y: margins in NDC; z: the orthographic distance.
 ranges:array<vec4<f32>,2>,
 fractions:array<vec4<f32>,2>,
 counts:vec4<u32>, // clusters, levels, perspective, vertices an item draws.
};
struct Cluster { min:vec3<f32>, first:u32, max:vec3<f32>, count:u32 };
@group(0) @binding(0) var<uniform> params:Params;
@group(0) @binding(1) var<storage,read> clusters:array<Cluster>;
@group(0) @binding(2) var<storage,read> order:array<u32>;
@group(0) @binding(3) var<storage,read_write> picks:array<vec4<u32>>; // level, count, offset, visible
@group(0) @binding(4) var<storage,read_write> items:array<u32>;
@group(0) @binding(5) var<storage,read_write> indirect:array<u32>;

fn rangeAt(i:u32)->f32 { return params.ranges[i/4u][i%4u]; }
fn fractionAt(i:u32)->f32 { return params.fractions[i/4u][i%4u]; }

fn outside(lo:vec3<f32>, hi:vec3<f32>)->bool {
 var common=0x3fu;
 for (var i=0u; i<8u; i++) {
  var p=lo;
  if (i&1u)!=0u { p.x=hi.x; }
  if (i&2u)!=0u { p.y=hi.y; }
  if (i&4u)!=0u { p.z=hi.z; }
  let c=params.matrix*vec4<f32>(p,1.0);
  var sides=0u;
  if c.x < -c.w*(1.0+params.pad.x) { sides|=1u; }
  if c.x > c.w*(1.0+params.pad.x) { sides|=2u; }
  if c.y < -c.w*(1.0+params.pad.y) { sides|=4u; }
  if c.y > c.w*(1.0+params.pad.y) { sides|=8u; }
  if c.z < 0.0 { sides|=16u; }
  if c.z > c.w { sides|=32u; }
  common&=sides;
  if common==0u { return false; }
 }
 return true;
}

@compute @workgroup_size(64) fn pick(@builtin(global_invocation_id) id:vec3<u32>) {
 let c=id.x;
 if c>=params.counts.x { return; }
 let cl=clusters[c];
 if outside(cl.min,cl.max) { picks[c]=vec4<u32>(0u,0u,0u,0u); return; }
 var d=params.pad.z;
 if params.counts.z!=0u {
  let center=(cl.min+cl.max)*0.5;
  let radius=length(cl.max-cl.min)*0.5;
  d=max(0.0,length(center-params.eye.xyz)-radius);
 }
 let levels=params.counts.y;
 var level=levels-1u;
 for (var i=0u; i<levels-1u; i++) {
  if d<rangeAt(i) { level=i; break; }
 }
 let f=fractionAt(level);
 var n=0u;
 if f!=0.0 { n=max(1u,u32(ceil(f32(cl.count)*f))); }
 picks[c]=vec4<u32>(level,n,0u,1u);
}

var<workgroup> sums:array<u32,256>;
@compute @workgroup_size(256) fn scan(@builtin(local_invocation_id) lid:vec3<u32>) {
 let n=params.counts.x;
 var running=0u;
 for (var base=0u; base<n; base+=256u) {
  let c=base+lid.x;
  var v=0u;
  if c<n { v=picks[c].y; }
  sums[lid.x]=v;
  workgroupBarrier();
  for (var off=1u; off<256u; off=off<<1u) {
   var t=0u;
   if lid.x>=off { t=sums[lid.x-off]; }
   workgroupBarrier();
   sums[lid.x]+=t;
   workgroupBarrier();
  }
  if c<n { picks[c].z=running+sums[lid.x]-v; }
  running+=sums[255u];
  workgroupBarrier();
 }
 if lid.x==0u { indirect[0]=params.counts.w; indirect[1]=running; indirect[2]=0u; indirect[3]=0u; }
}

// One workgroup a cluster; WebGPU allows 65535 a dimension, so the
// clusters fold over two.
@compute @workgroup_size(64) fn scatter(@builtin(workgroup_id) wg:vec3<u32>, @builtin(local_invocation_id) lid:vec3<u32>) {
 let c=wg.x+wg.y*65535u;
 if c>=params.counts.x { return; }
 let p=picks[c];
 let first=clusters[c].first;
 for (var i=lid.x; i<p.y; i+=64u) { items[p.z+i]=order[first+i]; }
}
