#!/usr/bin/env python3
"""Is Clack still doing what Slack does? One line per dimension.

Run from the repo root:

  python3 -I tools/parity.py                    every dimension: go test, then same or not
  python3 -I tools/parity.py emoji-rank markdown   just these (faster: fewer packages build)
  python3 -I tools/parity.py --list             the dimensions, their tests and snapshots
  python3 -I tools/parity.py --refresh emoji-rank  run Slack's side again, rewrite its snapshot
  python3 -I tools/parity.py --from DIR...      read go test -json output instead of running go

  --out DIR  also write DIR/parity-report.md and DIR/parity-report.json
  --fresh    go test -count=1 (by default Go's test cache can answer)
  --strict   exit 1 unless every selected dimension is the same as Slack
             (CI doesn't pass it: the report doesn't gate merges yet)

A dimension (tools/parity_checks.json) is one or more Go tests whose
expected values are Slack's own output: Slack's JavaScript run in node, or
answers captured from live Slack, never values someone wrote down from
reading Slack's code. Slack's side is committed: a snapshot in
testdata/parity/<dimension>.json, or the test's own table. The default run
only builds and runs those tests (no node, no network), so it takes about
as long as `go test` of the packages involved. --refresh is the separate,
slow step: it reruns the oracle where one is wired up (emoji-rank today)
and shows what changed on Slack's side.

The totals count only these comparisons. The report also names every area
of Slack's client no check covers, the comparisons that run in other
workflows, and the differences Clack keeps on purpose. docs/PARITY.md
explains it; docs/gaps/score.py counts the manual audits instead. Inspired
by horizon's tools/parity.py, minus its percentage badge.
"""
import argparse
import datetime
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
REGISTRY = os.path.join(ROOT, 'tools', 'parity_checks.json')
KNOWN = 'parity: known divergence'
DIFFERS = 'parity: differs from Slack'
PENDING = 'parity: no Slack output'
KINDS = {'slack-js': "Slack's JS run in node", 'slack-live': 'captured from live Slack'}
TIERS = ('product', 'fixture')
FRAMING = re.compile(r'^\s*(=== (RUN|PAUSE|CONT|NAME)|--- (PASS|FAIL|SKIP))\b')
VALUE = re.compile(r'^\s*(slack|clack): (.*)$')
SLACK_NAMED = re.compile(r'^func (Test\w*(?:Slack|Oracle)\w*)\(', re.M)
OUTPUT_BYTES = 8000
# what a dimension is when its checks disagree: the worst one wins
ORDER = ['diverge', 'fail', 'not-run', 'known', 'match']
WORD = {'match': 'same', 'diverge': 'DIVERGES', 'fail': 'FAILS', 'not-run': 'NOT MEASURED',
        'known': 'DIVERGES (known)'}
# how the totals line says it
PHRASE = {'diverge': 'diverge', 'fail': 'fail', 'not-run': 'not measured', 'known': 'with known divergences'}


class RegistryError(Exception):
    pass


class Unavailable(Exception):
    """A refresh that can't run here (no harness, no node)."""


# the registry and the snapshots

def read_json(path):
    with open(path, encoding='utf-8') as f:
        return json.load(f)


def load_registry(path=REGISTRY, root=ROOT):
    reg = read_json(path)
    errs = validate(reg, root)
    if errs:
        raise RegistryError('\n'.join(errs))
    return reg


def checks_of(reg):
    for d in reg['dimensions']:
        for c in d['checks']:
            yield d, c


def provenance(check, root=ROOT):
    """Where a check's Slack values come from: its snapshot's "slack" block, or the registry's "oracle"."""
    if check.get('snapshot'):
        return read_json(os.path.join(root, check['snapshot'])).get('slack') or {}
    return check.get('oracle') or {}


def validate(reg, root=ROOT):
    """Every problem with the registry and its snapshots (an empty list: none)."""
    errs = []
    for key in ('areas', 'dimensions', 'deliberate', 'elsewhere'):
        if not isinstance(reg.get(key), list):
            errs.append(f'"{key}" must be a list')
    if errs:
        return errs
    areas = set()
    for a in reg['areas']:
        if not a.get('id') or not a.get('name'):
            errs.append(f'an area without an id or a name: {a}')
        elif a['id'] in areas:
            errs.append(f'area {a["id"]} twice')
        areas.add(a.get('id'))
    dims, tests = set(), set()
    for d in reg['dimensions']:
        did = d.get('id') or '?'
        for field in ('id', 'area', 'tier', 'what', 'checks'):
            if not d.get(field):
                errs.append(f'dimension {did}: no "{field}"')
        if did in dims:
            errs.append(f'dimension {did} twice')
        dims.add(did)
        if d.get('area') and d['area'] not in areas:
            errs.append(f'dimension {did}: unknown area {d["area"]}')
        if d.get('tier') and d['tier'] not in TIERS:
            errs.append(f'dimension {did}: tier {d["tier"]}, want one of {", ".join(TIERS)}')
        for c in d.get('checks') or []:
            name = f'{did}: {c.get("package")} {c.get("test")}'
            for field in ('package', 'test', 'compares'):
                if not c.get(field):
                    errs.append(f'{name}: no "{field}"')
            if c.get('test') and not re.fullmatch(r'Test\w*', c['test']):
                errs.append(f'{name}: not a Go test name')
            if c.get('package', '').startswith(('/', '.')) or '\\' in c.get('package', ''):
                errs.append(f'{name}: the package must be a directory under the repo root')
            key = (c.get('package'), c.get('test'))
            if key in tests:
                errs.append(f'{name}: registered twice')
            tests.add(key)
            if bool(c.get('snapshot')) == bool(c.get('oracle')):
                errs.append(f'{name}: give either "snapshot" or "oracle"')
            if c.get('snapshot'):
                errs += [f'{name}: {e}' for e in validate_snapshot(os.path.join(root, c['snapshot']))]
            elif c.get('oracle'):
                errs += [f'{name}: {e}' for e in validate_slack(c['oracle'])]
    for d in reg['deliberate']:
        for field in ('id', 'area', 'what', 'slack', 'clack', 'decided'):
            if not d.get(field):
                errs.append(f'deliberate difference {d.get("id", "?")}: no "{field}"')
        if d.get('area') and d['area'] not in areas:
            errs.append(f'deliberate difference {d.get("id", "?")}: unknown area {d["area"]}')
    for e in reg['elsewhere']:
        for field in ('area', 'workflow', 'what'):
            if not e.get(field):
                errs.append(f'an "elsewhere" entry without "{field}": {e}')
        if e.get('area') and e['area'] not in areas:
            errs.append(f'an "elsewhere" entry: unknown area {e["area"]}')
    return errs


