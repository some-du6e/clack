#!/usr/bin/env python3
"""plugfall.py INDEX OUT.gif W H [--seed N]: every Taut and Slick plugin as a pill
falling into a Clack-purple frame with real rigid-body physics (pymunk), piling up,
then the floor opens and they all fall out, so the GIF loops from empty to empty.

Taut pills are white with purple text, Slick pills are Clack's orange with dark text
(the icon's colours: #7e64ef -> #3f23bd, accent #ffb547)."""
import json, math, random, sys

import pymunk
from PIL import Image, ImageDraw, ImageFont

idx, out, W, H = sys.argv[1], sys.argv[2], int(sys.argv[3]), int(sys.argv[4])
seed = int(sys.argv[sys.argv.index('--seed') + 1]) if '--seed' in sys.argv else 7
rng = random.Random(seed)

plugins = json.load(open(idx))['plugins']
names = [(p['system'], p['name']) for p in plugins]
rng.shuffle(names)

FPS = 30
SCALE = min(W, H) / 720.0                 # pill size follows the short side
FONT = ImageFont.truetype('/usr/share/fonts/truetype/lato/Lato-Bold.ttf', max(11, int(17 * SCALE)))
PAD_X, PILL_H = int(14 * SCALE), int(34 * SCALE)
TOP, BOT = (0x7e, 0x64, 0xef), (0x3f, 0x23, 0xbd)
STYLE = {'taut': ((255, 255, 255, 255), (0x4a, 0x2f, 0xc9, 255)),
         'slick': ((0xff, 0xb5, 0x47, 255), (0x2a, 0x17, 0x00, 255))}

# the background: the icon's middle purple, flat (GIF packs flat runs far better than a gradient)
bg = Image.new('RGB', (W, H), (0x5a, 0x3f, 0xd6))

# one sprite per plugin, drawn 2x and scaled down for smooth edges
sprites = []
for sysname, name in names:
    fill, ink = STYLE[sysname]
    tw = int(FONT.getlength(name))
    w, h = tw + 2 * PAD_X, PILL_H
    big = Image.new('RGBA', (w * 2, h * 2), (0, 0, 0, 0))
    bd = ImageDraw.Draw(big)
    bd.rounded_rectangle([0, 0, w * 2 - 1, h * 2 - 1], radius=h, fill=fill)
    f2 = FONT.font_variant(size=FONT.size * 2)
    bd.text((w, h), name, font=f2, fill=ink, anchor='mm')
    sprites.append(big.resize((w, h), Image.LANCZOS))

space = pymunk.Space()
space.gravity = (0, 1500 * SCALE)
space.iterations = 30
static = space.static_body
walls = [pymunk.Segment(static, (-20, -4000), (-20, H + 20), 20),
         pymunk.Segment(static, (W + 20, -4000), (W + 20, H + 20), 20)]
floor = pymunk.Segment(static, (-40, H + 20), (W + 40, H + 20), 20)
for s in walls + [floor]:
    s.friction, s.elasticity = 0.9, 0.05
space.add(*walls, floor)

bodies = []
SPAWN_END, OPEN_AT, END = 3.2, 5.0, 6.6     # seconds: all dropped, floor opens, loop ends
spawn_times = [i * SPAWN_END / len(sprites) for i in range(len(sprites))]
frames, t, spawned, opened = [], 0.0, 0, False
STEPS = 8
dt = 1.0 / FPS / STEPS
while t < END:
    while spawned < len(sprites) and spawn_times[spawned] <= t:
        sp = sprites[spawned]
        w, h = sp.size
        body = pymunk.Body(1, pymunk.moment_for_box(1, (w, h)))
        body.position = (rng.uniform(w / 2 + 4, W - w / 2 - 4), -h - rng.uniform(0, 80 * SCALE))
        body.angle = rng.uniform(-0.5, 0.5)
        body.angular_velocity = rng.uniform(-3, 3)
        body.velocity = (rng.uniform(-60, 60) * SCALE, rng.uniform(0, 200) * SCALE)
        shape = pymunk.Poly.create_box(body, (w - h, h - 2), radius=h / 2 - 1)
        shape.friction, shape.elasticity = 0.7, 0.08
        space.add(body, shape)
        bodies.append((body, sp))
        spawned += 1
    if not opened and t >= OPEN_AT:
        space.remove(floor)                  # the floor opens: everything falls out
        opened = True
    for _ in range(STEPS):
        space.step(dt)
    frame = bg.copy()
    for body, sp in bodies:
        x, y = body.position
        if y - sp.size[0] > H + 40:
            continue
        rot = sp.rotate(-math.degrees(body.angle), resample=Image.BICUBIC, expand=True)
        frame.paste(rot, (int(x - rot.size[0] / 2), int(y - rot.size[1] / 2)), rot)
    frames.append(frame)
    t += 1.0 / FPS

# frames as PNGs; ffmpeg makes the GIF (one palette, only changed rectangles per frame)
import os, subprocess
fd = out + '.frames'
os.makedirs(fd, exist_ok=True)
for i, f in enumerate(frames):
    f.save(os.path.join(fd, 'f%04d.png' % i))
subprocess.run(['ffmpeg', '-y', '-loglevel', 'error', '-framerate', str(FPS), '-i', os.path.join(fd, 'f%04d.png'),
                '-vf', 'split[s0][s1];[s0]palettegen=max_colors=32:stats_mode=full[p];[s1][p]paletteuse=dither=none:diff_mode=rectangle',
                '-loop', '0', out], check=True)
for n in os.listdir(fd):
    os.remove(os.path.join(fd, n))
os.rmdir(fd)
print(out, W, H, len(frames), 'frames')
