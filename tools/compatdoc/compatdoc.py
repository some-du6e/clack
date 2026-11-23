#!/usr/bin/env python3
"""compatdoc.py [--index INDEX_JSON] [--out OUT.md]: docs/PLUGIN-COMPAT.md
(getclack.club/compat), every Taut and Slick plugin in the published Library
with how far it works in Clack, from internal/pluginlib/compat.go (its
broken()/partial() rows; every other plugin works) and the Library's
index.json (tools/compatdoc/index.json, the `plugins` release's, refreshed
when the Library is republished).

Run it from the repository root after a change to compat.go or index.json;
TestCompatDocInStep (internal/pluginlib) fails while the page is out of step."""
import argparse, json, re

ap = argparse.ArgumentParser()
ap.add_argument('--compat', default='internal/pluginlib/compat.go')
ap.add_argument('--index', default='tools/compatdoc/index.json')
ap.add_argument('--out', default='docs/PLUGIN-COMPAT.md')
args = ap.parse_args()

rows = {}
for m in re.finditer(r'(Slick|Taut)\s*\+\s*"/([^"]+)":\s*(broken|partial)\("((?:[^"\\]|\\.)*)"\)', open(args.compat).read()):
    rows[(m.group(1).lower(), m.group(2))] = (m.group(3), m.group(4).replace('\\"', '"'))

plugins = json.load(open(args.index))['plugins']
SYS = {'taut': 'Taut', 'slick': 'Slick'}
broken, partial, ok = [], [], []
for p in sorted(plugins, key=lambda p: (p['name'].lower(), p['system'])):
    st = rows.get((p['system'], p['id']))
    item = (p['name'], SYS[p['system']], p.get('description', '').strip(), st[1] if st else '')
    (broken if st and st[0] == 'broken' else partial if st else ok).append(item)


def esc(s):
    return s.replace('|', '\\|')


L = []
L.append('# Plugin compatibility in Clack')
L.append('')
L.append(f'Clack runs Taut\'s and Slick\'s plugins natively, from the published plugin Library ({len(plugins)} plugins: '
         f'{sum(p["system"] == "taut" for p in plugins)} from Taut, {sum(p["system"] == "slick" for p in plugins)} from Slick). '
         f'This page is how far each one works in Clack.')
L.append('')
L.append('| | Plugins |')
L.append('|---|---:|')
L.append(f'| ✅ Works fully | {len(ok)} |')
L.append(f'| ⚠️ Partly works | {len(partial)} |')
L.append(f"| ❌ Doesn't work yet | {len(broken)} |")
L.append('')
L.append('A plugin both systems ship (under the same name or not) is one row in Preferences › Plugins › Browse; here each version has its own line.')
L.append('Compatibility follows the newest Clack release. Each row comes from `internal/pluginlib/compat.go`, which `TestCompatMatchesDocs` keeps in step with the per-system pages, [PLUGINS-live-taut.md](PLUGINS-live-taut.md) and [PLUGINS-live-slick.md](PLUGINS-live-slick.md); `tools/compatdoc/compatdoc.py` writes this page from it and the Library\'s index, and `TestCompatDocInStep` keeps the two in step.')
for title, items, with_note in (("❌ Doesn't work yet", broken, True), ('⚠️ Partly works', partial, True), ('✅ Works fully', ok, False)):
    L.append('')
    L.append(f'## {title}')
    L.append('')
    if with_note:
        L.append('| Plugin | From | What it does | In Clack |')
        L.append('|---|---|---|---|')
        for n, s, d, note in items:
            note = re.sub(r"^(Doesn't work in Clack|Partly works): ", '', note)
            L.append(f'| {esc(n)} | {s} | {esc(d)} | {esc(note)} |')
    else:
        L.append('| Plugin | From | What it does |')
        L.append('|---|---|---|')
        for n, s, d, _ in items:
            L.append(f'| {esc(n)} | {s} | {esc(d)} |')
L.append('')
open(args.out, 'w').write('\n'.join(L))
print(args.out, 'broken', len(broken), 'partial', len(partial), 'ok', len(ok))
