#!/usr/bin/env node
// Static checks on the built site (site/dist, or the directory given):
//   - index.html, which carries all of the page's CSS and JS, is at most
//     30 KB gzipped
//   - nothing loads from another host: no external src, srcset, stylesheet,
//     script, font or CSS url(); only links (<a href>), the canonical link
//     and Open Graph tags may name other hosts
//   - every local file the pages reference exists
//   - every <img> has alt, width and height, and every image but the one
//     fetchpriority="high" hero is lazy
//   - ids are unique, and no page hard-codes a hosting host (github.io,
//     workers.dev, pages.dev)
//   - CNAME names the same host as site.json's url
//   - the one web font is the local subset, preloaded, and covers every
//     character the headings use (fonts/clack-heading-bold.json)
//   - with --production: indexable (no noindex, robots.txt allows, a
//     sitemap and a canonical link); with --preview: not indexable
//   - with --pages: CNAME is in the build
// Node built-ins only.

import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { gzipSync } from 'node:zlib';

const site = join(dirname(fileURLToPath(import.meta.url)), '..');
const argv = process.argv.slice(2);
const mode = { production: argv.includes('--production'), preview: argv.includes('--preview'), pages: argv.includes('--pages') };
const dist = resolve(argv.find((a) => !a.startsWith('--')) ?? join(site, 'dist'));
const HTML_GZIP_BUDGET = 30 * 1024;
const errors = [];
const err = (m) => errors.push(m);

const files = walk(dist);
const pages = files.filter((f) => f.endsWith('.html'));
if (!pages.includes('index.html')) err('no index.html');

