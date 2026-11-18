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
    ("frames", "frames drawn", ""),
    ("total_ms", "sum of the frames", "ms"),
    ("cpu_ms", "process CPU", "ms"),
    ("allocs", "allocations / frame", ""),
    ("alloc_kb", "KB allocated / frame", ""),
    ("gcs", "collections", ""),
    ("gc_pause", "GC pauses", "ms"),
    ("trees", "plugin trees that changed (during the frames)", ""),
    ("trees_same", "plugin trees re-delivered unchanged", ""),
    ("renders", "Go→JS renders", ""),
    ("tasks", "Go→JS tasks (idle passes: the sweep's unmounts not counted)", ""),
    ("unmounts", "roots unmounted (slackc's sweep)", ""),
    ("polls", "core's polls during the frames (each reverts injected edits)", ""),
    ("polls_cpu", "core's polls during the measured CPU (the frames, then the JS drain)", ""),
    ("backlog_ms", "JS backlog after the frames", "ms"),
    # the gpu passes (Gio's frame stats, Clack patch 36, on trees that have it)
    ("draws", "GPU draw calls / frame", ""),
    ("api_calls", "graphics API calls / frame", ""),
    ("api_skipped", "API calls the state cache left out / frame", ""),
    ("crossings", "calls from Go into C or the system / frame", ""),
    ("paths_built", "paths tessellated and uploaded / frame", ""),
    ("cover_mpx", "Mpx covered through a path / frame", "mpx"),
    ("blit_mpx", "Mpx blitted / frame", "mpx"),
    ("anim_frames", "animation frames shown (every player's)", ""),
    ("players", "animations playing at the end", ""),
    ("quiet_frames", "quiet frames (no layout)", ""),
    # the idle passes, a second (none in the scroll passes)
    ("fps", "frames drawn a second", "x"),
    ("tasks_ps", "Go→JS tasks a second", "x"),
    ("renders_ps", "Go→JS renders a second", "x"),
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
    if unit == "mpx":
        return f"{v:.2f}"
    if unit == "ms":
        return f"{v:.1f} ms" if v < 100 else f"{v:.0f} ms"
    if unit == "x":
        return f"{v:.1f}"
    return f"{v:.0f}"


# what the gpu passes don't measure (perfGPUScroll): no row for them
GPU_UNMEASURED = {"polls", "polls_cpu", "tasks", "unmounts", "backlog_ms", "renders", "trees", "trees_same"}

# the idle passes: per-frame allocations read backwards when the frames
# fell, so they're given for the whole pass
IDLE_PER_PASS = {"allocs": "allocations (the whole pass)", "alloc_kb": "KB allocated (the whole pass)"}


def is_idle(p):
    return p.get("seconds") is not None or p.get("fps") is not None


def latency(sides):
    """Update to screen: edits of a message on screen, event to frame."""
    latency_table(sides, "edit_latency_ms", "edit_viewed", "### Update to screen: an edit of a message on screen\n",
                  "Edits of a message in the bench channel (after a first edit: Message Logger keeps an edited message's history in the messages slice), each timed from its event to the first frame that draws it, frames drawn back to back. A row drawing the plugins' view keeps the revision before until JS's copy of the new one is ready; \"never\" is an edit not drawn in 10 s. Not like for like across trees: where a row goes back to its source once the plugins leave its message as it is, the row isn't on the view here and draws the edit at once, while a tree that keeps it on the view waits for JS (see the last column). The next table is like for like.\n")
    latency_table(sides, "masked_edit_ms", "masked_edit_viewed", "### Update to screen: an edit the plugins change\n",
                  "The same, for edits with a word a test plugin masks in the messages slice (as Censorship does), so the row draws the plugins' view on both trees and both wait for JS's copy of each revision: timed to the first frame that draws the masked text.\n")


