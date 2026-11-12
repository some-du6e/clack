#!/usr/bin/env python3
"""Clack's text (Gio's GPU renderer) against Chrome/Edge, cell by cell.

Input: the directory TestTextRenderShots (internal/ui/theme/textrender_test.go)
wrote: cases.json, gio-<variant>-<s>x.png, plus the browser screenshots of
page-<s>x.html the workflow took, named <browser>-<s>x.png (chrome,
chrome-nolcd, chrome-linear, chrome-cdn, edge, ...).

For every scale, cell and image it measures, in sRGB-space "ink" (0 at the
cell's background, 1 at its text colour, the way a browser's blend counts):

  ink     total ink (px)
  stem    mean of the ink maxima along rows (each one a vertical stroke
          crossing): how dark stems get
  bar     the same along columns (horizontal strokes: x-height, baseline,
          crossbars), which vertical hinting moves most
  solid   pixels at >= 0.9 ink; edge: pixels in (0.1, 0.9)
  vsharp  row-profile gradient energy / ink (higher: crisper horizontals)
  hsharp  column-profile gradient energy / ink
  bbox    ink bounding box (font metrics, advances)
  lcd     mean chroma of ink pixels (ClearType colour fringes)

and, against the reference browser image of the same platform (best integer
shift in +-4 px): dx, dy, mad (mean |diff| over pixels inked in either) and
bias (mean signed diff: > 0 the image has more ink than the reference).

transfer: for the calibration cells, the reference's mean ink per bin of
Gio's exact coverage (from gio-raw: Gio blends linearly, so coverage is
(lin(p)-lin(bg))/(lin(fg)-lin(bg))), i.e. the browser's coverage->ink curve.

Output (in <dir>/compare): metrics-<platform>.json, metrics-<platform>.md and
400% side-by-side sheets sheet-<platform>-<s>x.png (main) and
refs-<platform>-<s>x.png (every browser variant).

usage: text_render_compare.py <dir> <platform> [reference browser, default chrome]
       text_render_compare.py fetch-fonts <dir>   (Slack's own Lato, public CDN, no cookie)
"""
import json
import os
import sys
import urllib.request

import numpy as np
from PIL import Image, ImageDraw

W709 = np.array([0.2126, 0.7152, 0.0722])


def lin(c):
    c = np.asarray(c, dtype=np.float64) / 255.0
    return np.where(c <= 0.04045, c / 12.92, ((c + 0.055) / 1.055) ** 2.4)


def tag(s):
    return ("%g" % s) + "x"


def load(path):
    if not os.path.exists(path):
        return None
    return np.asarray(Image.open(path).convert("RGB"), dtype=np.float64)


def ink_map(img, fg, bg):
    d = np.asarray(fg, dtype=np.float64) - bg
    den = (d * d * W709).sum()
    if den < 1e-9:
        return np.zeros(img.shape[:2])
    return ((img - bg) * d * W709).sum(-1) / den


def coverage_map(img, fg, bg):
    lf, lb, lp = lin(fg), lin(bg), lin(img)
    d = lf - lb
    den = (d * d * W709).sum()
    return ((lp - lb) * d * W709).sum(-1) / den


def peaks(a, axis):
    """Mean of local maxima (> 0.2) along axis."""
    a = np.moveaxis(a, axis, -1)
    left = np.concatenate([np.full(a.shape[:-1] + (1,), -1.0), a[..., :-1]], -1)
    right = np.concatenate([a[..., 1:], np.full(a.shape[:-1] + (1,), -1.0)], -1)
    m = (a >= left) & (a > right) & (a > 0.2)
    v = a[m]
    return float(v.mean()) if v.size else 0.0, int(v.size)


