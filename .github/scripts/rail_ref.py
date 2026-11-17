#!/usr/bin/env python3
"""Clack's tab rail buttons next to Slack's, measured and shown at 400%.

Clack's side is TestRailShots (internal/ui/rail_shots_test.go): each rail
button of fakeslack's Unreads fixtures (synthetic data) idle, hovered,
selected and after a mouse click, plus rail.json.

Slack's side is build fa938a086569's tab rail (boot-render: the tab list's
tb/tv button props, HomeTabContent, DmsTabContent, ActivityTabContent,
BrowseTabContent, AutomationsTabButton; client-boot's Badge Ex as InjD
useGetMentions renders it: themed, circle, maxCount 99) under
client-boot-styles cf726c4f2eaecc74 with Slack's own Lato, both from the
public CDN without a cookie (sidebar_rows_ref.py fetch), Slack's SVG icons
and the tokens Clack carries (slackvars.go; light mode adds the inverted
scope's, from rail.json):

  button.c-button-unstyled.c-tabs__tab.js-tab.p-tab_rail__button
      [--active, c-tabs__tab--active]
    span.c-tabs__tab_content
      div.p-tab_rail__button__icon > div.p-tab_rail__button__icon_inner > svg 20
      span.c-badge.c-badge--circle.c-badge--themed (count) | the unread dot
      div.p-tab_rail__button__label

:hover and :focus can't be forced in a screenshot: rules naming
.p-tab_rail__button:hover or .c-button--focus-visible:focus are copied with
those replaced by classes. "clicked" is Slack after a mouse click: the
button is active and has no c-button--focus-visible (a mouse focus shows no
ring). More's click opens its menu (BrowsePeekTrigger's forceOpenOnClick),
and while it shows PeekTrigger's .p-peek_trigger--is-peek-visible, the
div between the tab menu and the button, keeps the hover look (the tile,
content-inv-pry, the icon at 1.15), Slack's stylesheet drawing it; the
menu's own edge, 6px right of the button, falls in the 8px the measures
leave out. "focus" (keyboard focus, :focus-visible) is drawn for Slack
only: Clack's rail takes no keyboard focus.

Each crop is measured against the rail colour at its left edge: the tile
(any change over 10), the icon and label (strong ink), the badge's box (ink
in the badge's corner) and ring_px, pixels in a 3px band around the badge
darker than both the tile and the rail (a ring in the rail colour drawn
over the tile reads as a border), and ink, the label's pixel farthest from
the rail colour.

Slack's side is drawn from Slack's own numbers only: every shot takes its
account theme's tokens (Slack's default theme for the shots without one;
TestRailShots' railShotThemes: Karim's #FED4BE and a dark custom colour)
as Slack's code computes them, internal/ui/testdata/theme-oracle.json: the
root scope's --dt_color-theme-*, the inverted scope's when useInvertedTheme
puts the rail there, and .p-theme_background's radial + conic gradient over
the case's stops, sized to the window and cut at each crop's window y, so
the rail colour under a button is Slack's, not Clack's sampled one (each
cell's gradient fills its whole 120px slot, past the crop's bottom, and the
page under the cells is the primary stop). The
rightmost 8px of a crop are left out of the measures (a peek card's edge
can reach them), and "fill" is sampled inside the tile, clear of its
corners and the icon.

usage: rail_ref.py <shots dir> <slack css> <slackvars.go> <icons.json> <chrome> <platform>
"""
import html
import json
import os
import re
import subprocess
import sys
import tempfile

from PIL import Image, ImageDraw

shots, css_path, vars_path, icons_path, chrome, platform = sys.argv[1:7]
icons = json.load(open(icons_path))["icons"]
spec = json.load(open(os.path.join(shots, "rail.json")))
E = html.escape
ORACLE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "internal", "ui", "testdata", "theme-oracle.json")
oracle = json.load(open(ORACLE, encoding="utf-8"))["cases"] if os.path.exists(ORACLE) else []


def oracle_case(theme, dark):
    mode = "dark" if dark else "light"
    for c in oracle:
        if c["name"] == theme and c["mode"] == mode:
            return c
    raise SystemExit("theme-oracle.json has no %s %s case" % (theme, mode))

# Slack's icon per tab (inactive, active)
ICON = {
    "home": ("home", "home-filled"),
    "dms": ("direct-messages", "direct-messages-filled"),
    "activity": ("notifications", "notifications-filled"),
    "browse": ("ellipsis-horizontal-filled", "ellipsis-horizontal-filled"),
    "platform": ("ai-agents", "ai-agents-filled"),
}


