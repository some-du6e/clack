#!/usr/bin/env python3
"""perf.yml's regression gate: each gated metric, head against base, against
that metric's noise band.

  gate.py check <outdir> <noise.json>   pass/fail lines (markdown, also to the
                                        step summary); exit 1 on a regression
  gate.py sample <outdir>               one base-vs-base sample: JSON of each
                                        metric's base and head
  gate.py calibrate <sample.json>...    noise.json from those samples

Both sides of a perf run ran on one runner, interleaved (README.md), so a
head number worse than base's by more than the two sides of an unchanged
tree differ is the change's. The bands come from base-vs-base runs (the
same commit as both sides, `-f base=<its sha> -f force=true`): a metric's
band is 1.5 times the widest gap those runs showed, relative and absolute,
and never under the metric's floor. Lower is better for every metric, and
only worse fails: a head past base by more than both its relative and its
absolute band.
"""
import json
import os
import sys

sys.dont_write_bytecode = True
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from summary import headline, load  # noqa: E402

HEADROOM = 1.5


def bench(name, metric):
    return lambda d: d['bench'].get(name, {}).get(metric)


def head_line(key):
    return lambda d: headline(d)[key][0]


# key: (what, unit, value of a side, floor: relative, absolute)
METRICS = {
    'scroll-p50': ('scroll frame, median', 'ms', bench('BenchmarkScroll500', 'p50-ms'), 0.05, 1.0),
    'scroll-p95': ('scroll frame, p95', 'ms', bench('BenchmarkScroll500', 'p95-ms'), 0.05, 1.0),
    'typing-p50': ('typing frame, median', 'ms', bench('BenchmarkTypingFrame', 'p50-ms'), 0.05, 1.0),
    'typing-p95': ('typing frame, p95', 'ms', bench('BenchmarkTypingFrame', 'p95-ms'), 0.05, 1.0),
    'idle-p50': ('idle frame, median', 'ms', bench('BenchmarkIdleFrame', 'p50-ms'), 0.05, 1.0),
    'idle-p95': ('idle frame, p95', 'ms', bench('BenchmarkIdleFrame', 'p95-ms'), 0.05, 1.0),
    'idle-cpu': ('idle CPU, the real binary 20-95 s', 'ms/s', head_line('idle CPU (20-95 s)'), 0.10, 0.5),
    'idle-frames': ('idle frames asking for another (wakeups/op)', '', bench('BenchmarkIdleFrame', 'wakeups/op'), 0.0, 0.05),
    'scroll-allocs': ('allocations a scroll frame', '', bench('BenchmarkScroll500', 'allocs/op'), 0.02, 5),
    'typing-allocs': ('allocations a typing frame', '', bench('BenchmarkTypingFrame', 'allocs/op'), 0.02, 5),
    'idle-allocs': ('allocations an idle frame', '', bench('BenchmarkIdleFrame', 'allocs/op'), 0.02, 5),
    'idle-rss': ('idle RSS, the real binary at 95 s', 'MB', head_line('idle RSS (95 s)'), 0.03, 2.0),
    'go-heap': ('Go heap, the real binary at 95 s', 'MB', head_line('Go heap (95 s)'), 0.03, 2.0),
    'session-heap': ('session heap (BenchmarkSessionMemory)', 'MB', bench('BenchmarkSessionMemory', 'heap-MB'), 0.03, 2.0),
}


def values(out):
    base, head = load(os.path.join(out, 'base')), load(os.path.join(out, 'head'))
    return {k: (m[2](base), m[2](head)) for k, m in METRICS.items()}


def fmt(v, unit):
    if v is None:
        return '—'
    s = f'{v:,.2f}' if abs(v) < 100 else f'{v:,.0f}'
    return f'{s} {unit}'.strip()


