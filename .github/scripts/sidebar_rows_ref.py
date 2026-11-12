#!/usr/bin/env python3
"""Clack's sidebar channel rows next to the same rows in Slack's markup under
Slack's shipping CSS, measured and shown at 400%.

Clack's side is TestSidebarRowShots (internal/ui/sidebar_rows_shots_test.go):
fakeslack's Unreads fixtures (synthetic data) as row crops plus rows.json.

Slack's side is built from the public CDN, fetched without a cookie: the
stylesheet client-boot-styles (cf726c4f2eaecc74, the one compact-shots uses)
with Slack's own Lato faces next to it (its @font-face urls are relative),
the design tokens Clack carries (internal/ui/slackc/css/slackvars.go; in
light mode the inverted sidebar's tokens Clack computed, from rows.json),
Slack's SVG icons (slack-datamine-plus icons.json), and the markup of build
fa938a086569's channel row (boot-render):

  ChannelSidebarChannel     p-channel_sidebar__static_list__item >
                            __static_list__item__row_container (padding 0 8px) >
                            div.p-channel_sidebar__channel with --selected,
                            --muted, --unread (and not muted), --has-badge,
                            --show-avatars / --hide-avatars
  BaseChannelPrefixIcon     (akoD) div.p-channel_sidebar__channel_icon_prefix >
                            SvgIcon size 16, "channel", or "channel-filled"
                            for an unread channel that isn't muted
  BaseChannelName           span.p-channel_sidebar__name > span
  BaseChannelSuffix         span.p-channel_sidebar__channel_suffix > the
                            themed pill (c-badge c-badge--themed c-badge--pill
                            p-channel_sidebar__badge)

The column is a plain div of Clack's sidebar width (in CSS px) and the rows'
height: .p-channel_sidebar has contain: strict, so it needs a sized parent.

:hover can't be forced in a screenshot: the CSS rules that name
.p-channel_sidebar__channel:hover are copied with :hover replaced by a class
on the hovered row. Each row's surroundings are the colour Clack's crop shows
just right of the row (the sidebar under it), so the two sides differ only by
the row itself.

Per row the script measures ink (pixels that differ from the row's own fill):
the icon's box, the name's left edge, top and bottom, the badge's box, and
the name's darkest-contrast colour, and writes rows-compare.json, a table on
stdout and side-<mode>-<scale>x.png: each row as Clack, Slack and an overlay
(Clack's ink red, Slack's cyan, both white) at 400%.

usage: sidebar_rows_ref.py fetch <dir>
       sidebar_rows_ref.py <shots dir> <slack css> <slackvars.go> <icons.json> <chrome> <platform>
"""
import html
import json
import os
import re
import subprocess
import sys
import tempfile

from PIL import Image, ImageDraw

CDN = "https://a.slack-edge.com/bv1-13-br/"
CSS = "client-boot-styles.cf726c4f2eaecc74.min.css"
LATO = ("lato-regular-ef8e44a.woff2", "lato-bold-78da5d4.woff2", "lato-black-7b284cf.woff2")  # its Singer-Lato 400/700/900
ICONS = "https://raw.githubusercontent.com/IzieStratt/slack-datamine-plus/main/build/icons.json"


def fetch(root):
    """The stylesheet, the Lato faces next to it and the icon set, no cookie."""
    import brotli  # the bv1-13-br CDN may send brotli bodies whatever the client asks for
    import urllib.request
    os.makedirs(root, exist_ok=True)
    for f, url in [(CSS, CDN + CSS)] + [(f, CDN + f) for f in LATO] + [("icons.json", ICONS)]:
        b = urllib.request.urlopen(urllib.request.Request(url, headers={"Accept-Encoding": "br"}), timeout=120).read()
        if f.endswith(".woff2") and not b.startswith(b"wOF2") or f.endswith((".css", ".json")) and b[:1] not in (b".", b"@", b"{", b":", b"/"):
            b = brotli.decompress(b)
        if f.endswith(".woff2") and not b.startswith(b"wOF2"):
            sys.exit("%s: not a woff2 file" % f)
        open(os.path.join(root, f), "wb").write(b)
        print(f, len(b), "bytes")


