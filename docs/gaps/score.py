#!/usr/bin/env python3
"""Clack's fidelity score, the 2026-11-10 roadmap method (593 checks then).

Run from the repo root: python3 -I docs/gaps/score.py [git-ref] [-v]
Reads the docs at the ref (default: the working tree) and prints each
group's ok/total, the total, and with -v every check counted open.

Groups (the roadmap's, kept as they were):
  base:   every table row of PREFS, CHANNELS, FILES and VIEWS audits, plus
          VIEWS-AUDIT's "Not done" bullets; the plugin bench's plugins
          (works fully = ok); each distinct "not supported/available yet"
          message left in the Go code (each one is an open check).
  audits: the rows of SEARCH, MESSAGES, HUDDLES and COMPOSER audits that
          carry a bold status (matched/fixed = ok; open/partial/missing).
  extended (not in the 593): THREADS-AUDIT rows with a bold status, and
          docs/EXPERIMENTS.md's A rows (reads it/default/n/a = ok;
          missing/partial = open).
"""
import re, subprocess, sys, collections

args = [a for a in sys.argv[1:] if not a.startswith('-')]
REF = args[0] if args else None
VERBOSE = '-v' in sys.argv

def read(path):
    if REF is None:
        return open(path, encoding='utf-8').read()
    return subprocess.run(['git', 'show', f'{REF}:{path}'], capture_output=True, text=True, check=True).stdout

def grep_code(pattern):
    cmd = ['git', 'grep', '-h', '-E', pattern]
    if REF:
        cmd.append(REF)
    cmd += ['--', '*.go', ':!*_test.go']
    return subprocess.run(cmd, capture_output=True, text=True).stdout

SEP = re.compile(r'^\|[\s:|-]+\|\s*$')
def table_rows(text):
    lines = text.splitlines()
    i = 0
    while i < len(lines):
        if lines[i].startswith('|') and i + 1 < len(lines) and SEP.match(lines[i + 1]):
            i += 2
            while i < len(lines) and lines[i].startswith('|'):
                yield [c.strip() for c in re.split(r'(?<!\\)\|', lines[i].strip().strip('|'))]
                i += 1
        else:
            i += 1

# Base docs: the last cell is the status. Open when it says so.
OPEN_LAST = re.compile(r"\*\*open\*\*|\bopen\*\*|^open\b|\bis \*\*open|still different|\bmissing\b(?! from)|\bnot done\b|\bpartial\b|\bgreyed:(?! none)|not drawn\)|: open\b|\bopen \(", re.I)
# Hand calls where the regex misreads a cell (the verb "open", "missing"
# describing Slack, ...). Keyed by doc and the row's first cell.
OK_OVERRIDE = {
    'CHANNELS': {'Wide topic hover card', '"Channel Manager" tag', 'About: "Workspaces with access to this channel"', 'Settings: Channel member suggestions', 'Settings: Copy member names / emails', '"Reset"'},
    'FILES': {'⋯ menu', 'Opening'},
    'PREFS': {'Missing fonts'},
    'VIEWS': set(),
}
OPEN_OVERRIDE = {
    'VIEWS': {'DMs / Unreads empty'},  # Slack's random "No unread …" state + Show all DMs
    'PREFS': {'Language & region',  # locale applied in part (docs/I18N.md)
              'huddles_reaction_explosions ("Allow for reactions & effects"; the old row used the wrong key)'},  # no huddle effects
}
# PREFS keeps "Still different" in its Result cell, not the last one.
WHOLE_ROW = {'PREFS'}

def base_doc(name):
    c = collections.Counter(); bad = []
    for cells in table_rows(read(f'docs/{name}-AUDIT.md')):
        first, last = cells[0], cells[-1]
        if name in WHOLE_ROW:
            last = ' | '.join(cells[1:])
        if name == 'VIEWS' and len(cells) > 1 and first == 'DMs' and 'No unread' in last:
            first = 'DMs / Unreads empty'
        is_open = bool(OPEN_LAST.search(last))
        if first in OK_OVERRIDE.get(name, ()): is_open = False
        if first in OPEN_OVERRIDE.get(name, ()): is_open = True
        c['open' if is_open else 'ok'] += 1
        if is_open: bad.append(f'{name}: {first[:70]} | {last[:140]}')
    return c, bad

def views_not_done():
    t = read('docs/VIEWS-AUDIT.md')
    sec = t[t.index('## Not done'):]
    bullets = [b for b in re.findall(r'^- (.+)$', sec, re.M)]
    open_b = [b for b in bullets if not re.search(r'\bdone on 2026', b)]
    return len(bullets), [f'VIEWS Not done: {b[:140]}' for b in open_b]

def plugins():
    rows = [l for l in read('docs/PLUGIN-BENCH.md').splitlines() if l.startswith('| ') and len(l.split('|')) > 3 and re.match(r'\s*\*\*(Taut|Slick)', l.split('|')[2])]
    ok = sum('works fully' in r for r in rows)
    return len(rows), ok, [f'plugin: {r[:120]}' for r in rows if 'works fully' not in r]