for (const page of pages) {
  const html = readFileSync(join(dist, page), 'utf8');
  const local = (url) => !/^[a-z][a-z0-9+.-]*:/i.test(url) && !url.startsWith('//');

  // resources the browser fetches by itself
  const fetched = [];
  for (const m of html.matchAll(/<(img|source|script|iframe|video|audio|embed)\b[^>]*>/gi)) {
    for (const a of m[0].matchAll(/\s(src|srcset)="([^"]*)"/gi)) {
      if (a[1].toLowerCase() === 'srcset') fetched.push(...a[2].split(',').map((c) => c.trim().split(/\s+/)[0]));
      else fetched.push(a[2]);
    }
  }
  for (const m of html.matchAll(/<link\b[^>]*>/gi)) {
    const rel = (m[0].match(/\srel="([^"]*)"/i) || [])[1] || '';
    const href = (m[0].match(/\shref="([^"]*)"/i) || [])[1];
    if (href === undefined) continue;
    if (/\bcanonical\b|\balternate\b/i.test(rel)) continue;
    fetched.push(href);
  }
  for (const m of html.matchAll(/url\(\s*['"]?([^'")]+)/gi)) {
    if (!m[1].startsWith('#') && !m[1].startsWith('data:')) fetched.push(m[1]);
  }
  if (/@import\b/i.test(html)) err(`${page}: CSS @import`);
  if (/<script\b[^>]*\ssrc=/i.test(html)) err(`${page}: an external <script src>`);
  if (/<link\b[^>]*rel="?stylesheet/i.test(html)) err(`${page}: an external stylesheet`);
  for (const m of html.matchAll(/@font-face\s*\{([^}]*)\}/gi)) {
    for (const u of m[1].matchAll(/url\(\s*['"]?([^'")]+)/gi)) if (!/^\/?fonts\/[\w.-]+\.woff2$/.test(u[1])) err(`${page}: @font-face loads ${u[1]}, not a woff2 in fonts/`);
  }
  for (const u of fetched) {
    if (!local(u)) { err(`${page}: loads ${u} from another host`); continue; }
    const p = u.split(/[?#]/)[0];
    const f = p.startsWith('/') ? join(dist, p) : join(dist, dirname(page), p);
    if (p && !existsSync(f)) err(`${page}: ${u} is missing from the build`);
  }
  // links to files of the site itself
  for (const m of html.matchAll(/<a\b[^>]*\shref="([^"]*)"/gi)) {
    const u = m[1];
    if (!local(u) || u.startsWith('#') || u === './' || u === '/') {
      if (u.startsWith('#') && u.length > 1 && !html.includes(`id="${u.slice(1)}"`)) err(`${page}: link to ${u}, which isn't on the page`);
      continue;
    }
    const f = join(dist, dirname(page), u.split(/[?#]/)[0]);
    if (!existsSync(f)) err(`${page}: link to ${u}, which is missing`);
  }

  let eager = 0;
  for (const m of html.matchAll(/<img\b[^>]*>/gi)) {
    const tag = m[0];
    for (const a of ['alt', 'width', 'height']) if (!new RegExp(`\\s${a}="`).test(tag)) err(`${page}: <img> without ${a}: ${tag.slice(0, 120)}`);
    if (/fetchpriority="high"/.test(tag)) eager++;
    else if (!/loading="lazy"/.test(tag)) err(`${page}: <img> neither lazy nor the hero: ${tag.slice(0, 120)}`);
  }
  if (eager > 1) err(`${page}: ${eager} fetchpriority="high" images, at most one`);

  const ids = [...html.matchAll(/\sid="([^"]+)"/g)].map((m) => m[1]);
  for (const id of new Set(ids.filter((id, i) => ids.indexOf(id) !== i))) err(`${page}: duplicate id ${id}`);
  for (const host of ['github.io', 'workers.dev', 'pages.dev']) if (html.includes(host)) err(`${page}: hard-codes ${host}`);
}

// the heading font covers every character the headings show
const cover = new Set(JSON.parse(readFileSync(join(site, 'fonts', 'clack-heading-bold.json'), 'utf8')).codepoints);
const indexHtml = readFileSync(join(dist, 'index.html'), 'utf8');
if (!/<link rel="preload" href="fonts\/clack-heading-bold\.woff2" as="font" type="font\/woff2" crossorigin>/.test(indexHtml)) err('index.html does not preload fonts/clack-heading-bold.woff2');
const headingText = [];
for (const m of indexHtml.matchAll(/<(h1|h2|h3)\b[^>]*>([\s\S]*?)<\/\1>/g)) headingText.push(m[2]);
for (const m of indexHtml.matchAll(/<(\w+)\b[^>]*class="[^"]*\b(brand|strip|pills|gstat|stat|love-left|card-art|deep-title)\b[^"]*"[^>]*>([\s\S]*?)<\/\1>/g)) headingText.push(m[3]);
const missing = new Set();
for (const t of headingText) {
  const text = t.replace(/<svg[\s\S]*?<\/svg>/g, '').replace(/<[^>]+>/g, '').replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&#39;/g, "'");
  for (const ch of text) if (!/\s/.test(ch) && !cover.has(ch.codePointAt(0))) missing.add(ch);
}
if (missing.size) err(`the heading font lacks ${[...missing].map((c) => `"${c}" U+${c.codePointAt(0).toString(16).toUpperCase().padStart(4, '0')}`).join(', ')}: add them in tools/font.py and rerun it`);

// indexable in production, never in a preview
const robots = existsSync(join(dist, 'robots.txt')) ? readFileSync(join(dist, 'robots.txt'), 'utf8') : '';
const noindex = /<meta name="robots" content="[^"]*noindex/.test(indexHtml);
if (mode.production) {
  if (noindex) err('production build: index.html is noindex');
  if (/^Disallow:\s*\/\s*$/m.test(robots)) err('production build: robots.txt disallows everything');
  if (!existsSync(join(dist, 'sitemap.xml'))) err('production build: no sitemap.xml');
  if (!/<link rel="canonical" href="https:\/\//.test(indexHtml)) err('production build: no canonical link');
}
if (mode.preview) {
  if (!noindex) err('preview build: index.html is indexable');
  if (!/^Disallow:\s*\/\s*$/m.test(robots)) err('preview build: robots.txt lets crawlers in');
  if (existsSync(join(dist, 'sitemap.xml'))) err('preview build: has a sitemap.xml');
}
if (mode.pages && !existsSync(join(dist, 'CNAME'))) err('pages build: no CNAME');
if (mode.pages && readFileSync(join(dist, 'CNAME'), 'utf8').trim() !== readFileSync(join(site, 'CNAME'), 'utf8').trim()) err('pages build: CNAME differs from site/CNAME');

const cfg = JSON.parse(readFileSync(join(site, 'site.json'), 'utf8'));
const cname = readFileSync(join(site, 'CNAME'), 'utf8').trim();
if (cfg.url && new URL(cfg.url).host !== cname) err(`CNAME says ${cname}, site.json's url ${cfg.url}`);

const index = readFileSync(join(dist, 'index.html'));
const gz = gzipSync(index, { level: 9 }).length;
if (gz > HTML_GZIP_BUDGET) err(`index.html is ${kb(gz)} gzipped, over the ${kb(HTML_GZIP_BUDGET)} budget`);
const total = files.reduce((n, f) => n + statSync(join(dist, f)).size, 0);
const summary = [
  `index.html (HTML, CSS and JS): ${kb(index.length)} raw, ${kb(gz)} gzip (budget ${kb(HTML_GZIP_BUDGET)})`,
  `build: ${files.length} files, ${kb(total)} in all (every width and format; a visit loads one of each)`,
];
console.log(summary.join('\n'));
if (errors.length) {
  console.error(errors.map((e) => `check: ${e}`).join('\n'));
  process.exit(1);
}
console.log(`check: ${pages.length} pages ok${mode.production ? ', production' : ''}${mode.preview ? ', preview' : ''}${mode.pages ? ', pages' : ''}`);

function walk(dir, pre = '') {
  return readdirSync(join(dir, pre), { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? walk(dir, join(pre, e.name)) : [join(pre, e.name)]);
}
function kb(n) { return `${(n / 1024).toFixed(1)} KB`; }
