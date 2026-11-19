#!/usr/bin/env node
// The web demo in a real browser: headless Chrome (WebGL on SwiftShader,
// as a machine without a GPU has it) opens dist/demo/?probe from a local
// server and checks, in order, that
//   1. the program starts and Clack draws its first frame,
//   2. the channel list draws (the sidebar's channel names are among the
//      strings Clack drew, clackDemo.drawn()),
//   3. a message typed into the composer and sent with Enter reaches the
//      fake (clackDemo.lastPost on #general),
// and, all along, that the console has no errors, the page throws nothing,
// no request goes anywhere but the local server, and the program doesn't
// stop. It saves a screenshot after each step and report.json with the
// timings. CHROME_PATH picks Chrome.
//
//   node tools/demo-test.mjs [dist] [--out demo-test]
//
// Node 22+ (WebSocket), no packages.

import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { dirname, extname, join, normalize, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseArgs } from 'node:util';

const site = join(dirname(fileURLToPath(import.meta.url)), '..');
const { values: opt, positionals } = parseArgs({ allowPositionals: true, options: { out: { type: 'string' } } });
const dist = resolve(positionals[0] ?? join(site, 'dist'));
const out = resolve(opt.out ?? join(site, 'demo-test'));
const W = 1280, H = 800;
const TEXT = 'Hello from the browser demo';

const TYPES = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.json': 'application/json', '.svg': 'image/svg+xml',
  '.wasm': 'application/wasm', '.woff2': 'font/woff2', '.png': 'image/png', '.webp': 'image/webp', '.avif': 'image/avif',
};
const server = createServer((req, res) => {
  let p = decodeURIComponent(new URL(req.url, 'http://x').pathname);
  if (p.endsWith('/')) p += 'index.html';
  const f = normalize(join(dist, p));
  if (!f.startsWith(dist) || !existsSync(f)) { res.writeHead(404); return res.end(); }
  res.writeHead(200, { 'Content-Type': TYPES[extname(f)] || 'application/octet-stream', 'Cache-Control': 'no-store' });
  res.end(readFileSync(f));
});
await new Promise((r) => server.listen(0, '127.0.0.1', r));
const origin = `http://127.0.0.1:${server.address().port}`;

const chromePath = process.env.CHROME_PATH
  || ['/usr/bin/google-chrome', '/usr/bin/google-chrome-stable', '/usr/bin/chromium', '/usr/bin/chromium-browser'].find(existsSync);
