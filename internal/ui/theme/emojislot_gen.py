"""Builds emojislot.ttf: the placeholder face for unicode emoji.

Glyph 1 ("slot") is empty with a 1.27 em advance (Slack's 1.2 em emoji image
plus its 1px gap at 15px) and short vertical metrics so it never grows a
Lato line. Two kinds of text use it:

- Wrapping labels (theme.Text) replace each emoji with U+10FFFD, which only
  this face maps.
- Editors (widget.Editor through theme.Editor, the composer's own layout)
  keep the real emoji in their text, so caret, selection, IME and undo see
  the true runes. Their font names this face after Lato
  ("Clack Lato, Clack Emoji Slot"), and the face maps every emoji code
  point Lato lacks:
    * base emoji -> slot
    * regional indicators -> "ri" (as wide as slot; a pair ligates to slot)
    * ZWJ -> "zwj" (zero width)
    * VS16, skin tone modifiers, tags, the keycap mark -> "mark" (zero
      width, GDEF mark class)
  A ccmp lookup with IgnoreMarks ligates "slot zwj slot ..." (ZWJ
  sequences: families, professions) and "ri ri" (flags) into one slot
  glyph, so every emoji grapheme shapes as exactly one 1.27 em glyph and
  the image overlay goes where that glyph landed.

It also maps the space and no-break space at Lato Regular's advance
(0.256 em): go-text keeps a space in the run of the face before it, so
"😱 x" shapes that space here.

    python3 -I internal/ui/theme/emojislot_gen.py internal/emoji/standard.json.gz internal/ui/theme/emojislot.ttf

Needs fontTools (pip install fonttools).
"""
import gzip
import io
import json
import sys

from fontTools.feaLib.builder import addOpenTypeFeaturesFromString
from fontTools.fontBuilder import FontBuilder
from fontTools.pens.ttGlyphPen import TTGlyphPen

UPM = 1000
ADV = 1270
SLOT = 0x10FFFD
ZWJ = 0x200D
MAX_ZWJ_BASES = 6  # longest ZWJ chain in the table is 4 bases (families)


def is_mark(cp):
    return (
        cp == 0xFE0F
        or cp == 0xFE0E
        or cp == 0x20E3
        or 0x1F3FB <= cp <= 0x1F3FF
        or 0xE0020 <= cp <= 0xE007F
    )


def is_ri(cp):
    return 0x1F1E6 <= cp <= 0x1F1FF


def code_points(table):
    seen = set()
    for e in table["emoji"]:
        seqs = [e.get("i", ""), e.get("u", "")] + list((e.get("s") or {}).values())
        for seq in seqs:
            for part in seq.split("-"):
                if part:
                    seen.add(int(part, 16))
    return seen


def main(src, dst):
    with gzip.open(src) as f:
        table = json.load(io.TextIOWrapper(f, "utf-8"))
    cmap = {SLOT: "slot", 0x20: "space", 0xA0: "space", ZWJ: "zwj"}
    for cp in sorted(code_points(table)):
        if cp < 0x80 or cp in cmap:
            continue  # ASCII (#, *, digits of keycaps) stays Lato's
        if is_mark(cp):
            cmap[cp] = "mark"
        elif is_ri(cp):
            cmap[cp] = "ri"
        else:
            cmap[cp] = "slot"
    for cp in range(0x1F1E6, 0x1F200):
        cmap[cp] = "ri"
    for cp in list(range(0x1F3FB, 0x1F400)) + list(range(0xE0020, 0xE0080)) + [0xFE0F, 0x20E3]:
        cmap[cp] = "mark"

    order = [".notdef", "slot", "space", "mark", "zwj", "ri"]  # slot is GID 1, space 2
    fb = FontBuilder(UPM, isTTF=True)
    fb.setupGlyphOrder(order)
    fb.setupCharacterMap(cmap)
    empty = TTGlyphPen(None).glyph
    fb.setupGlyf({g: empty() for g in order})
    fb.setupHorizontalMetrics({
        ".notdef": (0, 0), "slot": (ADV, 0), "space": (256, 0),
        "mark": (0, 0), "zwj": (0, 0), "ri": (ADV, 0),
    })
    fb.setupHorizontalHeader(ascent=800, descent=-200)
    fb.setupNameTable({"familyName": "Clack Emoji Slot", "styleName": "Regular"})
    fb.setupOS2(sTypoAscender=800, sTypoDescender=-200, sTypoLineGap=0, usWinAscent=800, usWinDescent=200)
    fb.setupPost()

    ligs = []
    for n in range(MAX_ZWJ_BASES, 1, -1):  # longest first
        ligs.append("    sub " + " zwj ".join(["slot"] * n) + " by slot;")
    ligs.append("    sub ri ri by slot;")
    fea = """
languagesystem DFLT dflt;
languagesystem latn dflt;
table GDEF {
    GlyphClassDef [slot space zwj ri], , [mark], ;
} GDEF;
lookup emoji {
    lookupflag IgnoreMarks;
%s
} emoji;
feature ccmp {
    lookup emoji;
} ccmp;
""" % "\n".join(ligs)
    addOpenTypeFeaturesFromString(fb.font, fea)
    fb.save(dst)


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
