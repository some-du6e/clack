#!/usr/bin/env node
// Builds Clack's site into site/dist (or --out <dir>). Node built-ins only.
//
//   node build.mjs              build from the committed snapshots in data/
//   node build.mjs --refresh    fetch the releases and the plugin Library
//                               index from GitHub first, rewrite data/, build
//   node build.mjs --preview    a preview build: noindex, robots.txt turns
//                               every crawler away, no sitemap
//   node build.mjs --pages      the GitHub Pages build: adds CNAME
//   node build.mjs --strict     fail when a download is missing
//
// The canonical base (canonical link, og:url, og:image, sitemap) is
// site.json's "url"; --site-url or SITE_URL overrides it, and an empty one
// leaves those tags out. Every other URL on the page is relative.
//
// src/index.html is the page. {{a.b}} inserts escaped text, {{{a.b}}} raw
// HTML, <!--#if flag-->…<!--#endif--> keeps its body when the flag is set
// (site, preview), and <x-shot name=… sizes=… alt=… [eager]></x-shot>
// becomes a <picture> from img/images.json (tools/images.mjs writes it).
// Plugin verdicts come from ../internal/pluginlib/compat.go, the table
// Clack itself shows, and the perf numbers from data/perf.json.

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
if (args.preview && args.pages) fail('--preview and --pages are different builds');

if (args.refresh) await refresh();

const release = readJSON(join(here, 'data/release.json'));
const plugins = readJSON(join(here, 'data/plugins.json'));
const perf = readJSON(join(here, 'data/perf.json'));
const images = readJSON(join(here, 'img/images.json'));
const compat = readCompat(join(here, '..', 'internal', 'pluginlib', 'compat.go'));

const ctx = {
  site: { canonical: siteURL + '/', og_image: siteURL + '/img/og.jpg' },
  repo: { url: `https://github.com/${repo}` },
  release: {
    tag: release.tag,
    version: release.tag.replace(/^v/, ''),
    short: short(release.tag),
    date: fmtDate(release.published),
    url: release.url,
    packages: release.assets.filter((a) => !ASSETS.sums.test(a.name)).length,
    pills: release.recent.slice(0, 5).map((r) => `<li><a href="${esc(r.url)}"><span>${esc(r.headline)}</span><span class="badge">${esc(short(r.tag))}</span></a></li>`).join(''),
    cards: release.recent.slice(0, 3).map((r) => `<li><a class="card" href="${esc(r.url)}"><span class="card-art" aria-hidden="true">${esc(short(r.tag))}<small>${esc(fmtDate(r.published))}</small></span><span class="card-body"><span class="badge">New in ${esc(short(r.tag))}</span><h3>${esc(r.headline)}</h3><p>${esc(r.blurb)}</p></span></a></li>`).join(''),
  },
  releases: { count: release.count },
  dl: downloads(release),
  plugins: pluginContext(plugins, compat),
  perf: perfContext(perf),
  css: minCSS(read('src/style.css')),
  js: minJS(read('src/os.js')),
  icon: { top: icon('top'), menu: icon('menu'), foot: icon('foot') },
};
const flags = { site: !!siteURL, preview: !!args.preview };
ctx.hero = { preload: heroPreload(read('src/index.html')) };

