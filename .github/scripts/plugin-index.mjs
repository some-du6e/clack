// Writes the plugin library: every Taut and Slick plugin bundle (built by
// upstream's own build in .github/workflows/plugin-library.yml) copied under
// a unique asset name, plus index.json describing them for Clack's
// Preferences > Taut / Slick > Library (internal/pluginlib).
//
//   node .github/scripts/plugin-index.mjs <taut checkout> <taut built dir> \
//        <slick checkout> <slick built dir> <out dir>
//
// Metadata comes from each bundle's static fields, read by running the
// bundle against stub $taut / $slick modules in a throwaway VM context (no
// network, no real globals). Authors and category labels are resolved with
// the upstream checkout's own tables. Nothing here is Taut's or Slick's code.

import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { copyFileSync, existsSync, readdirSync, readFileSync, statSync, writeFileSync, mkdirSync } from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import vm from 'node:vm';

const [tautRoot, tautBuilt, slickRoot, slickBuilt, outDir] = process.argv.slice(2).map((p) => p && path.resolve(p));
if (!outDir) {
  console.error('usage: plugin-index.mjs <taut checkout> <taut built> <slick checkout> <slick built> <out dir>');
  process.exit(2);
}
mkdirSync(outDir, { recursive: true });

const sha256 = (buf) => createHash('sha256').update(buf).digest('hex');
const git = (root, ...args) => execFileSync('git', ['-C', root, ...args], { encoding: 'utf8' }).trim();
const str = (v) => (typeof v === 'string' ? v : '');
// the people who committed to a plugin's source, most commits first (credit when it names no authors)
const contributors = (root, rel) => {
  if (!rel) return [];
  try {
    return git(root, 'shortlog', '-sn', '--no-merges', 'HEAD', '--', rel)
      .split('\n')
      .map((l) => l.replace(/^\s*\d+\s+/, '').trim())
      .filter((n) => n && !/\[bot\]$/.test(n))
      .slice(0, 5);
  } catch {
    return [];
  }
};

// a value that tolerates any use at module top level (React.createContext, window.x, …)
function stub(name = 'stub') {
  const f = function () {
    return stub(name + '()');
  };
  return new Proxy(f, {
    get: (_t, k) => (k === Symbol.toPrimitive ? () => '' : k === 'then' ? undefined : stub(name + '.' + String(k))),
    apply: () => stub(name + '()'),
    construct: () => stub('new ' + name),
    has: () => true,
  });
}
function sandbox(extra) {
  const g = { console: { log() {}, info() {}, warn() {}, error() {}, debug() {} }, ...extra };
  g.globalThis = g;
  g.window = g;
  g.self = g;
  return vm.createContext(
    new Proxy(g, {
      get: (t, k) => (k in t ? t[k] : typeof k === 'string' && k in globalThis && /^[A-Z]/.test(k) ? globalThis[k] : stub(String(k))),
      has: () => true,
    }),
  );
}

function asset(src, name) {
  const buf = readFileSync(src);
  copyFileSync(src, path.join(outDir, name));
  return { file: name, sha256: sha256(buf), size: buf.length };
}

const plugins = [];
const problems = [];