# The roadmap's grep was "supported yet|available yet|in Clack yet"; this one
# also catches the same toasts worded "… in yoloslack", "… here yet", "… on
# this platform yet" and the create menu's unavailable("…") rows. Since
# gap-w4 (2026-11-11) the "… in yoloslack" toasts end in "+appName (the
# popups and views constant "Clack"), which the first version of this grep
# missed: six toasts and empty states dropped out of the count while still
# in the code. The grep takes `"+appName` and `" + AppName + "` now, and
# gap_messages() joins the pieces back into one string.
APPNAME = r'"\s*\+\s*[aA]pp[nN]ame'
GAP = (r"(supported|available|edited)( [a-zA-Z]+)*( ?" + APPNAME + r"\s*\+\s*\")? yet"
       r"|n.t supported in (Clack|yoloslack|" + APPNAME + r")|unavailable\(\"")
# Wordings of one gap counted once; the generic fallback (the roadmap
# filtered those too) isn't a gap of its own.
SAME_GAP = {'camera not supported on this platform yet': "camera isn't supported on this platform yet"}
GENERIC = {'not supported yet'}
def joined(line):
    """One Go line with its string concatenations joined: appName reads as
    Clack, any other call (d.teamName()) as {…}."""
    line = re.sub(APPNAME + r'\s*\+\s*"', 'Clack', line)
    line = re.sub(APPNAME + r'\b', 'Clack"', line)
    return re.sub(r'"\s*\+\s*[\w.]+\(\)\s*\+\s*"', '{…}', line)
def gap_messages():
    msgs = set(re.findall(r'"([^"]*)"', '\n'.join(joined(l) for l in grep_code(GAP).splitlines() if not l.lstrip().startswith('//'))))
    msgs = {m.strip() for m in msgs if re.search(r'yet|supported in|available$', m) and len(m.strip()) > 4}
    # one check per gap: the list field message comes as a format string and as a prefix
    out = {}
    for m in msgs:
        key = re.sub(r'\s*\((%w|\{…\})?\)?\s*$', '', m.lower()).strip()
        key = SAME_GAP.get(key, key)
        if key not in GENERIC:
            out.setdefault(key, m)
    return sorted(out.values())

STAT = re.compile(r'\*\*(matched|fixed|open|partial|missing|done|same)\b', re.I)
def status_doc(name):
    c = collections.Counter(); bad = []
    for cells in table_rows(read(f'docs/{name}-AUDIT.md')):
        st = None
        for cell in reversed(cells):
            m = STAT.search(cell)
            if m: st = m.group(1).lower(); break
        if st is None: continue
        k = 'open' if st in ('open', 'partial', 'missing') else 'ok'
        c[k] += 1
        if k == 'open': bad.append(f'{name}: {cells[0][:70]} | {cell[:140]}')
    return c, bad

def experiments():
    c = collections.Counter(); bad = []
    for l in read('docs/EXPERIMENTS.md').splitlines():
        if not l.startswith('| `'): continue
        cells = [x.strip() for x in re.split(r'(?<!\\)\|', l.strip().strip('|'))]
        k = 'open' if re.match(r'(missing|partial)', cells[-1]) else 'ok'
        c[k] += 1
        if k == 'open': bad.append(f'EXP: {cells[0]} | {cells[-1][:120]}')
    return c, bad

total = collections.Counter(); opens = []
def add(group, ok, n, bad):
    total[group + '_ok'] += ok; total[group] += n; opens.extend(bad)
    print(f'  {group:8} {ok:4}/{n:<4} {100*ok/max(n,1):5.1f}%')

print('base')
for d in ('PREFS', 'CHANNELS', 'FILES', 'VIEWS'):
    c, bad = base_doc(d); print(f'  {d}', end=''); add('base', c['ok'], c['ok'] + c['open'], bad)
n, bad = views_not_done(); print('  VIEWS not done', end=''); add('base', n - len(bad), n, bad)
n, ok, bad = plugins(); print('  plugins', end=''); add('base', ok, n, bad)
g = gap_messages(); print('  gap messages', end=''); add('base', 0, len(g), [f'gap message: {m}' for m in g])
print('audits')
for d in ('SEARCH', 'MESSAGES', 'HUDDLES', 'COMPOSER'):
    c, bad = status_doc(d); print(f'  {d}', end=''); add('audits', c['ok'], c['ok'] + c['open'], bad)
ok593 = total['base_ok'] + total['audits_ok']; n593 = total['base'] + total['audits']
print(f'SCORE (593 method): {ok593}/{n593} = {100*ok593/n593:.1f}%')
print('extended')
try:
    c, bad = status_doc('THREADS'); print('  THREADS', end=''); add('ext', c['ok'], c['ok'] + c['open'], bad)
except subprocess.CalledProcessError:
    print('  THREADS  (no THREADS-AUDIT.md at this ref)')
c, bad = experiments(); print('  EXPERIMENTS A', end=''); add('ext', c['ok'], c['ok'] + c['open'], bad)
okx = ok593 + total['ext_ok']; nx = n593 + total['ext']
print(f'SCORE (extended): {okx}/{nx} = {100*okx/nx:.1f}%')
if VERBOSE:
    print('\nopen checks:'); [print(' ', o) for o in opens]