def validate_slack(o):
    errs = []
    if o.get('kind') not in KINDS:
        errs.append(f'Slack side kind {o.get("kind")!r}, want one of {", ".join(KINDS)}')
    for field in ('source', 'captured'):
        if not o.get(field):
            errs.append(f'Slack side without "{field}"')
    return errs


def validate_snapshot(path):
    try:
        s = read_json(path)
    except (OSError, ValueError) as e:
        return [f'snapshot {path}: {e}']
    errs = [f'snapshot: {e}' for e in validate_slack(s.get('slack') or {})]
    names = set()
    for c in s.get('cases') or []:
        n = c.get('name', '')
        if not n or '/' in n or go_name(n) in names:
            errs.append(f'snapshot case name {n!r} is empty, holds a /, or repeats (as go test names it)')
        names.add(go_name(n))
        if 'in' not in c:
            errs.append(f'snapshot case {n!r} has no "in"')
        if c.get('known') and c.get('slack') is None:
            errs.append(f'snapshot case {n!r}: a known divergence needs Slack\'s output')
        if c.get('clack') is not None and not c.get('known'):
            errs.append(f'snapshot case {n!r}: "clack" goes with "known"')
    if not names:
        errs.append('snapshot has no cases')
    return errs


def module_path(root=ROOT):
    with open(os.path.join(root, 'go.mod'), encoding='utf-8') as f:
        for line in f:
            if line.startswith('module '):
                return line.split()[1]
    raise RegistryError('go.mod has no module line')


def has_test_func(root, package, test):
    pat = re.compile(r'^func ' + re.escape(test) + r'\(', re.M)
    d = os.path.join(root, package)
    try:
        names = sorted(os.listdir(d))
    except OSError:
        return False
    for n in names:
        if n.endswith('_test.go'):
            with open(os.path.join(d, n), encoding='utf-8', errors='replace') as f:
                if pat.search(f.read()):
                    return True
    return False


# go test -json

def json_files(paths):
    out = []
    for p in paths:
        if os.path.isdir(p):
            for d, _, names in os.walk(p):
                out += [os.path.join(d, n) for n in names if n.endswith(('.jsonl', '.json'))]
        else:
            out.append(p)
    return sorted(out)


def parse_events(lines):
    for line in lines:
        line = line.strip()
        if not line.startswith('{'):
            continue
        try:
            e = json.loads(line)
        except ValueError:
            continue
        if isinstance(e, dict) and 'Action' in e and ('Package' in e or 'ImportPath' in e):
            yield e


def read_events(paths):
    for path in json_files(paths):
        with open(path, encoding='utf-8', errors='replace') as f:
            yield from parse_events(f)


RANK = {'fail': 3, 'pass': 2, 'skip': 1}


def worse(a, b):
    if a is None or b is None:
        return a or b
    return a if RANK[a] >= RANK[b] else b


class Results:
    """What go test -json events say about each package, test and subtest."""

    def __init__(self):
        self.packages = {}  # import path -> {'status', 'output', 'build', 'build_failed'}
        self.tests = {}     # (import path, test) -> {'status', 'output', 'cases': {go name: {...}}}
        self.events = 0

    def package(self, path):
        return self.packages.setdefault(path, {'status': None, 'output': [], 'build': [], 'build_failed': False})

    def add(self, e):
        self.events += 1
        action = e.get('Action')
        if action in ('build-output', 'build-fail'):
            path = e.get('ImportPath', '').split(' ')[0]
            if path:
                p = self.package(path)
                if e.get('Output'):
                    p['build'].append(e['Output'])
                if action == 'build-fail':
                    p['build_failed'] = True
            return
        pkg, name = e.get('Package', ''), e.get('Test', '')
        if not pkg:
            return
        if not name:
            p = self.package(pkg)
            if action == 'output':
                p['output'].append(e.get('Output', ''))
            elif action in RANK:
                p['status'] = worse(p['status'], action)
                if e.get('FailedBuild'):
                    p['build_failed'] = True
            return
        self.package(pkg)
        top, _, rest = name.partition('/')
        t = self.tests.setdefault((pkg, top), {'status': None, 'output': [], 'cases': {}})
        node = t
        if rest:
            node = t['cases'].setdefault(rest.split('/')[0], {'status': None, 'output': []})
        if action == 'output':
            node['output'].append(e.get('Output', ''))
        elif action in RANK:
            node['status'] = worse(node['status'], action)


def collect(events):
    r = Results()
    for e in events:
        r.add(e)
    return r


def own_lines(lines):
    """The output's lines without go test's framing. go test -json can split one long line over several output
    events (a case's slack: or clack: JSON past a few hundred bytes), so the events are joined back first."""
    out = [l for l in ''.join(lines).split('\n') if not FRAMING.match(l)]
    while out and not out[-1].strip():
        out.pop()
    return out


def clip(text, limit=OUTPUT_BYTES):
    return text if len(text) <= limit else text[:limit] + '\n[cut]'


