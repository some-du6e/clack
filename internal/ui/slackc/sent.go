package slackc

import (
	"encoding/json"
	"hash/fnv"
	"reflect"
)

// What a native sent its patched components of a message, told by
// content.
//
// A tree hands its msg prop back parsed afresh, never the map the native
// sent, so whether a tree's msg is props the native sent (JS passed them
// on, maybe from a revision before the one the native draws now) or a
// plugin's rewrite (Slick Censorship's mask) is told by what the message
// says. The rows of Activity (views) and of the message lists (messages)
// judge their trees by these, each level of their components by its own
// set.

// ContentFP fingerprints what a message msg says: its text, blocks,
// attachments and files, as a component's msg prop carries them (0 for
// none).
func ContentFP(msg Props) uint64 {
	if msg == nil {
		return 0
	}
	h := fnv.New64a()
	h.Write([]byte(msg.StrOr("text", "")))
	for _, k := range [...]string{"blocks", "attachments", "files"} {
		h.Write([]byte{0, k[0]})
		if v, ok := msg[k]; ok && v != nil {
			if b, err := json.Marshal(fpJSONable(v)); err == nil {
				h.Write(b)
			}
		}
	}
	return h.Sum64() | 1 // never 0
}

// fpJSONable is v with what JSON can't say left out: a Props marshals as
// the map it is.
func fpJSONable(v any) any {
	switch v := v.(type) {
	case Props:
		return fpJSONable(map[string]any(v))
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			if j := fpJSONable(x); j != nil {
				out[k] = j
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, x := range v {
			if j := fpJSONable(x); j != nil {
				out = append(out, j)
			}
		}
		return out
	case string, bool, float64, float32, int, int64, int32, uint64, nil:
		return v
	}
	return nil
}

// SentFPs are the ContentFPs of the msgs a native handed one level of its
// components (SentMax at most, newest first): a tree whose msg has one of
// them changed nothing the message says, whichever revision it answers.
// Kept by content, so reactions, reply counts and conversions again take
// no room; a tree of an older revision than the oldest kept counts as a
// plugin's change.
type SentFPs []uint64

// SentMax is how many contents a SentFPs keeps.
const SentMax = 16

// Has reports whether fp was sent.
func (s SentFPs) Has(fp uint64) bool {
	for _, x := range s {
		if x == fp {
			return true
		}
	}
	return false
}

// Add notes fp sent now (0 is nothing).
func (s *SentFPs) Add(fp uint64) {
	if fp == 0 || len(*s) > 0 && (*s)[0] == fp {
		return
	}
	for i, x := range *s {
		if x == fp { // sent again: newest
			copy((*s)[1:i+1], (*s)[:i])
			(*s)[0] = fp
			return
		}
	}
	if len(*s) < SentMax {
		*s = append(*s, 0)
	}
	copy((*s)[1:], (*s)[:len(*s)-1])
	(*s)[0] = fp
}

// FPMemo is ContentFP of the last map it was asked about (held, so its
// address can't be another map's): natives ask every frame.
type FPMemo struct {
	m  Props
	fp uint64
}

// Of is ContentFP(msg), worked out again only for another map.
func (f *FPMemo) Of(msg Props) uint64 {
	if msg == nil {
		return 0
	}
	if reflect.ValueOf(f.m).Pointer() != reflect.ValueOf(msg).Pointer() {
		f.m, f.fp = msg, ContentFP(msg)
	}
	return f.fp
}
