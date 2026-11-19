#!/usr/bin/env node
// Packs the web demo for a static host, so a redeploy can never mix two
// builds:
//   - main.wasm is split into parts of at most 24 MiB (Cloudflare's static
//     assets take 25 MiB a file) and moved, with the page's scripts
//     (wasm_exec.js, fs.js, boot.js), into p/<hash>/, where <hash> is the
//     first 12 hex digits of a SHA-256 over all of them. build.mjs's
//     _headers serves /demo/p/* as immutable for a year.
//   - index.html, the one entry point (it revalidates on every visit),
//     points at those scripts and carries the manifest inline (the parts
//     and their sizes), which boot.js reads.
//   - build.json is the same manifest, for the record and the checks.
// It prints the sizes: as built, gzipped (-9) and brotli (quality 11), the
// way a host can send them.
//
//   node tools/demo-pack.mjs <dist/demo> [commit]
//
// Node built-ins only.

import { createHash } from 'node:crypto';
import { mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { brotliCompressSync, gzipSync, constants as zc } from 'node:zlib';

const PART = 24 * 1024 * 1024;
const SCRIPTS = ['wasm_exec.js', 'fs.js', 'boot.js'];

const [dir, commit = ''] = process.argv.slice(2);
if (!dir) {
  console.error('usage: node tools/demo-pack.mjs <dist/demo> [commit]');
  process.exit(2);
}
const wasm = readFileSync(join(dir, 'main.wasm'));
const h = createHash('sha256');
for (const b of [wasm, ...SCRIPTS.map((f) => readFileSync(join(dir, f)))]) h.update(b).update('\0');
const hash = h.digest('hex').slice(0, 12);
const p = `p/${hash}`;
mkdirSync(join(dir, p), { recursive: true });
const parts = [];
for (let at = 0, i = 0; at < wasm.length; at += PART, i++) {
  const file = `${p}/main.${i}.wasm`;
  const b = wasm.subarray(at, Math.min(at + PART, wasm.length));
  writeFileSync(join(dir, file), b);
  parts.push({ file, size: b.length });
}
rmSync(join(dir, 'main.wasm'));
for (const f of SCRIPTS) renameSync(join(dir, f), join(dir, p, f));

const size = (b) => ({
  raw: b.length,
  gzip: gzipSync(b, { level: 9 }).length,
  brotli: brotliCompressSync(b, { params: { [zc.BROTLI_PARAM_QUALITY]: 11, [zc.BROTLI_PARAM_LGWIN]: 24, [zc.BROTLI_PARAM_SIZE_HINT]: b.length } }).length,
});
const w = size(wasm);
const build = { wasm: w.raw, gzip: w.gzip, brotli: w.brotli, hash, parts, commit };
writeFileSync(join(dir, 'build.json'), JSON.stringify(build) + '\n');

// the page: its scripts under p/<hash>/, and the manifest inline
let page = readFileSync(join(dir, 'index.html'), 'utf8');
for (const f of SCRIPTS) {
  const tag = `<script src="${f}"></script>`;
  if (!page.includes(tag)) throw new Error(`index.html has no ${tag}`);
  page = page.replace(tag, `<script src="${p}/${f}"></script>`);
}
const first = `<script src="${p}/${SCRIPTS[1]}"></script>`;
page = page.replace(first, `<script type="application/json" id="build">${JSON.stringify(build)}</script>\n${first}`);
writeFileSync(join(dir, 'index.html'), page);

const x = size(readFileSync(join(dir, p, 'wasm_exec.js')));
const mb = (n) => `${(n / 1048576).toFixed(2)} MB`;
console.log(['### Web demo', '',
  `${p}/: main.wasm in ${parts.length} part${parts.length > 1 ? 's' : ''} (${parts.map((q) => q.file.split('/').pop()).join(', ')}) and the page's scripts.`, '',
  '| file | as built | gzip -9 | brotli 11 |', '|---|---|---|---|',
  `| main.wasm | ${mb(w.raw)} | ${mb(w.gzip)} | ${mb(w.brotli)} |`,
  `| wasm_exec.js | ${mb(x.raw)} | ${mb(x.gzip)} | ${mb(x.brotli)} |`, ''].join('\n'));