rmSync(out, { recursive: true, force: true });
mkdirSync(join(out, 'img'), { recursive: true });
mkdirSync(join(out, 'fonts'), { recursive: true });
const html = render(read('src/index.html'));
writeFileSync(join(out, 'index.html'), html);
writeFileSync(join(out, '404.html'), render(read('src/404.html')));
for (const f of readdirSync(join(here, 'img'))) {
  if (/\.(avif|webp|png|jpg)$/.test(f)) cpSync(join(here, 'img', f), join(out, 'img', f));
}
for (const f of readdirSync(join(here, 'fonts'))) {
  if (/\.woff2$/.test(f)) cpSync(join(here, 'fonts', f), join(out, 'fonts', f));
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
if (args.pages) cpSync(join(here, 'CNAME'), join(out, 'CNAME'));

const gz = gzipSync(html, { level: 9 }).length;
const br = brotliCompressSync(html, { params: { [zc.BROTLI_PARAM_QUALITY]: 11 } }).length;
const kind = flags.preview ? ' (preview)' : args.pages ? ' (pages)' : '';
console.log(`built ${out}${kind}: ${release.tag}, base ${siteURL || '(none)'}`);
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

// <picture>: AVIF first, WebP in the <img> itself. Width and height come
// from images.json, so the box is reserved before the image arrives.
function picture({ name, sizes, alt, eager }) {
  const im = images[name];
  if (!im) fail(`x-shot ${name} is not in img/images.json`);
  if (!sizes || typeof alt !== 'string' || !alt) fail(`x-shot ${name} needs sizes and alt`);
  const set = (file, fmt) => im.widths.map((w) => `img/${file}-${w}.${fmt} ${w}w`).join(', ');
  const fallback = im.widths[Math.min(1, im.widths.length - 1)];
  return `<picture><source type="image/avif" srcset="${set(im.files.light, 'avif')}" sizes="${sizes}">`
    + `<img src="img/${im.files.light}-${fallback}.webp" srcset="${set(im.files.light, 'webp')}" sizes="${sizes}"`
    + ` width="${im.width}" height="${im.height}" alt="${esc(alt)}"`
    + (eager ? ' fetchpriority="high"' : ' loading="lazy"') + ' decoding="async"></picture>';
}

// The eager (LCP) picture's AVIF, preloaded from the top of <head> so its
// request starts before the inline CSS is parsed. Same srcset and sizes as
// the <picture>'s AVIF source, so the browser uses one download for both.
function heroPreload(src) {
  const m = src.match(/<x-shot\b([^>]*\beager\b[^>]*)><\/x-shot>/);
  if (!m) return '';
  const { name, sizes } = attrs(m[1]);
  const im = images[name];
  const set = im.widths.map((w) => `img/${im.files.light}-${w}.avif ${w}w`).join(', ');
  return `<link rel="preload" as="image" type="image/avif" imagesrcset="${set}" imagesizes="${sizes}" fetchpriority="high">`;
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

// compat.go's table: system + "/" + id -> partial or broken (every other
// plugin works). A plugin's verdict is its best variant's, since Install
// picks the variant that works better.
function readCompat(file) {
  if (!existsSync(file)) fail(`${file} is missing: the site builds inside the clack repository`);
  const src = readFileSync(file, 'utf8');
  const m = new Map();
  for (const x of src.matchAll(/(Slick|Taut) \+ "\/(\w+)":\s*(partial|broken)\("([^"]*)"\)/g)) m.set(`${x[1].toLowerCase()}/${x[2]}`, { status: x[3], line: x[4] });
  if (m.size < 5) fail(`${file}: read only ${m.size} verdicts; has its format changed?`);
  return m;
}

function pluginContext(p, compat) {
  const rank = { ok: 0, partial: 1, broken: 2 };
  const rows = p.plugins.map((x) => {
    const vs = x.variants.map((v) => ({ ...v, ...(compat.get(`${v.system}/${v.id}`) || { status: 'ok', line: '' }) }));
    const best = vs.reduce((a, b) => (rank[b.status] < rank[a.status] ? b : a));
    return { name: x.name, status: best.status, line: best.line };
  });
  const count = (s) => rows.filter((r) => r.status === s).length;
  return {
    count: rows.length,
    builds: p.builds,
    ok: count('ok'),
    partial: count('partial'),
    broken: count('broken'),
    generated: fmtDate(p.generated),
    // a plugin that doesn't work never shows; one that partly works says so
    chips: rows.filter((r) => r.status !== 'broken').map((r) => r.status === 'partial'
      ? `<li title="${esc(r.line)}">${esc(r.name)}<span class="tag">partly</span></li>`
      : `<li>${esc(r.name)}</li>`).join(''),
  };
}

function perfContext(p) {
  for (const k of ['run', 'commit', 'release', 'firstFrame', 'settled', 'memWarm', 'memIdle', 'note1', 'note2']) {
    if (!(k in p)) fail(`data/perf.json has no ${k}`);
  }
  return p;
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
  const list = [];
  for (let page = 1; page <= 10; page++) {
    const got = await gh(`https://api.github.com/repos/${repo}/releases?per_page=100&page=${page}`);
    list.push(...got);
    if (got.length < 100) break;
  }
  // newest by tag version, not by published_at (re-publishing an old tag
  // resets its date); every Clack release so far is a pre-release, so
  // /releases/latest is a 404
  const tagged = list.filter((r) => !r.draft && r.published_at && semver(r.tag_name)).sort((a, b) => cmpSemver(semver(b.tag_name), semver(a.tag_name)));
  const up = (r, k) => r.assets.some((a) => ASSETS[k].test(a.name) && a.state === 'uploaded');
  // the newest release with every download up; with one missing (an ARM job
  // that failed, say) the site stays on the last complete release
  const rel = tagged.find((r) => Object.keys(ASSETS).every((k) => up(r, k)))
    || (!args.strict && tagged.find((r) => REQUIRED.every((k) => up(r, k))));
  if (!rel) fail('no release has all its downloads');
  const newer = tagged.indexOf(rel);
  if (newer > 0) console.warn(`refresh: ${tagged.slice(0, newer).map((r) => r.tag_name).join(', ')} not complete yet; using ${rel.tag_name}`);
  const recent = tagged.slice(newer, newer + 5).map((r) => ({ tag: r.tag_name, published: r.published_at, url: r.html_url, ...notes(r.body || '') }));
  writeJSON('data/release.json', {
    tag: rel.tag_name,
    published: rel.published_at,
    url: rel.html_url,
    count: tagged.filter((r) => /^v\d/.test(r.tag_name)).length,
    assets: rel.assets.filter((a) => a.state === 'uploaded').map((a) => ({ name: a.name, size: a.size, url: a.browser_download_url })),
    recent,
  });
  // the plugin Library: names, systems and ids only
  const idx = await gh(`https://github.com/${repo}/releases/download/plugins/index.json`, 'application/json');
  const by = new Map();
  for (const x of idx.plugins) {
    if (!by.has(x.name)) by.set(x.name, []);
    by.get(x.name).push({ system: x.system, id: x.id });
  }
  writeJSON('data/plugins.json', {
    generated: idx.generated,
    builds: idx.plugins.length,
    plugins: [...by].sort(([a], [b]) => a.localeCompare(b)).map(([name, variants]) => ({ name, variants: variants.sort((a, b) => a.system.localeCompare(b.system)) })),
  });
  console.log(`refreshed data/: ${rel.tag_name} (${recent.length} recent), ${by.size} plugins`);
}

// the notes' first bullet: its bold lead-in ("- **Thing that changed.** …")
// and the sentence after it
function notes(body) {
  const m = body.match(/^\s*[-*]\s+\*\*(.+?)\*\*\s*(.*)$/m);
  if (!m) return { headline: 'Read the release notes', blurb: '' };
  const plain = (t) => t.replace(/`([^`]*)`/g, '$1').replace(/\[([^\]]*)\]\([^)]*\)/g, '$1').replace(/\*\*?([^*]+)\*\*?/g, '$1').trim();
  let blurb = plain(m[2]);
  const end = blurb.search(/[.!?](\s|$)/);
  if (end > 0) blurb = blurb.slice(0, end + 1);
  if (blurb.length > 160) blurb = blurb.slice(0, 157).replace(/\s+\S*$/, '') + '…';
  return { headline: plain(m[1]), blurb };
}

function semver(tag) {
  const m = /^v(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$/.exec(tag);
  return m && { core: [+m[1], +m[2], +m[3]], pre: m[4] ? m[4].split('.') : [] };
}

function cmpSemver(a, b) {
  for (let i = 0; i < 3; i++) if (a.core[i] !== b.core[i]) return a.core[i] - b.core[i];
  if (!a.pre.length || !b.pre.length) return b.pre.length - a.pre.length;
  for (let i = 0; i < Math.max(a.pre.length, b.pre.length); i++) {
    const x = a.pre[i], y = b.pre[i];
    if (x === undefined || y === undefined) return x === undefined ? -1 : 1;
    const nx = /^\d+$/.test(x), ny = /^\d+$/.test(y);
    if (nx && ny && +x !== +y) return +x - +y;
    if (nx !== ny) return nx ? -1 : 1;
    if (x !== y) return x < y ? -1 : 1;
  }
  return 0;
}

// ---- helpers ----

function short(tag) { return tag.replace(/^v\d+(\.\d+)*-/, ''); }

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
