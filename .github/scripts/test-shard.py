#!/usr/bin/env python3
"""ci.yml's `go test ./...`, split over parallel jobs, and the check that
the split dropped nothing.

  test-shard.py run  --heavy PKG,PKG --shard K/N --out DIR
  test-shard.py run  --heavy PKG,PKG --rest      --out DIR
  test-shard.py check [--baseline FILE.jsonl] DIR...

The heavy packages' top-level tests (Test*, Fuzz*, Example*: what
`go test` runs without -bench) are hashed by name into N shards, so a
new test lands in one shard and the shards stay about even as tests are
added. --rest runs every other package of `go list ./...` whole. Each
run writes DIR/shard.json (the package list, the heavy packages' test
lists, this shard's share) and DIR/*.jsonl (`go test -json`).

check reads every shard's DIR and fails unless all shards saw the same
packages and test lists, every package finished in the shards that own
it, and each heavy package's tests ran (pass, fail or skip) exactly once
across the shards. With --baseline (a `go test -json ./...` of the
unsplit layout), it also diffs the two sets of test names.
"""
import argparse
import json
import os
import re
import subprocess
import sys
import threading
import zlib

TOP = re.compile(r'^(Test|Fuzz|Example)\w*$')


def go_list():
    out = subprocess.run(['go', 'list', './...'], check=True, capture_output=True, text=True).stdout
    return sorted(out.split())


def go_test_list(pkg):
    p = subprocess.run(['go', 'test', '-list', '.*', pkg], capture_output=True, text=True)
    if p.returncode != 0:
        sys.stdout.write(p.stdout + p.stderr)
        raise SystemExit(f'go test -list {pkg} failed ({p.returncode})')
    return sorted(l for l in p.stdout.split() if TOP.match(l))


def shard_of(pkg, name, n):
    return zlib.crc32(f'{pkg}.{name}'.encode()) % n


def summarize(path):
    """Prints what plain `go test` would: each package's ok/FAIL line, and
    the output of failed tests (and of failed packages outside a test)."""
    out, failed, done = {}, set(), []
    with open(path) as f:
        for line in f:
            try:
                e = json.loads(line)
            except ValueError:
                print(line, end='')
                continue
            key = (e.get('Package', ''), e.get('Test', ''))
            if e.get('Action') == 'output':
                out.setdefault(key, []).append(e.get('Output', ''))
            elif e.get('Action') == 'fail':
                failed.add(key)
            if e.get('Action') in ('pass', 'fail', 'skip') and not e.get('Test'):
                done.append(key[0])
    for key in sorted(failed):
        if key[1]:
            print(f'::group::FAIL {key[0]} {key[1]}')
            sys.stdout.write(''.join(out.get(key, [])))
            print('::endgroup::')
    for pkg in done:
        lines = out.get((pkg, ''), [])
        if (pkg, '') in failed:
            sys.stdout.write(''.join(lines))
        else:
            sys.stdout.write(''.join(l for l in lines if l.startswith(('ok', '?', 'FAIL', 'PASS'))) or f'ok\t{pkg}\n')


def durations(path, top=15):
    rows = []
    with open(path) as f:
        for line in f:
            try:
                e = json.loads(line)
            except ValueError:
                continue
            t = e.get('Test', '')
            if t and '/' not in t and e.get('Action') in ('pass', 'fail') and e.get('Elapsed') is not None:
                rows.append((e['Elapsed'], e['Package'], t))
    rows.sort(reverse=True)
    if rows:
        print(f'::group::slowest tests in {os.path.basename(path)} (total {sum(r[0] for r in rows):.0f}s)')
        for el, pkg, t in rows[:top]:
            print(f'{el:8.2f}s  {pkg} {t}')
        print('::endgroup::')


def run(args):
    os.makedirs(args.out, exist_ok=True)
    heavy = [p for p in args.heavy.split(',') if p]
    pkgs = go_list()
    missing = [p for p in heavy if p not in pkgs]
    if missing:
        raise SystemExit(f'heavy packages not in go list ./...: {missing}')
    meta = {'packages': pkgs, 'heavy': heavy}
    jobs = []  # (name, argv)
    if args.rest:
        meta['shard'] = 'rest'
        rest = [p for p in pkgs if p not in heavy]
        meta['rest'] = rest
        jobs.append(('rest', ['go', 'test', '-json', *rest]))
    else:
        k, n = (int(x) for x in args.shard.split('/'))
        if not 1 <= k <= n:
            raise SystemExit(f'bad --shard {args.shard}')
        meta.update(shard=f'{k}/{n}', k=k, n=n, lists={}, assigned={})
        lists = {}
        ts = [threading.Thread(target=lambda p=p: lists.__setitem__(p, go_test_list(p))) for p in heavy]
        for t in ts:
            t.start()
        for t in ts:
            t.join()
        for p in heavy:
            if p not in lists:
                raise SystemExit(f'no test list for {p}')
            mine = [t for t in lists[p] if shard_of(p, t, n) == k - 1]
            meta['lists'][p] = lists[p]
            meta['assigned'][p] = mine
            print(f'{p}: {len(mine)} of {len(lists[p])} tests in shard {k}/{n}')
            if mine:
                jobs.append((p.rsplit('/', 1)[-1], ['go', 'test', '-json', '-run', '^(' + '|'.join(mine) + ')$', p]))
    with open(os.path.join(args.out, 'shard.json'), 'w') as f:
        json.dump(meta, f, indent=1)
    # one go test per heavy package (a -run across packages would also run
    # another shard's test of the same name), all at once as `go test ./...`
    # runs packages in parallel
    procs = []
    for name, argv in jobs:
        path = os.path.join(args.out, f'{name}.jsonl')
        f = open(path, 'w')
        procs.append((name, path, f, subprocess.Popen(argv, stdout=f, stderr=subprocess.STDOUT)))
    rc = 0
    for name, path, f, p in procs:
        p.wait()
        f.close()
        rc = rc or p.returncode
    for name, path, f, p in procs:
        summarize(path)
        durations(path)
        if p.returncode:
            print(f'::error::go test ({name}) exited {p.returncode}')
    return rc