def latency_table(sides, key, viewed_key, title, text):
    rows = []
    for side in ("base", "head"):
        vals, viewed = [], []
        for r in sides[side]:
            vals += [v for v in (r.get(key) or [])]
            viewed += [bool(v) for v in (r.get(viewed_key) or [])]
        rows.append((side, vals, viewed))
    if not any(v for _, v, _ in rows):
        return
    print(title)
    print(text)
    print("| | edits | median | max | the row on the view |")
    print("|---|---|---|---|---|")
    for side, vals, viewed in rows:
        if not vals:
            print(f"| {side} | — | — | — | — |")
            continue
        drawn = [v for v in vals if v >= 0]
        never = len(vals) - len(drawn)
        med = f"{statistics.median(drawn):.0f} ms" if drawn else "—"
        mx = f"{max(drawn):.0f} ms" if drawn else "—"
        if never:
            mx += f", {never} never"
        print(f"| {side} | {len(vals)} | {med} | {mx} | {sum(viewed)} of {len(viewed)} |")
    print()


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
    print("Each scroll pass is 300 frames at 60 a second, a wheel step each (150 up, 150 down); a frame is the UI goroutine's drain + layout, no GPU. The idle passes (idle, emoji) leave the channel alone for 5 s and draw a frame only when something asks for one, as the window does (emoji: an animated custom emoji on screen); emoji-60hz draws one every 16 ms. Allocations and collections are the whole process's, per frame.")
    print("The gpu passes are the channel scrolled with Gio's GPU renderer drawing every frame (Mesa llvmpipe, which rasterizes on the CPU), each frame timed from its input to the end of its rasterization; their draw and API call counts are Gio's frame stats, where the tree has them.")
    print("Values are the median over the rounds, the rounds in brackets.\n")
    print("Not comparable with runs before 78375754 (plugin-render-3, 2026-11-17): the bench channel was the 41st conversation opened, and the messages hub of every tree before that commit kept no model for it, so the live passes drew a list that took no live event (only its own 30 s refresh); they draw a live list on both trees now (each pass's list_live). Scroll-pass tasks count unmounts too since round 5.\n")
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
    # a list that took no live event, or a poll in an idle pass: the
    # numbers beside it aren't what the pass means to measure
    for side in ("base", "head"):
        for r in sides[side]:
            for p in r.get("passes") or []:
                if p.get("list_live") == "dropped":
                    print(f"**Warning:** {side}'s {p['name']} drew a bench list the messages hub had dropped: it took no live event.\n")
                if is_idle(p) and (p.get("polls") or p.get("polls_cpu")):
                    print(f"**Warning:** {side}'s {p['name']} had {p.get('polls_cpu') or p.get('polls')} of core's polls in it (the alignment didn't hold).\n")
    recorder(sides["head"])
    latency(sides)
    for name in names:
        if ", recorder o" in name:
            continue  # in the recorder's table above
        print(f"### {name}\n")
        if name == "idle-busy":
            print("No input for 10 s while the workspace goes on around the open channel (messages and updates in channels opened earlier, profile changes of people no row draws, badges, a pref's echo, new emoji); a frame is drawn only when something asks for one, as the window draws them. Who asked for the frames, each side's first round:\n")
            for side in ("base", "head"):
                for r in sides[side][:1]:
                    for p in r.get("passes") or []:
                        if p["name"] == name and p.get("sources"):
                            top = sorted(p["sources"].items(), key=lambda kv: -kv[1])[:5]
                            print(f"- {side}: " + "; ".join(f"{n}× {k}" for k, n in top))
            print()
        if name == "channel-live, past 40":
            print("channel-live after 41 more conversations were drawn (the 38 others again, three new): on a tree with 78375754 the hub lets the bench's model go, drawn longest ago, and takes it back as the channel shows; before it, the bench's model stays and each new one is dropped as it's made.\n")
        print("| | before | after | change |")
        print("|---|---|---|---|")
        gpu = name.startswith("gpu ")
        for key, label, unit in METRICS:
            if gpu and key in GPU_UNMEASURED:
                continue
            cols = []
            meds = []
            for side in ("base", "head"):
                ps = [p for r in sides[side] for p in (r.get("passes") or []) if p["name"] == name]
                if key in IDLE_PER_PASS and ps and is_idle(ps[0]):
                    label = IDLE_PER_PASS[key]
                    vals = [p[key] * p.get("frames", 0) for p in ps if p.get(key) is not None]
                else:
                    vals = [p.get(key) for p in ps]
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
            if meds[0] is None and meds[1] is None:
                continue  # neither side has it (the base tree's file predates it)
            print(f"| {label} | {cols[0]} | {cols[1]} | {change} |")
        print()


if __name__ == "__main__":
    main()
