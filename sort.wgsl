// Copyright (c) 2026 Neomantra Corp.
// The frame-wide splat sort: keys quantizes each alpha point to a 28-bit
// key (slice, slot, depth, as blendPlan.key does), seven radix passes of
// histogram, scan and scatter order the keys four bits at a time, count
// finds where each (slice, slot) run begins and ends among the sorted keys,
// and runs lays out its record and draw arguments. Points
// behind the eye or past the far plane take the sentinel, which sorts last
// and is never counted. Workgroups fold over y past 65535 a dimension.
// Each "// ==" section is a module of its own, after what precedes the
// first: their groups' bindings overlap.
struct SortParams { matrix:mat4x4<f32>, near:f32, far:f32, base:u32, count:u32, slot:u32 };
struct PassParams { n:u32, shift:u32, blocks:u32, slots:u32 };
struct PointRecord { x:f32, y:f32, z:f32, radius:f32, color:u32 };

const sentinel=0xFFFFFFFFu;
const block=2048u; // Keys a workgroup sorts: 256 threads, 8 each.
const fold=65535u;

// == keys: the key pass's group.
@group(0) @binding(0) var<uniform> sp:SortParams;
@group(0) @binding(1) var<storage,read> points:array<PointRecord>;
@group(0) @binding(2) var<storage,read_write> keysAll:array<u32>;
@group(0) @binding(3) var<storage,read_write> payloadAll:array<u32>;

@compute @workgroup_size(64) fn keys(@builtin(workgroup_id) wg:vec3<u32>, @builtin(local_invocation_id) lid:vec3<u32>) {
 let i=(wg.x+wg.y*fold)*64u+lid.x;
 if i>=sp.count { return; }
 let p=points[i];
 let c=sp.matrix*vec4<f32>(p.x,p.y,p.z,1.0);
 var key=sentinel;
 if !(c.w<=0.0 || c.z<0.0 || c.z>c.w) {
  var t=0.0;
  if sp.far>sp.near { t=min(1.0,max(0.0,(c.z-sp.near)/(sp.far-sp.near))); }
  if (bitcast<u32>(t)&0x7FFFFFFFu)>0x7F800000u { t=0.0; } // NaN, tested by its bits past fast math.
  let k=65535u-u32(sqrt(t)*65535.0);
  key=(k>>10u)<<22u | min(sp.slot,4095u)<<10u | (k&1023u);
 }
 keysAll[sp.base+i]=key;
 payloadAll[sp.base+i]=i;
}

// == radix: the radix passes' group: keys and payload in and out, ping-ponged, and
// the blocks' digit counts, digit-major so their scan is each digit's start.
@group(0) @binding(0) var<uniform> pass_:PassParams;
@group(0) @binding(1) var<storage,read> keysIn:array<u32>;
@group(0) @binding(2) var<storage,read> payloadIn:array<u32>;
@group(0) @binding(3) var<storage,read_write> keysOut:array<u32>;
@group(0) @binding(4) var<storage,read_write> payloadOut:array<u32>;
@group(0) @binding(5) var<storage,read_write> hist:array<u32>;

fn digit(k:u32)->u32 { return (k>>pass_.shift)&15u; }

var<workgroup> digits:array<atomic<u32>,16>;
@compute @workgroup_size(256) fn histogram(@builtin(workgroup_id) wg:vec3<u32>, @builtin(local_invocation_id) lid:vec3<u32>) {
 let b=wg.x+wg.y*fold;
 if b>=pass_.blocks { return; }
 let first=b*block+lid.x*8u;
 for (var i=0u; i<8u; i++) {
  if first+i<pass_.n { atomicAdd(&digits[digit(keysIn[first+i])],1u); }
 }
 workgroupBarrier();
 if lid.x<16u { hist[lid.x*pass_.blocks+b]=atomicLoad(&digits[lid.x]); }
}

var<workgroup> sums:array<u32,256>;
@compute @workgroup_size(256) fn scan(@builtin(local_invocation_id) lid:vec3<u32>) {
 let n=16u*pass_.blocks;
 var running=0u;
 for (var base=0u; base<n; base+=256u) {
  let c=base+lid.x;
  var v=0u;
  if c<n { v=hist[c]; }
  sums[lid.x]=v;
  workgroupBarrier();
  for (var off=1u; off<256u; off=off<<1u) {
   var t=0u;
   if lid.x>=off { t=sums[lid.x-off]; }
   workgroupBarrier();
   sums[lid.x]+=t;
   workgroupBarrier();
  }
  if c<n { hist[c]=running+sums[lid.x]-v; }
  running+=sums[255u];
  workgroupBarrier();
 }
}

