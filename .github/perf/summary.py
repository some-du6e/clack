#!/usr/bin/env python3
"""Turns perf outputs into a markdown summary and one history row.

usage: summary.py <outdir> <sha> <ref>   (writes <outdir>/summary.md and <outdir>/history-row.md)
"""
import os, re, sys, statistics

out, sha, ref = sys.argv[1], sys.argv[2][:7], sys.argv[3]

def read(name):
    p = os.path.join(out, name)
    return open(p).read() if os.path.exists(p) else ""

# ---- go benchmarks ----
bench = {}
for line in read("bench.txt").splitlines():
    if not line.startswith("Benchmark"):
        continue
    parts = line.split()
    name = re.sub(r"-\d+$", "", parts[0])
    vals = {}
    for i in range(2, len(parts) - 1, 2):
        try:
            vals[parts[i + 1]] = float(parts[i])
        except ValueError:
            pass
    bench[name] = vals

def ms(v):
    return "—" if v is None else (f"{v/1e6:.2f} ms" if v >= 1e5 else f"{v/1e3:.0f} µs")

def num(v, unit=""):
    return "—" if v is None else (f"{v:,.0f}{unit}" if abs(v) >= 100 else f"{v:.1f}{unit}")

rows = []
desc = {
    "BenchmarkStartup": "warm start → sidebar frame / core ready (headless)",
    "BenchmarkBootCold": "first start ever, to core ready",
    "BenchmarkCoreBoot/cold": "core.Boot, no cache",
    "BenchmarkCoreBoot/warm": "core.Boot, warm metadata cache",
    "BenchmarkLoadCache": "metadata cache read (65k emoji, users, groups)",
    "BenchmarkChannelSwitch": "switch between cached channels (nav + frame)",
    "BenchmarkScroll500": "scroll frame, 500 msgs w/ images",
    "BenchmarkEmojiPickerOpen": "emoji picker open + frame + close",
    "BenchmarkEmojiSearch": "emoji search keystroke + frame",
    "BenchmarkQuickSwitcher": "Ctrl+K, type 4 chars, Esc",
    "BenchmarkTypingFrame": "typing frame (1 char into composer)",
    "BenchmarkIdleFrame": "idle frame (nothing changed)",
    "BenchmarkSessionMemory": "normal session (3 channels, picker, switcher)",
}
for name, v in bench.items():
    extra = ", ".join(f"{num(x)} {k}" for k, x in v.items() if k not in ("ns/op", "B/op", "allocs/op"))
    rows.append(f"| {desc.get(name, name)} | `{name}` | {ms(v.get('ns/op'))} | {num(v.get('allocs/op'))} | {num((v.get('B/op') or 0)/1024)} KB | {extra} |")

# ---- real process ----
proc = []
for line in read("process.tsv").splitlines():
    f = line.split("\t")
    if len(f) < 8:
        continue
    run, paint, ready, boot, rss, hwm, anon, heap = f[:8]
    paint_t, _, rows_n = paint.partition(" ")
    boot_t, _, cache_t = boot.partition(" ")
    proc.append(dict(run=run, paint=paint_t, rows=rows_n, ready=ready, boot=boot_t, cache=cache_t,
                     rss=int(rss or 0) / 1024, hwm=int(hwm or 0) / 1024, anon=int(anon or 0) / 1024, heap=heap))

def dur_ms(s):
    m = re.match(r"([0-9.]+)(ms|s|µs)?$", s or "")
    if not m:
        return None
    v = float(m.group(1))
    return v * {"s": 1000, "ms": 1, "µs": 0.001, None: 1}[m.group(2)]

size = read("binsize.txt").strip()

md = [f"## Clack perf — `{sha}` ({ref})", ""]
md += ["Real binary on Xvfb + llvmpipe (software GL) against `fakeslackd` (synthetic Hack Club-sized workspace: "
       "65k custom emoji, 400 channels, 3000 users). RSS includes Mesa/LLVM (~70 MB file-backed on llvmpipe).", ""]
md += ["| run | first paint | sidebar rows | core ready | boot (cache read) | RSS idle | peak RSS | anon RSS | Go heap |",
       "|---|---|---|---|---|---|---|---|---|"]
for p in proc:
    md.append(f"| {p['run']} | {p['paint']} | {p['rows']} | {p['ready']} | {p['boot']} ({p['cache']}) | {p['rss']:.0f} MB | {p['hwm']:.0f} MB | {p['anon']:.0f} MB | {p['heap']} MB |")
md += ["", f"Binary: {size}" if size else "", ""]
md += ["### Go benchmarks (headless layout" + (" + GPU" if os.environ.get("CLACK_BENCH_GPU") else "") + ")", "",
       "| what | benchmark | time/op | allocs/op | alloc/op | metrics |", "|---|---|---|---|---|---|"] + rows
heaptop = read("prof/BenchmarkSessionMemory.heap.top.txt")
if heaptop:
    md += ["", "<details><summary>Session heap (inuse_space top)</summary>", "", "```", heaptop.strip()[:6000], "```", "</details>"]
smaps = read("process/idle.smaps.txt")
if smaps:
    md += ["", "<details><summary>Idle RSS by mapping (kB)</summary>", "", "```", smaps.strip()[:3000], "```", "</details>"]
open(os.path.join(out, "summary.md"), "w").write("\n".join(md) + "\n")

warm = [p for p in proc if p["run"].startswith("warm")]
def med(xs):
    xs = [x for x in xs if x is not None]
    return statistics.median(xs) if xs else None
g = lambda n, k="ns/op": bench.get(n, {}).get(k)
row = "| {date} | `{sha}` | {ref} | {wp} | {cp} | {rss} | {sheap} | {sw} | {sc} | {ty} | {pk} | {bin} |".format(
    date=os.environ.get("RUN_DATE", ""), sha=sha, ref=ref,
    wp=num(med([dur_ms(p["paint"]) for p in warm]), " ms"),
    cp=num(next((dur_ms(p["paint"]) for p in proc if p["run"] == "cold"), None), " ms"),
    rss=num(med([p["rss"] for p in warm]), " MB"),
    sheap=num(g("BenchmarkSessionMemory", "heap-MB"), " MB"),
    sw=ms(g("BenchmarkChannelSwitch")), sc=ms(g("BenchmarkScroll500")),
    ty=f"{ms(g('BenchmarkTypingFrame'))} / {num(g('BenchmarkTypingFrame', 'allocs/op'))}",
    pk=ms(g("BenchmarkEmojiPickerOpen")), bin=size.split(" ")[0] if size else "—")
open(os.path.join(out, "history-row.md"), "w").write(row + "\n")
print("\n".join(md))
