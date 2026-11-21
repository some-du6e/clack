// Package mediatest is the video fixtures and what a player must show for
// them, shared by internal/media's tests, the backends' (mf, avf) and
// fakeslack, which serves them as F0FILESVID01.
//
// The fixtures are synthetic (testdata/gen.sh): H.264 Constrained Baseline
// 320×180 at 30 fps tagged BT.709 limited range, AAC-LC at 48 kHz, 3.000 s
// with a keyframe every 0.5 s; 0x404040 for the first second, 0xc0c0c0
// for the second, red for the third.
package mediatest

import (
	"bytes"
	"cmp"
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/media"
)

var (
	// GreyGreyRed is the fixture, its moov first (faststart).
	//go:embed testdata/grey-grey-red-3s.mp4
	GreyGreyRed []byte
	// MoovAtEnd is the same streams with the moov after the mdat.
	//go:embed testdata/moov-at-end-3s.mp4
	MoovAtEnd []byte
	// Silent is the fixture with no audio track.
	//go:embed testdata/grey-grey-red-3s-silent.mp4
	Silent []byte
)

// What the fixtures are.
const (
	Duration      = 3 * time.Second
	Width, Height = 320, 180
	FPS           = 30
	// MoovAtEndMoov is where MoovAtEnd's moov box starts.
	MoovAtEndMoov = 20981
	// SampleEntry is the video track's sample entry, Brand ftyp's.
	SampleEntry = "avc1"
	Brand       = "isom"
)

// MoovAtEndPadded is MoovAtEnd with pad bytes more before its mdat: a
// free box of pad bytes after the ftyp (and the fixture's own free box),
// every chunk offset moved with it. Its moov starts at MoovAtEndMoov+pad,
// past a demuxer's first reads, so a player has to seek there.
func MoovAtEndPadded(pad int) []byte {
	const head = 40 // ftyp (32 bytes), free (8): the mdat is next
	if pad < 8 {
		panic("mediatest: a box is 8 bytes at least")
	}
	out := make([]byte, 0, len(MoovAtEnd)+pad)
	out = append(out, MoovAtEnd[:head]...)
	free := make([]byte, pad)
	binary.BigEndian.PutUint32(free, uint32(pad))
	copy(free[4:], "free")
	out = append(out, free...)
	out = append(out, MoovAtEnd[head:]...)
	if n := shiftChunks(out[MoovAtEndMoov+pad:], uint32(pad)); n != 2 {
		panic(fmt.Sprintf("mediatest: %d chunk offset tables moved, want the two tracks'", n))
	}
	return out
}

// shiftChunks adds d to every chunk offset (stco) in the boxes of b, and
// says how many tables it moved. The fixtures have no co64.
func shiftChunks(b []byte, d uint32) int {
	n := 0
	for len(b) >= 8 {
		size := int(binary.BigEndian.Uint32(b))
		if size < 8 || size > len(b) {
			return n
		}
		body := b[8:size]
		switch string(b[4:8]) {
		case "moov", "trak", "mdia", "minf", "stbl":
			n += shiftChunks(body, d)
		case "stco":
			entries := int(binary.BigEndian.Uint32(body[4:]))
			for i := 0; i < entries; i++ {
				e := body[8+4*i:]
				binary.BigEndian.PutUint32(e, binary.BigEndian.Uint32(e)+d)
			}
			n++
		}
		b = b[size:]
	}
	return n
}

// Matches is whether c, a frame's centre pixel at t as read back after Gio
// drew it, is the fixture's colour there: grey 64 and 192 within 4 (a
// range mix-up reads 64 as about 56, a missing sRGB view as about 137),
// and for red R at least 200 with G and B at most 70 (a BGRA/RGBA swap
// reads blue; ffmpeg decodes the frame as 255,23,0, the generator's RGB
// having been tagged BT.709). Times within 100 ms of a change match
// either side.
func Matches(t time.Duration, c color.RGBA) bool {
	for _, d := range []time.Duration{0, -100 * time.Millisecond, 100 * time.Millisecond} {
		if matchesAt(t+d, c) {
			return true
		}
	}
	return false
}

func matchesAt(t time.Duration, c color.RGBA) bool {
	grey := func(v uint8) bool {
		near := func(x uint8) bool { return int(x) >= int(v)-4 && int(x) <= int(v)+4 }
		return near(c.R) && near(c.G) && near(c.B)
	}
	switch {
	case t < 0:
		return false
	case t < time.Second:
		return grey(64)
	case t < 2*time.Second:
		return grey(192)
	case t <= Duration+100*time.Millisecond:
		return c.R >= 200 && c.G <= 70 && c.B <= 70
	}
	return false
}

// Want describes the colour at t, for a test's message.
func Want(t time.Duration) string {
	switch {
	case t < time.Second:
		return "grey 64±4"
	case t < 2*time.Second:
		return "grey 192±4"
	}
	return "red (R≥200, G,B≤70)"
}