if len(sys.argv) == 3 and sys.argv[1] == "fetch":
    fetch(sys.argv[2])
    sys.exit(0)
if len(sys.argv) < 7:
    sys.exit(__doc__)
shots, css_path, vars_path, icons_path, chrome, platform = sys.argv[1:7]
icons = json.load(open(icons_path))["icons"]
spec = json.load(open(os.path.join(shots, "rows.json")))
E = html.escape


def tokens(name):
    src = open(vars_path, encoding="utf-8").read()
    m = re.search(r"var %s = map\[string\]string\{(.*?)\n\}" % name, src, re.S)
    return dict(re.findall(r'"(--[^"]+)":\s*"([^"]*)"', m.group(1)))


def split_rules(src):
    """Top-level rules and @media blocks as (prelude, body)."""
    out, i, n = [], 0, len(src)
    while i < n:
        j = src.find("{", i)
        if j < 0:
            break
        d, k = 1, j + 1
        while k < n and d:
            if src[k] == "{":
                d += 1
            elif src[k] == "}":
                d -= 1
            k += 1
        out.append((src[i:j].strip(), src[j + 1:k - 1]))
        i = k
    return out


def hover_rules(src):
    """Every rule naming .p-channel_sidebar__channel:hover, with the hover as
    the .rz-hover class."""
    out = []
    for sel, body in split_rules(src):
        if sel.startswith("@"):
            continue
        if ".p-channel_sidebar__channel:hover" in sel:
            parts = [p for p in sel.split(",") if ".p-channel_sidebar__channel:hover" in p]
            out.append("%s{%s}" % (",".join(p.replace(".p-channel_sidebar__channel:hover", ".p-channel_sidebar__channel.rz-hover") for p in parts), body))
    return "\n".join(out)


def svg(name):
    body = icons[name]["body"]
    return ('<svg data-qa="sidebar-channel-icon-prefix" data-sidebar-channel-icon="%s" viewBox="0 0 20 20" width="16" height="16" '
            'fill="currentColor" aria-hidden="true">%s</svg>' % (name, body))


def row_html(r, bg):
    cls = ["p-channel_sidebar__channel"]
    if r["selected"]:
        cls.append("p-channel_sidebar__channel--selected")
    if r["muted"]:
        cls.append("p-channel_sidebar__channel--muted")
    if r["unread"] and not r["muted"]:
        cls.append("p-channel_sidebar__channel--unread")
    if r["mentions"]:
        cls.append("p-channel_sidebar__channel--has-badge")
    cls.append("p-channel_sidebar__channel--show-avatars")
    if r["hover"]:
        cls.append("rz-hover")
    icon = "channel-filled" if r["unread"] and not r["muted"] else "channel"
    badge = ""
    if r["mentions"]:
        badge = ('<span class="p-channel_sidebar__channel_suffix%s" data-qa="sidebar-channel-suffix">'
                 '<span class="c-badge c-badge--themed c-badge--pill p-channel_sidebar__badge" data-qa="sidebar-channel-badge-suffix">%d</span></span>'
                 % (" p-channel_sidebar__channel_suffix--muted" if r["muted"] else "", r["mentions"]))
    return ('<div class="p-channel_sidebar__static_list__item" style="height:28px;background:%s">'
            '<div class="p-channel_sidebar__static_list__item__row_container">'
            '<div role="treeitem" class="%s" data-qa-channel-sidebar-channel-id="%s">'
            '<div class="p-channel_sidebar__channel_icon_prefix">%s</div>'
            '<span class="p-channel_sidebar__name"><span>%s</span></span>%s</div></div></div>'
            % (bg, " ".join(cls), E(r["id"]), svg(icon), E(r["name"]), badge))