def go_name(name):
    """A subtest's name as go test reports it (testing's rewrite: spaces become _)."""
    out = []
    for ch in name:
        if ch.isspace():
            out.append('_')
        elif not ch.isprintable():
            out.append(json.dumps(ch)[1:-1])
        else:
            out.append(ch)
    return ''.join(out)


def values(lines):
    """The slack: and clack: JSON a snapshot case printed, decoded where it parses."""
    got = {}
    for l in lines:
        m = VALUE.match(l)
        if m and m.group(1) not in got:
            try:
                got[m.group(1)] = json.loads(m.group(2))
            except ValueError:
                got[m.group(1)] = m.group(2)
    return got


def judge_case(c, snap_case=None):
    lines = own_lines(c['output'])
    text = '\n'.join(lines)
    v = values(lines)
    if c['status'] == 'pass':
        st = 'match'
    elif c['status'] == 'skip':
        st = 'known' if KNOWN in text else 'pending' if PENDING in text else 'skip'
    elif DIFFERS in text or snap_case is None:
        st = 'diverge'
    else:
        st = 'fail'  # an assert beside the comparison failed (a rule, or setup)
    out = {'status': st}
    if st != 'match':
        out['output'] = clip(text)
        if snap_case is not None:
            out['in'] = snap_case.get('in')
            out['slack'] = snap_case.get('slack')
            if snap_case.get('known'):
                out['why'] = snap_case['known']
            if snap_case.get('clack') is not None:
                out['recorded'] = snap_case['clack']
        if 'clack' in v:
            out['clack'] = v['clack']
        if 'slack' in v and snap_case is None:
            out['slack'] = v['slack']
    return out


def judge(check, results, module, root=ROOT):
    """A check's status, the reason and its cases, from the go test results."""
    path = module + '/' + check['package']
    t = results.tests.get((path, check['test']))
    out = {'status': None, 'why': '', 'cases': []}
    snap = read_json(os.path.join(root, check['snapshot'])) if check.get('snapshot') else None
    if t is None or (t['status'] is None and not t['cases'] and not t['output']):
        pkg = results.packages.get(path)
        out['status'] = 'not-run'
        if not has_test_func(root, check['package'], check['test']):
            out['why'] = f'no func {check["test"]} in {check["package"]}: renamed or removed? Fix tools/parity_checks.json'
        elif pkg is None:
            out['why'] = f'no results for {check["package"]}'
        elif pkg['build_failed'] or (pkg['status'] == 'fail' and not any(k[0] == path for k in results.tests)):
            out['why'] = f'{check["package"]} did not build, or its tests did not start'
            out['output'] = clip('\n'.join(own_lines(pkg['build'] or pkg['output'])[-30:]))
        else:
            out['why'] = f'{check["package"]} ran, but not {check["test"]}'
        return out
    if snap:
        by_name = {go_name(c['name']): c for c in snap['cases']}
        for gname, sc in by_name.items():
            c = t['cases'].get(gname)
            if c is None:
                x = {'name': sc['name'], 'status': 'not-run', 'output': 'no result for this case', 'in': sc.get('in'),
                     'slack': sc.get('slack')}
            else:
                x = {'name': sc['name'], **judge_case(c, sc)}
            out['cases'].append(x)
        extra = [n for n in t['cases'] if n not in by_name]
        if extra:
            out['extra'] = extra
    else:
        out['cases'] = [{'name': n, **judge_case(c)} for n, c in t['cases'].items()]
    statuses = [c['status'] for c in out['cases']]
    pkg_failed = (results.packages.get(path) or {}).get('status') == 'fail'
    if t['status'] == 'fail' or (t['status'] is None and pkg_failed):
        if 'diverge' in statuses:
            out['status'] = 'diverge'
            out['why'] = f'{statuses.count("diverge")} of {len(statuses)} cases differ from Slack'
        elif 'fail' in statuses:
            out['status'] = 'fail'
            out['why'] = f'{statuses.count("fail")} of {len(statuses)} cases failed an assert that is not the comparison'
        elif statuses or t['status'] is None:
            out['status'] = 'fail'
            out['why'] = ('failed outside its cases (setup, or an assert after them)' if t['status'] == 'fail'
                          else 'no result: the test binary died while it ran')
        else:
            out['status'] = 'diverge'
            out['why'] = 'the test failed (it has no subtests, so it can\'t say which input)'
        out['output'] = clip('\n'.join(own_lines(t['output'])))
    elif t['status'] == 'skip':
        out['status'] = 'not-run'
        lines = own_lines(t['output'])
        out['why'] = 'skipped' + (': ' + lines[-1].strip() if lines else '')
    elif t['status'] is None:
        out['status'] = 'not-run'
        out['why'] = 'no result for the test'
    elif 'not-run' in statuses or 'pending' in statuses:
        out['status'] = 'not-run'
        n = statuses.count('not-run') + statuses.count('pending')
        out['why'] = f'{n} of {len(statuses)} cases have no result or no Slack output yet'
    elif 'known' in statuses:
        out['status'] = 'known'
        out['why'] = f'{statuses.count("known")} of {len(statuses)} cases are known differences'
    else:
        out['status'] = 'match'
        if 'skip' in statuses:
            out['why'] = f'{statuses.count("skip")} of {len(statuses)} cases skipped, not compared'
    return out


def worst(statuses):
    return min(statuses, key=ORDER.index) if statuses else 'not-run'


