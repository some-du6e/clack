#!/usr/bin/env python3
"""ci.yml's `go test ./...`, split over parallel jobs, and the check that
the split dropped nothing.

  test-shard.py run  --heavy PKG,PKG --shard K/N [--times FILE] --out DIR
  test-shard.py run  --heavy PKG,PKG --rest      --out DIR
  test-shard.py full --heavy PKG,PKG --split N [--times FILE] [--timeout D] --out DIR
  test-shard.py check [--baseline FILE.jsonl] DIR...
  test-shard.py times [--heavy PKG,PKG] DIR... > .github/test-times.json

The heavy packages' top-level tests (Test*, Fuzz*, Example*: what
`go test` runs without -bench) are split into N shards. With --times
(a JSON object of "PKG Test": seconds, from earlier runs' `go test -json`)
each package's tests are packed by their recorded times, longest first,
each into the shard with the least so far, so the shards take about as
long; a test with no recorded time counts as its package's median.
Without it they are hashed by name. Every shard computes the same split
from the same test lists and file. --rest runs every other package of
`go list ./...` whole. Each run writes DIR/shard.json (the package list,
the heavy packages' test lists, this shard's share) and DIR/*.jsonl
(`go test -json`). `times` reads shard DIRs (test-shard-* artifacts) and
prints their tests' times, averaged over the DIRs that ran each.

full is `go test ./...` in one job, every package at once, with each
heavy package run as N go test processes of its shards' tests instead of
one: Go's -timeout counts from a test binary's start, and internal/ui's
alone neared ci full-test's 30 min (1,713 s in reply-box-2's 45128655580),
while the runner sat idle under it once the other packages were done.
The suite's load stays whole in the one job (what full-test is for), and
each process gets the timeout. It compiles every heavy package's test
binary once first (`go test -exec=true`: concurrent go commands compile
the same packages each), then starts the N shares and the rest at once,
DIR/1..N and DIR/rest laid out as `run` lays out a shard's DIR, and ends
with `check` over them.

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


def pack(pkg, names, n, times):
    """{name: shard} for one package's tests: longest recorded first, each
    into the least loaded shard (the lowest on a tie); a test with no
    recorded time counts as the package's median."""
    known = sorted(times[f'{pkg} {t}'] for t in names if f'{pkg} {t}' in times)
    default = known[len(known) // 2] if known else 1.0
    load = [0.0] * n
    out = {}
    for t in sorted(names, key=lambda t: (-times.get(f'{pkg} {t}', default), t)):
        k = min(range(n), key=lambda i: (load[i], i))
        out[t] = k
        load[k] += times.get(f'{pkg} {t}', default)
    return out, load


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


def plan(pkgs, heavy, lists, times, shard, extra):
    """(meta, jobs) of one shard ('K/N' or 'rest'): jobs is [(name, argv)],
    one go test per heavy package (a -run across packages would also run
    another shard's test of the same name), or one for the rest"""
    meta = {'packages': pkgs, 'heavy': heavy}
    jobs = []
    if shard == 'rest':
        meta['shard'] = 'rest'
        rest = [p for p in pkgs if p not in heavy]
        meta['rest'] = rest
        jobs.append(('rest', ['go', 'test', '-json', '-count=1', *extra, *rest]))
        return meta, jobs
    k, n = (int(x) for x in shard.split('/'))
    if not 1 <= k <= n:
        raise SystemExit(f'bad --shard {shard}')
    meta.update(shard=f'{k}/{n}', k=k, n=n, lists={}, assigned={})
    if times is not None:
        meta['times'] = times[0]
    for p in heavy:
        if p not in lists:
            raise SystemExit(f'no test list for {p}')
        if times is None:
            mine = [t for t in lists[p] if shard_of(p, t, n) == k - 1]
            est = ''
        else:
            at, load = pack(p, lists[p], n, times[1])
            mine = [t for t in lists[p] if at[t] == k - 1]
            est = f' (recorded {load[k - 1]:.0f}s of {sum(load):.0f}s; shards {", ".join(f"{x:.0f}" for x in load)})'
        meta['lists'][p] = lists[p]
        meta['assigned'][p] = mine
        print(f'{p}: {len(mine)} of {len(lists[p])} tests in shard {k}/{n}{est}')
        if mine:
            jobs.append((p.rsplit('/', 1)[-1], ['go', 'test', '-json', '-count=1', *extra, '-run', '^(' + '|'.join(mine) + ')$', p]))
    return meta, jobs


def setup(args):
    """go list ./..., the heavy packages, their test lists (at once) and
    the recorded times ((path, {test: s}) or None)"""
    heavy = [p for p in args.heavy.split(',') if p]
    pkgs = go_list()
    missing = [p for p in heavy if p not in pkgs]
    if missing:
        raise SystemExit(f'heavy packages not in go list ./...: {missing}')
    lists = {}
    ts = [threading.Thread(target=lambda p=p: lists.__setitem__(p, go_test_list(p))) for p in heavy]
    for t in ts:
        t.start()
    for t in ts:
        t.join()
    times = None
    if args.times:
        with open(args.times) as f:
            times = (args.times, json.load(f))
    return pkgs, heavy, lists, times


def start(out, jobs):
    """starts each job's go test, its -json output to out/NAME.jsonl"""
    procs = []
    for name, argv in jobs:
        path = os.path.join(out, f'{name}.jsonl')
        f = open(path, 'w')
        procs.append((name, path, f, subprocess.Popen(argv, stdout=f, stderr=subprocess.STDOUT)))
    return procs


def finish(procs, label=''):
    """waits for procs, then prints each one's packages and failures as
    plain go test would, and its slowest tests; returns the first non-zero
    exit"""
    rc = 0
    for name, path, f, p in procs:
        p.wait()
        f.close()
        rc = rc or p.returncode
    for name, path, f, p in procs:
        summarize(path)
        durations(path)
        if p.returncode:
            print(f'::error::go test ({label}{name}) exited {p.returncode}')
    return rc


def write_meta(out, meta):
    os.makedirs(out, exist_ok=True)
    with open(os.path.join(out, 'shard.json'), 'w') as f:
        json.dump(meta, f, indent=1)


def run(args):
    os.makedirs(args.out, exist_ok=True)
    if args.rest:
        heavy = [p for p in args.heavy.split(',') if p]
        pkgs = go_list()
        missing = [p for p in heavy if p not in pkgs]
        if missing:
            raise SystemExit(f'heavy packages not in go list ./...: {missing}')
        meta, jobs = plan(pkgs, heavy, {}, None, 'rest', [])
    else:
        pkgs, heavy, lists, times = setup(args)
        meta, jobs = plan(pkgs, heavy, lists, times, args.shard, [])
    write_meta(args.out, meta)
    # all at once, as `go test ./...` runs packages in parallel
    return finish(start(args.out, jobs))


def full(args):
    n = args.split
    if n < 1:
        raise SystemExit(f'bad --split {n}')
    os.makedirs(args.out, exist_ok=True)
    pkgs, heavy, lists, times = setup(args)
    extra = [f'-timeout={args.timeout}'] if args.timeout else []
    shares = []  # (dir, label, jobs)
    for k in range(1, n + 1):
        meta, jobs = plan(pkgs, heavy, lists, times, f'{k}/{n}', extra)
        d = os.path.join(args.out, str(k))
        write_meta(d, meta)
        shares.append((d, f'{k}/{n} ', jobs))
    meta, jobs = plan(pkgs, heavy, lists, times, 'rest', extra)
    d = os.path.join(args.out, 'rest')
    write_meta(d, meta)
    shares.append((d, '', jobs))
    # the heavy test binaries' packages compiled once, run nowhere (true
    # stands in for each binary; it overrides GOFLAGS' expect-fail -exec)
    pre = subprocess.run(['go', 'test', '-count=1', '-exec=true', '-run', '^$', *heavy],
                         stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    if pre.returncode:
        sys.stdout.write(pre.stdout)
        print(f'::error::building {", ".join(heavy)} failed ({pre.returncode})')
        return pre.returncode
    running = [(d, label, start(d, jobs)) for d, label, jobs in shares]
    rc = 0
    for d, label, procs in running:
        rc = finish(procs, label) or rc
    print('::group::every test ran in one of the shares (test-shard.py check)')
    crc = check(argparse.Namespace(dirs=[d for d, _, _ in shares], baseline=None))
    print('::endgroup::')
    if crc:
        print('::error::the split dropped or repeated tests: see test-shard.py check above')
    return rc or crc


def events(path):
    with open(path) as f:
        for line in f:
            try:
                e = json.loads(line)
            except ValueError:
                continue
            if isinstance(e, dict):
                yield e


def cached(paths):
    """the packages whose result go test replayed from its cache (an "ok
    ... (cached)" line): their tests didn't run. -count=1 keeps every one
    running; this catches a run without it."""
    out = set()
    for path in paths:
        for e in events(path):
            if e.get('Action') == 'output' and not e.get('Test') and '(cached)' in e.get('Output', ''):
                out.add(e.get('Package', '?'))
    return out


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
        for p in sorted(cached(paths)):
            errs.append(f'{d}: {p} was replayed from the test cache, not run')
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


def times(args):
    """every top-level test's elapsed seconds, averaged over the DIRs"""
    heavy = [p for p in (args.heavy or '').split(',') if p]
    got = {}
    for d in args.dirs:
        paths = [os.path.join(d, x) for x in sorted(os.listdir(d)) if x.endswith('.jsonl')]
        for path in paths:
            for e in events(path):
                t = e.get('Test', '')
                if t and '/' not in t and e.get('Action') in ('pass', 'fail', 'skip') and e.get('Elapsed') is not None:
                    if not heavy or e['Package'] in heavy:
                        got.setdefault(f"{e['Package']} {t}", []).append(e['Elapsed'])
    out = {k: round(sum(v) / len(v), 2) for k, v in sorted(got.items())}
    json.dump(out, sys.stdout, indent=0, sort_keys=True)
    sys.stdout.write('\n')
    return 0


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest='cmd', required=True)
    r = sub.add_parser('run')
    r.add_argument('--heavy', required=True)
    g = r.add_mutually_exclusive_group(required=True)
    g.add_argument('--shard')
    g.add_argument('--rest', action='store_true')
    r.add_argument('--times')
    r.add_argument('--out', required=True)
    fu = sub.add_parser('full')
    fu.add_argument('--heavy', required=True)
    fu.add_argument('--split', type=int, required=True)
    fu.add_argument('--times')
    fu.add_argument('--timeout', help="go test's -timeout for every process (e.g. 30m)")
    fu.add_argument('--out', required=True)
    c = sub.add_parser('check')
    c.add_argument('--baseline')
    c.add_argument('dirs', nargs='+')
    tm = sub.add_parser('times')
    tm.add_argument('--heavy', help='only these packages (comma-separated)')
    tm.add_argument('dirs', nargs='+')
    args = ap.parse_args()
    sys.exit({'run': run, 'full': full, 'check': check, 'times': times}[args.cmd](args))


if __name__ == '__main__':
    main()
