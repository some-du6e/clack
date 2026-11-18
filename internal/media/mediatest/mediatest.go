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
	"context"
	_ "embed"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net/http"
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
// its bodies gave and the most bodies open at once, and its bodies stop
// with the request's context.
type Opener struct {
	Data  []byte
	Whole bool
	// Fail, when set, is every Open's error.
	Fail error
	// Delay is waited before each Read of a body (a slow network).
	Delay time.Duration

	mu      sync.Mutex
	offsets []int64
	bytes   int64
	open    int
	maxOpen int
}

// OpenerStats are an Opener's counts.
type OpenerStats struct {
	// Offsets are the requests' offsets, in order.
	Offsets []int64
	// Bytes are what the bodies gave, MaxOpen the most bodies open at
	// once, Open those open now.
	Bytes         int64
	MaxOpen, Open int
}

// Open implements media.Source's Open.
func (o *Opener) Open(ctx context.Context, off int64) (io.ReadCloser, media.Info, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.offsets = append(o.offsets, off)
	if o.Fail != nil {
		return nil, media.Info{Total: -1}, o.Fail
	}
	total := int64(len(o.Data))
	info := media.Info{Status: http.StatusPartialContent, Total: total, Seekable: true}
	if o.Whole {
		info, off = media.Info{Status: http.StatusOK, Total: total}, 0
	} else if off >= total {
		return nil, media.Info{Status: http.StatusRequestedRangeNotSatisfiable, Total: total}, errors.New("mediatest: range not satisfiable")
	}
	o.open++
	o.maxOpen = max(o.maxOpen, o.open)
	return &body{o: o, ctx: ctx, data: o.Data[off:]}, info, nil
}

// Stats are the counts so far.
func (o *Opener) Stats() OpenerStats {
	o.mu.Lock()
	defer o.mu.Unlock()
	return OpenerStats{Offsets: append([]int64(nil), o.offsets...), Bytes: o.bytes, MaxOpen: o.maxOpen, Open: o.open}
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
	closed bool
}

func (b *body) Read(p []byte) (int, error) {
	if b.o.Delay > 0 {
		select {
		case <-time.After(b.o.Delay):
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
