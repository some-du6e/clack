#!/usr/bin/env python3
"""slack_icon_font.py FONT OUT: Slack's icon font ("Slack v2",
slack-icons-v2-*.woff2 on the CDN) as an Iconify-style JSON for
TestIconsMatchSlack (CLACK_SLACK_ICON_FONT): every glyph's outline as a
<path> body in the 20-unit icon view, the font's em at the 20px
c-icon:before size with the baseline at its ascent (for its 2000-unit em
and 1700 ascent: x/100, (1700-y)/100), keyed by lowercase hex codepoint
("e292"). Needs fontTools and brotli (pip)."""
import json
import sys

from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from fontTools.ttLib import TTFont


def main(src, dst):
    font = TTFont(src)
    upm = font["head"].unitsPerEm
    asc = font["hhea"].ascent
    k = 20 / upm
    glyphs = font.getGlyphSet()
    icons = {}
    for cp, name in sorted(font.getBestCmap().items()):
        pen = SVGPathPen(glyphs, ntos=lambda v: ("%.3f" % v).rstrip("0").rstrip("."))
        glyphs[name].draw(TransformPen(pen, (k, 0, 0, -k, 0, asc * k)))
        d = pen.getCommands()
        if d:
            icons["%04x" % cp] = {"body": '<path fill="currentColor" d="%s"/>' % d}
    with open(dst, "w") as f:
        json.dump({"prefix": "slack-icon-font", "width": 20, "height": 20, "icons": icons}, f)
    print(len(icons), "glyphs from", src)


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