def report(reg, results, module, source, commit=None, root=ROOT, dims=None):
    areas = {a['id']: a for a in reg['areas']}
    out_dims = []
    for d in reg['dimensions']:
        if dims and d['id'] not in dims:
            continue
        cs = []
        for c in d['checks']:
            cs.append({**c, **judge(c, results, module, root), 'slack_side': provenance(c, root),
                       'partial': bool(c.get('partial'))})
        out_dims.append({**{k: v for k, v in d.items() if k != 'checks'}, 'checks': cs,
                         'status': worst([c['status'] for c in cs])})
    product = [d for d in out_dims if d['tier'] == 'product']
    pchecks = [c for d in product for c in d['checks']]
    itemised = [c for c in pchecks if c['cases']]
    every_product = [d for d in reg['dimensions'] if d['tier'] == 'product']
    covered = [a for a in areas if any(d['area'] == a for d in every_product)]
    return {
        'commit': commit,
        'source': source,
        'selected': sorted(dims) if dims else None,
        'totals': {
            'dimensions': len(product),
            **{f'dimensions_{s.replace("-", "_")}': sum(d['status'] == s for d in product) for s in ORDER},
            'checks': len(pchecks),
            **{f'checks_{s.replace("-", "_")}': sum(c['status'] == s for c in pchecks) for s in ORDER},
            'itemised_checks': len(itemised),
            'cases': sum(len(c['cases']) for c in itemised),
            'cases_match': sum(x['status'] == 'match' for c in itemised for x in c['cases']),
            'areas': len(areas),
            'areas_covered': len(covered),
        },
        'dimensions': out_dims,
        'uncovered_areas': [a for a in areas if a not in covered],
        'areas': reg['areas'],
        'deliberate': reg['deliberate'],
        'elsewhere': reg['elsewhere'],
    }


def slack_named_unregistered(reg, root=ROOT):
    """Tests in the tree with Slack or Oracle in their names that aren't registered."""
    registered = {(c['package'], c['test']) for _, c in checks_of(reg)}
    found = []
    for d, dirs, names in os.walk(root):
        dirs[:] = sorted(x for x in dirs if not x.startswith('.') and x not in ('third_party', 'node_modules', 'testdata'))
        for n in sorted(names):
            if n.endswith('_test.go'):
                pkg = os.path.relpath(d, root)
                with open(os.path.join(d, n), encoding='utf-8', errors='replace') as f:
                    for t in SLACK_NAMED.findall(f.read()):
                        if (pkg, t) not in registered:
                            found.append(f'{pkg} {t}')
    return sorted(found)


# the blunt answer

def short(v, limit=160):
    s = v if isinstance(v, str) else json.dumps(v, ensure_ascii=False)
    return s if len(s) <= limit else s[:limit - 1] + '…'


def first_difference(slack, clack):
    """Where two lists part ways, as text (None for other values)."""
    if not (isinstance(slack, list) and isinstance(clack, list)):
        return None
    for i in range(max(len(slack), len(clack))):
        a = slack[i] if i < len(slack) else '(nothing)'
        b = clack[i] if i < len(clack) else '(nothing)'
        if a != b:
            return f'row {i + 1}: Slack {short(a, 60)}, Clack {short(b, 60)}'
    return None


def plural(n, word):
    return f'{n} {word}' + ('s' if n != 1 else '')


def case_lines(x, indent='      ', full=False, same_why=None):
    """What a case that isn't the same as Slack says, in a few lines (full: whole values; same_why: the
    reason the case before gave, which isn't repeated)."""
    word = {'diverge': 'differs', 'fail': 'fails', 'known': 'known difference', 'pending': 'no Slack output yet',
            'skip': 'skipped', 'not-run': 'no result'}[x['status']]
    why = x.get('why') or ''
    if why and why == same_why:
        why = 'the same reason'
    lines = [f'{indent}{x["name"]}: {word}' + (f': {why if full else short(why, 150)}' if why else '')]
    if 'slack' in x and 'clack' in x:
        fd = first_difference(x['slack'], x['clack'])
        limit = 100000 if full else 160
        if fd:
            lines.append(f'{indent}  {fd}')
        if full or not fd or len(short(x['slack'], limit)) + len(short(x['clack'], limit)) < 200:
            lines.append(f'{indent}  Slack {short(x["slack"], limit)}')
            lines.append(f'{indent}  Clack {short(x["clack"], limit)}')
        if 'recorded' in x and x['recorded'] != x['clack']:
            lines.append(f'{indent}  recorded Clack {short(x["recorded"], limit)}')
    else:
        msg = [l.strip() for l in (x.get('output') or '').splitlines() if l.strip()]
        lines += [f'{indent}  {short(l, 200)}' for l in msg[:6]]
    return lines


def cases_text(c):
    if not c['cases']:
        return 'one result (no subtests)'
    n = sum(x['status'] == 'match' for x in c['cases'])
    return f'{n} of {len(c["cases"])} same'


def blunt(r, limit=5):
    """The plain-text answer: totals first, then one line per dimension."""
    t = r['totals']
    lines = []
    parts = [f'{t["dimensions_" + s.replace("-", "_")]} {PHRASE[s]}' for s in ORDER[:-1]
             if t['dimensions_' + s.replace('-', '_')]]
    lines.append(f'parity: {t["dimensions_match"]} of {t["dimensions"]} dimensions the same as Slack'
                 + (f' ({", ".join(parts)})' if parts else '')
                 + f'; {t["checks_match"]} of {t["checks"]} checks, {t["cases_match"]} of {t["cases"]} itemised cases.')
    lines.append(f'  Only what these checks compare: {t["areas"] - t["areas_covered"]} of the {t["areas"]} areas of '
                 'Slack\'s client have no check at all. This is not a parity percentage.')
    for d in r['dimensions']:
        ok = sum(c['status'] == 'match' for c in d['checks'])
        tag = '' if d['tier'] == 'product' else '  [fake server, not counted]'
        if d['status'] == 'match':
            n = sum(len(c['cases']) for c in d['checks'])
            partial = sum(c['partial'] for c in d['checks'])
            what = plural(len(d['checks']), 'check') + (f' ({partial} partial)' if partial else '')
            what += f', {plural(n, "case")}' if n else ''
            lines.append(f'{d["id"]:18} {"same":17} {what}{tag}')
            continue
        lines.append(f'{d["id"]:18} {WORD[d["status"]]:17} {ok} of {plural(len(d["checks"]), "check")} same{tag}')
        for c in d['checks']:
            if c['status'] == 'match':
                continue
            lines.append(f'    {c["package"]} {c["test"]}: {WORD[c["status"]].lower()}, {c["why"]}')
            bad = [x for x in c['cases'] if x['status'] != 'match']
            for i, x in enumerate(bad[:limit]):
                lines += case_lines(x, same_why=bad[i - 1].get('why') if i else None)
            if len(bad) > limit:
                lines.append(f'      … {len(bad) - limit} more (--out DIR for the full report)')
            if not c['cases'] and c.get('output'):
                msg = [l.strip() for l in c['output'].splitlines() if l.strip()]
                lines += [f'      {short(l, 200)}' for l in msg[:8]]
    areas = {a['id']: a for a in r['areas']}
    lines.append('not covered: ' + '; '.join(areas[a]['name'] for a in r['uncovered_areas']))
    if r['deliberate']:
        lines.append('different on purpose: ' + '; '.join(f'{d["what"]} ({d["decided"].split(" (")[0]})'
                                                          for d in r['deliberate']))
    return lines


