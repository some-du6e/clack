#!/usr/bin/env node
// Before and after pictures of the page, for review: the same sections at
// the same widths from two builds (an earlier commit's dist and this
// one's). Each build is served from its own port on 127.0.0.1 and opened
// in headless Chrome over the DevTools protocol, as tools/weigh.mjs does.
// A shot is a section's whole box, or the window at a scroll position
// (sticky bars and all). A section one build doesn't have at that width is
// listed as missing. Lazy images are loaded first and the marquee is
// stopped 20 s in, so both builds are caught at the same moment.
//
//   node tools/shots.mjs --before <dist> --after <dist> [--out shots]
//
// Writes <name>-before.png and <name>-after.png, and index.html with each
// pair side by side. Node 22+ (WebSocket), no packages; CHROME_PATH picks
// Chrome.

import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { extname, join, normalize, resolve } from 'node:path';
import { parseArgs } from 'node:util';

const { values: opt } = parseArgs({ options: { before: { type: 'string' }, after: { type: 'string' }, out: { type: 'string' } } });
if (!opt.before || !opt.after) {
  console.error('usage: node tools/shots.mjs --before <dist> --after <dist> [--out shots]');
  process.exit(2);
}
const out = resolve(opt.out ?? 'shots');

const DESK = { width: 1440, height: 900, dpr: 1, mobile: false };
const PHONE = { width: 390, height: 844, dpr: 2, mobile: true };
// sel: a section's box (the first selector that's on the page), grown to
// take in `grow` when that's there too, or the `below` px under it; view:
// the window scrolled to a section's top plus `by` (0: the top of the page)
const SHOTS = [
  { name: 'top', what: 'the top of the page: the buttons, the Runs on strip, the pill tabs and the hero shot', ...DESK, view: 0 },
  { name: 'strip', what: 'the Runs on strip', ...DESK, dpr: 2, sel: ['.strip'] },
  ...[1440, 1280, 1180, 1024].map((w) => ({ name: `pills-${w}`, what: `the pill tabs at ${w}px`, ...DESK, width: w, sel: ['.pills'] })),
  { name: 'after-band', what: 'what comes after the aubergine band', ...DESK, sel: ['.band'], below: 1000 },
  { name: 'hero-shot', what: 'the hero screenshot and what sits under it', ...DESK, dpr: 2, sel: ['.hero-media'], grow: '.shot-note' },
  { name: 'tabs-in-features', what: 'the section tabs in the Themes section', ...DESK, view: '#themes', by: 300 },
  { name: 'tabs-after-features', what: 'the section tabs at the stats band, after the feature sections', ...DESK, view: '#speed', by: -120 },
  { name: 'plugins', what: 'the plugins section', ...DESK, sel: ['#plugins'] },
  { name: 'plugin-numbers', what: 'the plugin numbers and their note marker', ...DESK, dpr: 2, sel: ['#plugins .plug-stats', '#plugins .gstat'] },
  { name: 'themes', what: 'the Themes section', ...DESK, sel: ['#themes'] },
  { name: 'open', what: 'the Open source section', ...DESK, sel: ['#open'] },
  { name: 'stats', what: 'the stats band and its note markers', ...DESK, dpr: 2, sel: ['#speed'] },
  { name: 'stats-768', what: 'the stats band at 768px', ...DESK, width: 768, sel: ['#speed'] },
  { name: 'footer-1024', what: 'the footer at 1024px', ...DESK, width: 1024, view: 'footer', by: -300 },
  { name: 'phone-top', what: 'the top of the page on a phone', ...PHONE, view: 0 },
  { name: 'phone-plugins', what: 'the plugins section on a phone', ...PHONE, sel: ['#plugins'] },
];

const PREP = `(async () => {
  const st = document.createElement('style');
  st.textContent = 'html { scroll-behavior: auto !important; }';
  document.head.append(st);
  for (const i of document.images) i.loading = 'eager';
  await Promise.all([...document.images].map((i) => i.decode().catch(() => {})));
  await document.fonts.ready;
  for (const a of document.getAnimations()) { a.pause(); a.currentTime = 20000; }
  return true;
})()`;

