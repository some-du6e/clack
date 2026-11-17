// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"fmt"
	"image"

	"gioui.org/internal/f32"
)

// Clack patch 45: sticky atlas places for partial frames.
//
// A path's stencil coverage depends by a rounding on where its region
// lands in the coverage atlas, and the packer placed every region after
// the ones before it: a path added or dropped (the typing indicator's
// text appearing, a toolbar's icons) moved every later path, so every op
// drawn through one of them was damage and the frame was drawn whole.
// With partial frames a region that was in the last frame keeps its place:
// the last frame's packing sequence is replayed (the packer places the
// same sizes in the same order the same way), a region taking the slot
// the same region had (the same path geometry, region and offset), a
// slot nothing takes left as a hole; the regions that weren't there come
// after. When the holes outgrow the regions, or the sequence would need a
// new atlas page, the regions are packed afresh in the frame's order (a
// frame drawn whole, as any change to every place is).

// stickyKey is a stencil region's identity across frames: what decides
// its coverage but its place.
type stickyKey struct {
	h    [2]uint64
	clip image.Rectangle
	off  f32.Point
}

// stickySlot is a slot of the packing sequence: a region's size and who
// had it (zero: a hole).
type stickySlot struct {
	size image.Point
	key  stickyKey
	live bool
}

// stickyAtlas is the last frame's packing sequence.
type stickyAtlas struct {
	slots, next []stickySlot
	// first and same index the last frame's live slots by key (index + 1;
	// same: the next slot with the key)
	first map[stickyKey]int32
	same  []int32
	// scratch: each slot's region this frame, the new regions
	take  []stickyRegion
	fresh []stickyRegion
	// repacked counts the fresh packings (for tests)
	repacked int
}

// stickyRegion is a region of path p: its main one (i -1) or p.more[i].
type stickyRegion struct {
	p *pathOp
	i int
}

func (sr stickyRegion) clip() image.Rectangle {
	if sr.i < 0 {
		return sr.p.clip
	}
	return sr.p.more[sr.i].clip
}

func (sr stickyRegion) setPlace(pl placement) {
	if sr.i < 0 {
		sr.p.place = pl
	} else {
		sr.p.more[sr.i].place = pl
	}
}

func (sr stickyRegion) key() stickyKey {
	return stickyKey{h: sr.p.hash, clip: sr.clip(), off: sr.p.off}
}

func (s *stickyAtlas) reset() {
	s.slots, s.next = s.slots[:0], s.next[:0]
	clear(s.first)
	s.same = s.same[:0]
}

// pack places ops' regions with pk.
func (s *stickyAtlas) pack(pk *packer, ops []*pathOp) {
	if s.first == nil {
		s.first = make(map[stickyKey]int32)
	}
	n := len(s.slots)
	if cap(s.take) < n {
		s.take = make([]stickyRegion, n)
	} else {
		s.take = s.take[:n]
		clear(s.take)
	}
	s.fresh = s.fresh[:0]
	liveArea := int64(0)
	for _, p := range ops {
		for i := -1; i < len(p.more); i++ {
			sr := stickyRegion{p, i}
			liveArea += area(sr.clip())
			k := sr.key()
			at := s.first[k] - 1
			for at >= 0 && s.take[at].p != nil {
				at = s.same[at] - 1
			}
			if at >= 0 {
				s.take[at] = sr
				continue
			}
			s.fresh = append(s.fresh, sr)
		}
	}
	// replay the last frame's sequence, then the new regions
	pk.clear()
	next := s.next[:0]
	holes := int64(0)
	pages := 0
	for k, sl := range s.slots {
		place := stickyAdd(pk, sl.size)
		pages = max(pages, place.Idx+1)
		sr := s.take[k]
		if sr.p != nil {
			sr.setPlace(place)
			next = append(next, stickySlot{size: sl.size, key: sl.key, live: true})
		} else {
			next = append(next, stickySlot{size: sl.size})
			holes += int64(sl.size.X * sl.size.Y)
		}
	}
	for _, sr := range s.fresh {
		c := sr.clip()
		place := stickyAdd(pk, c.Size())
		pages = max(pages, place.Idx+1)
		sr.setPlace(place)
		next = append(next, stickySlot{size: c.Size(), key: sr.key(), live: true})
	}
	// the holes at the end hold no one's place
	for len(next) > 0 && !next[len(next)-1].live {
		holes -= int64(next[len(next)-1].size.X * next[len(next)-1].size.Y)
		next = next[:len(next)-1]
	}
	if holes > liveArea || (pages > 1 && holes > 0) {
		// afresh, in the frame's order
		s.repacked++
		pk.clear()
		next = next[:0]
		for _, p := range ops {
			for i := -1; i < len(p.more); i++ {
				sr := stickyRegion{p, i}
				c := sr.clip()
				sr.setPlace(stickyAdd(pk, c.Size()))
				next = append(next, stickySlot{size: c.Size(), key: sr.key(), live: true})
			}
		}
	}
	s.slots, s.next = next, s.slots
	// index the slots for the next frame, the first slot of a key first
	clear(s.first)
	if cap(s.same) < len(s.slots) {
		s.same = make([]int32, len(s.slots))
	} else {
		s.same = s.same[:len(s.slots)]
		clear(s.same)
	}
	for k := len(s.slots) - 1; k >= 0; k-- {
		sl := s.slots[k]
		if !sl.live {
			continue
		}
		s.same[k] = s.first[sl.key]
		s.first[sl.key] = int32(k + 1)
	}
	clear(s.take)
	clear(s.fresh)
}

func stickyAdd(pk *packer, sz image.Point) placement {
	place, ok := pk.add(sz)
	if !ok {
		// The clip area is at most the entire screen. Hopefully no
		// screen is larger than GL_MAX_TEXTURE_SIZE.
		panic(fmt.Errorf("clip area %v is larger than maximum texture size %v", sz, pk.maxDims))
	}
	return place
}