# the full report

def cell(s):
    return str(s).replace('|', '\\|').replace('\n', ' ')


def slack_text(o):
    parts = [KINDS.get(o.get('kind'), o.get('kind', '?')) + ': ' + o.get('source', '?')]
    if o.get('build'):
        parts.append(o['build'])
    parts.append('captured ' + o.get('captured', '?'))
    return ', '.join(parts)


def area_docs(a):
    """An area's docs: the manual audit score.py counts, and other notes."""
    out = [f'manual audit `{a["audit"]}` (score.py)'] if a.get('audit') else []
    return out + ([f'notes `{a["docs"]}`'] if a.get('docs') else [])


MD_WORD = {'match': 'same', 'diverge': '**DIVERGES**', 'fail': '**FAILS**', 'known': '**DIVERGES (known)**',
           'not-run': '**NOT MEASURED**'}


def markdown(r, unregistered=None):
    t = r['totals']
    areas = {a['id']: a for a in r['areas']}
    where = f'commit `{r["commit"][:12]}`, ' if r.get('commit') else ''
    out = ['# Slack parity', '',
           f'Measured at {where}from {r["source"]["what"]} ({r["source"]["events"]} go test events). '
           'Registry: `tools/parity_checks.json`. How to read and extend it: `docs/PARITY.md`.', '']
    if r.get('selected'):
        out += [f'Only these dimensions were selected: {", ".join(r["selected"])}. The totals cover them alone.', '']
    out += ['```', *blunt(r, limit=3), '```', '',
            f'**{t["dimensions_match"]} of {t["dimensions"]} dimensions are the same as Slack**, '
            f'{t["checks_match"]} of {t["checks"]} checks and {t["cases_match"]} of {t["cases"]} cases '
            f'(cases are counted in the {t["itemised_checks"]} checks that run one subtest per input).', '',
            f'These checks touch {t["areas_covered"]} of the {t["areas"]} areas of Slack\'s client that Clack '
            f'implements, each only in the way its row says. **{t["areas"] - t["areas_covered"]} areas have no '
            'check at all.** So this is not "how much of Clack matches Slack": it counts comparisons with Slack\'s '
            'own output that ran here. Slack\'s side is frozen. Each check holds what the Slack build in its row '
            'did on its inputs, captured on the date shown, and `--refresh` reruns the oracles that are wired up.', '']

    bad = [(d, c) for d in r['dimensions'] for c in d['checks'] if c['status'] in ('diverge', 'fail', 'known')]
    out += [f'## Differences from Slack ({len(bad)})', '']
    if not bad:
        out += ['None: every check that ran gave Slack\'s output on every input it compares.', '']
    for d, c in bad:
        out += [f'### {d["id"]}: {c["package"]} `{c["test"]}` {MD_WORD[c["status"]]}', '',
                f'{c["why"]}. Compares: {c["compares"]} Slack side: {slack_text(c["slack_side"])}.', '']
        for x in c['cases']:
            if x['status'] == 'match':
                continue
            out += ['```', *[l.strip() for l in case_lines(x, indent='', full=True)], '```', '']
        if c.get('output') and not any(x['status'] != 'match' for x in c['cases']):
            out += ['```', c['output'], '```', '']
    if r['deliberate']:
        out += ['Differences Clack keeps on purpose (decided, not bugs; no check measures them):', '']
        for x in r['deliberate']:
            out.append(f'- **{x["what"]}** ({areas[x["area"]]["name"]}). Slack: {x["slack"]} Clack: {x["clack"]} '
                       f'Decided: {x["decided"]}.' + (f' {x["test"]}.' if x.get('test') else ''))
        out.append('')

    missing = [(d, c) for d in r['dimensions'] for c in d['checks'] if c['status'] == 'not-run']
    if missing:
        out += [f'## Not measured ({len(missing)})', '', 'These count as not the same as Slack.', '']
        for d, c in missing:
            out.append(f'- {d["id"]}: {c["package"]} `{c["test"]}`: {c["why"]}')
            if c.get('output'):
                out += ['', '```', c['output'], '```']
        out.append('')

    out += ['## Dimensions', '']
    for d in r['dimensions']:
        a = areas[d['area']]
        tier = '' if d['tier'] == 'product' else ' Fake-server check: it measures a test double, not Clack, ' \
                                                 'so it is not in the totals.'
        docs = area_docs(a)
        out += [f'### {d["id"]}: {MD_WORD[d["status"]]}', '',
                f'{d["what"]} Area: {a["name"]}' + (f' ({"; ".join(docs)})' if docs else '') + f'.{tier}', '',
                '| Test | Result | Cases | Compares | Slack side |', '|---|---|---|---|---|']
        for c in d['checks']:
            what = c['compares'] + (' Partial: compares part of the output.' if c['partial'] else '')
            if c.get('notes'):
                what += ' ' + c['notes']
            if c.get('snapshot'):
                what += f' Snapshot: `{c["snapshot"]}`.'
            res = MD_WORD[c['status']] + (f' ({c["why"]})' if c['why'] and c['status'] == 'match' else '')
            out.append(f'| {cell(c["package"])} `{c["test"]}` | {cell(res)} | {cases_text(c)} | {cell(what)} | '
                       f'{cell(slack_text(c["slack_side"]))} |')
        out.append('')
        if a.get('gaps'):
            out += [f'Not compared in this area: {a["gaps"]}', '']

    out += ['## Not covered', '',
            f'{t["areas"] - t["areas_covered"]} of the {t["areas"]} areas have no check that compares Clack with '
            'Slack\'s own output. Their tests and audits may say a lot, but nothing here measures them.', '']
    for aid in r['uncovered_areas']:
        a = areas[aid]
        notes = []
        notes += area_docs(a)
        extra = [e['workflow'] for e in r['elsewhere'] if e['area'] == aid]
        if extra:
            notes.append('a comparison in ' + ', '.join(f'`{w}`' for w in extra))
        out.append(f'- {a["name"]}' + (f' ({"; ".join(notes)})' if notes else ''))
    out.append('')
    if r['elsewhere']:
        out += ['Comparisons with Slack, or with Chrome drawing Slack\'s CSS and fonts, run in other workflows. '
                'This runner doesn\'t collect them yet, so they\'re not in the totals:', '']
        out += [f'- {areas[e["area"]]["name"]}, `{e["workflow"]}`: {e["what"]}' for e in r['elsewhere']]
        out.append('')
    if unregistered is not None:
        out += ['## Not counted', '',
                f'{len(unregistered)} other tests have "Slack" or "Oracle" in their names but aren\'t registered, so '
                'they aren\'t counted. Most check Clack against rules people wrote from reading Slack\'s code; some '
                'are about Slack\'s hosts or data. A test whose expected values are Slack\'s real output belongs in '
                'the registry.', '', '<details><summary>The tests</summary>', '',
                *[f'- {n}' for n in unregistered], '', '</details>', '']
    out += ['`docs/gaps/score.py` is the manual side: it counts the audit docs\' rows, people comparing Clack '
            'with Slack by eye and by reading Slack\'s code. This report counts automated comparisons with Slack\'s '
            'own output. Neither one is a parity percentage for the whole client.', '']
    return '\n'.join(out)