// ---- Taut ----
const tautCommit = git(tautRoot, 'rev-parse', 'HEAD');
const tautAuthors = (await import(pathToFileURL(path.join(tautRoot, 'shared/authors.ts')).href)).AUTHORS ?? {};
const tautCats = { messages: 'Messages', messageBox: 'Message Box', people: 'People', privacy: 'Privacy', app: 'App', fun: 'Fun' };
try {
  const src = readFileSync(path.join(tautRoot, 'shared/Plugin.ts'), 'utf8');
  const m = /PLUGIN_CATEGORIES\s*=\s*\{([^}]*)\}/.exec(src);
  if (m) for (const [, k, v] of m[1].matchAll(/(\w+)\s*:\s*'([^']*)'/g)) tautCats[k] = v;
} catch {}
const tautAuthor = (a) => {
  if (typeof a === 'string') return tautAuthors[a] ? { name: tautAuthors[a].name, ...(tautAuthors[a].url ? { url: tautAuthors[a].url } : {}) } : { name: a };
  if (a && typeof a === 'object' && a.name) return { name: String(a.name), ...(a.url ? { url: String(a.url) } : {}) };
  return null;
};
const tautSources = new Map(
  readdirSync(path.join(tautRoot, 'plugins'))
    .filter((f) => /\.(ts|tsx)$/.test(f))
    .map((f) => [f.replace(/\.tsx?$/, ''), `plugins/${f}`]),
);
for (const f of readdirSync(tautBuilt).filter((f) => f.endsWith('.js')).sort()) {
  const base = f.replace(/\.js$/, '');
  const code = readFileSync(path.join(tautBuilt, f), 'utf8');
  let C;
  try {
    class TautPlugin {}
    C = vm.runInContext(code, sandbox({ TautPlugin, TautOpt: stub('opt'), React: stub('React') }), { timeout: 5000 });
  } catch (err) {
    problems.push(`taut ${f}: ${err?.message ?? err}`);
  }
  const id = str(C?.id) || base.replace(/\.disabled$/, '');
  const rawAuthors = typeof C?.authors === 'string' ? [C.authors] : Array.isArray(C?.authors) ? C.authors : [];
  const cfg = C?.defaultConfig && typeof C.defaultConfig === 'object' ? C.defaultConfig : {};
  const category = str(C?.category) || 'other';
  plugins.push({
    system: 'taut',
    id,
    name: str(C?.pluginName) || id,
    description: str(C?.description),
    category,
    categoryLabel: tautCats[category] ?? 'Other',
    authors: rawAuthors.map(tautAuthor).filter(Boolean),
    hackClubOnly: C?.hackClubOnly === true,
    defaultEnabled: cfg.enabled === true || cfg.enabled?.value === true,
    upstreamDisabled: base.endsWith('.disabled'),
    source: tautSources.get(base) ?? '',
    contributors: contributors(tautRoot, tautSources.get(base)),
    ...asset(path.join(tautBuilt, f), `taut-${base}.js`),
  });
}

