#!/usr/bin/env python3
"""plugdrop.py (from plugswap.py) INDEX OUT.gif SIZE [--fps N] [--seed N]

Every Taut and Slick plugin as a pill, packed into a justified wall that fills the
square edge to edge, and pairs of pills hop to each other's places along gravity arcs.
Each pass swaps a set of disjoint pairs, and the second pass swaps the same pairs back,
so the GIF loops exactly with no dead space. Taut pills are white with purple text,
Slick pills Clack's orange with dark text, on the icon's purple."""
import json, math, os, random, subprocess, sys

from PIL import Image, ImageDraw, ImageFilter, ImageFont

args = sys.argv[1:]
idx, out, S = args[0], args[1], int(args[2])
def opt(name, default, cast=float):
    return cast(args[args.index(name) + 1]) if name in args else default
FPS = opt('--fps', 30, int)
seed = opt('--seed', 5, int)
rng = random.Random(seed)
W = H = S
SS = 2                                       # supersampling for clean edges
BG = (0x5a, 0x3f, 0xd6)
STYLE = {'taut': ((255, 255, 255), (0x4a, 0x2f, 0xc9)),
         'slick': ((0xff, 0xb5, 0x47), (0x2a, 0x17, 0x00))}
LATO = '/usr/share/fonts/truetype/lato/Lato-Bold.ttf'

plugins = json.load(open(idx))['plugins']
items = [(p['system'], p['name']) for p in plugins]
rng.shuffle(items)
MARGIN, GAP = 4, 4

def layout(fs):
    font = ImageFont.truetype(LATO, fs)
    pad, ph = int(fs * 0.75), int(fs * 1.9)
    widths = [int(font.getlength(n)) + 2 * pad for _, n in items]
    rows, cur, cw = [], [], 0
    for i, w in enumerate(widths):
        need = w + (GAP if cur else 0)
        if cur and cw + need > W - 2 * MARGIN:
            rows.append(cur); cur, cw = [], 0; need = w
        cur.append(i); cw += need
    if cur:
        rows.append(cur)
    return font, pad, ph, widths, rows

# the biggest font whose wall fits the square
fs = 30
while True:
    font, pad, ph, widths, rows = layout(fs)
    if len(rows) * ph + (len(rows) - 1) * GAP + 2 * MARGIN <= H:
        break
    fs -= 1
# justify: rows span the width (widen pills), rows spread over the height
rgap = (H - 2 * MARGIN - len(rows) * ph) / max(1, len(rows) - 1)
slots = [None] * len(items)                  # slot i holds item i at the start: (x, y, w, h)
for r, row in enumerate(rows):
    natural = sum(widths[i] for i in row) + GAP * (len(row) - 1)
    extra = (W - 2 * MARGIN - natural) / len(row)
    x = MARGIN
    y = MARGIN + r * (ph + rgap)
    for i in row:
        w = widths[i] + extra
        slots[i] = (x, y, w, ph)
        x += w + GAP

