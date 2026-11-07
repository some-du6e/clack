// Builds Slick plugins into the files Clack imports, with Slick's own build.
//
//   node internal/slick/build/build.mjs <slick checkout> <out dir> [PluginDir…]
//
// For each src/plugins/<Id>/ (or the given plugin folders):
//   <Id>.js       the renderer, from Slick's `build:plugin` (scripts/build/plugin.ts
//                 buildUserPlugin, exactly what Slick users share). Slick's own
//                 first-party plugins that `build:plugin` refuses (they have a
//                 main.ts, or import Slick's shared code) are built by the same
//                 page build options (scripts/build/app.ts pageBuildOptions and
//                 pagePlugins) into the same file format, with $slick left to
//                 the host.
//   <Id>.main.js  the privileged half (main.ts), bundled like Slick's desktop
//                 build bundles main halves (node platform, electron external),
//                 queued on globalThis.__slickMainPlugins.
//   manifest.json how each file was built.
//
// Nothing here is Slick's code; it drives Slick's build from a Slick checkout.

import { createRequire } from 'node:module';
import { existsSync, readdirSync, statSync } from 'node:fs';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const [slickRoot, outDir, ...only] = process.argv.slice(2);
if (!slickRoot || !outDir) {
  console.error('usage: build.mjs <slick checkout> <out dir> [plugin dirs…]');
  process.exit(2);
}
const root = path.resolve(slickRoot);
const out = path.resolve(outDir);
const require = createRequire(path.join(root, 'package.json'));
const { build } = require('esbuild');
const { buildUserPlugin } = await import(pathToFileURL(path.join(root, 'scripts/build/plugin.ts')).href);
const { pageBuildOptions, pagePlugins } = await import(pathToFileURL(path.join(root, 'scripts/build/app.ts')).href);

const pluginsDir = path.join(root, 'src', 'plugins');
const dirs = (only.length ? only.map((d) => path.resolve(d)) : readdirSync(pluginsDir).map((d) => path.join(pluginsDir, d))).filter(
  (d) => statSync(d).isDirectory() && !path.basename(d).startsWith('.') && ['index.tsx', 'index.ts'].some((f) => existsSync(path.join(d, f))),
);

await mkdir(out, { recursive: true });

const wrap = (queue, text) =>
  `(globalThis.${queue} ??= []).push((require) => {\nvar module = { exports: {} }, exports = module.exports;\n${text}return module.exports.default;\n});\n`;

// $slick is provided by the host, like build:plugin does
const slickExternal = {
  name: 'clack-slick-external',
  setup(b) {
    b.onResolve({ filter: /^\$slick$/ }, () => ({ path: '$slick', external: true }));
  },
};

async function pageBuild(dir, id) {
  const entry = ['index.tsx', 'index.ts'].map((f) => path.join(dir, f)).find((f) => existsSync(f));
  const { outputFiles } = await build({
    ...(await pageBuildOptions({})),
    absWorkingDir: dir,
    entryPoints: [entry],
    format: 'cjs',
    minify: false,
    plugins: [slickExternal, ...pagePlugins],
  });
  const code = wrap('__slickPlugins', outputFiles[0].text);
  await writeFile(path.join(out, `${id}.js`), `// slick-plugin ${JSON.stringify({ id })}\n${code}\n//# sourceURL=slick:///plugins/${id}.js\n`);
}

async function mainBuild(dir, id) {
  const { outputFiles } = await build({
    absWorkingDir: root,
    entryPoints: [path.join(dir, 'main.ts')],
    bundle: true,
    write: false,
    platform: 'node',
    format: 'cjs',
    target: 'es2022',
    minify: false,
    external: ['electron'],
    loader: { '.gif': 'dataurl', '.png': 'dataurl', '.svg': 'dataurl', '.woff2': 'dataurl', '.txt': 'text' },
    define: { __SLICK_VERSION__: '"clack"', __SLICK_BUILD__: '0', __SLICK_LOADER__: '"electron"' },
    plugins: [slickExternal, ...pagePlugins.filter((p) => p.name !== 'slick-shared-alias')],
  });
  // Node's globals, from the host (a main half runs in Electron's main process)
  const globals = 'var process = require("node:process"), Buffer = require("node:buffer").Buffer;\n';
  await writeFile(path.join(out, `${id}.main.js`), `// slick-main ${JSON.stringify({ id })}\n${wrap('__slickMainPlugins', globals + outputFiles[0].text)}`);
}

const manifest = {};
let failed = 0;
for (const dir of dirs) {
  const id = path.basename(dir);
  const record = (manifest[id] = { renderer: null, main: false, errors: [] });
  try {
    await buildUserPlugin(dir, path.join(out, `${id}.js`));
    record.renderer = 'build:plugin';
  } catch (error) {
    record.refused = String(error?.message ?? error).split('\n')[0];
    try {
      await pageBuild(dir, id);
      record.renderer = 'page-build';
    } catch (error2) {
      record.errors.push(`renderer: ${error2?.message ?? error2}`);
      failed++;
    }
  }
  if (existsSync(path.join(dir, 'main.ts'))) {
    try {
      await mainBuild(dir, id);
      record.main = true;
    } catch (error) {
      record.errors.push(`main: ${error?.message ?? error}`);
      failed++;
    }
  }
  console.log(`${id}: ${record.renderer ?? 'FAILED'}${record.main ? ' + main' : ''}${record.refused ? ` (build:plugin: ${record.refused})` : ''}`);
}
await writeFile(path.join(out, 'manifest.json'), JSON.stringify(manifest, null, 2));
console.log(`built ${dirs.length - failed}/${dirs.length} plugins into ${out}`);
if (failed) process.exitCode = 1;