def page(shot, bgs, hover_css):
    dark = shot["dark"]
    t = tokens("slackVarsDark" if dark else "slackVarsLight")
    root = ";".join("%s:%s" % kv for kv in t.items())
    inv = ""
    scope = ""
    if not dark and shot.get("vars"):
        inv = ".sk-client-theme--light-inverted-sidebar{%s}" % ";".join("%s:%s" % kv for kv in shot["vars"].items())
        scope = "sk-client-theme--light-inverted-sidebar"
    rows = "".join(row_html(r, bgs[i]) for i, r in enumerate(shot["rows"]))
    return ("<!doctype html><html><head><meta charset=utf-8><link rel=stylesheet href='%s'>"
            "<style>:root{%s;color-scheme:%s}%s%s"
            "html,body{margin:0;background:%s;font-family:Singer-Lato,Lato,sans-serif;font-size:15px;line-height:1.46668;"
            "-webkit-font-smoothing:antialiased;-moz-osx-font-smoothing:grayscale}*{box-sizing:border-box}</style></head>"
            "<body class='%s'><div class='%s' style='width:%dpx;height:%dpx'>"
            "<div class='p-channel_sidebar'><div class='p-channel_sidebar__list'><div class='p-channel_sidebar__static_list'>%s"
            "</div></div></div></div></body></html>"
            % (file_url(css_path), root, "dark" if dark else "light", inv, hover_css, bgs[0],
               "sk-client-theme--dark" if dark else "", scope, shot["w_dp"], 28 * len(shot["rows"]), rows))


def file_url(p):
    p = os.path.abspath(p).replace("\\", "/")
    return "file:///" + p.lstrip("/")


def screenshot(hp, w, h, scale, out):
    prof = tempfile.mkdtemp(prefix="rzprof")
    subprocess.run([chrome, "--headless=new", "--no-sandbox", "--disable-gpu", "--hide-scrollbars", "--allow-file-access-from-files",
                    "--user-data-dir=" + prof, "--force-device-scale-factor=%g" % scale, "--window-size=%d,%d" % (w, h),
                    "--virtual-time-budget=5000", "--screenshot=" + os.path.abspath(out), file_url(hp)],
                   check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=180)


def px(im, x, y):
    return im.getpixel((max(0, min(im.width - 1, x)), max(0, min(im.height - 1, y))))