def pill(img, d, x, y, w, h, item, scale=1.0, angle=0.0, shadow=False):
    sysname, name = item
    fill, ink = STYLE[sysname]
    w2, h2 = w * scale, h * scale
    tile = Image.new('RGBA', (int(w2 * SS) + 4, int(h2 * SS) + 4), (0, 0, 0, 0))
    td = ImageDraw.Draw(tile)
    td.rounded_rectangle([1, 1, int(w2 * SS) + 1, int(h2 * SS) + 1], radius=int(h2 * SS / 2), fill=fill + (255,))
    fsz = font.size * scale * SS
    tw = font.font_variant(size=int(fsz)).getlength(name)
    room = (w2 - 2 * pad * 0.8) * SS
    if tw > room:
        fsz *= room / tw
    f2 = font.font_variant(size=max(6, int(fsz)))
    td.text((tile.size[0] / 2, tile.size[1] / 2), name, font=f2, fill=ink + (255,), anchor='mm')
    tile = tile.resize((max(1, tile.size[0] // SS), max(1, tile.size[1] // SS)), Image.LANCZOS)
    if angle:
        tile = tile.rotate(angle, resample=Image.BICUBIC, expand=True)
    cx, cy = x + w / 2, y + h / 2
    if shadow:
        sh = Image.new('RGBA', tile.size, (0, 0, 0, 0))
        sh.paste((20, 8, 60, 110), (0, 0), tile)
        sh = sh.filter(ImageFilter.GaussianBlur(3))
        img.paste(sh, (int(cx - tile.size[0] / 2 + 2), int(cy - tile.size[1] / 2 + 4)), sh)
    img.paste(tile, (int(round(cx - tile.size[0] / 2)), int(round(cy - tile.size[1] / 2))), tile)


# the loop: the full wall rests, its pills fall out the bottom (bottom row first), and the
# same pills drop in from above and land in their slots with a bounce (bottom row first),
# so the last frame is the first frame again
G = 2600.0 * (H / 520.0)                     # gravity, px/s^2
HOLD = 1.2                                   # the wall at rest
OUT0, IN0 = HOLD, HOLD + 0.55                # when the fall-out and the drop-in start
ROWSTEP_OUT, ROWSTEP_IN = 0.045, 0.085
nrows = len(rows)
row_of = {}
for r, row in enumerate(rows):
    for i in row:
        row_of[i] = r
jit_out = {i: rng.uniform(0, 0.08) for i in range(len(items))}
jit_in = {i: rng.uniform(0, 0.10) for i in range(len(items))}
spin = {i: rng.uniform(-1, 1) for i in range(len(items))}
START_Y = -60.0                              # where a dropping pill starts (top above the frame)

def land_time(i):
    x, y, w, h = slots[i]
    t0 = IN0 + (nrows - 1 - row_of[i]) * ROWSTEP_IN + jit_in[i]
    return t0, math.sqrt(2 * (y - START_Y) / G)

ends = [sum(land_time(i)) for i in range(len(items))]
TOTAL = max(ends) + 0.55                     # the last bounce settles, then the loop restarts

def bounce_y(t):
    """offset above the slot after landing at speed v: two damped bounces"""
    return 0.0

def pose(i, t):
    """(dx, dy, angle) of pill i at time t, relative to its slot, or None if off-screen"""
    x, y, w, h = slots[i]
    # falling out
    t_out = OUT0 + (nrows - 1 - row_of[i]) * ROWSTEP_OUT + jit_out[i]
    t_in0, fall = land_time(i)
    if t < t_out:
        return (0.0, 0.0, 0.0)
    if t < t_in0:
        u = t - t_out
        dy = 0.5 * G * u * u
        if y + dy > H + 40:
            return None
        return (0.0, dy, spin[i] * 40 * u)
    u = t - t_in0
    if u < fall:
        dy = (START_Y + 0.5 * G * u * u) - y      # negative: still above the slot
        return (0.0, dy, spin[i] * 14 * (1 - u / fall))
    # landed: two small bounces
    v = G * fall
    a = u - fall
    b1 = 0.22 * v / G * 2                     # flight time of the first bounce
    if a < b1:
        vb = 0.22 * v
        dy = -(vb * a - 0.5 * G * a * a)
        return (0.0, dy, 0.0)
    a -= b1
    b2 = 0.07 * v / G * 2
    if a < b2:
        vb = 0.07 * v
        dy = -(vb * a - 0.5 * G * a * a)
        return (0.0, dy, 0.0)
    return (0.0, 0.0, 0.0)

frames = []
n = int(round(TOTAL * FPS))
for fi in range(n):
    t = fi / FPS
    img = Image.new('RGBA', (W, H), BG + (255,))
    d = ImageDraw.Draw(img)
    for i, (x, y, w, h) in enumerate(slots):
        p = pose(i, t)
        if p is None:
            continue
        dx, dy, ang = p
        if y + dy + h < -2:
            continue
        pill(img, d, x + dx, y + dy, w, h, items[i], angle=ang)
    frames.append(img.convert('RGB'))

fd = out + '.frames'
os.makedirs(fd, exist_ok=True)
for i, f in enumerate(frames):
    f.save(os.path.join(fd, 'f%04d.png' % i))
subprocess.run(['ffmpeg', '-y', '-loglevel', 'error', '-framerate', str(FPS), '-i', os.path.join(fd, 'f%04d.png'),
                '-vf', 'split[s0][s1];[s0]palettegen=max_colors=64:stats_mode=full[p];[s1][p]paletteuse=dither=none:diff_mode=rectangle',
                '-loop', '0', out], check=True)
for nm in os.listdir(fd):
    os.remove(os.path.join(fd, nm))
os.rmdir(fd)
print(out, S, len(frames), 'frames', FPS, 'fps', 'font', font.size, 'rows', len(rows))
