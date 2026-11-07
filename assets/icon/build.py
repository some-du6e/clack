#!/usr/bin/env python3
"""Rasterises the Clack icon masters into PNGs, clack.ico and clack.icns.

Needs rsvg-convert and Pillow; uses optipng when it is installed.

    python3 assets/icon/gen_svg.py   # only after changing the design
    python3 assets/icon/build.py

16 and 24 px come from the thicker clack-small.svg; everything else from
clack.svg. The .icns is drawn from clack-macos.svg (Apple's icon grid).
"""
import io
import os
import shutil
import struct
import subprocess

from PIL import Image

HERE = os.path.dirname(os.path.abspath(__file__))
SMALL = 24  # sizes up to this use the small-size variant


def render(svg, n):
    out = subprocess.run(["rsvg-convert", "-w", str(n), "-h", str(n), os.path.join(HERE, svg)],
                         check=True, capture_output=True).stdout
    return Image.open(io.BytesIO(out)).convert("RGBA")


def full(n):
    return render("clack-small.svg" if n <= SMALL else "clack.svg", n)


def mac(n):
    return render("clack-macos-small.svg" if n <= 16 else "clack-macos.svg", n)


def png_bytes(im):
    b = io.BytesIO()
    im.save(b, "PNG", optimize=True)
    return b.getvalue()


def bmp_frame(im):
    """32-bit BGRA DIB with an AND mask, as ICO expects for small frames."""
    w, h = im.size
    header = struct.pack("<IiiHHIIiiII", 40, w, h * 2, 1, 32, 0, 0, 0, 0, 0, 0)
    px = im.tobytes("raw", "BGRA")
    rows = [px[y * w * 4:(y + 1) * w * 4] for y in range(h)]
    xor = b"".join(reversed(rows))
    stride = ((w + 31) // 32) * 4
    alpha = im.getchannel("A").tobytes()
    mask = bytearray()
    for y in reversed(range(h)):
        row = bytearray(stride)
        for x in range(w):
            if alpha[y * w + x] == 0:
                row[x // 8] |= 0x80 >> (x % 8)
        mask += row
    return header + xor + bytes(mask)


def write_ico(path, sizes):
    frames = []
    for n in sizes:
        im = full(n)
        frames.append((n, png_bytes(im) if n >= 256 else bmp_frame(im)))
    out = struct.pack("<HHH", 0, 1, len(frames))
    offset = 6 + 16 * len(frames)
    for n, data in frames:
        d = 0 if n >= 256 else n
        out += struct.pack("<BBBBHHII", d, d, 0, 0, 1, 32, len(data), offset)
        offset += len(data)
    out += b"".join(data for _, data in frames)
    with open(path, "wb") as fh:
        fh.write(out)


def rle_literal(data):
    """ICNS RLE using only literal runs (header n-1, then n bytes): valid and simple."""
    out = bytearray()
    for i in range(0, len(data), 128):
        chunk = data[i:i + 128]
        out.append(len(chunk) - 1)
        out += chunk
    return bytes(out)


def legacy_rgb(im):
    r, g, b, _ = im.split()
    return b"".join(rle_literal(c.tobytes()) for c in (r, g, b))


def write_icns(path):
    # 16 and 32 at 1x use the legacy RGB+mask types every macOS reads;
    # the rest are PNG types (ic11-ic14 are the @2x slots).
    elems = []
    for n, typ, mask in ((16, b"is32", b"s8mk"), (32, b"il32", b"l8mk")):
        im = mac(n)
        elems.append((typ, legacy_rgb(im)))
        elems.append((mask, im.getchannel("A").tobytes()))
    for typ, n in ((b"ic11", 32), (b"ic12", 64), (b"ic07", 128), (b"ic13", 256),
                   (b"ic08", 256), (b"ic14", 512), (b"ic09", 512), (b"ic10", 1024)):
        elems.append((typ, png_bytes(mac(n))))
    body = b"".join(t + struct.pack(">I", len(d) + 8) + d for t, d in elems)
    with open(path, "wb") as fh:
        fh.write(b"icns" + struct.pack(">I", len(body) + 8) + body)


def main():
    for n in (16, 24, 32, 48, 64, 128, 256, 512, 1024):
        full(n).save(os.path.join(HERE, f"clack-{n}.png"), optimize=True)
    write_ico(os.path.join(HERE, "clack.ico"), (16, 24, 32, 48, 64, 128, 256))

    write_icns(os.path.join(HERE, "clack.icns"))

    if shutil.which("optipng"):
        pngs = [os.path.join(HERE, f) for f in os.listdir(HERE) if f.endswith(".png")]
        subprocess.run(["optipng", "-quiet", "-o5", "-strip", "all", *pngs], check=True)

    shutil.copyfile(os.path.join(HERE, "clack.svg"), os.path.join(HERE, "..", "icon.svg"))


if __name__ == "__main__":
    main()
