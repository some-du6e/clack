#!/usr/bin/env node
// Makes site/img from the demo workflow's screenshots. Every picture on the
// site is `clack demo`, made-up data on a fake Slack server: the
// TestDemoReadmeShots and TestDemoSiteShots files in a demo run's
// demo-shots artifact. Needs sharp (an optional dependency of site/).
//
//   gh run download <run> --repo some-du6e/clack --name demo-shots --dir <dir>
//   node tools/images.mjs --run <run> --from <dir>
//
// Writes AVIF and WebP at each width in SHOTS, og.jpg, apple-touch-icon.png
// and img/images.json (sizes, files and where each came from), which
// build.mjs reads.

import { mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';

const site = join(dirname(fileURLToPath(import.meta.url)), '..');
const sharp = createRequire(join(site, 'package.json'))('sharp');

const { values: { run, from, only } } = parseArgs({ options: { run: { type: 'string' }, from: { type: 'string' }, only: { type: 'string' } } });
if (!run || !from) {
  console.error('usage: node tools/images.mjs --run <demo run id> --from <demo-shots dir>');
  process.exit(2);
}

// The window is 1440x900. slack.com has no dark mode, so neither does the
// site: every picture is the light theme. The theme tiles crop the window's
// top-left 720x450 (the rail, the sidebar and the start of the channel,
// where a theme shows); the dark band's picture crops it to the 677x463
// shape of the media slot it fills.
const TILE = { left: 0, top: 0, width: 720, height: 450 };
const BAND = { left: 0, top: 0, width: 1314, height: 900 };
const SHOTS = [
  { name: 'hero', light: 'readme/demo-main-light.png', widths: [640, 720, 960, 1440] },
  { name: 'band', light: 'demo-surface-light-emoji-picker.png', crop: BAND, widths: [680, 1020, 1314] },
  { name: 'channel', light: 'readme/demo-huddle-light.png', widths: [720, 1080, 1440] },
  { name: 'thread', light: 'readme/demo-thread-light.png', widths: [720, 1080, 1440] },
  { name: 'activity', light: 'readme/demo-activity-light.png', widths: [720, 1080, 1440] },
  { name: 'theme-jade', light: 'site/demo-theme-jade-light.png', crop: TILE, widths: [360, 540, 720] },
  { name: 'theme-lagoon', light: 'site/demo-theme-lagoon-light.png', crop: TILE, widths: [360, 540, 720] },
  { name: 'theme-clementine', light: 'site/demo-theme-clementine-light.png', crop: TILE, widths: [360, 540, 720] },
  { name: 'theme-mood-indigo-dark', light: 'site/demo-theme-mood-indigo-dark.png', crop: TILE, widths: [360, 540, 720] },
];
// AVIF keeps full-resolution colour (4:4:4) so coloured text stays sharp.
const AVIF = { quality: 55, effort: 6, chromaSubsampling: '4:4:4' };
const WEBP = { quality: 80, effort: 6, smartSubsample: true };

const img = join(site, 'img');
mkdirSync(img, { recursive: true });
// --only <name> redoes one picture and keeps the rest
const todo = only ? SHOTS.filter((s) => s.name === only) : SHOTS;
if (!todo.length) { console.error(`images: no picture named ${only}`); process.exit(2); }
for (const f of readdirSync(img)) {
  if (/\.(avif|webp|png|jpg)$/.test(f) && (!only || f.startsWith(`${only}-`))) rmSync(join(img, f));
}

const manifest = only ? JSON.parse(readFileSync(join(img, 'images.json'), 'utf8')) : {};
let total = 0;
for (const s of todo) {
  const files = {};
  let size;
  for (const variant of ['light', 'dark']) {
    if (!s[variant]) continue;
    const file = s.dark ? `${s.name}-${variant}` : s.name;
    files[variant] = file;
    const src = join(from, s[variant]);
    const base = () => (s.crop ? sharp(src).extract(s.crop) : sharp(src));
    const meta = await base().toBuffer({ resolveWithObject: true });
    size = { width: meta.info.width, height: meta.info.height };
    for (const w of s.widths) {
      const r = () => base().resize({ width: w, kernel: 'lanczos3' });
      const a = await r().avif(AVIF).toFile(join(img, `${file}-${w}.avif`));
      const b = await r().webp(WEBP).toFile(join(img, `${file}-${w}.webp`));
      total += a.size + b.size;
      console.log(`${file}-${w}: avif ${(a.size / 1024).toFixed(1)} KB, webp ${(b.size / 1024).toFixed(1)} KB`);
    }
  }
  manifest[s.name] = {
    ...size,
    widths: s.widths,
    files,
    source: { artifact: 'demo-shots', run: Number(run), light: s.light, ...(s.dark ? { dark: s.dark } : {}), ...(s.crop ? { crop: s.crop } : {}) },
  };
}

if (!only) {
// og:image, 1200x630 from the top of the light hero
await sharp(join(from, SHOTS[0].light)).resize({ width: 1200 }).extract({ left: 0, top: 0, width: 1200, height: 630 })
  .jpeg({ quality: 86, chromaSubsampling: '4:4:4', mozjpeg: true }).toFile(join(img, 'og.jpg'));
// home-screen icon, from the app icon
await sharp(readFileSync(join(site, '..', 'assets', 'icon.svg'))).resize(180, 180).png().toFile(join(img, 'apple-touch-icon.png'));
}

writeFileSync(join(img, 'images.json'), JSON.stringify(manifest, null, 2) + '\n');
console.log(`${Object.keys(manifest).length} pictures, ${(total / 1024).toFixed(0)} KB of AVIF and WebP in all`);