def check(out, noise_path):
    try:
        noise = json.load(open(noise_path))['bands']
    except FileNotFoundError:
        noise = None
    lines, fails = [], 0
    vals = values(out)
    if noise is None:
        lines.append(f'No noise bands ({noise_path}): this run reports, it doesn\'t gate.')
    nobase = noise is not None and all(b is None for b, _ in vals.values())
    if nobase:
        # the base tree didn't build or run: nothing to compare is no pass
        fails += 1
        lines.append('- ❌ **the base side has no numbers** (see the "build base" step): the gate can\'t compare')
    for k, (b, h) in vals.items():
        what, unit = METRICS[k][0], METRICS[k][1]
        band = (noise or {}).get(k)
        if h is None:
            if noise is not None:
                fails += 1
                lines.append(f'- ❌ **{what}**: no head number (base {fmt(b, unit)}): the gate can\'t tell; see the log')
            continue
        if b is None:
            lines.append(f'- ➖ {what}: no base number (head {fmt(h, unit)}): not gated')
            continue
        d = h - b
        rel = d / abs(b) if b else (0.0 if d == 0 else float('inf'))
        if band is None:
            lines.append(f'- ➖ {what}: {fmt(b, unit)} → {fmt(h, unit)}, {rel:+.1%}: no band, not gated')
            continue
        allowed = max(band['rel'] * abs(b), band['abs'])
        u = f' {unit}' if unit else ''
        txt = f'{fmt(b, unit)} → {fmt(h, unit)} ({d:+,.2f}{u}, {rel:+.1%}); band ±{band["rel"]:.1%} or ±{band["abs"]:g}{u}'
        if d > allowed:
            fails += 1
            lines.append(f'- ❌ **{what}: worse by {rel:+.1%}**, {txt}')
        else:
            lines.append(f'- ✅ {what}: {txt}')
    if noise is None:
        head = '### Perf gate: report only (no noise bands yet)'
    elif nobase:
        head = '### Perf gate: ❌ no base numbers to compare with'
    elif fails:
        head = f'### Perf gate: ❌ {fails} metric{"s" if fails != 1 else ""} worse than base past {"its" if fails == 1 else "their"} noise band'
    else:
        head = '### Perf gate: ✅ no metric worse than base past its noise band'
    md = '\n'.join([head, ''] + lines) + '\n'
    print(md)
    if os.environ.get('GITHUB_STEP_SUMMARY'):
        with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as f:
            f.write(md)
    return 1 if fails else 0


def sample(out):
    json.dump({k: {'base': b, 'head': h} for k, (b, h) in values(out).items()}, sys.stdout, indent=1)
    sys.stdout.write('\n')
    return 0


def calibrate(paths):
    bands = {}
    runs = []
    for p in paths:
        runs.append(os.path.basename(p))
    for k, m in METRICS.items():
        rels, abss = [], []
        for p in paths:
            s = json.load(open(p)).get(k) or {}
            b, h = s.get('base'), s.get('head')
            if b is None or h is None:
                continue
            abss.append(abs(h - b))
            if b:
                rels.append(abs(h - b) / abs(b))
        if not abss:
            continue
        bands[k] = {
            'rel': round(max(m[3], HEADROOM * max(rels, default=0.0)), 4),
            'abs': round(max(m[4], HEADROOM * max(abss)), 3),
            'widest': {'rel': round(max(rels, default=0.0), 4), 'abs': round(max(abss), 3)},
            'samples': len(abss),
        }
    json.dump({'about': f'perf gate noise bands: {HEADROOM} x the widest base-vs-base gap, at least the floor (gate.py)',
               'runs': runs, 'bands': bands}, sys.stdout, indent=1, sort_keys=True)
    sys.stdout.write('\n')
    return 0


def main():
    cmd, args = sys.argv[1], sys.argv[2:]
    if cmd == 'check':
        return check(args[0], args[1])
    if cmd == 'sample':
        return sample(args[0])
    if cmd == 'calibrate':
        return calibrate(args)
    raise SystemExit(f'unknown command {cmd}')


if __name__ == '__main__':
    sys.exit(main())
