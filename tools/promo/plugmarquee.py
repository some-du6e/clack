#!/usr/bin/env python3
"""plugmarquee.py INDEX OUT.gif W H [--rows N] [--fps N] [--seconds T] [--seed N]

Every Taut and Slick plugin as a pill, in rows that scroll sideways (alternate rows the
other way), like a logo wall: a wide banner for the top of a message. Each row's pills
repeat edge to edge, and each row moves exactly one copy of itself in T seconds, so the
GIF loops seamlessly. Taut pills are white with purple text, Slick pills Clack's orange
with dark text, on the icon's purple."""
import json, math, os, random, subprocess, sys

from PIL import Image, ImageDraw, ImageFont

args = sys.argv[1:]
idx, out, W, H = args[0], args[1], int(args[2]), int(args[3])
def opt(name, default, cast=float):
    return cast(args[args.index(name) + 1]) if name in args else default
ROWS = opt('--rows', 5, int)
FPS = opt('--fps', 25, int)
T = opt('--seconds', 10.0)
seed = opt('--seed', 4, int)
rng = random.Random(seed)
BG = (0x5a, 0x3f, 0xd6)
STYLE = {'taut': ((255, 255, 255), (0x4a, 0x2f, 0xc9)),
         'slick': ((0xff, 0xb5, 0x47), (0x2a, 0x17, 0x00))}
LATO = '/usr/share/fonts/truetype/lato/Lato-Bold.ttf'
SS = 3

plugins = json.load(open(idx))['plugins']
items = [(p['system'], p['name']) for p in plugins]
rng.shuffle(items)

MARGIN_Y, GAP_X = 8, 10
pitch = (H - 2 * MARGIN_Y) / ROWS
ph = int(pitch * 0.74)
fs = max(9, int(ph * 0.5))
font = ImageFont.truetype(LATO, fs * SS)
pad = int(fs * 0.85)

def sprite(item):
    sysname, name = item
    fill, ink = STYLE[sysname]
    w = int(font.getlength(name) / SS) + 2 * pad
    big = Image.new('RGBA', (w * SS, ph * SS), (0, 0, 0, 0))
    d = ImageDraw.Draw(big)
    d.rounded_rectangle([0, 0, w * SS - 1, ph * SS - 1], radius=ph * SS // 2, fill=fill + (255,))
    d.text((w * SS / 2, ph * SS / 2), name, font=font, fill=ink + (255,), anchor='mm')
    return big.resize((w, ph), Image.LANCZOS)

sprites = [sprite(it) for it in items]
# deal the pills into rows of about equal width (longest first, onto the shortest row)
order = sorted(range(len(items)), key=lambda i: -sprites[i].size[0])
rows = [[] for _ in range(ROWS)]
width = [0] * ROWS
for i in order:
    r = min(range(ROWS), key=lambda k: width[k])
    rows[r].append(i)
    width[r] += sprites[i].size[0] + GAP_X
for r in range(ROWS):
    rng.shuffle(rows[r])

# whole-pixel scrolling: every row moves STEP px a frame and is padded (its gaps widened) to
# exactly STEP * n px, so a loop is one copy and no frame blends sub-pixel positions
n = int(round(T * FPS))
natural = max(sum(sprites[i].size[0] + GAP_X for i in rows[r]) for r in range(ROWS))
STEP = max(1, int(math.ceil(natural / n)))
PERIOD = STEP * n
strips = []
for r in range(ROWS):
    period = PERIOD
    extra = PERIOD - sum(sprites[i].size[0] + GAP_X for i in rows[r])
    gaps = [GAP_X + extra // len(rows[r]) + (1 if k < extra % len(rows[r]) else 0) for k in range(len(rows[r]))]
    copies = int(math.ceil((W + period) / period)) + 1
    strip = Image.new('RGBA', (period * copies, ph), (0, 0, 0, 0))
    x = 0
    for c in range(copies):
        for k, i in enumerate(rows[r]):
            strip.paste(sprites[i], (x, 0), sprites[i])
            x += sprites[i].size[0] + gaps[k]
    strips.append((strip, period))

frames = []
for fi in range(n):
    img = Image.new('RGBA', (W, H), BG + (255,))
    for r, (strip, period) in enumerate(strips):
        off = fi * STEP
        x0 = -off if r % 2 == 0 else off - period   # even rows go left, odd rows right
        y = int(round(MARGIN_Y + r * pitch + (pitch - ph) / 2))
        img.paste(strip, (int(x0), y), strip)
    frames.append(img.convert('RGB'))

fd = out + '.frames'
os.makedirs(fd, exist_ok=True)
for i, f in enumerate(frames):
    f.save(os.path.join(fd, 'f%04d.png' % i))
subprocess.run(['ffmpeg', '-y', '-loglevel', 'error', '-framerate', str(FPS), '-i', os.path.join(fd, 'f%04d.png'),
                '-vf', 'split[s0][s1];[s0]palettegen=max_colors=48:stats_mode=full[p];[s1][p]paletteuse=dither=none',
                '-loop', '0', out], check=True)
for nm in os.listdir(fd):
    os.remove(os.path.join(fd, nm))
os.rmdir(fd)
print(out, W, H, len(frames), 'frames', FPS, 'fps', 'rows', ROWS, 'period', PERIOD, 'step', STEP, 'px/frame', 'font', fs)