// ---- Slick ----
const slickCommit = git(slickRoot, 'rev-parse', 'HEAD');
const slickCats = {};
try {
  const src = readFileSync(path.join(slickRoot, 'src/shared/settings.ts'), 'utf8');
  const m = /PLUGIN_CATEGORIES\s*=\s*\{([\s\S]*?)\}\s*(as const)?;/.exec(src);
  if (m) for (const [, k, v] of m[1].matchAll(/['"]?(\w+)['"]?\s*:\s*['"]([^'"]*)['"]/g)) slickCats[k] = v;
} catch {}
const manifest = existsSync(path.join(slickBuilt, 'manifest.json')) ? JSON.parse(readFileSync(path.join(slickBuilt, 'manifest.json'), 'utf8')) : {};
for (const f of readdirSync(slickBuilt).filter((f) => f.endsWith('.js') && !f.endsWith('.main.js') && !f.endsWith('.user.js')).sort()) {
  const base = f.replace(/\.js$/, '');
  if (!statSync(path.join(slickBuilt, f)).isFile()) continue;
  const code = readFileSync(path.join(slickBuilt, f), 'utf8');
  let C;
  try {
    class SlickPlugin {}
    const queue = [];
    vm.runInContext(code, sandbox({ __slickPlugins: queue, React: stub('React'), ReactDOM: stub('ReactDOM') }), { timeout: 5000 });
    const req = (n) => (n === '$slick' ? { __esModule: true, SlickPlugin, default: SlickPlugin, PLUGIN_APIS: [] } : stub(`require(${n})`));
    C = queue[0]?.(req);
  } catch (err) {
    problems.push(`slick ${f}: ${err?.message ?? err}`);
  }
  const id = str(C?.id) || base;
  const category = str(C?.category) || 'other';
  const authors = str(C?.authors)
    .split(/\s*,\s*|\s+and\s+/)
    .filter(Boolean)
    .map((name) => ({ name }));
  const mainPath = path.join(slickBuilt, `${base}.main.js`);
  plugins.push({
    system: 'slick',
    id,
    name: str(C?.pluginName) || id,
    description: str(C?.description),
    category,
    categoryLabel: slickCats[category] ?? (category === 'other' ? 'Other' : category),
    authors,
    defaultEnabled: C?.defaultEnabled === true,
    build: manifest[base]?.renderer ?? '',
    source: `src/plugins/${base}/`,
    contributors: contributors(slickRoot, `src/plugins/${base}`),
    ...asset(path.join(slickBuilt, f), `slick-${base}.js`),
    ...(existsSync(mainPath) ? { main: asset(mainPath, `slick-${base}.main.js`) } : {}),
  });
}

// ---- licenses, index, notes ----
copyFileSync(path.join(tautRoot, 'LICENSE'), path.join(outDir, 'taut-LICENSE'));
copyFileSync(path.join(slickRoot, existsSync(path.join(slickRoot, 'LICENSE')) ? 'LICENSE' : 'LICENSE.md'), path.join(outDir, 'slick-LICENSE'));

const index = {
  schema: 1,
  generated: new Date().toISOString(),
  sources: {
    taut: { name: 'Taut', repo: 'https://github.com/jeremy46231/taut', commit: tautCommit, license: 'GPL-3.0', licenseFile: 'taut-LICENSE', build: 'npm run build:user-plugin' },
    slick: { name: 'Slick', repo: 'https://github.com/3kh0/slick', commit: slickCommit, license: 'GPL-3.0', licenseFile: 'slick-LICENSE', build: 'internal/slick/build/build.mjs (Slick build:plugin / page build)' },
  },
  plugins,
};
writeFileSync(path.join(outDir, 'index.json'), JSON.stringify(index, null, 1) + '\n');

const line = (p) => {
  const s = index.sources[p.system];
  return `| ${p.system === 'taut' ? 'Taut' : 'Slick'} | ${p.name} (\`${p.id}\`) | ${(p.authors.length ? p.authors.map((a) => a.name) : p.contributors).join(', ')} | [${p.source}](${s.repo}/tree/${s.commit}/${p.source}) | \`${p.sha256.slice(0, 12)}\` |`;
};
const notes = `Official Taut and Slick plugins for Clack's plugin Library (Preferences > Taut > Library, Preferences > Slick > Library).

Each bundle was built unmodified by its project's own build from the upstream source below. They are third-party code, licensed **GPLv3** by their authors (see \`taut-LICENSE\` and \`slick-LICENSE\`). The corresponding source is the upstream repository at the listed commit; Clack doesn't change it.

- **Taut** (Jeremy and contributors): ${index.sources.taut.repo} at [\`${tautCommit}\`](${index.sources.taut.repo}/tree/${tautCommit}), built with \`npm run build:user-plugin\`.
- **Slick** (3kh0 and contributors): ${index.sources.slick.repo} at [\`${slickCommit}\`](${index.sources.slick.repo}/tree/${slickCommit}), built with Slick's build (\`build:plugin\`, or its page build for plugins with a privileged half).

\`index.json\` lists every plugin with its upstream repo, commit, source path and sha256. Generated ${index.generated}.

| from | plugin | authors | source | sha256 |
|---|---|---|---|---|
${plugins.map(line).join('\n')}
`;
writeFileSync(path.join(outDir, 'RELEASE_NOTES.md'), notes);

console.log(`library: ${plugins.filter((p) => p.system === 'taut').length} Taut + ${plugins.filter((p) => p.system === 'slick').length} Slick plugins (taut ${tautCommit.slice(0, 7)}, slick ${slickCommit.slice(0, 7)})`);
for (const p of problems) console.log(`::warning::metadata: ${p}`);
const bad = plugins.filter((p) => !p.name || !p.description);
for (const p of bad) console.log(`::warning::${p.system} ${p.id}: missing name or description`);
