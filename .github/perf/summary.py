#!/usr/bin/env python3
"""Turns one side's perf outputs into a markdown summary and one history row.

usage: summary.py <sidedir> <sha> <ref>
  (writes <sidedir>/summary.md and <sidedir>/history-row.md; compare.py
  imports load() and the formatters)
"""
import os, re, sys, statistics

DESC = {
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


def read(d, name):
    p = os.path.join(d, name)
    return open(p).read() if os.path.exists(p) else ""


def dur_ms(s):
    m = re.match(r"([0-9.]+)(ms|s|µs)?$", s or "")
    if not m:
        return None
    return float(m.group(1)) * {"s": 1000, "ms": 1, "µs": 0.001, None: 1}[m.group(2)]


def med(xs):
    xs = [x for x in xs if x is not None]
    return statistics.median(xs) if xs else None


def load(d):
    """Parses a side dir: go benchmarks, process runs, binary size."""
    bench = {}
    for line in read(d, "bench.txt").splitlines():
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
        if vals:
            bench[name] = vals
    proc = []
    for line in read(d, "process/process.tsv").splitlines():
        f = line.split("\t")
        if len(f) < 8:
            continue
        run, paint, ready, boot, rss, hwm, anon, heap = f[:8]
        paint_t, _, rows_n = paint.partition(" ")
        boot_t, _, cache_t = boot.partition(" ")
        num_or = lambda x: int(x) / 1024 if x.isdigit() and int(x) else None
        try:
            heap_v = float(heap)
        except ValueError:
            heap_v = None
        proc.append(dict(run=run, paint=paint_t, rows=rows_n, ready=ready, boot=boot_t, cache=cache_t,
                         rss=num_or(rss), hwm=num_or(hwm), anon=num_or(anon), heap=heap_v))
    size = read(d, "binsize.txt").strip()
    m = re.search(r"\((\d+) bytes\)", size)
    packages = {}
    for line in read(d, "binsize-packages.txt").splitlines():
        f = line.split()
        if len(f) >= 3:
            packages[" ".join(f[2:])] = float(f[0])
    return dict(bench=bench, proc=proc, size=size, size_bytes=int(m.group(1)) if m else None, packages=packages,
                harness=read(d, "harness.txt").strip())


def headline(data):
    """The numbers the history row and the comparison lead with."""
    proc, bench = data["proc"], data["bench"]
    warm = [p for p in proc if p["run"].startswith("warm")]
    one = lambda run, k: next((p[k] for p in proc if p["run"] == run), None)
    g = lambda n, k="ns/op": bench.get(n, {}).get(k)
    return {
        "warm first paint": (med([dur_ms(p["paint"]) for p in warm]), "ms"),
        "cold first paint": (dur_ms(one("cold", "paint")), "ms"),
        "warm core ready": (med([dur_ms(p["ready"]) for p in warm]), "ms"),
        "cold core ready": (dur_ms(one("cold", "ready")), "ms"),
        "idle RSS (20 s)": (med([p["rss"] for p in warm]), "MB"),
        "idle RSS (95 s)": (one("idle95", "rss"), "MB"),
        "anon RSS (95 s)": (one("idle95", "anon"), "MB"),
        "Go heap (95 s)": (one("idle95", "heap"), "MB"),
        "peak RSS (cold)": (one("cold", "hwm"), "MB"),
        "binary (stripped)": (data["size_bytes"] / 1048576 if data["size_bytes"] else None, "MB"),
        "session heap": (g("BenchmarkSessionMemory", "heap-MB"), "MB"),
    }


def ms(v):
    return "—" if v is None else (f"{v/1e6:.2f} ms" if v >= 1e5 else f"{v/1e3:.0f} µs")


def num(v, unit=""):
    return "—" if v is None else (f"{v:,.0f}{unit}" if abs(v) >= 100 else f"{v:.1f}{unit}")


def mb(v):
    return "—" if v is None else f"{v:.0f} MB"


def main():
    out, sha, ref = sys.argv[1], sys.argv[2][:7], sys.argv[3]
    data = load(out)
    bench, proc, size = data["bench"], data["proc"], data["size"]
    rows = []
    for name, v in bench.items():
        extra = ", ".join(f"{num(x)} {k}" for k, x in v.items() if k not in ("ns/op", "B/op", "allocs/op"))
        rows.append(f"| {DESC.get(name, name)} | `{name}` | {ms(v.get('ns/op'))} | {num(v.get('allocs/op'))} | {num((v.get('B/op') or 0)/1024)} KB | {extra} |")

    md = [f"## Clack perf — `{sha}` ({ref})", ""]
    md += ["Real binary on Xvfb + llvmpipe (software GL) against `fakeslackd` (synthetic Hack Club-sized workspace: "
           "65k custom emoji, 400 channels, 3000 users). RSS includes Mesa/LLVM (~70 MB file-backed on llvmpipe).", ""]
    md += ["| run | first paint | sidebar rows | core ready | boot (cache read) | RSS idle | peak RSS | anon RSS | Go heap |",
           "|---|---|---|---|---|---|---|---|---|"]
    for p in proc:
        heap = "—" if p["heap"] is None else f"{p['heap']:.0f} MB"
        md.append(f"| {p['run']} | {p['paint']} | {p['rows']} | {p['ready']} | {p['boot']} ({p['cache']}) | {mb(p['rss'])} | {mb(p['hwm'])} | {mb(p['anon'])} | {heap} |")
    md += ["", f"Binary: {size}" if size else "", ""]
    if data["harness"]:
        md += ["Perf tests: " + "; ".join(data["harness"].splitlines()), ""]
    md += ["### Go benchmarks (headless layout" + (" + GPU" if os.environ.get("CLACK_BENCH_GPU") else "") + ")", "",
           "| what | benchmark | time/op | allocs/op | alloc/op | metrics |", "|---|---|---|---|---|---|"] + rows
    heaptop = read(out, "prof/BenchmarkSessionMemory.heap.top.txt")
    if heaptop:
        md += ["", "<details><summary>Session heap (inuse_space top)</summary>", "", "```", heaptop.strip()[:6000], "```", "</details>"]
    smaps = read(out, "process/idle.smaps.txt")
    if smaps:
        md += ["", "<details><summary>Idle RSS by mapping (kB)</summary>", "", "```", smaps.strip()[:3000], "```", "</details>"]
    pk = read(out, "binsize-packages.txt")
    if pk:
        md += ["", "<details><summary>Binary size by package (top 30)</summary>", "", "```", "\n".join(pk.strip().splitlines()[:30]), "```", "</details>"]
    open(os.path.join(out, "summary.md"), "w").write("\n".join(md) + "\n")

    h = headline(data)
    g = lambda n, k="ns/op": bench.get(n, {}).get(k)
    row = "| {date} | `{sha}` | {ref} | {wp} | {cp} | {rss} | {sheap} | {sw} | {sc} | {ty} | {pk} | {bin} |".format(
        date=os.environ.get("RUN_DATE", ""), sha=sha, ref=ref,
        wp=num(h["warm first paint"][0], " ms"), cp=num(h["cold first paint"][0], " ms"),
        rss=num(h["idle RSS (95 s)"][0] or h["idle RSS (20 s)"][0], " MB"),
        sheap=num(h["session heap"][0], " MB"),
        sw=ms(g("BenchmarkChannelSwitch")), sc=ms(g("BenchmarkScroll500")),
        ty=f"{ms(g('BenchmarkTypingFrame'))} / {num(g('BenchmarkTypingFrame', 'allocs/op'))}",
        pk=ms(g("BenchmarkEmojiPickerOpen")), bin=size.split(" ")[0] if size else "—")
    open(os.path.join(out, "history-row.md"), "w").write(row + "\n")
    print("\n".join(md))


if __name__ == "__main__":
    main()
