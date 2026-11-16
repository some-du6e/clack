#!/usr/bin/env python3
"""Before/after table for perf.yml's plugins job.

Reads <dir>/{base,head}-<round>.json (internal/ui/perf_plugins_test.go)
and prints Markdown: for every pass, each side's median over the rounds,
with the rounds' values beside it, longest frame first (a freeze is one
long frame).

usage: plugins.py <dir> <base label> <head label>
"""
import glob
import json
import os
import statistics
import sys

METRICS = [
    ("max_ms", "longest frame", "ms"),
    ("over_250ms", "frames > 250 ms", ""),
    ("over_100ms", "frames > 100 ms", ""),
    ("over_50ms", "frames > 50 ms", ""),
    ("p99_ms", "p99 frame", "ms"),
    ("p95_ms", "p95 frame", "ms"),
    ("median_ms", "median frame", "ms"),
    ("total_ms", "sum of 300 frames", "ms"),
    ("cpu_ms", "process CPU", "ms"),
    ("allocs", "allocations / frame", ""),
    ("alloc_kb", "KB allocated / frame", ""),
    ("gcs", "collections", ""),
    ("gc_pause", "GC pauses", "ms"),
    ("trees", "plugin trees that changed (during the frames)", ""),
    ("trees_same", "plugin trees re-delivered unchanged", ""),
    ("renders", "Go→JS renders", ""),
]


def load(d, side):
    out = []
    for p in sorted(glob.glob(os.path.join(d, f"{side}-*.json"))):
        try:
            with open(p) as f:
                out.append(json.load(f))
        except (OSError, ValueError):
            pass
    return out


def fmt(v, unit):
    if v is None:
        return "—"
    if unit == "ms":
        return f"{v:.1f} ms" if v < 100 else f"{v:.0f} ms"
    return f"{v:.0f}"


def main():
    d, base_label, head_label = sys.argv[1], sys.argv[2], sys.argv[3]
    sides = {"base": load(d, "base"), "head": load(d, "head")}
    print(f"## Scroll frames under a plugin set like Karim's\n")
    print(f"Before: `{base_label}` ({len(sides['base'])} rounds). After: `{head_label}` ({len(sides['head'])} rounds).")
    print("Each pass is 300 frames at 60 a second, a wheel step each (150 up, 150 down); a frame is the UI goroutine's drain + layout, no GPU. Allocations and collections are the whole process's.")
    print("Values are the median over the rounds, the rounds in brackets.\n")
    ref = (sides["head"] or sides["base"] or [{}])[0]
    if ref:
        print(f"Plugins running: {', '.join(ref.get('plugins') or []) or 'none'}" +
              (f"; failed to start: {', '.join(ref['failed'])}" if ref.get("failed") else ""))
        print(f"\nPatched components: {', '.join(ref.get('patched') or [])}\n")
        print(f"{ref.get('messages', 0)} messages in the channel, {ref.get('replies', 0)} replies in the thread, {ref.get('visited', 0)} other channels opened first; live heap {ref.get('heap_mb', 0):.0f} MB.\n")
    names = []
    for runs in sides.values():
        for r in runs:
            for p in r.get("passes") or []:
                if p["name"] not in names:
                    names.append(p["name"])
    for name in names:
        print(f"### {name}\n")
        print("| | before | after | change |")
        print("|---|---|---|---|")
        for key, label, unit in METRICS:
            cols = []
            meds = []
            for side in ("base", "head"):
                vals = [p.get(key) for r in sides[side] for p in (r.get("passes") or []) if p["name"] == name]
                vals = [v for v in vals if v is not None]
                if not vals:
                    cols.append("—")
                    meds.append(None)
                    continue
                m = statistics.median(vals)
                meds.append(m)
                cols.append(f"{fmt(m, unit)} [{' / '.join(fmt(v, unit) for v in vals)}]")
            change = "—"
            if meds[0] is not None and meds[1] is not None:
                if meds[0] > 0:
                    change = f"{(meds[1] - meds[0]) / meds[0] * 100:+.0f}%"
                elif meds[1] == 0:
                    change = "0"
            print(f"| {label} | {cols[0]} | {cols[1]} | {change} |")
        print()


if __name__ == "__main__":
    main()
