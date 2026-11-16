package vp8l

// Clack: the byte budget a decode charges (CLACK_PATCHES.md). This file
// is Clack's, not upstream's.

import (
	"github.com/some-du6e/clack/internal/imgcache/internal/budget"
)

// ErrBudget is a decode that would allocate more than its Budget allows.
// The decoder returns it before making the allocation that doesn't fit.
// (A budget drawing from the process-wide pool fails with budget.ErrBusy
// instead when the pool can't cover the charge right now.)
var ErrBudget = budget.ErrBudget

// Budget is how many bytes one decode may allocate in all: the shared
// budget.Budget every vendored decoder charges. Every allocation the
// decoder makes (the Huffman groups and their trees, code lengths, the
// colour caches, transform data, pixel buffers, its reader) charges it
// first. Nothing is given back when memory is freed, so what a decode
// allocates over its whole life, not just at its peak, stays within the
// budget. A nil *Budget allows nothing.
type Budget = budget.Budget

// NewBudget returns a budget of n bytes that draws from no pool (the
// header reads; imgcache passes pooled budgets of its own).
func NewBudget(n int64) *Budget { return budget.NewLocal(n) }

// newNodes gives h a zeroed node array for a tree of nSymbols symbols:
// len 1 and cap 2·nSymbols−1, what upstream made and insert fills up to.
// A tree a group keeps charges the budget for a new array. The decoder's
// scratch trees (lenTree, dropTree) reuse theirs, charging only when it
// has to grow, and are cleared first, look-up table and all, so reading a
// code into one is as if into a new tree.
func (d *decoder) newNodes(h *hTree, nSymbols uint32) error {
	n := int(2*nSymbols - 1)
	var array *[]hNode
	switch h {
	case &d.lenTree:
		array = &d.lenNodes
	case &d.dropTree:
		array = &d.dropNodes
	default:
		if err := d.charge(int64(n) * hNodeBytes); err != nil {
			return err
		}
		h.nodes = make([]hNode, 1, n)
		return nil
	}
	if cap(*array) < n {
		if err := d.charge(int64(n) * hNodeBytes); err != nil {
			return err
		}
		*array = make([]hNode, n)
	}
	nodes := (*array)[:n]
	clear(nodes)
	h.nodes = nodes[:1:n]
	h.lut = [1 << lutSize]uint32{}
	return nil
}

// scratch returns the first n entries of *s, cleared, growing *s first
// (and charging the budget for it) when it's shorter.
func (d *decoder) scratch(s *[]uint32, n uint32) ([]uint32, error) {
	if uint32(cap(*s)) < n {
		if err := d.charge(4 * int64(n)); err != nil {
			return nil, err
		}
		*s = make([]uint32, n)
	}
	b := (*s)[:n]
	clear(b)
	return b, nil
}

// The sizes the decoder charges for what it allocates, at least what Go
// allocates for each on any platform (budget_test.go checks them against
// unsafe.Sizeof).
const (
	hNodeBytes  = 8                  // hNode: a uint32 and an int32
	hTreeBytes  = 24 + 4<<lutSize    // hTree: the nodes slice header and the look-up table
	hGroupBytes = nHuff * hTreeBytes // hGroup: five trees, 2680 bytes
	readerBytes = 4096 + 128         // bufio.NewReader: its 4 KB buffer and the Reader
	fixedBytes  = 4096               // the decoder, the image header and other small structs
)
