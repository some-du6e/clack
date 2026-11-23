#!/usr/bin/env python3
"""plugloop.py (from plugrain.py): rain in, hold the full wall a second, all fall out, loop.

plugrain.py (from plugswap.py): every pill falls from an empty frame, tumbling, into a dense messy wall that fills the square; played once.

plugswap.py INDEX OUT.gif SIZE [--fps N] [--seed N]

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


# the final wall is messy but has no dead space: a small tilt and jitter per pill, and each
# pill a little wider than its slot so neighbours overlap instead of leaving gaps
final = []
for i, (x, y, w, h) in enumerate(slots):
    final.append((x - 3, y + rng.uniform(-2.5, 2.5), w + 6, h + 3, rng.uniform(-5.0, 5.0)))
row_of = {}
for r, row in enumerate(rows):
    for i in row:
        row_of[i] = r
nrows = len(rows)
G = 2400.0 * (H / 520.0)
# landing times: bottom rows land first, but in a scrambled order within and across nearby rows
land = {}
for i in range(len(items)):
    r_from_bottom = nrows - 1 - row_of[i]
    land[i] = 0.55 + r_from_bottom * 0.075 + rng.uniform(0, 0.32)
start_y = {i: -rng.uniform(40, 260) for i in range(len(items))}
spin0 = {i: rng.uniform(-220, 220) for i in range(len(items))}
drift = {i: rng.uniform(-60, 60) for i in range(len(items))}

def state(i, t):
    """(x, y, angle) of pill i at time t, or None before it appears"""
    x1, y1, w, h, a1 = final[i]
    fall = math.sqrt(2 * max(1.0, y1 - start_y[i]) / G)
    t0 = land[i] - fall
    if t < t0:
        return None
    if t < land[i]:
        u = (t - t0) / fall
        y = start_y[i] + 0.5 * G * (t - t0) ** 2
        x = x1 + drift[i] * (1 - u) ** 2
        a = a1 + spin0[i] * (1 - u) ** 1.5
        return (x, y, a)
    # landed: one small bounce, settling into its tilt
    v = G * fall
    s = t - land[i]
    b1 = 2 * 0.16 * v / G
    if s < b1:
        vb = 0.16 * v
        return (x1, y1 - (vb * s - 0.5 * G * s * s), a1 * (1 + 0.6 * math.sin(math.pi * s / b1)))
    return (x1, y1, a1)

FULL = max(land.values()) + 0.45            # the last bounce has settled: the wall is full
HOLD = 1.0                                   # Karim: stay like that for a second
OUT0 = FULL + HOLD                           # then all of them fall out
out_delay = {}
for i in range(len(items)):
    r_from_bottom = nrows - 1 - row_of[i]
    out_delay[i] = r_from_bottom * 0.018 + rng.uniform(0, 0.09)   # bottom rows let go first, a ripple up
out_spin = {i: rng.uniform(-260, 260) for i in range(len(items))}
out_dx = {i: rng.uniform(-70, 70) for i in range(len(items))}
def gone_at(i):
    x1, y1, w, h, a1 = final[i]
    return OUT0 + out_delay[i] + math.sqrt(2 * (H + 40 - y1 + h) / G)
END = max(gone_at(i) for i in range(len(items))) + 0.05   # empty again: the loop's seam (frame 0 is empty too)

def pose(i, t):
    if t < OUT0 + out_delay[i]:
        return state(i, t)
    x1, y1, w, h, a1 = final[i]
    u = t - OUT0 - out_delay[i]
    y = y1 + 0.5 * G * u * u
    if y - h > H + 2:
        return None
    return (x1 + out_dx[i] * u, y, a1 + out_spin[i] * u)

frames = []
n = int(round(END * FPS))
for fi in range(n):
    t = fi / FPS
    img = Image.new('RGBA', (W, H), BG + (255,))
    d = ImageDraw.Draw(img)
    landed, moving = [], []
    for i in range(len(items)):
        s_ = pose(i, t)
        if s_ is None:
            continue
        (landed if land[i] <= t < OUT0 + out_delay[i] else moving).append((i, s_))
    for i, (x, y, a) in landed + moving:
        _, _, w, h, _ = final[i]
        if y + h < -2:
            continue
        pill(img, d, x, y, w, h, items[i], angle=a)
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