// ---- the two builds, each on its own port ----
const TYPES = {
  '.html': 'text/html; charset=utf-8', '.css': 'text/css', '.js': 'text/javascript', '.svg': 'image/svg+xml',
  '.json': 'application/json', '.avif': 'image/avif', '.webp': 'image/webp', '.png': 'image/png', '.jpg': 'image/jpeg', '.woff2': 'font/woff2',
};
const serve = async (root) => {
  const dir = resolve(root);
  const srv = createServer((req, res) => {
    let p = decodeURIComponent(new URL(req.url, 'http://x').pathname);
    if (p.endsWith('/')) p += 'index.html';
    const f = normalize(join(dir, p));
    if (!f.startsWith(dir) || !existsSync(f)) { res.writeHead(404); return res.end(); }
    res.writeHead(200, { 'Content-Type': TYPES[extname(f)] || 'application/octet-stream', 'Cache-Control': 'no-store' });
    res.end(readFileSync(f));
  });
  await new Promise((r) => srv.listen(0, '127.0.0.1', r));
  return { srv, origin: `http://127.0.0.1:${srv.address().port}` };
};
const sides = { before: await serve(opt.before), after: await serve(opt.after) };

// ---- Chrome over CDP ----
const chromePath = process.env.CHROME_PATH
  || ['/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium', '/usr/bin/chromium-browser'].find(existsSync);
if (!chromePath) die('no Chrome found; set CHROME_PATH');
const profile = mkdtempSync(join(tmpdir(), 'clack-shots-'));
const chrome = spawn(chromePath, [
  '--headless=new', '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--hide-scrollbars',
  '--disable-background-networking', '--disable-component-update', '--disable-sync', '--no-sandbox', '--no-proxy-server',
  `--user-data-dir=${profile}`, '--remote-debugging-port=0', 'about:blank',
], { stdio: ['ignore', 'ignore', 'pipe'] });
const wsURL = await new Promise((res, rej) => {
  let buf = '';
  const t = setTimeout(() => rej(new Error('Chrome did not start')), 30000);
  chrome.stderr.on('data', (d) => {
    buf += d;
    const m = buf.match(/DevTools listening on (ws:\/\/\S+)/);
    if (m) { clearTimeout(t); res(m[1]); }
  });
  chrome.on('exit', (c) => rej(new Error(`Chrome exited (${c}): ${buf.slice(-500)}`)));
});
const ws = new WebSocket(wsURL);
await new Promise((res, rej) => { ws.onopen = res; ws.onerror = () => rej(new Error('DevTools socket failed')); });
let seq = 0;
const pending = new Map();
const listeners = new Set();
ws.onmessage = (e) => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) {
    const { res, rej } = pending.get(m.id);
    pending.delete(m.id);
    if (m.error) rej(new Error(`${m.error.message} (${m.error.code})`)); else res(m.result);
  } else for (const l of listeners) l(m);
};
const send = (method, params = {}, sessionId) => new Promise((res, rej) => {
  const id = ++seq;
  pending.set(id, { res, rej });
  ws.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }));
});
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

mkdirSync(out, { recursive: true });
const rows = [];
let crashed;
try {
  for (const shot of SHOTS) {
    const row = { ...shot, got: {} };
    for (const side of ['before', 'after']) {
      const png = await take(sides[side].origin, shot);
      if (png) writeFileSync(join(out, `${shot.name}-${side}.png`), png);
      row.got[side] = !!png;
    }
    rows.push(row);
    console.log(`${shot.name}: before ${row.got.before ? 'ok' : 'missing'}, after ${row.got.after ? 'ok' : 'missing'}`);
  }
} catch (e) {
  crashed = e;
} finally {
  ws.close();
  chrome.kill('SIGTERM');
  for (const s of Object.values(sides)) s.srv.close();
  await sleep(300);
  rmSync(profile, { recursive: true, force: true });
}
if (crashed) die(crashed.message);

