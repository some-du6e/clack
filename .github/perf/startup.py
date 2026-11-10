#!/usr/bin/env python3
"""The startup timeline table (startup.sh's startup.tsv of each side).

usage: startup.py <outdir> <base-label> <head-label>
  writes <outdir>/startup.md
"""
import os, re, sys

sys.dont_write_bytecode = True
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from summary import dur_ms  # noqa: E402

out, base_label, head_label = sys.argv[1], sys.argv[2], sys.argv[3]
COLS = ["window", "first-frame", "core-ready", "data-ready", "reveal", "js-runtime", "taut-started", "sidebar-live", "history-live", "messages", "images", "plugins"]


def parse(line):
    t = {"marks": {}, "relayouts": None, "kinds": "", "warm": "", "settled": None, "note": ""}
    if not line:
        return t
    m = re.search(r"settled=(\d+)ms(?: (warm|cold)\b)?", line)
    if m:
        t["settled"], t["warm"] = int(m.group(1)), m.group(2) or ""
    for k, v in re.findall(r" ([a-z-]+)=(\d+)ms", line):
        if k != "settled":
            t["marks"][k] = int(v)
    m = re.search(r"relayouts=(\d+)(?: \(([^)]*)\))?", line)
    if m:
        t["relayouts"] = int(m.group(1))
        t["kinds"] = m.group(2) or ""
    m = re.search(r"\[(\w+)\]", line)
    if m:
        t["note"] = m.group(1)
    return t


def rows(side):
    p = os.path.join(out, side, "startup", "startup.tsv")
    if not os.path.exists(p):
        return []
    res = []
    for line in open(p).read().splitlines():
        f = line.split("\t")
        if len(f) < 4:
            f += [""] * (4 - len(f))
        run, paint, ready, tl = f[:4]
        res.append((run, dur_ms(paint), dur_ms(ready), parse(tl)))
    return res


def ms(v):
    return "—" if v is None else f"{v:,.0f}"


md = ["## Startup timeline", "",
      "The real binary on Xvfb against a Hack Club-sized `fakeslackd` with "
      f"{os.environ.get('LATENCY', '150ms')} per API call and {os.environ.get('IMG_LATENCY', '40ms')} per image, "
      "and an account theme that isn't Slack's default. Milestones are ms since the process started "
      "(applog/startup.go): `sidebar`/`messages` are the first frames showing them (from the boot snapshot on a warm "
      "start), `sidebar-live`/`history-live` when Slack's answers replaced them, `settled` the last change before "
      "1.5 s of quiet. `relayouts` counts frames where something already painted moved: sidebar rows, unread state, "
      "message rows, the theme, counted only once the window is revealed. `reveal` is when the loading cover lifted "
      "(internal/ui/reveal.go): from then the window shows everything at once; before it, nothing but the cover. "
      "`cold` starts have nothing on disk (no metadata cache, no compiled JS engine, no saved theme or plugin slots); "
      "`warm` starts have what a normal start has (meta.json, the engine's compile cache, ui.json). The base still "
      "has alpha.28's boot snapshot, which HEAD removed (docs/STARTUP.md).", ""]
pl = os.path.join(out, "startup-plugins.txt")
if os.path.exists(pl):
    md += [open(pl).read().strip(), ""]
hdr = ["side", "run", "first paint", "core ready (boot)"] + COLS + ["**settled**", "relayouts"]
md += ["| " + " | ".join(hdr) + " |", "|" + "---|" * len(hdr)]
for side, label in (("base", base_label), ("head", head_label)):
    for run, paint, ready, t in rows(side):
        cells = [label if side == "base" else "head", run + (f" ({t['warm']})" if t["warm"] else ""), ms(paint), ms(ready)]
        cells += [ms(t["marks"].get(c)) for c in COLS]
        st = ms(t["settled"])
        if t["note"]:
            st += f" ({t['note']})"
        cells.append(f"**{st}**")
        rl = "—" if t["relayouts"] is None else str(t["relayouts"]) + (f" ({t['kinds']})" if t["kinds"] else "")
        cells.append(rl)
        md.append("| " + " | ".join(cells) + " |")
cf = os.path.join(out, "head", "startup", "cache-files.txt")
if os.path.exists(cf):
    md += ["", "<details><summary>HEAD's cache directory after the runs (mode, size, path)</summary>", "", "```",
           open(cf).read().rstrip(), "```", "</details>"]
open(os.path.join(out, "startup.md"), "w").write("\n".join(md) + "\n")
print("\n".join(md))
