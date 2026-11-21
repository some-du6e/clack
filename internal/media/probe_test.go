package media_test

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
)

// box is an ISO BMFF box: a 32-bit size, or 1 and a 64-bit size (large),
// its type and body.
func box(typ string, body []byte) []byte {
	b := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(b, uint32(8+len(body)))
	copy(b[4:], typ)
	return append(b, body...)
}

// liar is a box header claiming size bytes (a 64-bit size), with no body.
func liar(typ string, size uint64) []byte {
	b := make([]byte, 16)
	binary.BigEndian.PutUint32(b, 1)
	copy(b[4:], typ)
	binary.BigEndian.PutUint64(b[8:], size)
	return b
}

func cat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

// Box sizes a file can't have (near 2^63, or 2^64-1, which is negative as
// an int64) at the top level, in the moov, in a trak, and past the first
// 64 KB: the probe gives up on them instead of wrapping its offsets (an
// upload crafted so crashed whoever played it).
func TestProbeLyingSizes(t *testing.T) {
	ftyp := mediatest.GreyGreyRed[:32]
	mvhd := box("mvhd", make([]byte, 100))
	pad := box("free", make([]byte, 70<<10)) // pushes what follows past the head
	for name, data := range map[string][]byte{
		"top level":        cat(ftyp, liar("free", math.MaxInt64)),
		"top level, 2^64":  cat(ftyp, liar("free", math.MaxUint64)),
		"top, then moov":   cat(ftyp, liar("free", math.MaxInt64-40), box("moov", mvhd)),
		"in the moov":      cat(ftyp, box("moov", cat(mvhd, liar("trak", math.MaxInt64)))),
		"in the moov, 2nd": cat(ftyp, box("moov", cat(mvhd, box("udta", nil), liar("free", math.MaxInt64-7)))),
		"in a trak":        cat(ftyp, box("moov", cat(mvhd, box("trak", cat(box("tkhd", make([]byte, 84)), liar("mdia", math.MaxInt64)))))),
		"in a trak, 32bit": cat(ftyp, box("moov", cat(mvhd, box("trak", cat(box("tkhd", make([]byte, 84)), []byte{0xff, 0xff, 0xff, 0xff, 'm', 'd', 'i', 'a'}))))),
		"past the head":    cat(ftyp, pad, liar("free", math.MaxInt64)),
		"past the head, 2": cat(ftyp, pad, box("free", nil), liar("mdat", math.MaxInt64-3)),
		"huge duration":    cat(ftyp, box("moov", box("mvhd", append([]byte{1, 0, 0, 0}, bytes.Repeat([]byte{0xff}, 28)...)))),
	} {
		t.Run(name, func(t *testing.T) {
			p := media.ProbeMP4(data) // a panic fails the test
			if p.Entry == "avc1" {
				t.Errorf("%+v from a file with no video track", p)
			}
		})
	}
}

// A moov longer than the probe keeps, its video track after what's kept,
// is a video unknown ("?"), not a file with no video ("none").
func TestProbeLongMoov(t *testing.T) {
	ftyp := mediatest.GreyGreyRed[:32]
	audio := box("trak", cat(box("tkhd", make([]byte, 84)), box("mdia", box("hdlr", append(make([]byte, 8), "soun"...))), box("free", make([]byte, 80<<10))))
	data := cat(ftyp, box("moov", cat(box("mvhd", make([]byte, 100)), audio, box("trak", nil))))
	if p := media.ProbeMP4(data); p.Entry != "?" {
		t.Errorf("entry %q, want ? (the video track may be past the 64 KB read)", p.Entry)
	}
}

// FuzzProbeMP4: whatever the bytes, the probe neither panics nor loops.
func FuzzProbeMP4(f *testing.F) {
	for _, seed := range [][]byte{
		mediatest.GreyGreyRed, mediatest.MoovAtEnd, mediatest.Silent,
		cat(mediatest.GreyGreyRed[:32], liar("free", math.MaxInt64)),
		cat(mediatest.GreyGreyRed[:32], box("moov", liar("trak", math.MaxInt64))),
		[]byte("<html>sign in</html>"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		media.ProbeMP4(b)
	})
}
