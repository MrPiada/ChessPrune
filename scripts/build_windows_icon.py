#!/usr/bin/env python3
"""Build Windows ICO and PE icon COFF .syso from pre-rendered PNG files.

Python 3 standard library only. The .syso file is compiled into the Go
Windows binary automatically; no mingw, external Go modules or network needed.
"""
from pathlib import Path
import argparse
import struct

SIZES = (16, 24, 32, 48, 64, 128, 256)
ROOT = Path(__file__).resolve().parent.parent
PNG_DIR = ROOT / 'packaging' / 'windows_icon'

def u16(n): return struct.pack('<H', n)
def u32(n): return struct.pack('<I', n)
def align4(blob):
    while len(blob)%4: blob.append(0)

def assets():
    files = [(s,(PNG_DIR / f'icon_{s}.png').read_bytes()) for s in SIZES]
    for size,png in files:
        if not png.startswith(b'\x89PNG\r\n\x1a\n'):
            raise ValueError(f'Invalid PNG image: {size}')
    return files

def write_ico(files, destination):
    header = struct.pack('<HHH',0,1,len(files))
    images = bytearray()
    offset=6+16*len(files)
    for size,png in files:
        b = size if size<256 else 0
        header+=struct.pack('<BBBBHHII',b,b,0,0,1,32,len(png),offset)
        images+=png
        offset+=len(png)
    destination.write_bytes(header+images)

class Rsrc:
    def __init__(self): self.content=bytearray();self.reloc=[]
    def reserve(self,n):
        align4(self.content)
        offset=len(self.content)
        self.content+=bytes(n)
        return offset
    def directory(self,ids):
        off=self.reserve(16+len(ids)*8)
        struct.pack_into('<IIHHHH',self.content,off,0,0,0,0,0,len(ids))
        for i,resource_id in enumerate(ids): struct.pack_into('<I',self.content,off+16+i*8,resource_id)
        return off
    def point_dir(self,parent,i,offset):struct.pack_into('<I',self.content,parent+16+i*8+4,0x80000000|offset)
    def point_data(self,parent,i,offset):struct.pack_into('<I',self.content,parent+16+i*8+4,offset)
    def leaf(self,blob):
        entry=self.reserve(16)
        ptr=self.reserve(len(blob))
        self.content[ptr:ptr+len(blob)]=blob
        struct.pack_into('<IIII',self.content,entry,ptr,len(blob),0,0)
        self.reloc.append(entry)
        return entry

def write_syso(files,destination):
    r=Rsrc()
    # RT_ICON=3 and RT_GROUP_ICON=14; each resource has language 0x409.
    root=r.directory([3,14]); icons_dir=r.directory(list(range(1,len(files)+1)))
    r.point_dir(root,0,icons_dir)
    group_dir=r.directory([1]);r.point_dir(root,1,group_dir)
    blobs=[]
    for idx,(size,png) in enumerate(files):
        lang_dir=r.directory([0x409]);r.point_dir(icons_dir,idx,lang_dir)
        blobs.append((lang_dir,png))
    group_lang=r.directory([0x409]);r.point_dir(group_dir,0,group_lang)
    group=struct.pack('<HHH',0,1,len(files))
    for index,(size,png) in enumerate(files,1):
        group+=struct.pack('<BBBBHHIH',size if size<256 else 0,size if size<256 else 0,0,0,1,32,len(png),index)
    blobs.append((group_lang,group))
    for parent,payload in blobs:r.point_data(parent,0,r.leaf(payload))
    align4(r.content)
    raw=r.content
    # IMAGE_FILE_MACHINE_AMD64 / image section .rsrc / IMAGE_REL_AMD64_ADDR32NB.
    header_size=20+40
    relocation_off=header_size+len(raw)
    sym_off=relocation_off+10*len(r.reloc)
    file_header=struct.pack('<HHIIIHH',0x8664,1,0,sym_off,1,0,0)
    section_header=struct.pack('<8sIIIIIIHHI',b'.rsrc\0\0\0',0,0,len(raw),header_size,relocation_off,0,len(r.reloc),0,0x40000040)
    relocations=b''.join(struct.pack('<IIH',off,0,0x0003) for off in r.reloc)
    symbol=struct.pack('<8sIhHBB',b'.rsrc\0\0\0',0,1,0,3,0)
    destination.write_bytes(file_header+section_header+raw+relocations+symbol+u32(4))

if __name__=='__main__':
    ap=argparse.ArgumentParser()
    ap.add_argument('--output',default=str(ROOT/'rsrc_windows_amd64.syso'))
    args=ap.parse_args()
    files=assets()
    write_ico(files,ROOT/'packaging'/'ChessPrune.ico')
    write_syso(files,Path(args.output))
    print(f'Windows icon created: {args.output}')
