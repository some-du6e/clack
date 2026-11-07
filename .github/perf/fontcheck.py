#!/usr/bin/env python3
"""Compares our embedded Lato with the faces Slack's client serves.

Slack's CSS (found through the public, signed-out slack.com/signin page; no
cookie is ever sent) declares Slack-Lato @font-face rules whose woff2 files
live on a.slack-edge.com. For each weight/style we compare codepoint
coverage with assets/fonts. Exit status 1 when coverage differs.
usage: fontcheck.py <outdir>
"""
import io, os, re, sys, urllib.request
from fontTools.ttLib import TTFont

UA = {"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"}
def get(u):
    with urllib.request.urlopen(urllib.request.Request(u, headers=UA), timeout=30) as r:
        return r.read()

out = sys.argv[1] if len(sys.argv) > 1 else "."
html = get("https://slack.com/signin").decode("utf-8", "replace")
css_urls = sorted(set(re.findall(r"https://a\.slack-edge\.com/[^\"' )]+\.css", html)))
faces = {}
for cu in css_urls:
    css = get(cu).decode("utf-8", "replace")
    for rule in re.findall(r"@font-face\{[^}]*\}", css):
        if "Slack-Lato" not in rule:
            continue
        style = re.search(r"font-style:(\w+)", rule).group(1)
        weight = re.search(r"font-weight:(\d+)", rule).group(1)
        m = re.search(r"url\(([^)]+\.woff2)\)", rule)
        if m:
            faces[(weight, style)] = urllib.request.urljoin(cu, m.group(1).strip("\"'"))
ours = {("300", "normal"): "Light", ("400", "normal"): "Regular", ("700", "normal"): "Bold", ("900", "normal"): "Black",
        ("300", "italic"): "LightItalic", ("400", "italic"): "Italic", ("700", "italic"): "BoldItalic", ("900", "italic"): "BlackItalic"}
lines = ["### Lato vs Slack-Lato", "", "| face | Slack codepoints | ours | only Slack | only ours |", "|---|---|---|---|---|"]
bad = False
for key, name in ours.items():
    o = set(TTFont(f"assets/fonts/Lato-{name}.ttf").getBestCmap())
    if key not in faces:
        lines.append(f"| {name} | (not served) | {len(o)} | | |")
        bad = True
        continue
    s = set(TTFont(io.BytesIO(get(faces[key]))).getBestCmap())
    if s != o:
        bad = True
    lines.append(f"| {name} | {len(s)} | {len(o)} | {len(s - o)} | {len(o - s)} |")
lines.append("")
lines.append("Coverage matches Slack's faces." if not bad else "**Coverage differs from Slack's faces.**")
open(os.path.join(out, "fontcheck.md"), "w").write("\n".join(lines) + "\n")
print("\n".join(lines))
sys.exit(1 if bad else 0)
