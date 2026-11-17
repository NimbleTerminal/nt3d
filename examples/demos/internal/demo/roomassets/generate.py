"""Generate original, synthetic HDR fixtures. This is not a GI baker.
Run from any directory: python3 path/to/generate.py
"""
from pathlib import Path
import math
ROOT = Path(__file__).parent

def hdr(name, w, h, pixel):
    data = bytearray(f'#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n-Y {h} +X {w}\n'.encode())
    for y in range(h):
        for x in range(w):
            rgb = pixel((x+.5)/w, (y+.5)/h)
            peak = max(rgb)
            if peak < 1e-32:
                data.extend((0,0,0,0)); continue
            mantissa, exponent = math.frexp(peak)
            scale = mantissa*256/peak
            data.extend([min(255,int(max(0,c)*scale)) for c in rgb]+[exponent+128])
    (ROOT/name).write_bytes(data)

def wall(u,v):
    warm=math.exp(-((u-.22)**2+(v-.35)**2)/.12)
    cool=math.exp(-((u-.8)**2+(v-.3)**2)/.08)
    edge=.5+.5*min(1, min(u,1-u,v,1-v)*9)
    return [math.pi*edge*(.2+.75*warm+.12*cool), math.pi*edge*(.2+.3*warm+.35*cool), math.pi*edge*(.22+.1*warm+.9*cool)]

def floor(u,v):
    v=1-v
    warm=math.exp(-((u-.2)**2+(v-.85)**2)/.15)
    cool=math.exp(-((u-.85)**2+(v-.8)**2)/.12)
    # Fixed pedestal contact darkening, unrelated to the moving sphere.
    contact=1-.75*math.exp(-((u-.5)**2+(v-.7)**2)/.015)
    return [math.pi*contact*(.17+.8*warm+.1*cool),math.pi*contact*(.18+.35*warm+.3*cool),math.pi*contact*(.2+.1*warm+.75*cool)]

def panorama(u,v):
    az=(u-.5)*2*math.pi; polar=v*math.pi
    d=[math.sin(polar)*math.cos(az),math.sin(polar)*math.sin(az),math.cos(polar)]
    origin=[0,0,1]; lo=[-2,-1.5,0]; hi=[2,1.5,2.4]
    distances=[((hi[i] if d[i]>0 else lo[i])-origin[i])/d[i] if abs(d[i])>1e-9 else 1e9 for i in range(3)]
    axis=min(range(3),key=lambda i:distances[i]); p=[origin[i]+d[i]*distances[axis] for i in range(3)]
    if axis==1 and d[1]>0 and 1.35<p[2]<2.05:
        if -1.65<p[0]<-.55: return [5,2.4,.7]
        if .55<p[0]<1.65: return [.7,2.8,5]
    if axis==2: return [.3,.32,.36] if d[2]<0 else [.55,.58,.65]
    if axis==0: return [.8,.16,.06] if d[0]<0 else [.06,.25,.8]
    # High contrast wall stripes make parallax visible on polished surfaces.
    stripe=.18 if int((p[0]+2)*5)%2 else .5
    return [stripe,stripe*.95,stripe*.9]

hdr('floor.hdr',64,64,floor)
hdr('wall.hdr',64,64,wall)
hdr('room.hdr',256,128,panorama)