// Opener serves a file from memory as a file server does: a range from
// the offset asked for (a 206), or with Whole the whole file from byte 0
// whatever was asked (a 200). It records each request's offset, the bytes
// its bodies gave (each body's span of the file too) and the most bodies
// open at once, and its bodies stop with the request's context.
type Opener struct {
	Data  []byte
	Whole bool
	// Fail, when set, is every Open's error, answered with FailStatus.
	Fail       error
	FailStatus int
	// Delay is waited before each Read of a body (a slow network).
	Delay time.Duration
	// Cut, when set, ends every body cleanly at that byte of the file (a
	// truncated object, a proxy's cap).
	Cut int64
	// ETag is the answers' validator.
	ETag string
	// UnknownTotal answers ranges with "bytes N-M/*", and a range past the
	// end with a 416 naming the size.
	UnknownTotal bool

	mu      sync.Mutex
	offsets []int64
	spans   [][2]int64
	bytes   int64
	open    int
	maxOpen int
}

// OpenerStats are an Opener's counts.
type OpenerStats struct {
	// Offsets are the requests' offsets, in order.
	Offsets []int64
	// Spans are the bytes each body gave, [first, end) in the file, in
	// the order they opened.
	Spans [][2]int64
	// Bytes are what the bodies gave, MaxOpen the most bodies open at
	// once, Open those open now.
	Bytes         int64
	MaxOpen, Open int
}

// Again is how many of the bytes the bodies gave were given before: Bytes
// less the bytes of the file the spans cover.
func (s OpenerStats) Again() int64 {
	sp := slices.Clone(s.Spans)
	slices.SortFunc(sp, func(a, b [2]int64) int { return cmp.Compare(a[0], b[0]) })
	var covered, end int64
	for _, x := range sp {
		if x[1] > end {
			covered += x[1] - max(x[0], end)
			end = x[1]
		}
	}
	return s.Bytes - covered
}

// Open implements media.Source's Open.
func (o *Opener) Open(ctx context.Context, off int64) (io.ReadCloser, media.Info, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.offsets = append(o.offsets, off)
	if o.Fail != nil {
		return nil, media.Info{Status: o.FailStatus, Total: -1}, o.Fail
	}
	total := int64(len(o.Data))
	info := media.Info{Status: http.StatusPartialContent, Total: total, Seekable: true, ETag: o.ETag}
	if o.UnknownTotal {
		info.Total = -1
	}
	if o.Whole {
		info, off = media.Info{Status: http.StatusOK, Total: total, ETag: o.ETag}, 0
	} else if off >= total {
		return nil, media.Info{Status: http.StatusRequestedRangeNotSatisfiable, Total: total}, errors.New("mediatest: range not satisfiable")
	}
	end := total
	if o.Cut > 0 && o.Cut < end {
		end = max(o.Cut, off)
	}
	o.open++
	o.maxOpen = max(o.maxOpen, o.open)
	o.spans = append(o.spans, [2]int64{off, off})
	return &body{o: o, ctx: ctx, data: o.Data[off:end], span: len(o.spans) - 1}, info, nil
}

// Change runs f with o locked: a test changing the server between GETs.
func (o *Opener) Change(f func(o *Opener)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	f(o)
}

// RetryAfter is an error whose answer said to come back after D.
type RetryAfter struct{ D time.Duration }

func (e RetryAfter) Error() string                     { return fmt.Sprintf("mediatest: come back after %v", e.D) }
func (e RetryAfter) RetryAfterDuration() time.Duration { return e.D }

// Stats are the counts so far.
func (o *Opener) Stats() OpenerStats {
	o.mu.Lock()
	defer o.mu.Unlock()
	return OpenerStats{Offsets: append([]int64(nil), o.offsets...), Spans: slices.Clone(o.spans), Bytes: o.bytes, MaxOpen: o.maxOpen, Open: o.open}
}

// Source is a media.Source of data through a new Opener.
func Source(id string, data []byte, whole bool) (media.Source, *Opener) {
	o := &Opener{Data: data, Whole: whole}
	return media.Source{FileID: id, Mime: "video/mp4", Ext: "mp4", Open: o.Open}, o
}

type body struct {
	o      *Opener
	ctx    context.Context
	data   []byte
	span   int // its index in o.spans
	closed bool
}

func (b *body) Read(p []byte) (int, error) {
	b.o.mu.Lock()
	delay := b.o.Delay
	b.o.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-b.ctx.Done():
		}
	}
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if len(b.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.data[:min(len(b.data), 32<<10)])
	b.data = b.data[n:]
	b.o.mu.Lock()
	b.o.bytes += int64(n)
	b.o.spans[b.span][1] += int64(n)
	b.o.mu.Unlock()
	return n, nil
}

func (b *body) Close() error {
	b.o.mu.Lock()
	defer b.o.mu.Unlock()
	if !b.closed {
		b.closed = true
		b.o.open--
	}
	return nil
}

// Handler serves data at every path as files.slack.com would: ranges
// through http.ServeContent (206s), or with whole the whole file to every
// request (200s, Range ignored).
func Handler(data []byte, whole bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		if whole {
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.Write(data)
			return
		}
		http.ServeContent(w, r, "video.mp4", time.Time{}, bytes.NewReader(data))
	})
}
