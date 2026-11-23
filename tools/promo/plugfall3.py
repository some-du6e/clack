#!/usr/bin/env python3
"""plugfall3.py INDEX OUT.gif SIZE [--fps N] [--seed N]

Every Taut and Slick plugin as a pill, falling from the first frame into a messy pile
(rigid-body physics, pymunk), resting, then the floor opens and the pile falls out.
One wave is simulated; the GIF draws each wave's tail (falling out at the bottom) over
the next wave's head (falling in at the top), so pills are falling from frame 0, the
frame never goes empty, and the last frame runs straight into the first."""
import json, math, os, random, subprocess, sys

import pymunk
from PIL import Image, ImageDraw, ImageFont

args = sys.argv[1:]
idx, out, S = args[0], args[1], int(args[2])
def opt(name, default, cast=float):
    return cast(args[args.index(name) + 1]) if name in args else default
FPS = opt('--fps', 30, int)
seed = opt('--seed', 3, int)
rng = random.Random(seed)
W = H = S

plugins = json.load(open(idx))['plugins']
names = [(p['system'], p['name']) for p in plugins]
rng.shuffle(names)

SCALE = S / 720.0 * 1.38
FONT = ImageFont.truetype('/usr/share/fonts/truetype/lato/Lato-Bold.ttf', max(10, int(15 * SCALE)))
PAD_X, PILL_H = int(12 * SCALE), int(30 * SCALE)
BG = (0x5a, 0x3f, 0xd6)
STYLE = {'taut': ((255, 255, 255, 255), (0x4a, 0x2f, 0xc9, 255)),
         'slick': ((0xff, 0xb5, 0x47, 255), (0x2a, 0x17, 0x00, 255))}

sprites = []
for sysname, name in names:
    fill, ink = STYLE[sysname]
    w, h = int(FONT.getlength(name)) + 2 * PAD_X, PILL_H
    big = Image.new('RGBA', (w * 3, h * 3), (0, 0, 0, 0))
    bd = ImageDraw.Draw(big)
    bd.rounded_rectangle([0, 0, w * 3 - 1, h * 3 - 1], radius=h * 3 // 2, fill=fill)
    bd.text((w * 3 // 2, h * 3 // 2), name, font=FONT.font_variant(size=FONT.size * 3), fill=ink, anchor='mm')
    sprites.append(big.resize((w, h), Image.LANCZOS))

space = pymunk.Space()
space.gravity = (0, 1700 * SCALE)
space.iterations = 35
st = space.static_body
walls = [pymunk.Segment(st, (-14, -4000), (-14, H + 20), 14), pymunk.Segment(st, (W + 14, -4000), (W + 14, H + 20), 14)]
floor = pymunk.Segment(st, (-60, H + 14), (W + 60, H + 14), 14)
for sgm in walls + [floor]:
    sgm.friction, sgm.elasticity = 0.9, 0.05
space.add(*walls, floor)

SPAWN_END, OPEN_AT, OUT_BY = 1.5, 3.0, 4.2     # all in, the floor opens, all gone
T = OPEN_AT + 0.24                             # the loop: the next wave starts a beat after the floor opens
spawn_at = sorted(rng.uniform(0, SPAWN_END) for _ in sprites)
bodies = []
states = []                                    # per frame: [(i, x, y, angle)]
STEPS = 10
dt = 1.0 / FPS / STEPS
n_sim = int(round(OUT_BY * FPS)) + 1
k = 0
opened = False
for fi in range(n_sim):
    t = fi / FPS
    while k < len(sprites) and spawn_at[k] <= t:
        sp = sprites[k]
        w, h = sp.size
        b = pymunk.Body(1, pymunk.moment_for_box(1, (w, h)))
        b.position = (rng.uniform(w / 2, W - w / 2), -h / 2 - rng.uniform(0, 140 * SCALE))
        b.angle = rng.uniform(-math.pi, math.pi)
        b.angular_velocity = rng.uniform(-5, 5)
        b.velocity = (rng.uniform(-60, 60) * SCALE, 0)   # from rest: the new wave falls behind the old pile, never through it
        shp = pymunk.Poly.create_box(b, (w - h + 1, h - 1), radius=h / 2 - 0.5)
        shp.friction, shp.elasticity = 0.35, 0.05     # they slide into the gaps: a tighter pile
        space.add(b, shp)
        bodies.append((k, b))
        k += 1
    if not opened and t >= OPEN_AT:
        space.remove(floor)
        for sgm in walls:
            sgm.friction = 0.0                 # the pile falls out as one block, nothing held back by the walls
        opened = True
    states.append([(i, b.position.x, b.position.y, b.angle) for i, b in bodies])
    for _ in range(STEPS):
        space.step(dt)

def draw(img, state):
    for i, x, y, a in state:
        sp = sprites[i]
        if y - sp.size[0] > H + 4 or y + sp.size[0] < -4:
            continue
        r = sp.rotate(-math.degrees(a), resample=Image.BICUBIC, expand=True)
        img.paste(r, (int(round(x - r.size[0] / 2)), int(round(y - r.size[1] / 2))), r)

n = int(round(T * FPS))
nT = n                                          # frame offset of one loop
frames = []
for fi in range(n):
    img = Image.new('RGB', (W, H), BG)
    if fi + nT < len(states):
        draw(img, states[fi + nT])              # the last wave, falling out at the bottom
    draw(img, states[fi])                       # this wave, falling in from the top
    frames.append(img)

fd = out + '.frames'
os.makedirs(fd, exist_ok=True)
for i, f in enumerate(frames):
    f.save(os.path.join(fd, 'f%04d.png' % i))
subprocess.run(['ffmpeg', '-y', '-loglevel', 'error', '-framerate', str(FPS), '-i', os.path.join(fd, 'f%04d.png'),
                '-vf', 'split[s0][s1];[s0]palettegen=max_colors=48:stats_mode=full[p];[s1][p]paletteuse=dither=none:diff_mode=rectangle',
                '-loop', '0', out], check=True)
for nm in os.listdir(fd):
    os.remove(os.path.join(fd, nm))
os.rmdir(fd)
print(out, S, len(frames), 'frames', FPS, 'fps', 'loop', T, 's')