// Each thread's digit counts, two digits of 16 bits a word, scanned over
// the threads so a key's place is its digit's start, then the keys of
// threads before it, then its thread's earlier keys: stable.
var<workgroup> ranks:array<array<u32,8>,256>;
var<workgroup> starts:array<u32,16>;
@compute @workgroup_size(256) fn scatter(@builtin(workgroup_id) wg:vec3<u32>, @builtin(local_invocation_id) lid:vec3<u32>) {
 let b=wg.x+wg.y*fold;
 if b>=pass_.blocks { return; }
 let first=b*block+lid.x*8u;
 var ks:array<u32,8>;
 var mine:array<u32,8>;
 for (var i=0u; i<8u; i++) {
  if first+i<pass_.n {
   ks[i]=keysIn[first+i];
   let d=digit(ks[i]);
   mine[d>>1u]+=1u<<((d&1u)*16u);
  }
 }
 ranks[lid.x]=mine;
 if lid.x<16u { starts[lid.x]=hist[lid.x*pass_.blocks+b]; }
 workgroupBarrier();
 for (var off=1u; off<256u; off=off<<1u) {
  var t=array<u32,8>(); // Initialized, so each step zeroes it: naga hoists a bare declaration.
  if lid.x>=off { t=ranks[lid.x-off]; }
  workgroupBarrier();
  for (var j=0u; j<8u; j++) { ranks[lid.x][j]+=t[j]; }
  workgroupBarrier();
 }
 var run=ranks[lid.x];
 for (var j=0u; j<8u; j++) { run[j]-=mine[j]; }
 for (var i=0u; i<8u; i++) {
  if first+i<pass_.n {
   let d=digit(ks[i]);
   let s=(d&1u)*16u;
   let to=starts[d]+((run[d>>1u]>>s)&0xFFFFu);
   run[d>>1u]+=1u<<s;
   keysOut[to]=ks[i];
   payloadOut[to]=payloadIn[first+i];
  }
 }
}

// == runs: the runs' group: the sorted keys, each run's first and end
// index, found where the sorted keys change run (an end of 0 marks a run
// with no keys), its record of start and count, 256 bytes apart to bind at
// a dynamic offset, and its draw arguments.
@group(0) @binding(0) var<uniform> runParams:PassParams;
@group(0) @binding(1) var<storage,read> sorted:array<u32>;
@group(0) @binding(2) var<storage,read_write> bounds:array<u32>;
@group(0) @binding(3) var<storage,read_write> runTable:array<u32>;
@group(0) @binding(4) var<storage,read_write> args:array<u32>;

// run is a key's (slice, slot) index among the frame's runs, or none for a
// sentinel.
const none=0xFFFFFFFFu;
fn run(k:u32)->u32 {
 let slot=(k>>10u)&4095u;
 if k>=1u<<28u || slot>=runParams.slots { return none; }
 return (k>>22u)*runParams.slots+slot;
}

@compute @workgroup_size(256) fn count(@builtin(workgroup_id) wg:vec3<u32>, @builtin(local_invocation_id) lid:vec3<u32>) {
 let i=(wg.x+wg.y*fold)*256u+lid.x;
 let n=runParams.n;
 if i>=n { return; }
 let r=run(sorted[i]);
 if r==none { return; }
 if i==0u || run(sorted[i-1u])!=r { bounds[r*2u]=i; }
 if i+1u==n || run(sorted[i+1u])!=r { bounds[r*2u+1u]=i+1u; }
}

var<workgroup> runSums:array<u32,256>;
@compute @workgroup_size(256) fn runs(@builtin(local_invocation_id) lid:vec3<u32>) {
 let n=64u*runParams.slots; // blendSlices
 var running=0u;
 for (var base=0u; base<n; base+=256u) {
  let r=base+lid.x;
  var v=0u;
  if r<n && bounds[r*2u+1u]!=0u { v=bounds[r*2u+1u]-bounds[r*2u]; }
  runSums[lid.x]=v;
  workgroupBarrier();
  for (var off=1u; off<256u; off=off<<1u) {
   var t=0u;
   if lid.x>=off { t=runSums[lid.x-off]; }
   workgroupBarrier();
   runSums[lid.x]+=t;
   workgroupBarrier();
  }
  if r<n {
   runTable[r*64u]=running+runSums[lid.x]-v;
   runTable[r*64u+1u]=v;
   args[r*4u]=6u;
   args[r*4u+1u]=v;
   args[r*4u+2u]=0u;
   args[r*4u+3u]=0u;
  }
  running+=runSums[255u];
  workgroupBarrier();
 }
}