def measure(ink, rgb=None):
    p = np.clip(ink, 0, None)
    rows, cols = p.sum(1), p.sum(0)
    tot = float(p.sum())
    ys, xs = np.nonzero(ink > 0.25)
    m = {
        "ink": round(tot, 2),
        "stem": round(peaks(ink, 1)[0], 4),
        "bar": round(peaks(ink, 0)[0], 4),
        "solid": int((ink >= 0.9).sum()),
        "edge": int(((ink > 0.1) & (ink < 0.9)).sum()),
        "vsharp": round(float(np.abs(np.diff(rows)).sum()) / max(tot, 1e-9), 4),
        "hsharp": round(float(np.abs(np.diff(cols)).sum()) / max(tot, 1e-9), 4),
        "hist": np.histogram(np.clip(ink, 0, 1), bins=10, range=(0.05, 1.0001))[0].tolist(),
        "bbox": [int(xs.min()), int(ys.min()), int(xs.max()) + 1, int(ys.max()) + 1] if xs.size else None,
    }
    if rgb is not None:
        sel = ink > 0.1
        if sel.any():
            px = rgb[sel]
            m["lcd"] = round(float((px.max(-1) - px.min(-1)).mean()), 2)
    return m


def shift(a, dx, dy):
    out = np.zeros_like(a)
    h, w = a.shape
    ys, yd = (slice(0, h - dy), slice(dy, h)) if dy >= 0 else (slice(-dy, h), slice(0, h + dy))
    xs, xd = (slice(0, w - dx), slice(dx, w)) if dx >= 0 else (slice(-dx, w), slice(0, w + dx))
    out[yd, xd] = a[ys, xs]
    return out


def align(ref, img, r=4):
    best = None
    for dy in range(-r, r + 1):
        for dx in range(-r, r + 1):
            e = np.abs(shift(img, dx, dy) - ref).sum()
            if best is None or e < best[0]:
                best = (e, dx, dy)
    return best[1], best[2]


def compare(ref, img):
    dx, dy = align(ref, img)
    a = shift(img, dx, dy)
    sel = (ref > 0.05) | (a > 0.05)
    d = (a - ref)[sel]
    return {"dx": dx, "dy": dy, "mad": round(float(np.abs(d).mean()), 4) if d.size else 0.0,
            "bias": round(float(d.mean()), 4) if d.size else 0.0}, a


def transfer(cov, ref_ink, bins=10):
    """Mean reference ink per bin of exact coverage (bins over (0.02, 1])."""
    edges = np.linspace(0.02, 1.0001, bins + 1)
    out = []
    for i in range(bins):
        sel = (cov > edges[i]) & (cov <= edges[i + 1])
        out.append([round(float((edges[i] + edges[i + 1]) / 2), 3),
                    round(float(ref_ink[sel].mean()), 4) if sel.any() else None, int(sel.sum())])
    return out


def up(a, k=4):
    return np.repeat(np.repeat(a, k, 0), k, 1)


def diff_rgb(d):
    d = np.clip(d, -1, 1)
    out = np.zeros(d.shape + (3,))
    out[..., 0] = np.clip(-d, 0, 1) * 255  # red: less ink than the reference
    out[..., 2] = np.clip(d, 0, 1) * 255   # blue: more ink
    return out


def sheet(path, columns, cells, rows):
    """columns: names; rows: per cell a list of RGB crops (or None)."""
    k = 4
    ch = max(r[0].shape[0] for r in rows if r and r[0] is not None) * k
    cw = max(c.shape[1] for r in rows for c in r if c is not None) * k
    head, gap, lab = 18, 6, 150
    W = lab + len(columns) * (cw + gap)
    H = head + len(rows) * (ch + gap)
    out = Image.new("RGB", (W, H), (96, 96, 96))
    d = ImageDraw.Draw(out)
    for i, name in enumerate(columns):
        d.text((lab + i * (cw + gap) + 4, 3), name, fill=(255, 255, 255))
    for j, (cell, r) in enumerate(zip(cells, rows)):
        y = head + j * (ch + gap)
        d.text((4, y + 4), cell["name"], fill=(255, 255, 255))
        for i, crop in enumerate(r):
            if crop is None:
                continue
            im = Image.fromarray(np.clip(up(crop, k), 0, 255).astype(np.uint8))
            out.paste(im, (lab + i * (cw + gap), y))
    out.save(path)