def tokens(name):
    src = open(vars_path, encoding="utf-8").read()
    m = re.search(r"var %s = map\[string\]string\{(.*?)\n\}" % name, src, re.S)
    return dict(re.findall(r'"(--[^"]+)":\s*"([^"]*)"', m.group(1)))


def split_rules(src):
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


def forced(src):
    out = []
    for sel, body in split_rules(src):
        if sel.startswith("@"):
            continue
        parts = []
        for p in sel.split(","):
            if ".p-tab_rail__button:hover" in p:
                parts.append(p.replace(".p-tab_rail__button:hover", ".p-tab_rail__button.rz-hover"))
            elif "c-tabs__tab" in p and ".c-button--focus-visible:focus" in p:
                parts.append(p.replace(".c-button--focus-visible:focus", ".c-button--focus-visible.rz-focus"))
        if parts:
            out.append("%s{%s}" % (",".join(parts), body))
    return "\n".join(out)


def svg(name):
    return '<svg viewBox="0 0 20 20" width="20" height="20" fill="currentColor" aria-hidden="true">%s</svg>' % icons[name]["body"]


def button(b, state):
    active = state in ("selected", "clicked", "focus") and b["active"]
    cls = ["c-button-unstyled", "c-tabs__tab", "js-tab", "c-tabs__tab--full_width", "p-tab_rail__button"]
    if active:
        cls += ["c-tabs__tab--active", "p-tab_rail__button--active"]
    if state == "hover":
        cls.append("rz-hover")
    if state == "focus":
        cls += ["c-button--focus-visible", "rz-focus"]
    badge = ""
    if b["badge"] > 0:
        n = b["badge"]
        badge = '<span aria-hidden="true" class="c-badge c-badge--circle c-badge--themed%s" data-qa="badge">%s</span>' % (
            " c-badge--max" if n > 99 else "", "99+" if n > 99 else n)
    elif b["dot"]:
        badge = '<span class="p-unread_dot"></span>'
    btn = ('<button class="%s" role="tab" aria-selected="%s"><span class="c-tabs__tab_content">'
           '<div class="p-tab_rail__button__icon"><div class="p-tab_rail__button__icon_inner">%s</div></div>%s'
           '<div class="p-tab_rail__button__label">%s</div></span></button>'
           % (" ".join(cls), "true" if active else "false", svg(ICON[b["key"]][1 if active else 0]), badge, E(b["label"])))
    if b["key"] == "browse" and state == "clicked":  # More's menu open (the module doc)
        btn = '<div class="p-peek_trigger p-peek_trigger--is-peek-visible">%s</div>' % btn
    return '<div role="tablist" class="c-tabs__tab_menu c-tabs__tab_menu--vertical p-tab_rail__tab_menu">%s</div>' % btn


def page(shot, cells, rules):
    dark = shot["dark"]
    scale = shot["scale"]
    t = tokens("slackVarsDark" if dark else "slackVarsLight")
    inv, scope = "", ""
    # Slack's tokens for the shot's account theme, from its own code
    oc = oracle_case(shot.get("theme") or "default", dark)
    t.update(oc["tokens"]["dark" if dark else "light"])
    if oc["scopes"]["tabRail"] == "inverted":
        inv = ".sk-client-theme--light-inverted-sidebar{%s}" % ";".join("%s:%s" % kv for kv in oc["tokens"]["inverted"].items())
        scope = " sk-client-theme--light-inverted-sidebar"
    root = ";".join("%s:%s" % kv for kv in t.items())
    # .p-theme_background (getConicGradient: primary at the centre and 25/75%,
    # highlight1 at 0/50/100%) over the whole window
    st = oc["background"]
    P, Hl = st[0], st[1] if len(st) > 1 else st[0]
    grad = ("radial-gradient(circle at 50%% 50%%, %s 20%%, transparent 80%%), conic-gradient(from 45deg at 50%% 50%%, %s 0%%, %s 25%%, %s 50%%, %s 75%%, %s 100%%)"
            % (P, Hl, P, Hl, P, Hl))
    win = shot.get("win") or [70, 1000]
    ww, wh = win[0] / scale, win[1] / scale

    def cell(i, b, st):
        # the crop: top px above the button, its window y = b["y"]
        return ('<div class="p-tab_rail%s" style="position:absolute;left:0;top:%dpx;width:70px;height:120px;padding-top:%gpx;overflow:visible;'
                'background:%s;background-size:%gpx %gpx;background-position:0px %gpx">%s</div>'
                % (scope, i * 120, b["top"] / scale, grad, ww, wh, -b.get("y", 0) / scale, button(b, st)))
    body = "".join(cell(i, b, st) for i, (b, st) in enumerate(cells))
    return ("<!doctype html><html><head><meta charset=utf-8><link rel=stylesheet href='%s'>"
            "<style>:root{%s;color-scheme:%s}%s%s html,body{margin:0;background:%s;font-family:Singer-Lato,Lato,sans-serif;"
            "font-size:15px;line-height:1.46668;-webkit-font-smoothing:antialiased}*{box-sizing:border-box}</style></head>"
            "<body class='%s'>%s</body></html>"
            % (file_url(css_path), root, "dark" if dark else "light", inv, rules, P, "sk-client-theme--dark" if dark else "", body))