def events(path):
    with open(path) as f:
        for line in f:
            try:
                e = json.loads(line)
            except ValueError:
                continue
            if isinstance(e, dict):
                yield e


def ran(paths):
    """{(pkg, top-level test): count} of tests that finished, and the set
    of packages that finished."""
    tests, done = {}, set()
    for path in paths:
        for e in events(path):
            if e.get('Action') not in ('pass', 'fail', 'skip'):
                continue
            t = e.get('Test', '')
            if not t:
                done.add(e['Package'])
            elif '/' not in t:
                key = (e['Package'], t)
                tests[key] = tests.get(key, 0) + 1
    return tests, done


def check(args):
    errs = []
    shards = []
    for d in args.dirs:
        with open(os.path.join(d, 'shard.json')) as f:
            meta = json.load(f)
        paths = [os.path.join(d, x) for x in sorted(os.listdir(d)) if x.endswith('.jsonl')]
        shards.append((d, meta, *ran(paths)))
    if not shards:
        raise SystemExit('no shards')
    pkgs, heavy = shards[0][1]['packages'], shards[0][1]['heavy']
    rest = [s for s in shards if s[1]['shard'] == 'rest']
    split = [s for s in shards if s[1]['shard'] != 'rest']
    for d, meta, _, _ in shards:
        if meta['packages'] != pkgs or meta['heavy'] != heavy:
            errs.append(f'{d}: a different package list from {shards[0][0]}')
    if len(rest) != 1:
        errs.append(f'{len(rest)} rest shards, want 1')
    ns = {s[1]['n'] for s in split}
    if len(ns) != 1:
        errs.append(f'shards disagree on N: {sorted(ns)}')
    else:
        n = ns.pop()
        ks = sorted(s[1]['k'] for s in split)
        if ks != list(range(1, n + 1)):
            errs.append(f'shards {ks} of {n}')
    owned = {}  # package -> shards that ran it
    total = {}
    for d, meta, tests, done in shards:
        for p in done:
            owned.setdefault(p, []).append(meta['shard'])
        for key, c in tests.items():
            total[key] = total.get(key, 0) + c
            if meta['shard'] != 'rest' and key[1] not in meta['assigned'].get(key[0], []):
                errs.append(f'{meta["shard"]} ran {key[0]} {key[1]}, which is not its share')
    for p in pkgs:
        want = [s[1]['shard'] for s in split if s[1]['assigned'].get(p)] if p in heavy else ['rest']
        if sorted(owned.get(p, [])) != sorted(want):
            errs.append(f'{p} finished in shards {sorted(owned.get(p, []))}, want {sorted(want)}')
    for p in heavy:
        lists = {json.dumps(s[1]['lists'][p]) for s in split}
        if len(lists) != 1:
            errs.append(f'shards listed different tests for {p}')
            continue
        names = split[0][1]['lists'][p]
        union = set()
        for s in split:
            union.update(s[1]['assigned'][p])
        if union != set(names):
            errs.append(f'{p}: the shares miss {sorted(set(names) - union)[:20]}')
        for t in names:
            c = total.get((p, t), 0)
            if c != 1:
                errs.append(f'{p} {t}: ran {c} times, want once')
    print(f'{len(shards)} shards, {len(pkgs)} packages, {len(total)} top-level tests ran')
    if args.baseline:
        base, base_done = ran([args.baseline])
        new = set(total)
        old = set(base)
        for key in sorted(old - new):
            errs.append(f'in the baseline only: {key[0]} {key[1]}')
        for key in sorted(new - old):
            print(f'new (not in the baseline): {key[0]} {key[1]}')
        for p in sorted(base_done - set(owned)):
            errs.append(f'package in the baseline only: {p}')
        print(f'baseline: {len(old)} top-level tests, {len(base_done)} packages; split: {len(new)}, {len(owned)}')
    for e in errs:
        print(f'::error::{e}')
    return 1 if errs else 0


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest='cmd', required=True)
    r = sub.add_parser('run')
    r.add_argument('--heavy', required=True)
    g = r.add_mutually_exclusive_group(required=True)
    g.add_argument('--shard')
    g.add_argument('--rest', action='store_true')
    r.add_argument('--out', required=True)
    c = sub.add_parser('check')
    c.add_argument('--baseline')
    c.add_argument('dirs', nargs='+')
    args = ap.parse_args()
    sys.exit(run(args) if args.cmd == 'run' else check(args))


if __name__ == '__main__':
    main()
