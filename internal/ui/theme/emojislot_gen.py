"""Builds emojislot.ttf: the placeholder face for unicode emoji in wrapping
labels (theme.Text). One empty glyph, mapped from U+10FFFD, with a 1.27 em
advance (Slack's 1.2 em emoji image plus its 1px gap at 15px) and short
vertical metrics so it never grows a Lato line. It also maps the space and
no-break space at Lato Regular's advance (0.256 em): go-text keeps a space
in the run of the face before it, so "😱 x" shapes that space here.

    python3 -I internal/ui/theme/emojislot_gen.py internal/ui/theme/emojislot.ttf

Needs fontTools (pip install fonttools).
"""
import sys

from fontTools.fontBuilder import FontBuilder
from fontTools.pens.ttGlyphPen import TTGlyphPen

UPM = 1000
ADV = 1270
SLOT = 0x10FFFD


def empty():
    return TTGlyphPen(None).glyph()


fb = FontBuilder(UPM, isTTF=True)
fb.setupGlyphOrder([".notdef", "slot", "space"])
fb.setupCharacterMap({SLOT: "slot", 0x20: "space", 0xA0: "space"})
fb.setupGlyf({".notdef": empty(), "slot": empty(), "space": empty()})
fb.setupHorizontalMetrics({".notdef": (0, 0), "slot": (ADV, 0), "space": (256, 0)})
fb.setupHorizontalHeader(ascent=800, descent=-200)
fb.setupNameTable({"familyName": "Clack Emoji Slot", "styleName": "Regular"})
fb.setupOS2(sTypoAscender=800, sTypoDescender=-200, sTypoLineGap=0, usWinAscent=800, usWinDescent=200)
fb.setupPost()
fb.save(sys.argv[1])