def file_url(p):
    return "file:///" + os.path.abspath(p).replace("\\", "/").lstrip("/")


def screenshot(hp, w, h, scale, out):
    prof = tempfile.mkdtemp(prefix="rzprof")
    subprocess.run([chrome, "--headless=new", "--no-sandbox", "--disable-gpu", "--hide-scrollbars", "--allow-file-access-from-files",
                    "--user-data-dir=" + prof, "--force-device-scale-factor=%g" % scale, "--window-size=%d,%d" % (w, h),
                    "--virtual-time-budget=5000", "--screenshot=" + os.path.abspath(out), file_url(hp)],
                   check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=180)


def dist(a, b):
    return sum(abs(a[i] - b[i]) for i in range(3))


def measure(im, scale, top, has_badge):
    """Boxes in button px (y from the button's top)."""
    w, h = im.size
    bg = im.getpixel((1, h // 2))[:3]
    S = lambda v: int(round(v * scale))
    bx0, by0 = S(9), top
    tile = [None] * 4
    fillpt = (bx0 + S(12), by0 + S(24))  # inside the tile, clear of its corners, the icon and the badge
    fill = im.getpixel(fillpt)[:3]
    strong, ink = [], set()
    w = max(1, w - S(8))  # a peek card's edge can reach the crop's right edge
    for y in range(h):
        for x in range(w):
            p = im.getpixel((x, y))[:3]
            if dist(p, bg) > 10:
                tile = [min(tile[0], x) if tile[0] is not None else x, min(tile[1], y) if tile[1] is not None else y,
                        max(tile[2] or 0, x), max(tile[3] or 0, y)]
            if dist(p, fill) > 120 and dist(p, bg) > 120:
                strong.append((x, y))
                ink.add((x, y))

    def box(pts):
        if not pts:
            return None
        return {"x0": min(p[0] for p in pts) - bx0, "x1": max(p[0] for p in pts) - bx0,
                "y0": min(p[1] for p in pts) - by0, "y1": max(p[1] for p in pts) - by0}

    icon = [p for p in strong if S(16) <= p[0] - bx0 <= S(38) and S(14) <= p[1] - by0 <= S(39)]
    label = [p for p in strong if p[1] - by0 >= S(46)]
    # the label's ink: its pixel farthest from the rail colour
    inkc, far = None, -1
    for y in range(min(h, by0 + S(46)), h):
        for x in range(w):
            p = im.getpixel((x, y))[:3]
            if dist(p, bg) > far:
                inkc, far = p, dist(p, bg)
    badge = [p for p in strong if has_badge and p[1] - by0 < S(24) and (p[0] - bx0 > S(38) or p[1] - by0 < S(14))]
    out = {"bg": "#%02x%02x%02x" % bg, "fill": "#%02x%02x%02x" % fill, "icon": box(icon), "label": box(label), "badge": box(badge),
           "ink": "#%02x%02x%02x" % inkc if inkc else None}
    if tile[0] is not None:
        out["tile"] = {"x0": tile[0] - bx0, "y0": tile[1] - by0, "x1": tile[2] - bx0, "y1": tile[3] - by0}
    ring = 0
    if out["badge"]:
        b = out["badge"]
        bb = (b["x0"] + bx0, b["y0"] + by0, b["x1"] + bx0, b["y1"] + by0)
        lum = lambda p: 0.299 * p[0] + 0.587 * p[1] + 0.114 * p[2]
        for y in range(max(0, bb[1] - S(3)), min(h, bb[3] + S(3) + 1)):
            for x in range(max(0, bb[0] - S(3)), min(w, bb[2] + S(3) + 1)):
                if bb[0] <= x <= bb[2] and bb[1] <= y <= bb[3]:
                    continue
                p = im.getpixel((x, y))[:3]
                if lum(p) < min(lum(bg), lum(fill)) - 6:
                    ring += 1
    out["ring_px"] = ring
    return out, ink


def up(im, k):
    return im.resize((im.width * k, im.height * k), Image.NEAREST)


rules = forced(open(css_path, encoding="utf-8").read())
report, bad = [], []
for shot in spec:
    scale = shot["scale"]
    mode = "dark" if shot["dark"] else "light"
    if shot.get("theme"):
        mode = shot["theme"].replace(" ", "-") + "-" + mode
    name = "%s-%gx" % (mode, scale)
    crops = [Image.open(os.path.join(shots, b["file"])).convert("RGB") for b in shot["btns"]]
    if not crops:
        continue
    cells = [(b, b["state"]) for b in shot["btns"]]
    # keyboard focus, Slack only: each button once, active where it can be
    seen = []
    for b in shot["btns"]:
        if b["key"] not in seen:
            seen.append(b["key"])
            fb = dict(b, state="focus", active=b["key"] in ("home", "dms", "activity"))
            cells.append((fb, "focus"))
    hp = os.path.join(shots, "slack-rail-%s.html" % name)
    open(hp, "w", encoding="utf-8").write(page(shot, cells, rules))
    sp = os.path.join(shots, "slack-rail-%s.png" % name)
    screenshot(hp, 70, 120 * len(cells) + 20, scale, sp)
    print("%s: Slack's side drawn with theme-oracle.json's %s %s case under %s" % (
        name, shot.get("theme") or "default", "dark" if shot["dark"] else "light", shot.get("ia_theme") or "no ia_theme"))
    slack = Image.open(sp).convert("RGB")
    k, gap, lab = 4, 6, 16
    W = shot["w"]
    cw = W * k
    rows = []
    for i, (b, st) in enumerate(cells):
        top = b["top"]
        ch = crops[i].height if i < len(crops) else b["h"] + top + int(round(2 * scale))
        sy = int(round(i * 120 * scale))  # the cell's top: top px above its button, as Clack's crop
        s = slack.crop((0, sy, W, sy + ch))
        sm, _ = measure(s, scale, top, b["badge"] > 0)
        entry = {"shot": name, "platform": platform, "key": b["key"], "state": st, "slack": sm}
        if i < len(crops):
            cm, _ = measure(crops[i], scale, top, b["badge"] > 0)
            entry["clack"] = cm
            entry["diff"] = {p: {kk: cm[p][kk] - sm[p][kk] for kk in cm[p]} for p in ("tile", "icon", "label", "badge") if cm.get(p) and sm.get(p)}
            if cm["ring_px"] > sm["ring_px"] + 2:
                bad.append("%s %s %s: a ring around the badge (%d px, Slack %d)" % (name, b["key"], st, cm["ring_px"], sm["ring_px"]))
            for p, dd in entry["diff"].items():
                for kk, v in dd.items():
                    if abs(v) > 1:
                        bad.append("%s %s %s %s.%s %+d" % (name, b["key"], st, p, kk, v))
        report.append(entry)
        rows.append((b, st, crops[i] if i < len(crops) else None, s))
    sheet = Image.new("RGB", (gap + 2 * (cw + gap), sum(r[3].height * k + lab + gap for r in rows) + gap), (96, 96, 96))
    d = ImageDraw.Draw(sheet)
    y = gap
    for b, st, c, s in rows:
        d.text((gap, y), "%s %s: Clack | Slack" % (b["label"], st) + ("  (Slack only)" if c is None else ""), fill=(255, 255, 255))
        y += lab
        if c is not None:
            sheet.paste(up(c, k), (gap, y))
        sheet.paste(up(s, k), (gap + cw + gap, y))
        y += s.height * k + gap
    out = os.path.join(shots, "side-rail-%s.png" % name)
    sheet.save(out)
    print("saved", out)

json.dump(report, open(os.path.join(shots, "rail-compare.json"), "w"), indent=1)
print("%-24s %-9s %-9s %-22s %-22s %-22s %-8s %-16s %s" % ("shot", "tab", "state", "tile dx0 dy0 dx1 dy1", "icon", "badge", "ring", "fill clack/slack", "label ink clack/slack (rail)"))
for e in report:
    if "clack" not in e:
        continue
    f = lambda p: " ".join("%+d" % e["diff"][p][kk] for kk in ("x0", "y0", "x1", "y1")) if p in e["diff"] else "-"
    print("%-24s %-9s %-9s %-22s %-22s %-22s %3d/%-4d %s/%s %s/%s (%s)" % (e["shot"], e["key"], e["state"], f("tile"), f("icon"), f("badge"),
                                                                       e["clack"]["ring_px"], e["slack"]["ring_px"], e["clack"]["fill"], e["slack"]["fill"],
                                                                       e["clack"]["ink"], e["slack"]["ink"], e["clack"]["bg"]))
if bad:
    print("\nmore than 1px from Slack, or a ring Slack doesn't draw:\n  " + "\n  ".join(bad))
