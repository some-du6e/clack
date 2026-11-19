// SPDX-License-Identifier: Unlicense OR MIT

package gpu

import (
	"fmt"
	"image"

	"gioui.org/internal/f32"
)

// Clack patch 46: sticky atlas places for partial frames.
//
// A path's stencil coverage depends by a rounding on where its region
// lands in the coverage atlas, and the packer placed every region after
// the ones before it: a path added or dropped (the typing indicator's
// text appearing, a toolbar's icons) moved every later path, so every op
// drawn through one of them was damage and the frame was drawn whole.
// With partial frames a region that was in the last frame keeps its place:
// the last frame's packing sequence is replayed (the packer places the
// same sizes in the same order the same way), a region taking the slot
// the same region had (the same path geometry, region and offset). A new
// region takes a slot nobody took of exactly its size, else the smallest
// that holds it, keeping the slot's size, so nothing else moves either: a
// path that changes every frame (a working agent's spinner, every 33 ms)
// takes back the slots its last frames left instead of piling up holes
// the replay packs every frame. The regions no slot holds come after; a
// slot nothing takes is a hole. When the holes outnumber the live slots
// or stickyMaxHoles, the slots (holes and the slack of bigger slots
// included) cover more than stickySlack halves of the regions' own area
// (after a lower scale or a smaller window every region fits an old,
// bigger slot), or the sequence needs more atlas pages than the last
// fresh packing did, the regions are packed afresh in the frame's order (a
// frame drawn whole, as any change to every place is). A frame with more
// paths of new geometry than a partial frame can add (a scroll, a live
// resize, a channel switch) is whole however it's packed, so it's packed
// afresh at once, without matching holes or replaying the sequence.

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
	// scratch: each slot's region this frame, the new regions, the slots
	// nobody took, and those indexed by size (exactFirst: the first hole
	// of a size, index + 1; exactNext: the next hole of its size)
	take       []stickyRegion
	fresh      []stickyRegion
	holes      []int32
	exactFirst map[image.Point]int32
	exactNext  []int32
	// holeSlots are the holes in slots; freshPages the pages the last
	// fresh packing needed; repacked counts the fresh packings of a
	// sequence there was (FrameStats.StickyRepacks, tests)
	holeSlots  int
	freshPages int
	repacked   int
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
	s.holeSlots, s.freshPages = 0, 0
}

const (
	// stickyMaxHoles bounds the holes the sequence keeps: past it (or past
	// as many holes as live slots) the regions are packed afresh. The
	// replay packs every slot each frame, holes included.
	stickyMaxHoles = 256
	// stickySlack bounds the slots' area against the regions' own (in
	// halves: 3 is 1.5 times): holes, and the slack of slots bigger than
	// the regions that took them, are atlas texels nothing uses, which a
	// whole frame clears and stores all the same.
	stickySlack = 3
)

