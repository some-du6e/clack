#!/usr/bin/env node
// Writes the web demo's build.json next to its program and prints the
// sizes: main.wasm and wasm_exec.js as they are, gzipped (-9) and brotli
// (quality 11), the way a host would send them. The page reads build.json
// for main.wasm's size, to show the download's progress.
//
//   node tools/demo-size.mjs <dist/demo> [commit]
//
// Node built-ins only.

import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { brotliCompressSync, gzipSync, constants as zc } from 'node:zlib';

const [dir, commit = ''] = process.argv.slice(2);
if (!dir) {
  console.error('usage: node tools/demo-size.mjs <dist/demo> [commit]');
  process.exit(2);
}
const sizes = {};
for (const f of ['main.wasm', 'wasm_exec.js']) {
  const b = readFileSync(join(dir, f));
  sizes[f] = {
    raw: b.length,
    gzip: gzipSync(b, { level: 9 }).length,
    brotli: brotliCompressSync(b, { params: { [zc.BROTLI_PARAM_QUALITY]: 11, [zc.BROTLI_PARAM_LGWIN]: 24, [zc.BROTLI_PARAM_SIZE_HINT]: b.length } }).length,
  };
}
const w = sizes['main.wasm'];
writeFileSync(join(dir, 'build.json'), JSON.stringify({ wasm: w.raw, gzip: w.gzip, brotli: w.brotli, commit }) + '\n');
const mb = (n) => `${(n / 1048576).toFixed(2)} MB`;
console.log(['### Web demo', '', '| file | raw | gzip -9 | brotli 11 |', '|---|---|---|---|',
  ...Object.entries(sizes).map(([f, s]) => `| ${f} | ${mb(s.raw)} | ${mb(s.gzip)} | ${mb(s.brotli)} |`), ''].join('\n'));
