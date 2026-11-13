// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package font

import (
	"sync"
	"sync/atomic"

	"github.com/go-text/typesetting/font/opentype/tables"
)

// Clack patch 1 (CLACK_PATCHES.md): the 'glyf' table parsed one glyph at a
// time, the first time a glyph is used, instead of every glyph when the
// font is loaded. A CJK system font the shaper falls back to has 20,000
// to 60,000 glyphs, of which a message uses a few dozen; parsed whole, the
// handful a Windows client loads held 70 MB. The raw table stays (parsed
// glyphs' instructions were subslices of it anyway); a glyph costs 40
// bytes until it's used.

type glyfTable struct {
	src  []byte
	loca []uint32
	mu   sync.Mutex
	done []uint32 // atomic: 1 once gs[i] is set
	gs   []tables.Glyph
}

// newGlyfTable is the lazy counterpart of tables.ParseGlyf: nil, as a
// table ParseGlyf rejects, when the offsets don't fit the table.
func newGlyfTable(src []byte, loca []uint32) *glyfTable {
	n := len(loca) - 1
	if n <= 0 {
		return nil
	}
	for i := 0; i < n; i++ {
		if loca[i] > loca[i+1] || int(loca[i+1]) > len(src) {
			return nil
		}
	}
	return &glyfTable{src: src, loca: loca, done: make([]uint32, n), gs: make([]tables.Glyph, n)}
}

// len is the glyph count (0 without a table).
func (g *glyfTable) len() int {
	if g == nil {
		return 0
	}
	return len(g.gs)
}

// glyph is glyph gid, parsed on first use; gid < len(). A glyph that
// doesn't parse is empty (ParseGlyf dropped the whole table instead).
func (g *glyfTable) glyph(gid tables.GlyphID) tables.Glyph {
	i := int(gid)
	if atomic.LoadUint32(&g.done[i]) == 1 {
		return g.gs[i]
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if atomic.LoadUint32(&g.done[i]) == 0 {
		if start, end := g.loca[i], g.loca[i+1]; start != end {
			if gl, _, err := tables.ParseGlyph(g.src[start:end]); err == nil {
				g.gs[i] = gl
			}
		}
		atomic.StoreUint32(&g.done[i], 1)
	}
	return g.gs[i]
}