async function take(origin, shot) {
  const { targetId } = await send('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true });
  const s = (m, p) => send(m, p, sessionId);
  let loaded = false;
  const onEvent = (m) => { if (m.sessionId === sessionId && m.method === 'Page.loadEventFired') loaded = true; };
  listeners.add(onEvent);
  try {
    await s('Page.enable');
    await s('Emulation.setDeviceMetricsOverride', { width: shot.width, height: shot.height, deviceScaleFactor: shot.dpr, mobile: shot.mobile });
    await s('Page.navigate', { url: `${origin}/` });
    for (let i = 0; !loaded && i < 300; i++) await sleep(50);
    if (!loaded) throw new Error(`${shot.name}: no load event from ${origin}`);
    const ev = async (expr) => (await s('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true })).result.value;
    await ev(PREP);
    if ('view' in shot) {
      const y = await ev(`(() => {
        const v = ${JSON.stringify(shot.view)};
        if (v === 0) return 0;
        const e = document.querySelector(v);
        return e ? e.getBoundingClientRect().top + scrollY + ${shot.by || 0} : null;
      })()`);
      if (y == null) return null;
      await ev(`new Promise((r) => { scrollTo(0, ${y}); requestAnimationFrame(() => requestAnimationFrame(r)); })`);
      await sleep(150);
      return Buffer.from((await s('Page.captureScreenshot', { format: 'png' })).data, 'base64');
    }
    const box = await ev(`(() => {
      const shown = (e) => e && e.getClientRects().length > 0 && getComputedStyle(e).visibility !== 'hidden';
      const e = ${JSON.stringify(shot.sel)}.map((q) => document.querySelector(q)).find(shown);
      if (!e) return null;
      const r = e.getBoundingClientRect();
      let top = r.top + scrollY, bottom = r.bottom + scrollY;
      const g = ${JSON.stringify(shot.grow || '')} && document.querySelector(${JSON.stringify(shot.grow || '')});
      if (shown(g)) bottom = Math.max(bottom, g.getBoundingClientRect().bottom + scrollY);
      if (${shot.below || 0}) { top = bottom; bottom += ${shot.below || 0}; }
      bottom = Math.min(bottom, document.documentElement.scrollHeight);
      const wide = ${shot.below ? 'true' : 'false'};
      return { x: wide ? 0 : Math.max(0, r.left + scrollX - 8), y: Math.max(0, top - 8), width: wide ? document.documentElement.clientWidth : r.width + 16, height: bottom - top + 16 };
    })()`);
    if (!box) return null;
    const { data } = await s('Page.captureScreenshot', { format: 'png', clip: { ...box, scale: 1 }, captureBeyondViewport: true });
    return Buffer.from(data, 'base64');
  } finally {
    listeners.delete(onEvent);
    await send('Target.closeTarget', { targetId });
  }
}

const esc = (t) => String(t).replace(/&/g, '&amp;').replace(/</g, '&lt;');
const cell = (r, side) => (r.got[side] ? `<img src="${r.name}-${side}.png" alt="${side}">` : '<p class="none">not on the page at this width</p>');
writeFileSync(join(out, 'index.html'), `<!doctype html><meta charset="utf-8"><title>Before and after</title>
<style>body{margin:24px;font:15px/1.4 system-ui,sans-serif}section{margin:0 0 48px}h2{margin:0 0 4px;font-size:18px}p{margin:0 0 12px;color:#555}
.pair{display:grid;grid-template-columns:1fr 1fr;gap:16px;align-items:start}.pair img{width:100%;border:1px solid #ddd}.none{padding:24px;border:1px dashed #bbb}</style>
<h1>Before (${esc(opt.before)}) and after (${esc(opt.after)})</h1>
${rows.map((r) => `<section><h2>${esc(r.name)}</h2><p>${esc(r.what)}, ${r.width}×${r.height}${r.dpr > 1 ? ` at ${r.dpr}x` : ''}${r.mobile ? ', phone' : ''}</p>
<div class="pair"><div><b>before</b>${cell(r, 'before')}</div><div><b>after</b>${cell(r, 'after')}</div></div></section>`).join('\n')}
`);
console.log(`${rows.length} pairs in ${out}`);

function die(m) { console.error(`shots: ${m}`); process.exit(1); }
