import struct,json,base64,math,pathlib
buf=bytearray(); views=[];access=[]
def acc(vals,typ,fmt='f'):
 while len(buf)%4:buf.append(0)
 flat=[x for v in vals for x in v] if isinstance(vals[0],(list,tuple)) else vals
 data=struct.pack('<'+fmt*len(flat),*flat);views.append(dict(buffer=0,byteOffset=len(buf),byteLength=len(data)));buf.extend(data)
 access.append(dict(bufferView=len(views)-1,componentType={'f':5126,'H':5123}[fmt],count=len(vals),type=typ));return len(access)-1
pos=acc([[-.3,0,0],[.3,0,0],[-.3,1,0],[.3,1,0],[-.3,2,0],[.3,2,0]],'VEC3')
norm=acc([[0,0,1]]*6,'VEC3');inds=acc([0,1,2,2,1,3,2,3,4,4,3,5],'SCALAR','H')
joints=acc([[0,0,0,0]]*2+[[0,1,0,0]]*2+[[1,0,0,0]]*2,'VEC4','H')
weights=acc([[1,0,0,0]]*2+[[.5,.5,0,0]]*2+[[1,0,0,0]]*2,'VEC4')
morph=acc([[0,0,0]]*4+[[-.2,0,.15],[.2,0,.15]],'VEC3')
ibm=acc([[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1],[1,0,0,0,0,1,0,0,0,0,1,0,0,-1,0,1]],'MAT4')
times=acc([0,1,2,3,4],'SCALAR')
rot=acc([[0,0,math.sin(a/2),math.cos(a/2)] for a in [0,-.7,0,.7,0]],'VEC4');mw=acc([0,1,0,1,0],'SCALAR')
trans=acc([[0,0,0],[.2,0,0],[0,0,0],[-.2,0,0],[0,0,0]],'VEC3')
scale=acc([[1,1,1],[1,1.1,1],[1,1,1],[1,.9,1],[1,1,1]],'VEC3')
doc=dict(asset=dict(version='2.0',generator='nt3d generated animation fixture; CC0'),scene=0,scenes=[dict(nodes=[0,1])],nodes=[dict(name='Ribbon',mesh=0,skin=0),dict(name='Base',children=[2]),dict(name='Tip',translation=[0,1,0])],meshes=[dict(primitives=[dict(attributes=dict(POSITION=pos,NORMAL=norm,JOINTS_0=joints,WEIGHTS_0=weights),indices=inds,targets=[dict(POSITION=morph)],material=0)],weights=[0])],materials=[dict(doubleSided=True,pbrMetallicRoughness=dict(baseColorFactor=[.1,.6,.9,1],metallicFactor=0,roughnessFactor=.7))],skins=[dict(joints=[1,2],inverseBindMatrices=ibm)],animations=[dict(name='Sway and stretch',samplers=[dict(input=times,output=o,interpolation='LINEAR') for o in [rot,mw,trans,scale]],channels=[dict(sampler=i,target=dict(node=n,path=p)) for i,(n,p) in enumerate([(2,'rotation'),(0,'weights'),(1,'translation'),(1,'scale')])])],accessors=access,bufferViews=views,buffers=[dict(byteLength=len(buf),uri='data:application/octet-stream;base64,'+base64.b64encode(buf).decode())])
text=json.dumps(doc,indent=2)+'\n'
for p in ['mesh/gltf/testdata/animated.gltf','examples/demos/internal/demo/models/animated.gltf']:pathlib.Path(p).write_text(text)