# running go test

def run_go(reg, dims, fresh=False):
    """go test -json over the selected dimensions' tests; returns its events."""
    checks = [c for d, c in checks_of(reg) if not dims or d['id'] in dims]
    pkgs = sorted({c['package'] for c in checks})
    pattern = '^(' + '|'.join(sorted({c['test'] for c in checks})) + ')$'
    cmd = ['go', 'test', '-json', *(['-count=1'] if fresh else []), '-run', pattern, *('./' + p for p in pkgs)]
    if not shutil.which('go'):
        raise SystemExit('parity: no go on PATH (use --from with go test -json output)')
    p = subprocess.run(cmd, cwd=ROOT, capture_output=True, text=True)
    events = list(parse_events(p.stdout.splitlines()))
    if p.returncode not in (0, 1) or not events:
        sys.stderr.write(p.stderr[-4000:])
        raise SystemExit(f'parity: {" ".join(cmd[:4])} … exited {p.returncode} with no test results')
    return events, ' '.join(cmd[:3]) + ' over ' + ', '.join(pkgs)


def current_commit():
    if os.environ.get('GITHUB_SHA'):
        return os.environ['GITHUB_SHA']
    try:
        p = subprocess.run(['git', 'rev-parse', 'HEAD'], cwd=ROOT, capture_output=True, text=True)
        return p.stdout.strip() or None
    except OSError:
        return None


def annotate(r):
    """GitHub annotations: every dimension that isn't the same as Slack, loudly."""
    for d in r['dimensions']:
        for c in d['checks']:
            if c['status'] != 'match':
                print(f'::warning title=parity {d["id"]}::{c["package"]} {c["test"]} '
                      f'{WORD[c["status"]].lower()}: {c["why"]}')
    t = r['totals']
    print(f'::notice title=parity::{t["dimensions_match"]} of {t["dimensions"]} dimensions the same as Slack; '
          f'{t["areas"] - t["areas_covered"]} of {t["areas"]} areas have no check (docs/PARITY.md)')


# --refresh: Slack's side again

EMOJI_ORACLE = '/root/clack-ops/scratch/emoji-rank/tools/oracle.cjs'
EMOJI_NOW_MS = 1794657600000  # the oracle's Date.now: 2026-11-14 12:00 UTC, as tSlackNow


def duration_ms(s):
    """A Go duration ("90m", "48h", "1h30m") in milliseconds."""
    units = {'ms': 1, 's': 1000, 'm': 60000, 'h': 3600000}
    parts = re.findall(r'(\d+(?:\.\d+)?)(ms|s|m|h)', s)
    if not parts or ''.join(a + b for a, b in parts) != s:
        raise ValueError(f'duration {s!r}')
    return int(sum(float(a) * units[b] for a, b in parts))


def emoji_frecency(uses):
    return {u['key']: {'id': u['id'], 'count': u['count'], 'visits': [EMOJI_NOW_MS - duration_ms(a) for a in u['ago']]}
            for u in uses}


