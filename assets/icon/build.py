#!/usr/bin/env python3
"""Rasterises the Clack icon masters into PNGs, clack.ico and clack.icns.

Needs rsvg-convert and Pillow; uses optipng when it is installed.

    python3 assets/icon/gen_svg.py   # only after changing the design
    python3 assets/icon/build.py
    python3 assets/icon/build.py tray   # only the Windows shell's icons

16 and 24 px come from the thicker clack-small.svg; everything else from
clack.svg. The .icns is drawn from clack-macos.svg (Apple's icon grid).

The Windows shell's icons are drawn here, not from a master:

  - clack-tray-{rest,unread,highlight}.ico, the notification-area icon's
    states (Slack desktop's resources/slack-taskbar-{rest,unread,
    highlight}.ico, which TrayHandler swaps by unread state). Slack's are
    its mark in white with a half-black halo, 16, 20, 24 and 32 px DIB
    frames; unread and highlight add a dot at the bottom right (cyan,
    pink) in a dark ring. These are Clack's small glyph drawn the same
    way, with the dot where Slack's sits in each frame size.
  - screen.ico, the jump list's "Move Clack to Current Display" task
    (Slack's resources/screen.ico): a white monitor outline, 16 to 256 px.
"""
import io
import os
import re
import shutil
import struct
import subprocess
import sys

from PIL import Image

sys.dont_write_bytecode = True  # no __pycache__ next to the masters
import gen_svg  # noqa: E402

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
    write_frames(path, [(n, full(n)) for n in sizes])


def write_frames(path, images):
    """An .ico of (size, image) frames: PNG at 256, DIBs below."""
    frames = [(n, png_bytes(im) if n >= 256 else bmp_frame(im)) for n, im in images]
    out = struct.pack("<HHH", 0, 1, len(frames))
    offset = 6 + 16 * len(frames)
    for n, data in frames:
        d = 0 if n >= 256 else n
        out += struct.pack("<BBBBHHII", d, d, 0, 0, 1, 32, len(data), offset)
        offset += len(data)
    out += b"".join(data for _, data in frames)
    with open(path, "wb") as fh:
        fh.write(out)


def render_svg(svg, n):
    out = subprocess.run(["rsvg-convert", "-w", str(n), "-h", str(n)], input=svg.encode(),
                         check=True, capture_output=True).stdout
    return Image.open(io.BytesIO(out)).convert("RGBA")


def f(v):
    return f"{v:.3f}".rstrip("0").rstrip(".")


# The small glyph's bounding box on the 1024 canvas (rendered alpha).
GLYPH_BOX = (142, 166, 866, 834)
# Slack's dot in each tray frame (measured from 4.53.276's
# slack-taskbar-unread.ico): centre, fill radius, ring width, in px.
TRAY_DOTS = {16: (11, 4, 1), 20: (13.5, 4.5, 1.25), 24: (16.5, 5.5, 1.5), 32: (22, 8, 2)}
# The dot's fill, top to bottom (the same frames' gradients).
TRAY_FILL = {"unread": ("#00E0F9", "#00C9F5"), "highlight": ("#F74194", "#F33171")}
TRAY_SIZES = (16, 20, 24, 32)


def tray_svg(state, n):
    """The tray icon for state at n px: the white glyph over a halo of
    n/16 px at half black, then the dot in its 80% black ring."""
    halo = n / 16
    w, h = GLYPH_BOX[2] - GLYPH_BOX[0], GLYPH_BOX[3] - GLYPH_BOX[1]
    s = (n - 2 * halo) / max(w, h)
    cx, cy = (GLYPH_BOX[0] + GLYPH_BOX[2]) / 2, (GLYPH_BOX[1] + GLYPH_BOX[3]) / 2
    tf = f"translate({f(n / 2 - cx * s)} {f(n / 2 - cy * s)}) scale({f(s)})"
    white = gen_svg.glyph(True).replace(gen_svg.ACCENT, gen_svg.GLYPH)
    grow = 2 * halo / s
    dark = re.sub(r'stroke-width="([0-9.]+)"', lambda m: f'stroke-width="{f(float(m.group(1)) + grow)}"', white)
    dark = dark.replace(gen_svg.GLYPH, "#000000")
    parts = [f'<g opacity="0.5" transform="{tf}">{dark}</g>', f'<g transform="{tf}">{white}</g>']
    if state != "rest":
        c, r, ring = TRAY_DOTS[n]
        top, bottom = TRAY_FILL[state]
        parts.append('<defs><linearGradient id="dot" x1="0" y1="0" x2="0" y2="1">'
                     f'<stop offset="0" stop-color="{top}"/><stop offset="1" stop-color="{bottom}"/>'
                     '</linearGradient></defs>')
        parts.append(f'<circle cx="{f(c)}" cy="{f(c)}" r="{f(r + ring / 2)}" fill="none" stroke="#000000" '
                     f'stroke-opacity="0.8" stroke-width="{f(ring)}"/>')
        parts.append(f'<circle cx="{f(c)}" cy="{f(c)}" r="{f(r)}" fill="url(#dot)"/>')
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {n} {n}" width="{n}" height="{n}">'
            + "".join(parts) + "</svg>")


# Slack's screen.ico at 256 px: the screen's outline from (19, 32) to
# (236, 185) in a 19 px stroke, the stand 26 px wide down to the base, a
# 19 px line from x 67 to 187 at y 220.
SCREEN_SVG = ('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256" width="256" height="256">'
              '<g fill="none" stroke="#FFFFFF" stroke-linecap="round" stroke-linejoin="round">'
              '<rect x="28.5" y="41.5" width="198" height="134" rx="14" stroke-width="19"/>'
              '<path d="M127.5 176V211" stroke-width="26" stroke-linecap="butt"/>'
              '<path d="M77 220.5H177" stroke-width="19"/>'
              '</g></svg>')


def write_shell_icons():
    for state in ("rest", "unread", "highlight"):
        write_frames(os.path.join(HERE, f"clack-tray-{state}.ico"),
                     [(n, render_svg(tray_svg(state, n), n)) for n in TRAY_SIZES])
    write_frames(os.path.join(HERE, "screen.ico"),
                 [(n, render_svg(SCREEN_SVG, n)) for n in (256, 128, 64, 48, 32, 16)])


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
    write_shell_icons()
    if sys.argv[1:] == ["tray"]:
        return
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
