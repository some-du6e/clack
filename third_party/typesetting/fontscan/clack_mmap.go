// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package fontscan

import (
	"fmt"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
)

// Clack patch 3 (CLACK_PATCHES.md): a system font the shaper falls back to
// is memory-mapped and parsed in place (patch 2's BytesResource) instead of
// read table by table into the heap. Its pages are the file's, shared and
// reclaimable rather than the process's own, and only the pages of tables
// and glyphs actually used are ever read: a CJK font's glyf table alone is
// 10 to 20 MB. The mapping lives as long as the process (the face stays in
// the FontMap's cache); where mapping isn't available, or fails, the font
// is read as before.

func (fp *Footprint) loadFromDiskMapped() (*font.Face, error) {
	b, err := mapFontFile(fp.Location.File)
	if err != nil || len(b) == 0 {
		return fp.loadFromDisk()
	}
	loaders, err := ot.NewLoaders(ot.NewBytesResource(b))
	if err != nil {
		return nil, err
	}
	if index := int(fp.Location.Index); len(loaders) <= index {
		return nil, fmt.Errorf("invalid font index in collection: %d >= %d", index, len(loaders))
	}
	ft, err := font.NewFont(loaders[fp.Location.Index])
	if err != nil {
		return nil, fmt.Errorf("reading font at %s: %s", fp.Location.File, err)
	}
	return font.NewFace(ft), nil
}
