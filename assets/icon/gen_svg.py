#!/usr/bin/env python3
"""Generates the Clack icon SVG masters. Run from anywhere:

    python3 assets/icon/gen_svg.py && python3 assets/icon/build.py

The mark: a bold rounded "C" that is also a speech bubble (tail at the
bottom left), with two short "clack" strokes springing out of its mouth.
"""
import math
import os

HERE = os.path.dirname(os.path.abspath(__file__))

BG_TOP = "#7B5CFF"     # bright violet
BG_BOTTOM = "#3B1FB8"  # deep indigo
GLYPH = "#FFFFFF"
ACCENT = "#FFB547"     # warm amber clack strokes


def pt(cx, cy, r, deg):
    a = math.radians(deg)
    return cx + r * math.cos(a), cy - r * math.sin(a)  # y up, degrees CCW


def f(v):
    return f"{v:.1f}".rstrip("0").rstrip(".")


def glyph(small=False):
    """Returns SVG elements for the glyph on a 1024 canvas."""
    cx, cy = 476, 500
    if small:
        R, w = 236, 196       # ring centre-line radius, stroke width
        gap = 46              # half-angle of the C's mouth
        strokes = [(0, 110, 190, 112)]
    else:
        R, w = 248, 150
        gap = 42
        strokes = [(32, 128, 214, 62), (-32, 128, 214, 62)]
    # Ring as a stroked arc with round caps, from +gap going CCW round to -gap.
    x0, y0 = pt(cx, cy, R, gap)
    x1, y1 = pt(cx, cy, R, -gap)
    ring = (f'<path d="M{f(x0)} {f(y0)}A{R} {R} 0 1 0 {f(x1)} {f(y1)}" '
            f'fill="none" stroke="{GLYPH}" stroke-width="{w}" stroke-linecap="round"/>')
    # Speech tail: a rounded wedge leaving the ring's outer edge at the
    # bottom left and pointing down-left.
    ro = R + w / 2
    a1, a2 = 196, 258
    p1 = pt(cx, cy, ro - 30, a1)
    p2 = pt(cx, cy, ro - 30, a2)
    tip = pt(cx, cy, ro + (78 if small else 88), 229)
    # Outer side bulges a touch, inner side is scooped: a classic bubble tail.
    c1 = pt(cx, cy, ro + 22, 214)
    c2 = pt(cx, cy, ro - 22, 240)
    tail = (f'<path d="M{f(p1[0])} {f(p1[1])}Q{f(c1[0])} {f(c1[1])} {f(tip[0])} {f(tip[1])}'
            f'Q{f(c2[0])} {f(c2[1])} {f(p2[0])} {f(p2[1])}Z" fill="{GLYPH}" '
            f'stroke="{GLYPH}" stroke-width="{36 if small else 28}" stroke-linejoin="round"/>')
    parts = [ring, tail]
    mx, my = cx + R * math.cos(math.radians(gap)) - 20, cy  # mouth
    for deg, r0, r1, sw in strokes:
        a, b = pt(mx, my, r0, deg), pt(mx, my, r1, deg)
        parts.append(f'<path d="M{f(a[0])} {f(a[1])}L{f(b[0])} {f(b[1])}" '
                     f'stroke="{ACCENT}" stroke-width="{sw}" stroke-linecap="round"/>')
    return "\n    ".join(parts)


def defs(prefix):
    return (f'<defs>\n'
            f'    <linearGradient id="{prefix}bg" x1="0" y1="0" x2="0.35" y2="1">\n'
            f'      <stop offset="0" stop-color="{BG_TOP}"/>\n'
            f'      <stop offset="1" stop-color="{BG_BOTTOM}"/>\n'
            f'    </linearGradient>\n'
            f'    <radialGradient id="{prefix}sheen" cx="0.3" cy="0.12" r="0.75">\n'
            f'      <stop offset="0" stop-color="#FFFFFF" stop-opacity="0.18"/>\n'
            f'      <stop offset="1" stop-color="#FFFFFF" stop-opacity="0"/>\n'
            f'    </radialGradient>\n'
            f'  </defs>')


def full(small=False):
    rx = 225  # 22% of 1024
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" width="1024" height="1024">
  <title>Clack</title>
  {defs("c")}
  <rect width="1024" height="1024" rx="{rx}" fill="url(#cbg)"/>
  <rect width="1024" height="1024" rx="{rx}" fill="url(#csheen)"/>
  <g>
    {glyph(small)}
  </g>
</svg>
'''


def macos(small=False):
    # Apple grid: 824x824 body at (100,100), ~185 radius, drop shadow below.
    s = 824 / 1024
    return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" width="1024" height="1024">
  <title>Clack</title>
  {defs("m")}
  <defs>
    <filter id="mshadow" x="-20%" y="-20%" width="140%" height="140%">
      <feGaussianBlur in="SourceAlpha" stdDeviation="14"/>
      <feOffset dy="12"/>
      <feComponentTransfer><feFuncA type="linear" slope="0.30"/></feComponentTransfer>
      <feMerge><feMergeNode/><feMergeNode in="SourceGraphic"/></feMerge>
    </filter>
  </defs>
  <g filter="url(#mshadow)">
    <rect x="100" y="100" width="824" height="824" rx="185" fill="url(#mbg)"/>
  </g>
  <rect x="100" y="100" width="824" height="824" rx="185" fill="url(#msheen)"/>
  <g transform="translate(100 100) scale({s:.6f})">
    {glyph(small)}
  </g>
</svg>
'''


if __name__ == "__main__":
    for name, txt in [("clack.svg", full()), ("clack-small.svg", full(True)),
                      ("clack-macos.svg", macos()), ("clack-macos-small.svg", macos(True))]:
        with open(os.path.join(HERE, name), "w") as fh:
            fh.write(txt)
        print("wrote", name)
