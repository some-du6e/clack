#!/usr/bin/env python3
"""plugswap.py INDEX OUT.gif SIZE [--fps N] [--seed N]

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

# the static wall, drawn once per state, as sprites per slot
def ease(u):
    return u * u * (3 - 2 * u)

# one pass: disjoint pairs, staggered
row_of = {}
for r, row in enumerate(rows):
    for i in row:
        row_of[i] = r
by_w = sorted(range(len(items)), key=lambda i: slots[i][2])
cand = []
used = set()
for k in range(len(by_w) - 1):
    a, b = by_w[k], by_w[k + 1]
    if a in used or b in used or abs(row_of[a] - row_of[b]) < 2:
        continue
    if abs(slots[a][2] - slots[b][2]) > 0.12 * max(slots[a][2], slots[b][2]):
        continue
    cand.append((a, b)); used.update((a, b))
rng.shuffle(cand)
PAIRS = min(len(cand), 16)
pairs = cand[:PAIRS]
STAGGER, DUR = 0.2, 0.85
PASS = PAIRS * STAGGER
TOTAL = 2 * PASS
events = []                                   # (start, a_slot, b_slot)
for p in range(2):
    for k, (a, b) in enumerate(pairs):
        events.append((p * PASS + k * STAGGER, a, b))

def at(t):
    """which item sits in each slot at time t (moves finished), plus the moves in flight"""
    holder = list(range(len(items)))
    flying = []
    for (t0, a, b) in events:
        tt = t % TOTAL
        if tt >= t0 + DUR:
            holder[a], holder[b] = holder[b], holder[a]
        elif tt >= t0:
            flying.append(((tt - t0) / DUR, a, b))
    # a wrapped event from the previous loop's end that's still flying at the start
    for (t0, a, b) in events:
        if t0 + DUR > TOTAL and (t % TOTAL) < (t0 + DUR - TOTAL):
            flying.append((((t % TOTAL) + TOTAL - t0) / DUR, a, b))
    return holder, flying

frames = []
n = int(round(TOTAL * FPS))
for fi in range(n):
    t = fi / FPS
    holder, flying = at(t)
    img = Image.new('RGBA', (W, H), BG + (255,))
    d = ImageDraw.Draw(img)
    busy = set()
    for u, a, b in flying:
        busy.add(a); busy.add(b)
    for s, (x, y, w, h) in enumerate(slots):
        if s not in busy:
            pill(img, d, x, y, w, h, items[holder[s]])
    for u, a, b in flying:
        # the pills now in a and b swap: holder[] still has them in their old slots
        for src, dst, sgn in ((a, b, 1), (b, a, -1)):
            item = items[holder[src]]
            (x0, y0, w0, h0), (x1, y1, w1, h1) = slots[src], slots[dst]
            e = ease(u)
            x = x0 + (x1 - x0) * e
            y = y0 + (y1 - y0) * e
            dist = math.hypot(x1 - x0, y1 - y0)
            y -= (0.18 * dist + 18) * 4 * u * (1 - u)      # the hop: up, then gravity back down
            w = w0 + (w1 - w0) * e
            lift = 4 * u * (1 - u)
            pill(img, d, x, y, w, h0, item, scale=1 + 0.12 * lift, angle=sgn * 10 * math.sin(math.pi * u), shadow=True)
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
