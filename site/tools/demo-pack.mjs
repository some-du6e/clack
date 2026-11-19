#!/usr/bin/env node
// Packs the web demo's program for a static host: splits main.wasm into
// parts of at most 24 MiB (main.0.wasm, main.1.wasm, …; Cloudflare's
// static assets take 25 MiB a file), writes build.json (the page reads
// the parts and the total size from it, to stream them into one module
// and show the download's progress), removes main.wasm, and prints the
// sizes: as built, gzipped (-9) and brotli (quality 11), the way a host
// can send them.
//
//   node tools/demo-pack.mjs <dist/demo> [commit]
//
// Node built-ins only.

import { readFileSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { brotliCompressSync, gzipSync, constants as zc } from 'node:zlib';

const PART = 24 * 1024 * 1024;

const [dir, commit = ''] = process.argv.slice(2);
if (!dir) {
  console.error('usage: node tools/demo-pack.mjs <dist/demo> [commit]');
  process.exit(2);
}
const wasm = readFileSync(join(dir, 'main.wasm'));
const parts = [];
for (let at = 0, i = 0; at < wasm.length; at += PART, i++) {
  const file = `main.${i}.wasm`;
  const b = wasm.subarray(at, Math.min(at + PART, wasm.length));
  writeFileSync(join(dir, file), b);
  parts.push({ file, size: b.length });
}
rmSync(join(dir, 'main.wasm'));
const size = (b) => ({
  raw: b.length,
  gzip: gzipSync(b, { level: 9 }).length,
  brotli: brotliCompressSync(b, { params: { [zc.BROTLI_PARAM_QUALITY]: 11, [zc.BROTLI_PARAM_LGWIN]: 24, [zc.BROTLI_PARAM_SIZE_HINT]: b.length } }).length,
});
const w = size(wasm);
const x = size(readFileSync(join(dir, 'wasm_exec.js')));
writeFileSync(join(dir, 'build.json'), JSON.stringify({ wasm: w.raw, gzip: w.gzip, brotli: w.brotli, parts, commit }) + '\n');
const mb = (n) => `${(n / 1048576).toFixed(2)} MB`;
console.log(['### Web demo', '',
  `main.wasm in ${parts.length} part${parts.length > 1 ? 's' : ''} (${parts.map((p) => p.file).join(', ')}).`, '',
  '| file | as built | gzip -9 | brotli 11 |', '|---|---|---|---|',
  `| main.wasm | ${mb(w.raw)} | ${mb(w.gzip)} | ${mb(w.brotli)} |`,
  `| wasm_exec.js | ${mb(x.raw)} | ${mb(x.gzip)} | ${mb(x.brotli)} |`, ''].join('\n'));