// pack places ops' regions with pk.
func (s *stickyAtlas) pack(pk *packer, ops []*pathOp) {
	if s.first == nil {
		s.first = make(map[stickyKey]int32)
		s.exactFirst = make(map[image.Point]int32)
	}
	n := len(s.slots)
	if cap(s.take) < n {
		s.take = make([]stickyRegion, n)
	} else {
		s.take = s.take[:n]
		clear(s.take)
	}
	s.fresh = s.fresh[:0]
	// each region takes its own slot from the last frame
	freshOps := 0
	var last *pathOp
	for _, p := range ops {
		for i := -1; i < len(p.more); i++ {
			sr := stickyRegion{p, i}
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
			if p != last {
				freshOps, last = freshOps+1, p
			}
		}
	}
	// More paths of new geometry than a partial frame can add (each is an
	// op whose signature the last frame didn't have): the frame is drawn
	// whole however it's packed. A scroll's frames, a live resize's, a
	// channel switch's: packed afresh in the frame's order, as Gio packs
	// them, with no holes matched and no sequence replayed, and the next
	// frame replays a compact one.
	if n == 0 || freshOps > partialMaxEdits {
		if n > 0 {
			s.repacked++
		}
		s.packAfresh(pk, ops)
		return
	}
	// a new region takes the smallest slot nobody took that holds it, one
	// of exactly its size first: the slot keeps its size, so the packer is
	// given the same sizes in the same order and no other place moves. A
	// path that changes every frame (a spinner's arcs) takes the slots its
	// last frames left.
	s.holes = s.holes[:0]
	for k := range s.slots {
		if s.take[k].p == nil {
			s.holes = append(s.holes, int32(k))
		}
	}
	clear(s.exactFirst)
	if cap(s.exactNext) < n {
		s.exactNext = make([]int32, n)
	} else {
		s.exactNext = s.exactNext[:n]
	}
	for hi := len(s.holes) - 1; hi >= 0; hi-- {
		k := s.holes[hi]
		sz := s.slots[k].size
		s.exactNext[k] = s.exactFirst[sz]
		s.exactFirst[sz] = k + 1
	}
	appended := s.fresh[:0]
	for _, sr := range s.fresh {
		sz := sr.clip().Size()
		pk.work++
		at := s.exactFirst[sz] - 1
		for at >= 0 && s.take[at].p != nil {
			at = s.exactNext[at] - 1
		}
		if at >= 0 {
			s.take[at] = sr
			s.exactFirst[sz] = s.exactNext[at]
			continue
		}
		best, bestArea := int32(-1), 0
		pk.work += int64(len(s.holes))
		for _, k := range s.holes {
			if s.take[k].p != nil {
				continue
			}
			hs := s.slots[k].size
			if hs.X < sz.X || hs.Y < sz.Y {
				continue
			}
			if a := hs.X * hs.Y; best < 0 || a < bestArea {
				best, bestArea = k, a
			}
		}
		if best < 0 {
			appended = append(appended, sr)
			continue
		}
		s.take[best] = sr
	}
	// replay the last frame's sequence, then the regions no slot held
	pk.clear()
	next := s.next[:0]
	var holes, regionArea, slotArea int64
	holeSlots, pages := 0, 0
	for k, sl := range s.slots {
		place := stickyAdd(pk, sl.size)
		pages = max(pages, place.Idx+1)
		slotArea += int64(sl.size.X * sl.size.Y)
		sr := s.take[k]
		if sr.p != nil {
			sr.setPlace(place)
			next = append(next, stickySlot{size: sl.size, key: sr.key(), live: true})
			regionArea += area(sr.clip())
		} else {
			next = append(next, stickySlot{size: sl.size})
			holes += int64(sl.size.X * sl.size.Y)
			holeSlots++
		}
	}
	for _, sr := range appended {
		c := sr.clip()
		place := stickyAdd(pk, c.Size())
		pages = max(pages, place.Idx+1)
		sr.setPlace(place)
		next = append(next, stickySlot{size: c.Size(), key: sr.key(), live: true})
		regionArea += area(c)
		slotArea += area(c)
	}
	// the holes at the end hold no one's place
	for len(next) > 0 && !next[len(next)-1].live {
		a := int64(next[len(next)-1].size.X * next[len(next)-1].size.Y)
		holes -= a
		slotArea -= a
		holeSlots--
		next = next[:len(next)-1]
	}
	liveSlots := len(next) - holeSlots
	if holeSlots > liveSlots || holeSlots > stickyMaxHoles || slotArea*2 > regionArea*stickySlack ||
		(pages > s.freshPages && holes > 0) {
		// afresh, in the frame's order
		s.repacked++
		s.packAfresh(pk, ops)
		return
	}
	s.slots, s.next = next, s.slots
	s.holeSlots = holeSlots
	s.index()
	clear(s.take)
	clear(s.fresh)
}

// packAfresh places ops' regions in the frame's order and makes them the
// sequence.
func (s *stickyAtlas) packAfresh(pk *packer, ops []*pathOp) {
	pk.clear()
	next := s.next[:0]
	for _, p := range ops {
		for i := -1; i < len(p.more); i++ {
			sr := stickyRegion{p, i}
			c := sr.clip()
			sr.setPlace(stickyAdd(pk, c.Size()))
			next = append(next, stickySlot{size: c.Size(), key: sr.key(), live: true})
		}
	}
	s.slots, s.next = next, s.slots
	s.holeSlots = 0
	s.freshPages = len(pk.sizes)
	s.index()
	clear(s.take)
	clear(s.fresh)
}

// index indexes the slots for the next frame, the first slot of a key
// first.
func (s *stickyAtlas) index() {
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
