#!/usr/bin/env python3
"""Makes site/fonts/clack-heading-bold.woff2, the page's one web font.

It's a subset of TeX Gyre Adventor Bold 2.501 (GUST e-foundry, GUST Font
License), the free face closest to the Avant Garde slack.com sets its
headings in: over ten test strings its advance widths are within 1.4% of
Salesforce-Avant-Garde's, and cap height and x-height are the same. The
subset keeps printable ASCII and a few typographic characters, keeps
kerning, and is renamed "Clack Heading", as the GUST Font License asks of
derived fonts. site/fonts/clack-heading-bold.json lists the characters it
covers; tools/check.mjs fails a build whose headings use any other.

    curl -LO https://www.gust.org.pl/projects/e-foundry/tex-gyre/adventor/qag2_501otf.zip
    unzip qag2_501otf.zip   # or CTAN: fonts/tex-gyre/opentype/texgyreadventor-bold.otf
    python3 tools/font.py qag2_501otf/texgyreadventor-bold.otf

Needs fontTools and brotli (python3 -m pip install fonttools brotli).
"""
import hashlib, json, os, sys
from fontTools import subset
from fontTools.ttLib import TTFont

SOURCE_SHA256 = "e373ae29c808c6d9149e3add46564f9acdb6ebfcd0ee311f6a55560226994720"
EXTRA = " ©·×–—‘’“”…→"

def main(src):
    data = open(src, "rb").read()
    got = hashlib.sha256(data).hexdigest()
    if got != SOURCE_SHA256:
        sys.exit(f"font.py: {src} is sha256 {got}, expected TeX Gyre Adventor Bold 2.501 ({SOURCE_SHA256})")
    site = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
    out = os.path.join(site, "fonts", "clack-heading-bold.woff2")
    font = TTFont(src)
    cmap = font.getBestCmap()
    want = [c for c in list(range(0x20, 0x7F)) + [ord(ch) for ch in EXTRA] if c in cmap]
    opts = subset.Options()
    opts.layout_features = ["kern"]
    opts.name_IDs = [0, 1, 2, 3, 4, 5, 6, 13, 14]
    opts.flavor = "woff2"
    opts.hinting = False
    opts.desubroutinize = True
    sub = subset.Subsetter(opts)
    sub.populate(unicodes=want)
    sub.subset(font)
    names = font["name"]
    for rec in list(names.names):
        if rec.nameID in (1, 4):
            rec.string = "Clack Heading" if rec.nameID == 1 else "Clack Heading Bold"
        elif rec.nameID in (3, 6):
            rec.string = "ClackHeading-Bold"
        elif rec.nameID == 5:
            rec.string = str(rec) + "; subset of TeX Gyre Adventor Bold for getclack.club"
    font.flavor = "woff2"
    font.save(out)
    json.dump({
        "family": "Clack Heading",
        "source": "TeX Gyre Adventor Bold 2.501 (texgyreadventor-bold.otf), GUST e-foundry, GUST Font License (fonts/GUST-FONT-LICENSE.txt)",
        "sourceSha256": SOURCE_SHA256,
        "metrics": {"unitsPerEm": 1000, "ascender": 739, "descender": -185, "capHeight": 739, "xHeight": 554},
        "codepoints": want,
    }, open(out.replace(".woff2", ".json"), "w"), indent=1)
    print(f"{out}: {os.path.getsize(out)} bytes, {len(want)} characters")

if __name__ == "__main__":
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    main(sys.argv[1])
