package hunspell

import (
	"crypto/md5"
	"encoding/binary"
	"errors"
	"strings"
)

// Chromium's BDict format (third_party/hunspell/google/bdict.h, BSD):
//
//	header: "BDic" | u16 major | u16 minor | u32 aff offset | u32 dic offset |
//	        md5 of everything after the header (v2)
//	aff:    u32 affix group table | u32 affix rules | u32 REP pairs | u32 other
//	        lines; each table is NUL-terminated strings ending in an empty one
//	dic:    a trie of the words. Leaf nodes 0abxxxxx xxxxxxxx: x the AF
//	        index, a: a NUL-terminated tail string follows, b: a u16 list of
//	        more AF indices follows (0xFFFF ends it; 0x1FFF in x means only
//	        the list counts). Lookup nodes 110000ab | first | count [| 0th
//	        entry] | table: b 32-bit absolute offsets, else 16-bit relative
//	        to the node; a: the 0th (end of word) entry comes first. List
//	        nodes 111bxxxx: x (char, offset) pairs, b 16-bit offsets, both
//	        relative to the end of the pairs.

const bdicSignature = 0x63694442 // "BDic"

var errBDic = errors.New("hunspell: not a valid .bdic file")

// ParseBDic loads a Chromium .bdic dictionary.
func ParseBDic(data []byte) (*Dict, error) {
	if len(data) < 16 || binary.LittleEndian.Uint32(data) != bdicSignature {
		return nil, errBDic
	}
	major := binary.LittleEndian.Uint16(data[4:])
	if major > 2 {
		return nil, errBDic
	}
	affOff := int(binary.LittleEndian.Uint32(data[8:]))
	dicOff := int(binary.LittleEndian.Uint32(data[12:]))
	if major >= 2 {
		if len(data) < 32 {
			return nil, errBDic
		}
		if sum := md5.Sum(data[32:]); string(sum[:]) != string(data[16:32]) {
			return nil, errors.New("hunspell: .bdic checksum mismatch")
		}
	}
	if affOff+16 > len(data) || dicOff >= len(data) {
		return nil, errBDic
	}
	groupOff := int(binary.LittleEndian.Uint32(data[affOff:]))
	ruleOff := int(binary.LittleEndian.Uint32(data[affOff+4:]))
	repOff := int(binary.LittleEndian.Uint32(data[affOff+8:]))
	otherOff := int(binary.LittleEndian.Uint32(data[affOff+12:]))

	d := newDict()
	var lines []string
	lines = append(lines, cstrings(data, otherOff)...) // FLAG, TRY, … first
	groups := cstrings(data, groupOff)
	lines = append(lines, groups...)
	lines = append(lines, cstrings(data, ruleOff)...)
	d.parseAff(lines)
	reps := cstrings(data, repOff)
	for i := 0; i+1 < len(reps); i += 2 {
		d.rep = append(d.rep, [2]string{strings.ReplaceAll(reps[i], "_", " "), strings.ReplaceAll(reps[i+1], "_", " ")})
	}
	w := bdicWalker{data: data, d: d}
	w.walk(dicOff, nil, 0)
	if w.bad {
		return nil, errBDic
	}
	return d, nil
}

// cstrings reads NUL-terminated strings from off up to an empty one.
func cstrings(data []byte, off int) []string {
	var out []string
	if off <= 0 || off >= len(data) {
		return nil
	}
	for off < len(data) && data[off] != 0 {
		end := off
		for end < len(data) && data[end] != 0 {
			end++
		}
		out = append(out, string(data[off:end]))
		off = end + 1
	}
	return out
}

type bdicWalker struct {
	data []byte
	d    *Dict
	bad  bool
}

func (w *bdicWalker) u16(off int) int {
	if off+2 > len(w.data) || off < 0 {
		w.bad = true
		return 0
	}
	return int(binary.LittleEndian.Uint16(w.data[off:]))
}

func (w *bdicWalker) u32(off int) int {
	if off+4 > len(w.data) || off < 0 {
		w.bad = true
		return 0
	}
	return int(binary.LittleEndian.Uint32(w.data[off:]))
}

func (w *bdicWalker) walk(off int, prefix []byte, depth int) {
	if w.bad || depth > 256 || off < 0 || off >= len(w.data) {
		w.bad = w.bad || off < 0 || off >= len(w.data)
		return
	}
	id := w.data[off]
	switch {
	case id&0x80 == 0: // leaf
		word := append([]byte(nil), prefix...)
		pos := off + 2
		if id&0xC0 == 0x40 {
			end := pos
			for end < len(w.data) && w.data[end] != 0 {
				end++
			}
			if end >= len(w.data) {
				w.bad = true
				return
			}
			word = append(word, w.data[pos:end]...)
			pos = end + 1
		}
		if off+1 >= len(w.data) {
			w.bad = true
			return
		}
		first := int(id&0x1F)<<8 | int(w.data[off+1])
		var ids []int
		if first != 0x1FFF {
			ids = append(ids, first)
		}
		if id&0xA0 == 0x20 || first == 0x1FFF {
			for i := 0; i < 32; i++ {
				v := w.u16(pos)
				pos += 2
				if w.bad || v == 0xFFFF {
					break
				}
				ids = append(ids, v)
			}
		}
		s := string(word)
		for _, a := range ids {
			if a == 0 {
				w.d.words[s] = append(w.d.words[s], nil)
				continue
			}
			var fs flags
			if a >= 1 && a <= len(w.d.aliases) {
				fs = w.d.aliases[a-1]
			}
			w.d.words[s] = append(w.d.words[s], fs)
		}
	case id&0xFC == 0xC0: // lookup
		is32 := id&0x02 != 0
		has0 := id&0x01 != 0
		if off+3 > len(w.data) {
			w.bad = true
			return
		}
		firstChar, n := int(w.data[off+1]), int(w.data[off+2])
		table := off + 3
		entry := func(at int) int {
			if is32 {
				return w.u32(at)
			}
			v := w.u16(at)
			if v == 0 {
				return 0
			}
			return off + v
		}
		if has0 {
			if is32 {
				if c := w.u32(table); c != 0 {
					w.walk(c, prefix, depth)
				}
				table += 4
			} else {
				if v := w.u16(table); v != 0 {
					w.walk(off+v, prefix, depth)
				}
				table += 2
			}
		}
		size := 2
		if is32 {
			size = 4
		}
		for i := 0; i < n && !w.bad; i++ {
			c := entry(table + i*size)
			if c == 0 {
				continue
			}
			ch := byte(firstChar + i)
			if ch == 0 {
				w.walk(c, prefix, depth)
				continue
			}
			w.walk(c, append(prefix, ch), depth+1)
		}
	case id&0xE0 == 0xE0: // list
		n := int(id & 0x0F)
		per := 2
		if id&0xF0 == 0xF0 {
			per = 3
		}
		begin := off + 1
		children := begin + n*per
		if children > len(w.data) {
			w.bad = true
			return
		}
		for i := 0; i < n && !w.bad; i++ {
			it := begin + i*per
			ch := w.data[it]
			var rel int
			if per == 3 {
				rel = w.u16(it + 1)
			} else {
				rel = int(w.data[it+1])
			}
			c := children + rel
			if ch == 0 {
				w.walk(c, prefix, depth)
				continue
			}
			w.walk(c, append(prefix, ch), depth+1)
		}
	default:
		w.bad = true
	}
}
