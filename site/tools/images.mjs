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

const { values: { run, from } } = parseArgs({ options: { run: { type: 'string' }, from: { type: 'string' } } });
if (!run || !from) {
  console.error('usage: node tools/images.mjs --run <demo run id> --from <demo-shots dir>');
  process.exit(2);
}

// The window is 1440x900. The theme tiles crop its top-left 720x450: the
// rail, the sidebar and the start of the channel, where a theme shows.
const TILE = { left: 0, top: 0, width: 720, height: 450 };
const SHOTS = [
  { name: 'hero', light: 'readme/demo-main-light.png', dark: 'readme/demo-main-dark.png', widths: [720, 1080, 1440] },
  { name: 'channel', light: 'readme/demo-huddle-light.png', dark: 'readme/demo-huddle-dark.png', widths: [640, 960, 1200] },
  { name: 'thread', light: 'readme/demo-thread-light.png', dark: 'readme/demo-thread-dark.png', widths: [640, 960, 1200] },
  { name: 'activity', light: 'readme/demo-activity-light.png', dark: 'readme/demo-activity-dark.png', widths: [640, 960, 1200] },
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
for (const f of readdirSync(img)) {
  if (/\.(avif|webp|png|jpg)$/.test(f)) rmSync(join(img, f));
}

const manifest = {};
let total = 0;
for (const s of SHOTS) {
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

// og:image, 1200x630 from the top of the light hero
await sharp(join(from, SHOTS[0].light)).resize({ width: 1200 }).extract({ left: 0, top: 0, width: 1200, height: 630 })
  .jpeg({ quality: 86, chromaSubsampling: '4:4:4', mozjpeg: true }).toFile(join(img, 'og.jpg'));
// home-screen icon, from the app icon
await sharp(readFileSync(join(site, '..', 'assets', 'icon.svg'))).resize(180, 180).png().toFile(join(img, 'apple-touch-icon.png'));

writeFileSync(join(img, 'images.json'), JSON.stringify(manifest, null, 2) + '\n');
console.log(`${Object.keys(manifest).length} pictures, ${(total / 1024).toFixed(0)} KB of AVIF and WebP in all`);