CDN = "https://a.slack-edge.com/bv1-13-br/"
CDN_LATO = ("lato-regular-ef8e44a.woff2", "lato-bold-78da5d4.woff2")  # client-boot-styles' Singer-Lato 400/700


def fetch_fonts(root):
    import brotli  # the bv1-13-br CDN may send brotli bodies whatever the client asks for
    for f in CDN_LATO:
        req = urllib.request.Request(CDN + f, headers={"Accept-Encoding": "br"})
        b = urllib.request.urlopen(req, timeout=120).read()
        if not b.startswith(b"wOF2"):
            b = brotli.decompress(b)
        if not b.startswith(b"wOF2"):
            sys.exit("%s: not a woff2 file" % f)
        open(os.path.join(root, f), "wb").write(b)
        print(f, len(b), "bytes")


def main():
    if len(sys.argv) < 3:
        sys.exit(__doc__)
    if sys.argv[1] == "fetch-fonts":
        return fetch_fonts(sys.argv[2])
    root, platform = sys.argv[1], sys.argv[2]
    refname = sys.argv[3] if len(sys.argv) > 3 else "chrome"
    cases = json.load(open(os.path.join(root, "cases.json")))
    cells, scales, variants = cases["cells"], cases["scales"], cases["variants"]
    cw, chh, tx = cases["cell_w"], cases["cell_h"], cases["text_x"]
    outdir = os.path.join(root, "compare")
    os.makedirs(outdir, exist_ok=True)
    browsers = sorted({f.rsplit("-", 1)[0] for f in os.listdir(root)
                       if f.endswith("x.png") and not f.startswith("gio-")})
    if refname not in browsers:
        sys.exit("no %s-*.png in %s (have %s)" % (refname, root, browsers))
    report = {"platform": platform, "reference": refname, "browsers": browsers, "variants": variants, "scales": {}}
    md = ["# Text rendering, %s: Gio vs %s" % (platform, refname), ""]
    problems = []
    for s in scales:
        t = tag(s)
        imgs = {"gio-" + v: load(os.path.join(root, "gio-%s-%s.png" % (v, t))) for v in variants}
        for b in browsers:
            imgs[b] = load(os.path.join(root, "%s-%s.png" % (b, t)))
        W, H = int(round(cw * s)), int(round(len(cells) * chh * s))
        for name, im in list(imgs.items()):
            if im is None:
                problems.append("%s %s: missing" % (name, t))
                del imgs[name]
            elif im.shape[:2] != (H, W):
                problems.append("%s %s: %dx%d, want %dx%d" % (name, t, im.shape[1], im.shape[0], W, H))
                del imgs[name]
        if refname not in imgs:
            continue
        sres = report["scales"][t] = {}
        main_cols = [refname] + ["gio-" + v for v in variants] + ["diff clack", "diff skia"]
        ref_cols = [b for b in browsers if b in imgs]
        main_rows, ref_rows = [], []
        md += ["## %s" % t, "",
               "ink ratio vs %s (stem/bar: peak ink difference; mad: mean abs per-pixel diff)" % refname, "",
               "| cell | " + " | ".join(n for n in imgs if n != refname) + " |",
               "|---|" + "---|" * (len(imgs) - 1)]
        for c in cells:
            y0, y1 = int(round(c["y"] * s)), int(round((c["y"] + chh) * s))
            fg = np.array(c["fg"][:3], dtype=np.float64)
            bg = np.array(c["bg"], dtype=np.float64)
            cres = sres[c["name"]] = {}
            inks = {}
            for name, im in imgs.items():
                crop = im[y0:y1, :]
                inks[name] = ink_map(crop, fg, bg)
                cres[name] = measure(inks[name], crop if not name.startswith("gio-") else None)
            ref = inks[refname]
            aligned = {}
            for name in imgs:
                if name == refname:
                    continue
                cmpd, aligned[name] = compare(ref, inks[name])
                cres[name].update(cmpd)
                r, i = cres[refname], cres[name]
                cres[name]["ink_ratio"] = round(i["ink"] / max(r["ink"], 1e-9), 4)
                if r["bbox"] and i["bbox"]:
                    rw, iw = r["bbox"][2] - r["bbox"][0], i["bbox"][2] - i["bbox"][0]
                    if abs(iw - rw) > 0.06 * rw:
                        problems.append("%s %s %s: ink width %d vs %s %d (a fallback font?)" % (t, c["name"], name, iw, refname, rw))
            if "gio-raw" in imgs and c["mode"] == "cal":
                # Gio's exact coverage, moved onto the reference's grid
                cov = coverage_map(imgs["gio-raw"][y0:y1, :], fg, bg)
                cr, cov = compare(ref, cov)
                cres["transfer"] = {"coverage_shift": [cr["dx"], cr["dy"]]}
                for name in imgs:
                    if name == "gio-raw":
                        continue
                    tgt = ref if name == refname else aligned[name]
                    cres["transfer"][name] = transfer(cov, tgt)
            md.append("| %s | " % c["name"] + " | ".join(
                "%.1f%% s%+.3f b%+.3f m%.3f%s" % (
                    100 * cres[n]["ink_ratio"], cres[n]["stem"] - cres[refname]["stem"],
                    cres[n]["bar"] - cres[refname]["bar"], cres[n]["mad"],
                    (" lcd%.0f" % cres[n]["lcd"]) if "lcd" in cres[n] else "")
                for n in imgs if n != refname) + " |")
            # 400% crops: the first ~120 device px of the line
            x0 = max(0, int(round(tx * s)) - 3)
            x1 = min(int(round(cw * s)), x0 + 120)

            def crop_of(name):
                return imgs[name][y0:y1, x0:x1] if name in imgs else None
            row = [crop_of(refname)] + [crop_of("gio-" + v) for v in variants]
            for v in ("gio-clack", "gio-skia"):
                row.append(diff_rgb(aligned[v] - ref)[:, x0:x1] if v in aligned else None)
            main_rows.append(row)
            ref_rows.append([crop_of(b) for b in ref_cols])
        md.append("")
        sheet(os.path.join(outdir, "sheet-%s-%s.png" % (platform, t)), main_cols, cells, main_rows)
        sheet(os.path.join(outdir, "refs-%s-%s.png" % (platform, t)), ref_cols, cells, ref_rows)
        # aggregate: mean ink ratio per variant over the non-calibration cells, per mode
        md += ["mean ink ratio, stem and bar difference by mode:", ""]
        for mode in ("dark", "light", "cal"):
            sel = [c["name"] for c in cells if c["mode"] == mode]
            parts = []
            for n in imgs:
                if n == refname:
                    continue
                ir = np.mean([sres[x][n]["ink_ratio"] for x in sel])
                st = np.mean([sres[x][n]["stem"] - sres[x][refname]["stem"] for x in sel])
                br = np.mean([sres[x][n]["bar"] - sres[x][refname]["bar"] for x in sel])
                parts.append("%s %.1f%% s%+.3f b%+.3f" % (n, 100 * ir, st, br))
            md.append("- %s: %s" % (mode, "; ".join(parts)))
        md.append("")
    md += ["problems:", ""] + ["- " + p for p in problems] if problems else ["no problems"]
    report["problems"] = problems
    json.dump(report, open(os.path.join(outdir, "metrics-%s.json" % platform), "w"), indent=1)
    open(os.path.join(outdir, "metrics-%s.md" % platform), "w").write("\n".join(md) + "\n")
    print("\n".join(md))


if __name__ == "__main__":
    main()