def refresh_emoji_rank(cases, tmp):
    """Slack's ":" rows for each case: the emoji-rank harness (node, Slack's own modules)."""
    oracle = os.environ.get('CLACK_PARITY_EMOJI_ORACLE', EMOJI_ORACLE)
    node = shutil.which('node')
    if not node:
        raise Unavailable('no node on PATH')
    if not os.path.exists(oracle):
        raise Unavailable(f'no emoji-rank harness at {oracle} (set CLACK_PARITY_EMOJI_ORACLE); it lives outside '
                          'the repo because it loads Slack\'s own code from the CDN')
    out = {}
    for i, c in enumerate(cases):
        argv = [node, oracle, c['in']['q'], c['in']['model']]
        if c['in'].get('frecency'):
            f = os.path.join(tmp, f'frecency-{i}.json')
            with open(f, 'w', encoding='utf-8') as fh:
                json.dump(emoji_frecency(c['in']['frecency']), fh)
            argv.append(f)
        # groups over Karim's assignments: emoji_v17 on runs Slack's 17.0 table
        argv += [f'--exp={k}={v}' for k, v in sorted((c['in'].get('experiments') or {}).items())]
        p = subprocess.run(argv, cwd=os.path.dirname(oracle), capture_output=True, text=True, timeout=300)
        rows = None
        for line in reversed(p.stdout.splitlines()):
            if line.startswith('{'):
                rows = [x['name'] for x in json.loads(line)['rows']]
                break
        if p.returncode or rows is None:
            raise Unavailable(f'the harness failed on {c["name"]!r} ({p.returncode}): {p.stderr.strip()[-500:]}')
        out[c['name']] = rows
    return out


QUIP_ORACLE = os.path.join(ROOT, 'tools', 'oracles', 'quip.cjs')
QUIP_SNAPSHOTS = ('quip-wire-encode', 'quip-wire-decode', 'quip-order', 'quip-rtml-text', 'quip-rtml-parse', 'quip-rtml-write',
                  'quip-save-section', 'quip-save-title', 'quip-apply-runs', 'quip-save-new-section')


def quip_refresher(kind):
    """Slack's values for one of internal/quip's snapshots: tools/oracles/quip.cjs runs Slack's Quip modules in
    node (QUIP_CHUNKS names the two CDN chunks, JSDOM the directory holding jsdom; see the script's header)."""
    def refresh(cases, tmp):
        node = shutil.which('node')
        if not node:
            raise Unavailable('no node on PATH')
        f = os.path.join(tmp, 'cases.json')
        with open(f, 'w', encoding='utf-8') as fh:
            json.dump(cases, fh, ensure_ascii=False)
        p = subprocess.run([node, QUIP_ORACLE, kind, f], capture_output=True, text=True, timeout=600)
        if p.returncode:
            raise Unavailable(f'the harness failed ({p.returncode}): {p.stderr.strip()[-500:]}')
        # one JSON line; its strings can hold U+2028, so split on newlines only
        return json.loads(p.stdout.rstrip('\n').split('\n')[-1])
    return refresh


MESSAGE_ACTIONS_ORACLE = os.path.join(ROOT, 'tools', 'oracles', 'message_actions.cjs')


def refresh_message_actions(cases, tmp, base=None):
    """Slack's hover toolbar, ⋮ menu and right-click menu for each case: tools/oracles/message_actions.cjs runs
    Slack's own toolbar and menu code in node over its CDN chunks (SLACK_CHUNKS names their directory, JSDOM the
    directory holding jsdom; see the script's header), each case's store built through Slack's reducers."""
    node = shutil.which('node')
    if not node:
        raise Unavailable('no node on PATH')
    f = os.path.join(tmp, 'snapshot.json')
    with open(f, 'w', encoding='utf-8') as fh:
        json.dump({'base': base or {}, 'cases': cases}, fh, ensure_ascii=False)
    p = subprocess.run([node, MESSAGE_ACTIONS_ORACLE, f], capture_output=True, text=True, timeout=900)
    if p.returncode:
        raise Unavailable(f'the harness failed ({p.returncode}): {p.stderr.strip()[-500:]}')
    return json.loads(p.stdout.rstrip('\n').split('\n')[-1])


SLASH_ORACLE = os.path.join(ROOT, 'tools', 'oracles', 'slash_commands.cjs')


def refresh_slash_routing(cases, tmp, base=None):
    """What Slack's composer does with each case's typed command: tools/oracles/slash_commands.cjs runs Slack's
    send path (handleSlashCommand, executeCommand) in node over its CDN chunks (SLACK_CHUNKS, JSDOM)."""
    node = shutil.which('node')
    if not node:
        raise Unavailable('no node on PATH')
    f = os.path.join(tmp, 'snapshot.json')
    with open(f, 'w', encoding='utf-8') as fh:
        json.dump({'base': base or {}, 'cases': cases}, fh, ensure_ascii=False)
    p = subprocess.run([node, SLASH_ORACLE, f], capture_output=True, text=True, timeout=900)
    if p.returncode:
        raise Unavailable(f'the harness failed ({p.returncode}): {p.stderr.strip()[-500:]}')
    return json.loads(p.stdout.rstrip('\n').split('\n')[-1])


REFRESHERS = {'emoji-rank': refresh_emoji_rank, 'message-actions': refresh_message_actions, 'slash-routing': refresh_slash_routing,
              **{k: quip_refresher(k) for k in QUIP_SNAPSHOTS}}


def dump_snapshot(s):
    """One case per line, so a refresh's diff shows which inputs changed."""
    head = [f'  {json.dumps(k)}: {json.dumps(v, ensure_ascii=False)},' for k, v in s.items() if k != 'cases']
    cases = [json.dumps(c, ensure_ascii=False) for c in s['cases']]
    body = ['    ' + c + (',' if i < len(cases) - 1 else '') for i, c in enumerate(cases)]
    return '\n'.join(['{', *head, '  "cases": [', *body, '  ]', '}']) + '\n'


