#!/usr/bin/env node
// Builds Clack's site into site/dist (or --out <dir>). Node built-ins only.
//
//   node build.mjs              build from the committed snapshots in data/
//   node build.mjs --refresh    fetch the newest release and the plugin
//                               Library index from GitHub first, rewrite
//                               data/*.json, then build
//   node build.mjs --preview    a preview build: noindex, robots.txt
//                               disallows everything, no sitemap
//   node build.mjs --strict     fail when a download is missing
//
// The canonical base (canonical link, og:url, og:image, sitemap) is
// site.json's "url"; --site-url or SITE_URL overrides it, and an empty one
// leaves those tags out. Every other URL on the page is relative, so the
// output works at a domain's root and under a path.
//
// src/index.html is the page. {{a.b}} inserts escaped text, {{{a.b}}} raw
// HTML, <!--#if flag-->…<!--#endif--> keeps its body when the flag is set
// (site, preview), and <x-shot name=… sizes=… alt=… [eager]></x-shot>
// becomes a <picture> from img/images.json (tools/images.mjs writes it).

import { cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { brotliCompressSync, gzipSync, constants as zc } from 'node:zlib';

const here = dirname(fileURLToPath(import.meta.url));
const args = parseArgs(process.argv.slice(2));
const cfg = readJSON(join(here, 'site.json'));
const siteURL = String(args['site-url'] ?? process.env.SITE_URL ?? cfg.url ?? '').replace(/\/+$/, '');
const repo = process.env.SITE_REPO || cfg.repo;
const out = resolve(args.out ?? join(here, 'dist'));

// the release downloads the page links, by file name
const ASSETS = {
  win: /-windows-amd64-setup\.exe$/,
  win_zip: /-windows-amd64\.zip$/,
  win_arm: /-windows-arm64\.zip$/,
  mac: /-macos-universal\.dmg$/,
  mac_zip: /-macos-universal\.zip$/,
  linux: /-x86_64\.AppImage$/,
  linux_arm: /-aarch64\.AppImage$/,
  deb: /_amd64\.deb$/,
  deb_arm: /_arm64\.deb$/,
  rpm: /\.x86_64\.rpm$/,
  rpm_arm: /\.aarch64\.rpm$/,
  sums: /^SHA256SUMS$/,
};
const REQUIRED = ['win', 'mac', 'linux', 'sums'];

if (siteURL && !/^https:\/\/[a-z0-9.-]+(:\d+)?(\/[\w./-]*)?$/i.test(siteURL)) fail(`site url ${siteURL} is not an https URL`);

if (args.refresh) await refresh();

const release = readJSON(join(here, 'data/release.json'));
const plugins = readJSON(join(here, 'data/plugins.json'));
const images = readJSON(join(here, 'img/images.json'));

const ctx = {
  site: { canonical: siteURL + '/', og_image: siteURL + '/img/og.jpg' },
  repo: { url: `https://github.com/${repo}` },
  release: {
    tag: release.tag,
    short: release.tag.replace(/^v\d+(\.\d+)*-/, ''),
    date: fmtDate(release.published),
    url: release.url,
    headline: release.headline || 'Read the release notes',
  },
  dl: downloads(release),
  plugins: pluginContext(plugins),
  css: minCSS(read('src/style.css')),
  js: minJS(read('src/os.js')),
  icon: { top: icon('top'), foot: icon('foot') },
};
const flags = { site: !!siteURL, preview: !!args.preview };

rmSync(out, { recursive: true, force: true });
mkdirSync(join(out, 'img'), { recursive: true });
const html = render(read('src/index.html'));
writeFileSync(join(out, 'index.html'), html);
writeFileSync(join(out, '404.html'), render(read('src/404.html')));
for (const f of readdirSync(join(here, 'img'))) {
  if (/\.(avif|webp|png|jpg)$/.test(f)) cpSync(join(here, 'img', f), join(out, 'img', f));
}
cpSync(join(here, '..', 'assets', 'icon.svg'), join(out, 'icon.svg'));
writeFileSync(join(out, 'robots.txt'), flags.preview
  ? 'User-agent: *\nDisallow: /\n'
  : 'User-agent: *\nAllow: /\n' + (siteURL ? `Sitemap: ${siteURL}/sitemap.xml\n` : ''));
if (siteURL && !flags.preview) {
  writeFileSync(join(out, 'sitemap.xml'), '<?xml version="1.0" encoding="UTF-8"?>\n'
    + '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n'
    + `<url><loc>${siteURL}/</loc><lastmod>${release.published.slice(0, 10)}</lastmod></url>\n</urlset>\n`);
}

const gz = gzipSync(html, { level: 9 }).length;
const br = brotliCompressSync(html, { params: { [zc.BROTLI_PARAM_QUALITY]: 11 } }).length;
console.log(`built ${out}${flags.preview ? ' (preview)' : ''}: ${release.tag}, base ${siteURL || '(none)'}`);
console.log(`index.html with its CSS and JS: ${kb(html.length)} raw, ${kb(gz)} gzip, ${kb(br)} brotli`);

// ---- template ----

function render(src) {
  let s = src.replace(/<!--#if (\w+)-->([\s\S]*?)<!--#endif-->/g, (_, f, body) => {
    if (!(f in flags)) fail(`unknown flag ${f}`);
    return flags[f] ? body : '';
  });
  s = s.replace(/<x-shot\b([^>]*)><\/x-shot>/g, (_, a) => picture(attrs(a)));
  s = s.replace(/\{\{\{\s*([\w.]+)\s*\}\}\}/g, (_, k) => String(lookup(k)));
  s = s.replace(/\{\{\s*([\w.]+)\s*\}\}/g, (_, k) => esc(lookup(k)));
  if (/\{\{\{?\s*[\w.]+\s*\}|<x-shot|<!--#/.test(s)) fail('unrendered template syntax left in the page');
  return s.replace(/\n[ \t]+</g, '\n<');
}

function lookup(path) {
  let v = ctx;
  for (const p of path.split('.')) {
    if (v == null || !(p in v)) fail(`template key ${path} is undefined`);
    v = v[p];
  }
  return v;
}

function attrs(s) {
  const a = {};
  for (const m of s.matchAll(/([\w-]+)(?:="([^"]*)")?/g)) a[m[1]] = m[2] ?? true;
  return a;
}

// <picture>: dark variants under prefers-color-scheme, AVIF first, WebP in
// the <img> itself. Widths and height come from images.json so the box is
// reserved before the image arrives (no layout shift).
function picture({ name, sizes, alt, eager }) {
  const im = images[name];
  if (!im) fail(`x-shot ${name} is not in img/images.json`);
  if (!sizes || typeof alt !== 'string' || !alt) fail(`x-shot ${name} needs sizes and alt`);
  const set = (file, fmt) => im.widths.map((w) => `img/${file}-${w}.${fmt} ${w}w`).join(', ');
  let p = '<picture>';
  if (im.files.dark) {
    for (const fmt of ['avif', 'webp']) {
      p += `<source media="(prefers-color-scheme: dark)" type="image/${fmt}" srcset="${set(im.files.dark, fmt)}" sizes="${sizes}">`;
    }
  }
  p += `<source type="image/avif" srcset="${set(im.files.light, 'avif')}" sizes="${sizes}">`;
  const fallback = im.widths[Math.min(1, im.widths.length - 1)];
  p += `<img src="img/${im.files.light}-${fallback}.webp" srcset="${set(im.files.light, 'webp')}" sizes="${sizes}"`
    + ` width="${im.width}" height="${im.height}" alt="${esc(alt)}"`
    + (eager ? ' fetchpriority="high"' : ' loading="lazy"') + ' decoding="async">';
  return p + '</picture>';
}

// assets/icon.svg inline, with ids made unique per copy
function icon(id) {
  let s = read('../assets/icon.svg').replace(/<title>[^<]*<\/title>/, '').replace(/\s*\n\s*/g, '');
  s = s.replace(/ width="\d+" height="\d+"/, '').replace('<svg ', '<svg aria-hidden="true" focusable="false" ');
  for (const g of ['cbg', 'csheen']) s = s.replaceAll(`id="${g}"`, `id="${g}-${id}"`).replaceAll(`url(#${g})`, `url(#${g}-${id})`);
  return s;
}

// ---- data ----

function downloads(r) {
  const dl = {};
  for (const [k, re] of Object.entries(ASSETS)) {
    const a = r.assets.find((x) => re.test(x.name));
    if (!a && (args.strict || REQUIRED.includes(k))) fail(`${r.tag} has no asset for ${k} (${re})`);
    dl[k] = a ? { url: a.url, name: a.name, size: `${(a.size / 1048576).toFixed(1)} MB` }
      : { url: r.url, name: 'see the release page', size: '' };
  }
  return dl;
}

function pluginContext(p) {
  const both = p.plugins.filter((x) => x.systems.length > 1);
  return {
    count: p.plugins.length,
    builds: p.builds,
    both: both.length,
    generated: fmtDate(p.generated),
    chips: both.map((x) => `<li>${esc(x.name)}</li>`).join(''),
  };
}

async function refresh() {
  const gh = async (url, accept = 'application/vnd.github+json') => {
    const headers = { Accept: accept, 'User-Agent': 'clack-site-build' };
    const tok = process.env.GITHUB_TOKEN || process.env.GH_TOKEN;
    if (tok && url.startsWith('https://api.github.com/')) headers.Authorization = `Bearer ${tok}`;
    const r = await fetch(url, { headers, redirect: 'follow' });
    if (!r.ok) fail(`GET ${url}: HTTP ${r.status}`);
    return r.json();
  };
  // the newest published v* release that has its downloads up (pre-releases
  // count: every Clack release so far is one, so /releases/latest is a 404)
  const list = await gh(`https://api.github.com/repos/${repo}/releases?per_page=30`);
  const rel = list
    .filter((r) => !r.draft && /^v\d/.test(r.tag_name) && r.published_at)
    .sort((a, b) => b.published_at.localeCompare(a.published_at))
    .find((r) => REQUIRED.every((k) => r.assets.some((a) => ASSETS[k].test(a.name) && a.state === 'uploaded')));
  if (!rel) fail('no release with all its downloads');
  writeJSON('data/release.json', {
    tag: rel.tag_name,
    published: rel.published_at,
    url: rel.html_url,
    headline: headline(rel.body || ''),
    assets: rel.assets.filter((a) => a.state === 'uploaded')
      .map((a) => ({ name: a.name, size: a.size, url: a.browser_download_url })),
  });
  // the plugin Library: names and systems only
  const idx = await gh(`https://github.com/${repo}/releases/download/plugins/index.json`, 'application/json');
  const by = new Map();
  for (const x of idx.plugins) {
    if (!by.has(x.name)) by.set(x.name, new Set());
    by.get(x.name).add(x.system);
  }
  writeJSON('data/plugins.json', {
    generated: idx.generated,
    builds: idx.plugins.length,
    plugins: [...by].sort(([a], [b]) => a.localeCompare(b)).map(([name, s]) => ({ name, systems: [...s].sort() })),
  });
  console.log(`refreshed data/: ${rel.tag_name}, ${by.size} plugins`);
}

// the first bold lead-in of the notes ("- **Thing that changed.** …")
function headline(body) {
  const m = body.match(/^\s*[-*]\s+\*\*(.+?)\*\*/m);
  if (!m) return '';
  return m[1].replace(/`([^`]*)`/g, '$1').replace(/\[([^\]]*)\]\([^)]*\)/g, '$1').trim();
}

// ---- helpers ----

function minCSS(s) {
  return s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/\s+/g, ' ')
    .replace(/\s*([{};])\s*/g, '$1').replace(/,\s+/g, ',').replace(/;}/g, '}').trim();
}

function minJS(s) {
  return s.split('\n').map((l) => l.trim()).filter((l) => l && !l.startsWith('//')).join('\n');
}

function fmtDate(iso) {
  return new Date(iso).toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' });
}

function esc(v) {
  return String(v).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function kb(n) { return `${(n / 1024).toFixed(1)} KB`; }
function read(p) { return readFileSync(join(here, p), 'utf8'); }
function readJSON(p) {
  if (!existsSync(p)) fail(`${p} is missing`);
  return JSON.parse(readFileSync(p, 'utf8'));
}
function writeJSON(p, v) { writeFileSync(join(here, p), JSON.stringify(v, null, 2) + '\n'); }
function fail(msg) { console.error(`build: ${msg}`); process.exit(1); }

function parseArgs(argv) {
  const a = {};
  for (let i = 0; i < argv.length; i++) {
    const m = argv[i].match(/^--([\w-]+)(?:=(.*))?$/);
    if (!m) fail(`unknown argument ${argv[i]}`);
    if (m[2] !== undefined) a[m[1]] = m[2];
    else if (['site-url', 'out'].includes(m[1])) a[m[1]] = argv[++i] ?? '';
    else a[m[1]] = true;
  }
  return a;
}
