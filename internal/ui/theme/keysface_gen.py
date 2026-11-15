"""Builds keysface-regular.ttf and keysface-bold.ttf: the Mac key symbols.

Lato has none of the glyphs macOS writes shortcuts with (⌃ ⌥ ⇧ ⌘, ⎋ ⌫ ⌦
⏎ ↩ ⇥ ⇤ ⇪ ⌅ ⇞ ⇟), and Gio's system-font fallback found only some of
them on macOS: "⌃⌥⇧⌘D" drew as "□□⇧⌘D" (α.85). theme.Typeface lists
KeysFamily after the UI font's families, so these code points always
resolve to this face when the UI font lacks them, on every platform, before
any system font is tried.

The glyphs are Inter's (SIL Open Font License 1.1, no Reserved Font Name;
keysface-OFL.txt), whose key symbols follow Apple's: Regular and Bold,
subset to the code points above, CFF outlines converted to TrueType
quadratics, renamed "Clack Keys". Vertical metrics are cut to Lato's
(0.987 / 0.213 em, no line gap) so a hint never grows a Lato line: Gio
takes a line's ascent and descent from the tallest run on it, and Inter's
own descent (0.241 em) is deeper than Lato's.

    python3 -I internal/ui/theme/keysface_gen.py \
        /usr/share/fonts/opentype/inter internal/ui/theme

(Debian's fonts-inter 4.0+ds-1.) Needs fontTools (pip install fonttools).
"""

import os
import sys

from fontTools import subset
from fontTools.pens.cu2quPen import Cu2QuPen
from fontTools.pens.ttGlyphPen import TTGlyphPen
from fontTools.ttLib import TTFont, newTable

# Keep in sync with keysRunes in keysface_test.go.
KEYS = "⌃⌥⇧⌘⎋⌫⌦⏎↩⇥⇤⇪⌅⇞⇟"

FAMILY = "Clack Keys"

LATO_ASCENT, LATO_DESCENT = 0.987, 0.213


def to_truetype(font):
    """Replaces font's CFF table with glyf/loca (cu2qu at 1 unit)."""
    order = font.getGlyphOrder()
    gs = font.getGlyphSet()
    glyf = newTable("glyf")
    glyf.glyphOrder = order
    glyf.glyphs = {}
    for name in order:
        pen = TTGlyphPen(gs)
        gs[name].draw(Cu2QuPen(pen, max_err=1.0, reverse_direction=True))
        glyf.glyphs[name] = pen.glyph()
    del font["CFF "]
    if "VORG" in font:
        del font["VORG"]
    font["glyf"] = glyf
    font["loca"] = newTable("loca")
    maxp = font["maxp"]
    maxp.tableVersion = 0x00010000
    for k in ("maxZones", "maxTwilightPoints", "maxStorage", "maxFunctionDefs",
              "maxInstructionDefs", "maxStackElements", "maxSizeOfInstructions",
              "maxComponentElements", "maxPoints", "maxContours",
              "maxCompositePoints", "maxCompositeContours", "maxComponentDepth"):
        setattr(maxp, k, 0)
    maxp.maxZones = 1
    font["head"].indexToLocFormat = 0
    font["head"].glyphDataFormat = 0
    font["post"].formatType = 3.0  # no glyph names
    font.sfntVersion = "\x00\x01\x00\x00"


def build(src, dst, style):
    font = TTFont(src)
    opts = subset.Options()
    opts.layout_features = []
    opts.name_IDs = [0, 13, 14]  # copyright and license; names are set below
    opts.notdef_outline = True
    opts.hinting = False
    opts.drop_tables += ["GDEF", "GPOS", "GSUB", "STAT"]
    s = subset.Subsetter(opts)
    s.populate(unicodes=[ord(c) for c in KEYS])
    s.subset(font)
    to_truetype(font)

    upem = font["head"].unitsPerEm
    asc = int(LATO_ASCENT * upem)  # floor: never taller than Lato
    desc = int(LATO_DESCENT * upem)
    hhea, os2 = font["hhea"], font["OS/2"]
    hhea.ascent, hhea.descent, hhea.lineGap = asc, -desc, 0
    os2.sTypoAscender, os2.sTypoDescender, os2.sTypoLineGap = asc, -desc, 0
    os2.usWinAscent, os2.usWinDescent = asc, desc

    name = font["name"]
    full = FAMILY + ("" if style == "Regular" else " " + style)
    ps = FAMILY.replace(" ", "") + "-" + style
    for nid, val in ((1, FAMILY), (2, style), (3, ps), (4, full), (6, ps)):
        name.setName(val, nid, 3, 1, 0x409)
        name.setName(val, nid, 1, 0, 0)
    font.save(dst)
    print(dst, os.path.getsize(dst), "bytes")


def main():
    srcdir, outdir = sys.argv[1], sys.argv[2]
    for style in ("Regular", "Bold"):
        build(os.path.join(srcdir, "Inter-%s.otf" % style),
              os.path.join(outdir, "keysface-%s.ttf" % style.lower()), style)


if __name__ == "__main__":
    main()
