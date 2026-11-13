#!/usr/bin/env python3
"""Clack's standard emoji (Gio's GPU renderer) against Chrome, cell by cell.

Input: the directory TestEmojiRenderShots (internal/ui/theme/
emojirender_test.go) wrote: cases.json, gio-clack-<s>x.png (now),
gio-old-<s>x.png (before: the 22px image stretched by the GPU), and the
workflow's chrome-<s>x.png screenshots of page-<s>x.html (each emoji an
<img> of the URL Slack's client loads at that scale).

Per cell (one emoji at its device rect, x and y snapped like Chromium):

  sharp   mean gradient magnitude of the cell's luminance (central
          differences): how crisp the edges are; reported against Chrome
          (ratio 1.0 = as crisp as Chrome)
  mad     mean |difference| from Chrome over the cell, 0-255 per channel

Output (in <dir>/compare): metrics-<platform>.json and .md, and 400%
side-by-sides sheet-<platform>-<s>x.png: for each mode and set, the first
:grin: of each size, Chrome | Clack | before, nearest-neighbour.

Fails (exit 1) when Clack is less crisp than 0.85x Chrome's, or than the old
path, at any scale above 1 (where the old path stretched the image), or
when it is further from Chrome than the old path there.

usage: emoji_render_compare.py <dir> <platform>
"""
import json
import os
import sys

import numpy as np
from PIL import Image, ImageDraw

W709 = np.array([0.2126, 0.7152, 0.0722])


def tag(s):
    return ("%g" % s) + "x"


def load(path):
    return np.asarray(Image.open(path).convert("RGB"), dtype=np.float64)


def rect(c, s):
    x0, y0 = round(c["x"] * s), round(c["y"] * s)
    n = round(c["size"] * s)
    return x0, y0, x0 + n, y0 + n


def sharp(a):
    l = a @ W709
    gx = np.zeros_like(l)
    gy = np.zeros_like(l)
    gx[:, 1:-1] = (l[:, 2:] - l[:, :-2]) / 2
    gy[1:-1, :] = (l[2:, :] - l[:-2, :]) / 2
    return float(np.mean(np.hypot(gx, gy)))


def main():
    d, plat = sys.argv[1], sys.argv[2]
    cases = json.load(open(os.path.join(d, "cases.json")))
    out = os.path.join(d, "compare")
    os.makedirs(out, exist_ok=True)
    res = {"platform": plat, "urls": cases.get("urls"), "scales": {}}
    md = ["# Standard emoji vs Chrome (%s)" % plat, "",
          "sharp: mean luminance gradient, Clack and before as a fraction of Chrome's. mad: mean |diff| from Chrome (0-255).", "",
          "| scale | size | set | mode | sharp clack | sharp before | mad clack | mad before |", "|---|---|---|---|---|---|---|---|"]
    bad = []
    for s in cases["scales"]:
        t = tag(s)
        imgs = {k: load(os.path.join(d, "%s-%s.png" % (k, t))) for k in ("chrome", "gio-clack", "gio-old")}
        groups = {}
        for c in cases["cells"]:
            x0, y0, x1, y1 = rect(c, s)
            m = {k: v[y0:y1, x0:x1] for k, v in imgs.items()}
            g = groups.setdefault((c["size"], c["style"], c["mode"]), {"sc": [], "so": [], "mc": [], "mo": []})
            ref = sharp(m["chrome"]) or 1e-9
            g["sc"].append(sharp(m["gio-clack"]) / ref)
            g["so"].append(sharp(m["gio-old"]) / ref)
            g["mc"].append(float(np.mean(np.abs(m["gio-clack"] - m["chrome"]))))
            g["mo"].append(float(np.mean(np.abs(m["gio-old"] - m["chrome"]))))
        rs = {}
        for (size, style, mode), g in sorted(groups.items()):
            r = {k: float(np.mean(v)) for k, v in g.items()}
            rs["%d-%s-%s" % (size, style, mode)] = r
            md.append("| %s | %d | %s | %s | %.3f | %.3f | %.2f | %.2f |" % (t, size, style, mode, r["sc"], r["so"], r["mc"], r["mo"]))
            stretched = s > 1 or size > 22
            if r["sc"] < 0.85:
                bad.append("%s %dpx %s %s: Clack's edges %.2fx Chrome's" % (t, size, style, mode, r["sc"]))
            if stretched and (r["sc"] < r["so"] - 0.01 or r["mc"] > r["mo"] + 0.25):
                bad.append("%s %dpx %s %s: no better than before (sharp %.3f vs %.3f, mad %.2f vs %.2f)" % (t, size, style, mode, r["sc"], r["so"], r["mc"], r["mo"]))
        res["scales"][t] = rs
        sheet(imgs, cases, s, os.path.join(out, "sheet-%s-%s.png" % (plat, t)))
    res["failures"] = bad
    json.dump(res, open(os.path.join(out, "metrics-%s.json" % plat), "w"), indent=1)
    md += ["", "failures: %s" % ("none" if not bad else "; ".join(bad))]
    open(os.path.join(out, "metrics-%s.md" % plat), "w").write("\n".join(md) + "\n")
    print("\n".join(md))
    sys.exit(1 if bad else 0)


def sheet(imgs, cases, s, path):
    """400%: per mode/set row, the first :grin: at 22 and 32px, Chrome | Clack | before."""
    z, pad = 4, 8
    rows = []
    for mode in ("light", "dark"):
        for style in ("google", "apple"):
            row = []
            for size in (22, 32):
                c = next(c for c in cases["cells"] if c["mode"] == mode and c["style"] == style and c["name"] == "grin" and c["size"] == size)
                x0, y0, x1, y1 = rect(c, s)
                m = max(1, round(3 * s))
                for k in ("chrome", "gio-clack", "gio-old"):
                    a = imgs[k][y0 - m:y1 + m, x0 - m:x1 + m].astype(np.uint8)
                    row.append(Image.fromarray(a).resize((a.shape[1] * z, a.shape[0] * z), Image.NEAREST))
            rows.append(row)
    cw = max(im.width for r in rows for im in r)
    chh = max(im.height for r in rows for im in r)
    W = len(rows[0]) * (cw + pad) + pad
    H = 20 + len(rows) * (chh + pad) + pad
    sh = Image.new("RGB", (W, H), (128, 128, 128))
    dr = ImageDraw.Draw(sh)
    for i, lab in enumerate(["22 chrome", "22 clack", "22 before", "32 chrome", "32 clack", "32 before"]):
        dr.text((pad + i * (cw + pad), 4), lab + " " + tag(s), fill=(255, 255, 255))
    for j, r in enumerate(rows):
        for i, im in enumerate(r):
            sh.paste(im, (pad + i * (cw + pad), 20 + pad + j * (chh + pad)))
    sh.save(path)


if __name__ == "__main__":
    main()
