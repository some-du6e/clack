#!/usr/bin/env python3
"""compatdoc.py [--compat COMPAT_GO] [--index INDEX_JSON] [--out OUT_MD]:
docs/PLUGIN-COMPAT.md (getclack.club/compat), every Taut and Slick plugin
in the published Library with how far it works in Clack, from
internal/pluginlib/compat.go (its broken()/partial() rows; every other
plugin works) and tools/compatdoc/index.json (the `plugins` release's
index, refreshed when the Library is republished).

TestCompatDocInStep (internal/pluginlib) is the source of truth: it renders
the page from compat.go's map and fails while the file differs, and

    go test ./internal/pluginlib -run TestCompatDocInStep -update

writes it. This script writes the same page where go test can't run. It
stops, saying which line, at anything in compat.go's map it can't read,
rather than leave a plugin out. Run it from the repository root."""
import argparse
import json
import re
import string
import sys

ap = argparse.ArgumentParser()
ap.add_argument('--compat', default='internal/pluginlib/compat.go')
ap.add_argument('--index', default='tools/compatdoc/index.json')
ap.add_argument('--out', default='docs/PLUGIN-COMPAT.md')
args = ap.parse_args()

GO_TEST = 'go test ./internal/pluginlib -run TestCompatDocInStep -update'


def fail(msg):
    sys.exit(f'compatdoc.py: {msg}\n(the page can always be written with {GO_TEST})')


ESCAPES = {'a': '\a', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v', '\\': '\\', '"': '"'}


def go_unquote(body, where):
    """the value of a Go interpreted string literal's body (between the quotes)"""
    out, i = [], 0
    while i < len(body):
        c = body[i]
        if c != '\\':
            out.append(c)
            i += 1
            continue
        e = body[i + 1] if i + 1 < len(body) else ''
        if e in ESCAPES:
            out.append(ESCAPES[e])
            i += 2
        elif e in ('u', 'U'):
            n = 4 if e == 'u' else 8
            h = body[i + 2:i + 2 + n]
            if len(h) != n or any(ch not in string.hexdigits for ch in h):
                fail(f'{where}: a bad \\{e} escape')
            out.append(chr(int(h, 16)))
            i += 2 + n
        else:
            fail(f'{where}: an escape this script can\'t read: \\{e}')
    return ''.join(out)


# compat.go's map: every line in it is a row, a comment or blank
ENTRY = re.compile(r'^\t(Slick|Taut) \+ "/([^"\\]+)":\s*(broken|partial)\("((?:[^"\\]|\\.)*)"\),(\s*//.*)?$')
rows = {}
with open(args.compat, encoding='utf-8', newline='') as f:
    lines = f.read().split('\n')
start = next((i for i, l in enumerate(lines) if l.startswith('var compat = map[string]Compat{')), None)
if start is None:
    fail(f"{args.compat}: no 'var compat = map[string]Compat{{' block")
end = None
for i in range(start + 1, len(lines)):
    line = lines[i].rstrip('\r')
    where = f'{args.compat}:{i + 1}'
    if line == '}':
        end = i
        break
    if not line.strip() or line.strip().startswith('//'):
        continue
    m = ENTRY.match(line)
    if not m:
        fail(f'{where}: an entry this script can\'t read: {line.strip()}')
    key = (m.group(1).lower(), m.group(2))
    if key in rows:
        fail(f'{where}: {m.group(1)}/{m.group(2)} twice')
    rows[key] = (m.group(3), go_unquote(m.group(4), where))
if end is None:
    fail(f"{args.compat}: the compat map doesn't end")

with open(args.index, encoding='utf-8') as f:
    plugins = json.load(f)['plugins']
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
L.append('Compatibility follows the newest Clack release. Each row comes from `internal/pluginlib/compat.go`, which `TestCompatMatchesDocs` keeps in step with the per-system pages, '
         '[PLUGINS-live-taut.md](PLUGINS-live-taut.md) and [PLUGINS-live-slick.md](PLUGINS-live-slick.md). '
         f'`{GO_TEST}` writes this page from it and the Library\'s index (`tools/compatdoc/index.json`), and the test fails while the page is out of step.')
for title, items, with_note in (("❌ Doesn't work yet", broken, True), ('⚠️ Partly works', partial, True), ('✅ Works fully', ok, False)):
    L.append('')
    L.append(f'## {title}')
    L.append('')
    if with_note:
        L.append('| Plugin | From | What it does | In Clack |')
        L.append('|---|---|---|---|')
        for n, s, d, note in items:
            L.append(f'| {esc(n)} | {s} | {esc(d)} | {esc(note)} |')
    else:
        L.append('| Plugin | From | What it does |')
        L.append('|---|---|---|')
        for n, s, d, _ in items:
            L.append(f'| {esc(n)} | {s} | {esc(d)} |')
L.append('')
with open(args.out, 'w', encoding='utf-8', newline='\n') as f:
    f.write('\n'.join(L))
print(args.out, 'broken', len(broken), 'partial', len(partial), 'ok', len(ok))
