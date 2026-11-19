#!/usr/bin/env node
// Weighs the built site the way a visitor loads it. Serves site/dist on
// 127.0.0.1 with gzip, as GitHub Pages and Cloudflare do, and opens it in
// headless Chrome over the DevTools protocol with the cache off: desktop
// windows at 1x and 2x, a tablet and two phones. It reads the paint
// timings, then scrolls to the bottom so every lazy image loads, and adds up
// the bytes on the wire, the web font included. It fails when a request
// goes anywhere but 127.0.0.1 or to the web demo (/demo/, which only its
// own page loads), when the page with its CSS and JS goes over
// 30 KB, when a whole visit goes over 300 KB, when it loads more than one
// font, or when the page is wider than the window (it names the elements
// that stick out). First it checks that last test on a page that's too wide
// on purpose, so a broken check fails the run instead of passing it.
//
//   node tools/weigh.mjs [dist] [--out weigh]   (CHROME_PATH picks Chrome)
//
// Writes weigh/report.json and weigh/report.md, and appends the table to
// $GITHUB_STEP_SUMMARY when it's set. Node 22+ (WebSocket), no packages.

import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, appendFileSync, writeFileSync, rmSync } from 'node:fs';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { dirname, extname, join, normalize, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';
import { gzipSync } from 'node:zlib';

const site = join(dirname(fileURLToPath(import.meta.url)), '..');
const { values: opt, positionals } = parseArgs({ allowPositionals: true, options: { out: { type: 'string' } } });
const dist = resolve(positionals[0] ?? join(site, 'dist'));
const outDir = resolve(opt.out ?? join(site, 'weigh'));
const BUDGET = { page: 300 * 1024, doc: 30 * 1024 };
const SCENARIOS = [
  { name: 'desktop 1440×900', width: 1440, height: 900, dpr: 1, mobile: false },
  { name: 'desktop 1440×900 at 2x', width: 1440, height: 900, dpr: 2, mobile: false },
  { name: 'laptop 1280×800 at 2x', width: 1280, height: 800, dpr: 2, mobile: false },
  { name: 'small laptop 1024×768', width: 1024, height: 768, dpr: 1, mobile: false },
  { name: 'tablet 768×1024 at 2x', width: 768, height: 1024, dpr: 2, mobile: true },
  { name: 'phone 412×915 at 2.625x', width: 412, height: 915, dpr: 2.625, mobile: true },
  { name: 'phone 360×780 at 3x', width: 360, height: 780, dpr: 3, mobile: true },
];
// the self-test pages: one 520px-wide element, and a control that fits
const SELF_TEST = {
  '/__weigh-overflow.html': '<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1"><div id="wide" style="width:520px;height:40px;background:#c00"></div>',
  '/__weigh-fits.html': '<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1"><div style="max-width:100%;height:40px;background:#0a0"></div>',
};
const TYPES = {
  '.html': 'text/html; charset=utf-8', '.css': 'text/css', '.js': 'text/javascript', '.svg': 'image/svg+xml',
  '.json': 'application/json', '.txt': 'text/plain; charset=utf-8', '.xml': 'application/xml',
  '.avif': 'image/avif', '.webp': 'image/webp', '.png': 'image/png', '.jpg': 'image/jpeg',
};

// ---- a static server like the hosts' ----
const server = createServer((req, res) => {
  let p = decodeURIComponent(new URL(req.url, 'http://x').pathname);
  if (SELF_TEST[p]) { res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); return res.end(SELF_TEST[p]); }
  if (p.endsWith('/')) p += 'index.html';
  const f = normalize(join(dist, p));
  if (!f.startsWith(dist) || !existsSync(f)) {
    res.writeHead(404, { 'Content-Type': 'text/html' });
    return res.end(existsSync(join(dist, '404.html')) ? readFileSync(join(dist, '404.html')) : 'not found');
  }
  let body = readFileSync(f);
  const type = TYPES[extname(f)] || 'application/octet-stream';
  const headers = { 'Content-Type': type, 'Cache-Control': 'no-store' };
  if (/^(text|application\/(json|xml))|svg/.test(type) && /\bgzip\b/.test(req.headers['accept-encoding'] || '')) {
    body = gzipSync(body, { level: 9 });
    headers['Content-Encoding'] = 'gzip';
  }
  headers['Content-Length'] = body.length;
  res.writeHead(200, headers);
  res.end(body);
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const origin = `http://127.0.0.1:${server.address().port}`;

// ---- Chrome over CDP ----
const chromePath = process.env.CHROME_PATH
  || ['/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium', '/usr/bin/chromium-browser'].find(existsSync);
if (!chromePath) die('no Chrome found; set CHROME_PATH');
const profile = mkdtempSync(join(tmpdir(), 'clack-weigh-'));
const chrome = spawn(chromePath, [
  '--headless=new', '--no-first-run', '--no-default-browser-check', '--disable-extensions',
  '--disable-background-networking', '--disable-component-update', '--disable-sync', '--metrics-recording-only',
  '--no-sandbox', '--no-proxy-server', `--user-data-dir=${profile}`, '--remote-debugging-port=0', 'about:blank',
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

const results = [];
const selfTest = [];
let crashed;
try {
  // the sideways-scroll check, on a page that's too wide and one that fits:
  // under phone emulation Chrome widens the layout viewport to the content,
  // so innerWidth can't tell (it grows to 520 too); clientWidth stays at the
  // phone's width
  for (const sc of SCENARIOS.filter((x) => x.width <= 412)) {
    for (const [path, want] of [['/__weigh-overflow.html', true], ['/__weigh-fits.html', false]]) {
      const r = await visit(sc, path, false);
      selfTest.push({ scenario: sc.name, path, want, got: r.overflow.length > 0, wide: r.overflow });
    }
  }
  await visit(SCENARIOS[0]); // warm-up, not counted: Chrome's first request pays for its own start-up
  for (const sc of SCENARIOS) results.push(await visit(sc));
} catch (e) {
  crashed = e;
} finally {
  ws.close();
  chrome.kill('SIGTERM');
  server.close();
  await sleep(300);
  rmSync(profile, { recursive: true, force: true });
}
if (crashed) die(crashed.message);

async function visit(sc, path = '/', scroll = true) {
  const { targetId } = await send('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true });
  const s = (m, p) => send(m, p, sessionId);
  const reqs = new Map();
  let loaded = false;
  const onEvent = (m) => {
    if (m.sessionId !== sessionId) return;
    const p = m.params;
    if (m.method === 'Network.requestWillBeSent') {
      reqs.set(p.requestId, { url: p.request.url, type: p.type, bytes: 0, done: false });
    } else if (m.method === 'Network.loadingFinished' || m.method === 'Network.loadingFailed') {
      const r = reqs.get(p.requestId);
      if (r) { r.done = true; r.bytes = p.encodedDataLength || 0; r.failed = m.method === 'Network.loadingFailed'; }
    } else if (m.method === 'Page.loadEventFired') loaded = true;
  };
  listeners.add(onEvent);
  await s('Network.enable');
  await s('Network.setCacheDisabled', { cacheDisabled: true });
  await s('Page.enable');
  await s('Emulation.setDeviceMetricsOverride', { width: sc.width, height: sc.height, deviceScaleFactor: sc.dpr, mobile: sc.mobile });
  await s('Page.navigate', { url: `${origin}${path}` });
  for (let i = 0; !loaded && i < 300; i++) await sleep(50);
  if (!loaded) throw new Error(`${sc.name}: no load event`);
  await idle(reqs);
  const evalJSON = async (expr) => JSON.parse((await s('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true })).result.value);
  // paint timings before scrolling: a scripted scroll isn't input, so LCP
  // would keep counting the lazy images it brings in
  const timing = await evalJSON(`new Promise((done) => {
    let lcp = 0;
    new PerformanceObserver((l) => { for (const e of l.getEntries()) lcp = e.startTime; }).observe({ type: 'largest-contentful-paint', buffered: true });
    setTimeout(() => {
      const n = performance.getEntriesByType('navigation')[0];
      const fcp = performance.getEntriesByName('first-contentful-paint')[0];
      done(JSON.stringify({ ttfb: n.responseStart, dcl: n.domContentLoadedEventEnd, load: n.loadEventEnd, fcp: fcp ? fcp.startTime : 0, lcp }));
    }, 200);
  })`);
  const above = sum(reqs);
  for (let i = 0; scroll && i < 80; i++) {
    const { y, max } = await evalJSON('JSON.stringify({ y: scrollY + innerHeight, max: document.documentElement.scrollHeight })');
    if (y >= max) break;
    await s('Runtime.evaluate', { expression: 'scrollBy(0, innerHeight * 0.9)' });
    await sleep(120);
  }
  await idle(reqs);
  // wider than the window: the elements whose right edge passes the
  // document's clientWidth (the window's width, also under phone emulation)
  const overflow = await evalJSON(`JSON.stringify((() => {
    const cw = document.documentElement.clientWidth;
    if (document.documentElement.scrollWidth <= cw) return [];
    return [...document.querySelectorAll('body *')].filter((e) => { const r = e.getBoundingClientRect(); return r.width && r.right > cw + 0.5; })
      .slice(0, 8).map((e) => e.tagName.toLowerCase() + (e.id ? '#' + e.id : '') + (e.className && typeof e.className === 'string' ? '.' + e.className.trim().split(/\\s+/).join('.') : '') + ' (right ' + Math.round(e.getBoundingClientRect().right) + ' > ' + cw + ')');
  })())`);
  listeners.delete(onEvent);
  await send('Target.closeTarget', { targetId });

  const all = [...reqs.values()];
  const offsite = all.filter((r) => !r.url.startsWith(origin + '/') && !r.url.startsWith('data:')).map((r) => r.url);
  const demo = all.filter((r) => r.url.startsWith(origin + '/demo/')).map((r) => r.url);
  const failed = all.filter((r) => r.failed).map((r) => r.url);
  const doc = all.find((r) => r.type === 'Document')?.bytes ?? 0;
  const images = all.filter((r) => r.type === 'Image').reduce((n, r) => n + r.bytes, 0);
  const fonts = all.filter((r) => r.type === 'Font');
  return { ...sc, requests: all.length, firstScreen: above, total: sum(reqs), doc, images, fonts: fonts.length, fontBytes: fonts.reduce((n, r) => n + r.bytes, 0), offsite, demo, failed, overflow, timing };
}

function sum(reqs) { return [...reqs.values()].reduce((n, r) => n + r.bytes, 0); }

async function idle(reqs) {
  for (let quiet = 0, i = 0; quiet < 6 && i < 200; i++) {
    await sleep(100);
    quiet = [...reqs.values()].every((r) => r.done) ? quiet + 1 : 0;
  }
}

// ---- report ----
const kb = (n) => `${(n / 1024).toFixed(1)} KB`;
const ms = (n) => `${Math.round(n)} ms`;
const problems = [];
for (const t of selfTest) {
  if (t.got !== t.want) problems.push(`self-test, ${t.scenario}: the sideways-scroll check ${t.want ? 'missed' : 'flagged'} ${t.path}${t.wide.length ? ` (${t.wide.join(', ')})` : ''}`);
}
for (const r of results) {
  if (r.offsite.length) problems.push(`${r.name}: requests off 127.0.0.1: ${r.offsite.join(', ')}`);
  if (r.demo.length) problems.push(`${r.name}: the homepage loaded the web demo's files: ${r.demo.join(', ')}`);
  if (r.failed.length) problems.push(`${r.name}: failed requests: ${r.failed.join(', ')}`);
  if (r.total > BUDGET.page) problems.push(`${r.name}: ${kb(r.total)} in all, over the ${kb(BUDGET.page)} budget`);
  if (r.doc > BUDGET.doc) problems.push(`${r.name}: the page is ${kb(r.doc)} on the wire, over ${kb(BUDGET.doc)}`);
  if (r.overflow.length) problems.push(`${r.name}: the page scrolls sideways: ${r.overflow.join(', ')}`);
  if (r.fonts > 1) problems.push(`${r.name}: ${r.fonts} fonts loaded, the page has one`);
}
const md = [
  '### Page weight (headless Chrome, local server with gzip, cache off)',
  '',
  `Budgets: the page with its CSS and JS ≤ ${kb(BUDGET.doc)}, a whole visit scrolled to the bottom ≤ ${kb(BUDGET.page)} with the font, one font, every request to the site itself, nothing wider than the window.`,
  '',
  '| visit | requests | page (HTML+CSS+JS) | font | first screen | whole visit | images | FCP | LCP | load | wider than the window |',
  '|---|---|---|---|---|---|---|---|---|---|---|',
  ...results.map((r) => `| ${r.name} | ${r.requests} | ${kb(r.doc)} | ${kb(r.fontBytes)} | ${kb(r.firstScreen)} | **${kb(r.total)}** | ${kb(r.images)} | ${ms(r.timing.fcp)} | ${ms(r.timing.lcp)} | ${ms(r.timing.load)} | ${r.overflow.length ? r.overflow.join(', ') : 'no'} |`),
  '',
  `Self-test of the sideways-scroll check: ${selfTest.map((t) => `${t.scenario} ${t.path.replace('/__weigh-', '').replace('.html', '')}: ${t.got ? 'flagged' : 'fits'}${t.got === t.want ? '' : ' (WRONG)'}`).join('; ')}.`,
  '',
  'Timings are unthrottled on this machine; Lighthouse gives the throttled ones.',
  '',
  problems.length ? `**Problems:**\n\n${problems.map((p) => `- ${p}`).join('\n')}` : 'No problems: nothing left 127.0.0.1 and every visit is under budget.',
  '',
].join('\n');
mkdirSync(outDir, { recursive: true });
writeFileSync(join(outDir, 'report.json'), JSON.stringify({ origin, budget: BUDGET, selfTest, results }, null, 2) + '\n');
writeFileSync(join(outDir, 'report.md'), md);
if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, md + '\n');
console.log(md);
if (problems.length) process.exit(1);

function die(m) { console.error(`weigh: ${m}`); process.exit(1); }