def measure(im, scale, mentions):
    """Ink boxes: the row's fill is the colour 10px into the row box."""
    w, h = im.size
    fill = px(im, int(18 * scale), h // 2)
    x0, x1 = int(8 * scale), w - int(8 * scale)
    cols = []
    ink = set()
    for x in range(x0, x1):
        on = False
        for y in range(h):
            p = px(im, x, y)
            if sum(abs(p[i] - fill[i]) for i in range(3)) > 90:
                ink.add((x, y))
                on = True
        cols.append(on)
    # runs of ink columns, merged across gaps under 5px
    runs, start, gap = [], None, 0
    for i, on in enumerate(cols):
        if on:
            if start is None:
                start = i
            gap = 0
            end = i
        elif start is not None:
            gap += 1
            if gap >= int(5 * scale):
                runs.append((start + x0, end + x0))
                start = None
    if start is not None:
        runs.append((start + x0, end + x0))

    def box(r):
        ys = [y for (x, y) in ink if r[0] <= x <= r[1]]
        return {"x0": r[0], "x1": r[1], "y0": min(ys), "y1": max(ys)} if ys else None

    out = {"fill": "#%02x%02x%02x" % fill[:3]}
    if runs:
        out["icon"] = box(runs[0])
    if len(runs) > 1:
        name_runs = runs[1:-1] if mentions and len(runs) > 2 else runs[1:]
        out["name"] = box((name_runs[0][0], name_runs[-1][1]))
        # the name's colour: its pixel furthest from the fill
        best, bp = -1, None
        for (x, y) in ink:
            if name_runs[0][0] <= x <= name_runs[-1][1]:
                p = px(im, x, y)
                d = sum(abs(p[i] - fill[i]) for i in range(3))
                if d > best:
                    best, bp = d, p
        out["name_color"] = "#%02x%02x%02x" % bp[:3]
        if mentions and len(runs) > 2:
            out["badge"] = box(runs[-1])
    return out, ink


def up(im, k):
    return im.resize((im.width * k, im.height * k), Image.NEAREST)


def overlay(a_ink, b_ink, w, h):
    o = Image.new("RGB", (w, h), (24, 24, 24))
    for (x, y) in a_ink | b_ink:
        if (x, y) in a_ink and (x, y) in b_ink:
            c = (255, 255, 255)
        elif (x, y) in a_ink:
            c = (255, 64, 64)
        else:
            c = (0, 220, 255)
        o.putpixel((x, y), c)
    return o


report = []
bad = []
for shot in spec:
    scale = shot["scale"]
    mode = "dark" if shot["dark"] else "light"
    name = "%s-%gx" % (mode, scale)
    crops = [Image.open(os.path.join(shots, "%s-%s.png" % (shot["file"], r["id"]))).convert("RGB") for r in shot["rows"]]
    # the sidebar just right of each row (outside its 8px-inset box)
    bgs = ["#%02x%02x%02x" % px(c, c.width - 2, c.height // 2)[:3] for c in crops]
    hp = os.path.join(shots, "slack-%s.html" % name)
    open(hp, "w", encoding="utf-8").write(page(shot, bgs, hover_rules(open(css_path, encoding="utf-8").read())))
    sp = os.path.join(shots, "slack-%s.png" % name)
    n = len(shot["rows"])
    screenshot(hp, shot["w_dp"], 28 * n + 40, scale, sp)
    slack = Image.open(sp).convert("RGB")
    k, gap, lab = 4, 6, 18
    W = shot["w"]
    sheet = Image.new("RGB", (W * k * 3 + gap * 4, sum(c.height * k + lab + gap for c in crops) + gap), (96, 96, 96))
    d = ImageDraw.Draw(sheet)
    y = gap
    for i, (r, c) in enumerate(zip(shot["rows"], crops)):
        rh = c.height
        sy = int(round(i * 28 * scale))
        s = slack.crop((0, sy, W, sy + rh))
        cm, cink = measure(c, scale, r["mentions"])
        sm, sink = measure(s, scale, r["mentions"])
        state = ",".join(k2 for k2 in ("selected", "hover", "unread", "muted") if r[k2]) or "read"
        if r["mentions"]:
            state += ",%d mention" % r["mentions"]
        d.text((gap, y), "%s (%s)  Clack | Slack | overlay (Clack red, Slack cyan)" % (r["name"], state), fill=(255, 255, 255))
        y += lab
        for j, im in enumerate((c, s, overlay(cink, sink, W, rh))):
            sheet.paste(up(im, k), (gap + j * (W * k + gap), y))
        y += rh * k + gap
        diffs = {}
        for part in ("icon", "name", "badge"):
            a, b = cm.get(part), sm.get(part)
            if a and b:
                diffs[part] = {kk: a[kk] - b[kk] for kk in a}
        report.append({"shot": name, "platform": platform, "row": r["name"], "state": state, "clack": cm, "slack": sm, "diff": diffs})
        for part, dd in diffs.items():
            for kk, v in dd.items():
                if part == "name" and kk == "x1":
                    continue  # the name's right end follows its width (shaping), reported only
                if abs(v) > 1:
                    bad.append("%s %s %s.%s %+d" % (name, r["name"], part, kk, v))
    out = os.path.join(shots, "side-%s.png" % name)
    sheet.save(out)
    print("saved", out)

json.dump(report, open(os.path.join(shots, "rows-compare.json"), "w"), indent=1)
print("%-10s %-16s %-26s %-28s %-28s %s" % ("shot", "row", "state", "icon dx0 dx1 dy0 dy1", "name dx0 dy0 dy1 (dx1)", "colours clack/slack"))
for e in report:
    def f(p, keys):
        dd = e["diff"].get(p)
        return " ".join("%+d" % dd[kk] for kk in keys) if dd else "-"
    print("%-10s %-16s %-26s %-28s %-28s %s %s / %s %s" % (
        e["shot"], e["row"], e["state"], f("icon", ("x0", "x1", "y0", "y1")), f("name", ("x0", "y0", "y1", "x1")),
        e["clack"].get("fill"), e["clack"].get("name_color"), e["slack"].get("fill"), e["slack"].get("name_color")))
if bad:
    print("\nmore than 1px from Slack:\n  " + "\n  ".join(bad))
    if os.environ.get("SIDEBAR_ROWS_STRICT"):
        sys.exit(1)