if (!chromePath) die('no Chrome found; set CHROME_PATH');
const profile = mkdtempSync(join(tmpdir(), 'clack-demo-test-'));
const chrome = spawn(chromePath, [
  '--headless=new', '--no-first-run', '--no-default-browser-check', '--disable-extensions', '--hide-scrollbars',
  '--disable-background-networking', '--disable-component-update', '--disable-sync', '--no-sandbox', '--no-proxy-server',
  '--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist',
  `--window-size=${W},${H}`, `--user-data-dir=${profile}`, '--remote-debugging-port=0', 'about:blank',
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
const report = { url: `${origin}/demo/?probe`, steps: [], console: [], errors: [], requests: [] };
let failed;
try {
  await run();
} catch (e) {
  failed = e;
} finally {
  writeFileSync(join(out, 'report.json'), JSON.stringify(report, null, 2) + '\n');
  ws.close();
  chrome.kill('SIGTERM');
  server.close();
  await sleep(300);
  rmSync(profile, { recursive: true, force: true });
}
const problems = [...report.errors];
if (failed) problems.unshift(failed.message);
const md = ['### Web demo in headless Chrome', '', '| step | after |', '|---|---|',
  ...report.steps.map((s) => `| ${s.step} | ${(s.ms / 1000).toFixed(1)} s |`), '',
  `Requests: ${report.requests.length}, all to the local server: ${report.requests.every((u) => u.startsWith(origin + '/')) ? 'yes' : 'no'}.`, '',
  problems.length ? `**Problems:**\n\n${problems.map((p) => `- ${p}`).join('\n')}` : 'No problems.', ''].join('\n');
writeFileSync(join(out, 'report.md'), md);
if (process.env.GITHUB_STEP_SUMMARY) writeFileSync(process.env.GITHUB_STEP_SUMMARY, md + '\n', { flag: 'a' });
console.log(md);
process.exit(problems.length ? 1 : 0);

async function run() {
  const { targetId } = await send('Target.createTarget', { url: 'about:blank' });
  const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true });
  const s = (m, p) => send(m, p, sessionId);
  listeners.add((m) => {
    if (m.sessionId !== sessionId) return;
    const p = m.params;
    if (m.method === 'Runtime.consoleAPICalled') {
      const text = p.args.map((a) => (a.value !== undefined ? String(a.value) : a.description || '')).join(' ');
      report.console.push(`${p.type}: ${text}`);
      if (p.type === 'error' || p.type === 'assert') report.errors.push(`console.${p.type}: ${text}`);
      if (/\bpanic:|fatal error:/.test(text)) report.errors.push(`the program crashed: ${text}`);
    } else if (m.method === 'Runtime.exceptionThrown') {
      const d = p.exceptionDetails;
      report.errors.push(`exception: ${(d.exception && d.exception.description) || d.text}`);
    } else if (m.method === 'Log.entryAdded' && p.entry.level === 'error') {
      report.errors.push(`log: ${p.entry.text}${p.entry.url ? ` (${p.entry.url})` : ''}`);
    } else if (m.method === 'Network.requestWillBeSent') {
      const u = p.request.url;
      report.requests.push(u);
      if (!u.startsWith(origin + '/') && !u.startsWith('data:') && !u.startsWith('blob:')) report.errors.push(`a request off the local server: ${u}`);
    } else if (m.method === 'Network.webSocketCreated') {
      report.errors.push(`a WebSocket from the page: ${p.url}`);
    } else if (m.method === 'Network.loadingFailed' && !p.canceled) {
      report.errors.push(`a request failed: ${p.errorText}`);
    }
  });
  for (const d of ['Runtime', 'Log', 'Network', 'Page']) await s(`${d}.enable`);
  await s('Emulation.setDeviceMetricsOverride', { width: W, height: H, deviceScaleFactor: 1, mobile: false });
  const ev = async (expr) => (await s('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true })).result.value;
  const shot = async (name) => {
    const { data } = await s('Page.captureScreenshot', { format: 'png' });
    writeFileSync(join(out, `${name}.png`), Buffer.from(data, 'base64'));
  };
  const t0 = Date.now();
  const step = (name) => { report.steps.push({ step: name, ms: Date.now() - t0 }); console.log(`demo-test: ${name} after ${((Date.now() - t0) / 1000).toFixed(1)} s`); };
  const until = async (what, ms, expr) => {
    for (const end = Date.now() + ms; Date.now() < end; await sleep(250)) {
      if (report.errors.length) throw new Error(`${what}: stopped by ${report.errors[0]}`);
      if (await ev(`(() => { try { return !!(${expr}); } catch { return false; } })()`)) return;
      if (await ev('document.querySelector(".card.stopped") !== null')) throw new Error(`${what}: the page says "${await ev('document.getElementById("state").textContent')}"`);
    }
    await shot(`timeout-${what.replace(/\W+/g, '-')}`);
    throw new Error(`${what}: not within ${ms / 1000} s`);
  };

  await s('Page.navigate', { url: report.url });
  await until('first frame', 180000, 'document.documentElement.classList.contains("ready")');
  step('first frame');
  await until('channel list', 120000, '["general", "random", "it-help"].every((n) => clackDemo.drawn().includes(n))');
  step('channel list');
  await shot('1-channel-list');

  // the composer: a click in its text area (the bottom of the message
  // pane, under #general), then text and Enter as a keyboard sends them
  await until('the composer', 60000, 'clackDemo.drawn().some((s) => s.startsWith("Message #general"))');
  const click = async (x, y) => {
    for (const type of ['mouseMoved', 'mousePressed', 'mouseReleased']) {
      await s('Input.dispatchMouseEvent', { type, x, y, button: type === 'mouseMoved' ? 'none' : 'left', clickCount: 1 });
    }
  };
  await click(Math.round((400 + W) / 2), H - 90);
  await sleep(500);
  await s('Input.insertText', { text: TEXT });
  await sleep(500);
  await shot('2-typed');
  for (const type of ['keyDown', 'keyUp']) {
    await s('Input.dispatchKeyEvent', { type, key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13, nativeVirtualKeyCode: 13, ...(type === 'keyDown' ? { text: '\r' } : {}) });
  }
  await until('the message at the fake', 30000, `(clackDemo.lastPost(clackDemo.general) || {}).text === ${JSON.stringify(TEXT)}`);
  step('message sent');
  await sleep(1500);
  await shot('3-sent');
  if (await ev('document.querySelector(".card.stopped") !== null')) throw new Error('the program stopped');
  report.wasm = await ev(`(() => { const e = performance.getEntriesByType('resource').find((r) => r.name.endsWith('/main.wasm')); return e ? { bytes: e.decodedBodySize, ms: Math.round(e.duration) } : null; })()`);
}

function die(m) { console.error(`demo-test: ${m}`); process.exit(1); }
