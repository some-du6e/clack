#!/usr/bin/env node
// Weighs the built site the way a visitor loads it. Serves site/dist on
// 127.0.0.1 with gzip, as GitHub Pages and Cloudflare do, and opens it in
// headless Chrome over the DevTools protocol with the cache off: a desktop
// window at 1x and 2x and a phone, in light and dark. It reads the paint
// timings, then scrolls to the bottom so every lazy image loads, and adds up
// the bytes on the wire. It fails when a request goes anywhere but
// 127.0.0.1, when the page with its CSS and JS goes over 30 KB, or when a
// whole visit goes over 300 KB.
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
  { name: 'desktop 1440×900, light', width: 1440, height: 900, dpr: 1, mobile: false, scheme: 'light' },
  { name: 'desktop 1440×900, dark', width: 1440, height: 900, dpr: 1, mobile: false, scheme: 'dark' },
  { name: 'desktop 1440×900 at 2x, light', width: 1440, height: 900, dpr: 2, mobile: false, scheme: 'light' },
  { name: 'desktop 1440×900 at 2x, dark', width: 1440, height: 900, dpr: 2, mobile: false, scheme: 'dark' },
  { name: 'phone 412×915 at 2.625x, light', width: 412, height: 915, dpr: 2.625, mobile: true, scheme: 'light' },
  { name: 'phone 412×915 at 2.625x, dark', width: 412, height: 915, dpr: 2.625, mobile: true, scheme: 'dark' },
  { name: 'phone 360×780 at 3x, light', width: 360, height: 780, dpr: 3, mobile: true, scheme: 'light' },
];
const TYPES = {
  '.html': 'text/html; charset=utf-8', '.css': 'text/css', '.js': 'text/javascript', '.svg': 'image/svg+xml',
  '.json': 'application/json', '.txt': 'text/plain; charset=utf-8', '.xml': 'application/xml',
  '.avif': 'image/avif', '.webp': 'image/webp', '.png': 'image/png', '.jpg': 'image/jpeg',
};

// ---- a static server like the hosts' ----
const server = createServer((req, res) => {
  let p = decodeURIComponent(new URL(req.url, 'http://x').pathname);
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
let crashed;
try {
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

async function visit(sc) {
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
  await s('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: sc.scheme }] });
  await s('Page.navigate', { url: `${origin}/` });
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
  for (let i = 0; i < 80; i++) {
    const { y, max } = await evalJSON('JSON.stringify({ y: scrollY + innerHeight, max: document.documentElement.scrollHeight })');
    if (y >= max) break;
    await s('Runtime.evaluate', { expression: 'scrollBy(0, innerHeight * 0.9)' });
    await sleep(120);
  }
  await idle(reqs);
  const overflow = await evalJSON('JSON.stringify(document.documentElement.scrollWidth > innerWidth)');
  listeners.delete(onEvent);
  await send('Target.closeTarget', { targetId });

  const all = [...reqs.values()];
  const offsite = all.filter((r) => !r.url.startsWith(origin + '/') && !r.url.startsWith('data:')).map((r) => r.url);
  const failed = all.filter((r) => r.failed).map((r) => r.url);
  const doc = all.find((r) => r.type === 'Document')?.bytes ?? 0;
  const images = all.filter((r) => r.type === 'Image').reduce((n, r) => n + r.bytes, 0);
  return { ...sc, requests: all.length, firstScreen: above, total: sum(reqs), doc, images, offsite, failed, overflow, timing };
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
for (const r of results) {
  if (r.offsite.length) problems.push(`${r.name}: requests off 127.0.0.1: ${r.offsite.join(', ')}`);
  if (r.failed.length) problems.push(`${r.name}: failed requests: ${r.failed.join(', ')}`);
  if (r.total > BUDGET.page) problems.push(`${r.name}: ${kb(r.total)} in all, over the ${kb(BUDGET.page)} budget`);
  if (r.doc > BUDGET.doc) problems.push(`${r.name}: the page is ${kb(r.doc)} on the wire, over ${kb(BUDGET.doc)}`);
  if (r.overflow) problems.push(`${r.name}: the page scrolls sideways`);
}
const md = [
  '### Page weight (headless Chrome, local server with gzip, cache off)',
  '',
  `Budgets: the page with its CSS and JS ≤ ${kb(BUDGET.doc)}, a whole visit scrolled to the bottom ≤ ${kb(BUDGET.page)}, every request to the site itself.`,
  '',
  '| visit | requests | page (HTML+CSS+JS) | first screen | whole visit | images | FCP | LCP | load |',
  '|---|---|---|---|---|---|---|---|---|',
  ...results.map((r) => `| ${r.name} | ${r.requests} | ${kb(r.doc)} | ${kb(r.firstScreen)} | **${kb(r.total)}** | ${kb(r.images)} | ${ms(r.timing.fcp)} | ${ms(r.timing.lcp)} | ${ms(r.timing.load)} |`),
  '',
  'Timings are unthrottled on this machine; Lighthouse gives the throttled ones.',
  '',
  problems.length ? `**Problems:**\n\n${problems.map((p) => `- ${p}`).join('\n')}` : 'No problems: nothing left 127.0.0.1 and every visit is under budget.',
  '',
].join('\n');
mkdirSync(outDir, { recursive: true });
writeFileSync(join(outDir, 'report.json'), JSON.stringify({ origin, budget: BUDGET, results }, null, 2) + '\n');
writeFileSync(join(outDir, 'report.md'), md);
if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, md + '\n');
console.log(md);
if (problems.length) process.exit(1);

function die(m) { console.error(`weigh: ${m}`); process.exit(1); }
