#!/usr/bin/env python3
"""Before/after report: the base side against the head side of one run.

Both sides ran on the same runner, interleaved, against the same
fakeslackd (see README.md), so their numbers compare; numbers from
different runs don't (shared runners differ by up to 2x).

usage: compare.py <outdir> <base-label> <head-label>
  reads <outdir>/base and <outdir>/head, writes <outdir>/compare.md
"""
import os, sys

sys.dont_write_bytecode = True
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from summary import DESC, headline, load, ms, num  # noqa: E402

out, base_label, head_label = sys.argv[1], sys.argv[2], sys.argv[3]
base, head = load(os.path.join(out, "base")), load(os.path.join(out, "head"))

# Deltas inside this band are within run-to-run noise of a single
# interleaved run on a shared runner; they're shown, not flagged.
NOISE = 0.10
# metrics where neither direction is better (counts of the workload)
NEUTRAL = {"msgs"}


def delta(b, h, neutral=False):
    if b is None or h is None:
        return "—"
    if b == 0:
        return "=" if h == 0 else "new"
    d = (h - b) / abs(b)
    s = f"{d:+.0%}" if abs(d) >= 0.005 else "±0%"
    if neutral or abs(d) < NOISE:
        return s
    return f"**{s}** {'🔴' if d > 0 else '🟢'}"


def fmt(v, unit):
    if v is None:
        return "—"
    if unit == "ms":
        return f"{v:,.0f} ms" if v >= 100 else f"{v:.1f} ms"
    if unit == "MB":
        return f"{v:.1f} MB"
    return num(v)


md = [f"## Before/after: `{base_label}` → `{head_label}`", "",
      "Same runner, interleaved run by run and benchmark by benchmark; each side built entirely from its own tree, "
      "both against HEAD's `fakeslackd`. Lower is better everywhere; 🔴/🟢 mark changes beyond "
      f"±{NOISE:.0%} (single runs: treat smaller ones as noise).", ""]
md += ["| | base | head | Δ |", "|---|---|---|---|"]
hb, hh = headline(base), headline(head)
for k, (bv, unit) in hb.items():
    md.append(f"| {k} | {fmt(bv, unit)} | {fmt(hh[k][0], unit)} | {delta(bv, hh[k][0])} |")

names = list(head["bench"]) + [n for n in base["bench"] if n not in head["bench"]]
if names:
    md += ["", "### Go benchmarks", "",
           "| what | time/op base → head | Δ | allocs/op base → head | Δ | alloc/op Δ | other metrics (base → head) |",
           "|---|---|---|---|---|---|---|"]
    for n in names:
        b, h = base["bench"].get(n, {}), head["bench"].get(n, {})
        extra = []
        for k in list(h) + [k for k in b if k not in h]:
            if k in ("ns/op", "B/op", "allocs/op"):
                continue
            extra.append(f"{k} {num(b.get(k))} → {num(h.get(k))} ({delta(b.get(k), h.get(k), k in NEUTRAL)})")
        md.append(f"| {DESC.get(n, n)} | {ms(b.get('ns/op'))} → {ms(h.get('ns/op'))} | {delta(b.get('ns/op'), h.get('ns/op'))} "
                  f"| {num(b.get('allocs/op'))} → {num(h.get('allocs/op'))} | {delta(b.get('allocs/op'), h.get('allocs/op'))} "
                  f"| {delta(b.get('B/op'), h.get('B/op'))} | {'; '.join(extra)} |")

pk = [(k, base["packages"].get(k, 0.0), head["packages"].get(k, 0.0)) for k in set(base["packages"]) | set(head["packages"])]
pk = sorted((x for x in pk if abs(x[2] - x[1]) >= 0.01), key=lambda x: -abs(x[2] - x[1]))[:15]
if pk:
    md += ["", "<details><summary>Binary size change by package (15 largest changes)</summary>", "",
           "| package | base | head | Δ |", "|---|---|---|---|"]
    md += [f"| `{k}` | {b:.2f} MB | {h:.2f} MB | {h - b:+.2f} MB |" for k, b, h in pk]
    md += ["</details>"]

notes = []
for side, d in (("base", base), ("head", head)):
    if d["harness"]:
        notes.append(f"{side} perf tests: " + "; ".join(d["harness"].splitlines()))
    if not d["bench"]:
        notes.append(f"**{side}: no benchmark results** (see the log)")
    if not any(p["paint"] not in ("", "?") for p in d["proc"]):
        notes.append(f"**{side}: the real binary never painted** (see {side}/process/*.log in the artifact)")
if notes:
    md += [""] + [f"- {n}" for n in notes]

open(os.path.join(out, "compare.md"), "w").write("\n".join(md) + "\n")
print("\n".join(md))
