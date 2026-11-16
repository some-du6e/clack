package vp8l

// Clack: a tiny VP8L bit-writer for the budget tests, so they need no
// import outside this package. Nothing here comes from Slack.

type bitWriter struct {
	b    []byte
	acc  uint64
	nacc uint
}

func (w *bitWriter) put(v uint32, n uint) {
	w.acc |= uint64(v) << w.nacc
	w.nacc += n
	for w.nacc >= 8 {
		w.b = append(w.b, byte(w.acc))
		w.acc >>= 8
		w.nacc -= 8
	}
}

func (w *bitWriter) bytes() []byte {
	if w.nacc > 0 {
		w.b = append(w.b, byte(w.acc))
		w.acc, w.nacc = 0, 0
	}
	return w.b
}

// simpleTree writes a one-symbol prefix code (the symbol decodes in zero
// bits), as used for a solid picture's five codes.
func (w *bitWriter) simpleTree(symbol uint8) {
	w.put(1, 1) // simple code
	w.put(0, 1) // one symbol
	w.put(1, 1) // of 8 bits
	w.put(uint32(symbol), 8)
}

// solidVP8L is a w×h VP8L bitstream of one opaque colour: no transforms,
// no colour cache, no meta prefix codes, five one-symbol codes.
func solidVP8L(w, h int, r, g, b uint8) []byte {
	var bw bitWriter
	bw.put(0x2f, 8)
	bw.put(uint32(w-1), 14)
	bw.put(uint32(h-1), 14)
	bw.put(0, 1) // alpha hint
	bw.put(0, 3) // version
	bw.put(0, 1) // no transform
	bw.put(0, 1) // no colour cache
	bw.put(0, 1) // no meta prefix codes
	for _, sym := range []uint8{g, r, b, 0xff, 0} {
		bw.simpleTree(sym)
	}
	return bw.bytes()
}
