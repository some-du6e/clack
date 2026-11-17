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


IDLE_METRICS = [
    ("fps", "frames a second", "/s"),
    ("other_fps", "frames a second besides the caret's", "/s"),
    ("event_frames", "frames an Invalidate asked for", ""),
    ("caret_frames", "caret blinks drawn", ""),
    ("layout_ms", "layout per frame (UI goroutine)", "ms"),
    ("layout_max_ms", "longest layout", "ms"),
    ("gpu_ms", "GPU frame (llvmpipe)", "ms"),
    ("cpu_ms", "process CPU over the pass", "ms"),
    ("snapshot_builds", "sidebar snapshots built", ""),
    ("events", "realtime events", ""),
]


def idle(sides):
    """The window left alone in the bench channel, composer focused:
    quiet, then under a busy workspace's socket about nothing on screen
    (perf_plugins_test.go perfIdlePasses)."""
    names = []
    for runs in sides.values():
        for r in runs:
            for p in r.get("idle") or []:
                if p["name"] not in names:
                    names.append(p["name"])
    if not names:
        return
    print("### Idle frames\n")
    print("The bench channel left alone with its composer focused for 10 s: quiet (`idle`: only the caret's blinks should draw), then under a busy workspace's socket about nothing on screen (`idle-traffic`: status changes every 50 ms, renames, presence, messages in a channel that is already unread, badge counts, a channel the viewer isn't in renamed, deletes, reactions, threads' read states, other people's DND, new emoji, typing elsewhere). Frames come from idleprobe's window loop over the real router; each is the UI goroutine's drain + layout, and in the quiet pass also the GPU renderer's frame in llvmpipe (software: the CPU doing a GPU's work, an upper bound on a blink's cost; left out under traffic, where its ~130 ms frames would cap the count). The traffic pass runs with the plugin set, whose state mirror (internal/taut) draws frames of its own. Medians over the rounds, the rounds in brackets.\n")
    for name in names:
        print(f"#### {name}\n")
        print("| | before | after | change |")
        print("|---|---|---|---|")
        for key, label, unit in IDLE_METRICS:
            cols, meds = [], []
            for side in ("base", "head"):
                vals = [p.get(key) for r in sides[side] for p in (r.get("idle") or []) if p["name"] == name]
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
        for side in ("base", "head"):
            for r in sides[side][:1]:
                for p in r.get("idle") or []:
                    if p["name"] == name and p.get("causes"):
                        print(f"{side} round 1, what asked for the frames: " + "; ".join(f"`{c}`" for c in p["causes"][:6]) + "\n")
    # a caret blink's cost: the quiet pass's frames are the caret's
    for side in ("base", "head"):
        for r in sides[side][:1]:
            for p in r.get("idle") or []:
                if p["name"] == "idle" and p.get("caret_frames"):
                    per = (p.get("layout_ms") or 0) + (p.get("gpu_ms") or 0)
                    rate = p["caret_frames"] / (p.get("seconds") or 10)
                    print(f"{side}: a caret blink costs {p.get('layout_ms', 0):.2f} ms of layout + {p.get('gpu_ms', 0):.2f} ms of GPU frame (llvmpipe) = {per:.2f} ms, {rate:.1f} a second: {per * rate:.1f} ms of CPU a second while the composer is focused.\n")


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
    if unit == "/s":
        return f"{v:.1f}/s"
    if unit == "ms":
        return f"{v:.1f} ms" if v < 100 else f"{v:.0f} ms"
    return f"{v:.0f}"


def recorder(runs):
    """The flight recorder's cost: head's channel-live passes with it off
    and on, in turns on one runner (PERF_PLUGIN_RECORDER)."""
    vals = {"off": {}, "on": {}}
    for r in runs:
        for p in r.get("passes") or []:
            for mode in ("off", "on"):
                if p["name"].endswith(", recorder " + mode):
                    for key, _, _ in METRICS + [("trace_mb", "", ""), ("trace_ms", "", "")]:
                        if p.get(key) is not None:
                            vals[mode].setdefault(key, []).append(p[key])
    if not vals["off"] or not vals["on"]:
        return
    n = len(vals["off"].get("cpu_ms", []))
    print("### The execution-trace flight recorder (head only)\n")
    print(f"channel-live again, {n} passes with the recorder off and {len(vals['on'].get('cpu_ms', []))} with it on, in turns (off-on, on-off, …), without the CPU profiler. Medians, the passes in brackets.\n")
    print("| | off | on | change |")
    print("|---|---|---|---|")
    for key, label, unit in METRICS:
        a, b = vals["off"].get(key), vals["on"].get(key)
        if not a or not b:
            continue
        ma, mb = statistics.median(a), statistics.median(b)
        change = f"{(mb - ma) / ma * 100:+.1f}%" if ma > 0 else ("0" if mb == 0 else "—")
        print(f"| {label} | {fmt(ma, unit)} [{' / '.join(fmt(v, unit) for v in a)}] | {fmt(mb, unit)} [{' / '.join(fmt(v, unit) for v in b)}] | {change} |")
    mb, ms = vals["on"].get("trace_mb"), vals["on"].get("trace_ms")
    if mb and ms:
        print(f"| its window written to a file | | {statistics.median(mb):.1f} MB in {statistics.median(ms):.0f} ms [{' / '.join(f'{v:.1f} MB' for v in mb)}] | |")
    print()


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
        heaps = "; ".join(
            f"{side}: live heap {runs[0].get('heap_mb', 0):.0f} MB" + (f" + {runs[0]['wasm_mb']:.0f} MB of wasm memory outside it" if runs[0].get("wasm_mb") else "")
            for side, runs in sides.items() if runs)
        print(f"{ref.get('messages', 0)} messages in the channel, {ref.get('replies', 0)} replies in the thread, {ref.get('visited', 0)} other channels opened first; {heaps}.\n")
    names = []
    for runs in sides.values():
        for r in runs:
            for p in r.get("passes") or []:
                if p["name"] not in names:
                    names.append(p["name"])
    recorder(sides["head"])
    idle(sides)
    for name in names:
        if ", recorder o" in name:
            continue  # in the recorder's table above
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