def refresh(reg, dims, dry_run=False, root=ROOT, refreshers=None, today=None):
    """Run each selected snapshot's oracle again; print what changed and (unless dry_run) rewrite it."""
    refreshers = REFRESHERS if refreshers is None else refreshers
    today = today or datetime.datetime.now(datetime.timezone.utc).strftime('%Y-%m-%d')
    code = 0
    for d in reg['dimensions']:
        if d['id'] not in dims:
            continue
        snaps = [c for c in d['checks'] if c.get('snapshot')]
        if not snaps:
            print(f'{d["id"]}: no snapshot to refresh; Slack\'s values are in the tests themselves')
            code = 2
        for c in snaps:
            path = os.path.join(root, c['snapshot'])
            s = read_json(path)
            name = s['slack'].get('refresh')
            fn = refreshers.get(name) if name else None
            if fn is None:
                print(f'{d["id"]}: {c["snapshot"]} has no refresh wired up. {s["slack"].get("notes", "")}'.rstrip())
                code = 2
                continue
            started = time.monotonic()
            try:
                with tempfile.TemporaryDirectory(dir=os.environ.get('CLACK_PARITY_TMP')) as tmp:
                    # a snapshot's base (the inputs its cases share) goes to its oracle too
                    fresh = fn(s['cases'], tmp, base=s['base']) if 'base' in s else fn(s['cases'], tmp)
            except Unavailable as e:
                print(f'{d["id"]}: can\'t refresh here: {e}')
                code = 2
                continue
            changed = 0
            for case in s['cases']:
                new = fresh.get(case['name'])
                if new is None:
                    print(f'  {case["name"]}: the oracle gave nothing')
                    code = 2
                elif case.get('slack') is None:
                    print(f'  {case["name"]}: new: {short(new)}')
                    case['slack'] = new
                    changed += 1
                elif new != case['slack']:
                    fd = first_difference(case['slack'], new)
                    print(f'  {case["name"]}: Slack CHANGED' + (f', {fd.replace("Clack", "now")}' if fd else ''))
                    print(f'    was {short(case["slack"])}\n    now {short(new)}')
                    case['slack'] = new
                    changed += 1
            print(f'{d["id"]}: {len(s["cases"])} cases through the oracle in {time.monotonic() - started:.1f}s, '
                  f'{changed} changed on Slack\'s side')
            if changed and not dry_run:
                s['slack']['captured'] = today
                with open(path, 'w', encoding='utf-8') as f:
                    f.write(dump_snapshot(s))
                print(f'  wrote {c["snapshot"]}; now run tools/parity.py {d["id"]} to see whether Clack still matches')
    return code


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument('dims', nargs='*', metavar='DIMENSION', help='only these dimensions (default: all)')
    ap.add_argument('--from', dest='inputs', nargs='+', metavar='PATH', help='go test -json files or directories')
    ap.add_argument('--list', action='store_true', help='print the dimensions and stop')
    ap.add_argument('--refresh', action='store_true', help='rerun Slack\'s side of the named dimensions')
    ap.add_argument('--dry-run', action='store_true', help='with --refresh: show changes, write nothing')
    ap.add_argument('--out', metavar='DIR', help='write parity-report.md and parity-report.json here')
    ap.add_argument('--fresh', action='store_true', help='go test -count=1')
    ap.add_argument('--strict', action='store_true', help='exit 1 unless every selected dimension is the same')
    ap.add_argument('--registry', default=REGISTRY, help=argparse.SUPPRESS)
    args = ap.parse_args(argv)
    started = time.monotonic()
    try:
        reg = load_registry(args.registry)
        module = module_path()
    except (RegistryError, OSError, ValueError) as e:
        print(f'parity: bad registry {args.registry}:\n{e}', file=sys.stderr)
        return 2
    known = [d['id'] for d in reg['dimensions']]
    unknown = [d for d in args.dims if d not in known]
    if unknown:
        print(f'parity: unknown dimensions {", ".join(unknown)}; there are {", ".join(known)}', file=sys.stderr)
        return 2
    dims = set(args.dims) or None
    if args.list:
        for d in reg['dimensions']:
            snaps = [c for c in d['checks'] if c.get('snapshot')]
            refreshable = [c for c in snaps if read_json(os.path.join(ROOT, c['snapshot']))['slack'].get('refresh')]
            n = len(d['checks'])
            print(f'{d["id"]:18} {d["tier"]:8} {d["area"]:18} {n} test{"s" * (n != 1)}, {len(snaps)} with a snapshot, '
                  f'refresh: {"yes" if refreshable else "no"}')
            for c in d['checks']:
                print(f'    {c["package"]} {c["test"]}' + (f'  [{c["snapshot"]}]' if c.get('snapshot') else ''))
        return 0
    if args.refresh:
        if not args.dims:
            print('parity: --refresh needs the dimensions to refresh', file=sys.stderr)
            return 2
        return refresh(reg, set(args.dims), args.dry_run)
    if args.inputs:
        missing = [p for p in args.inputs if not os.path.exists(p)]
        if missing:
            print(f'parity: no such input: {", ".join(missing)}', file=sys.stderr)
            return 2
        events = list(read_events(args.inputs))
        what = f'{len(json_files(args.inputs))} go test -json files'
    else:
        events, what = run_go(reg, dims, args.fresh)
    results = collect(events)
    r = report(reg, results, module, {'what': what, 'events': results.events}, current_commit(), dims=dims)
    print('\n'.join(blunt(r)))
    if args.out or os.environ.get('GITHUB_STEP_SUMMARY'):
        md = markdown(r, slack_named_unregistered(reg))
        if args.out:
            os.makedirs(args.out, exist_ok=True)
            with open(os.path.join(args.out, 'parity-report.md'), 'w', encoding='utf-8') as f:
                f.write(md + '\n')
            with open(os.path.join(args.out, 'parity-report.json'), 'w', encoding='utf-8') as f:
                json.dump(r, f, indent=1, ensure_ascii=False)
                f.write('\n')
        if os.environ.get('GITHUB_STEP_SUMMARY'):
            with open(os.environ['GITHUB_STEP_SUMMARY'], 'a', encoding='utf-8') as f:
                f.write(md + '\n')
    if os.environ.get('GITHUB_ACTIONS') == 'true':
        annotate(r)
    print(f'({time.monotonic() - started:.1f}s)')
    if args.strict and any(d['status'] != 'match' for d in r['dimensions']):
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
