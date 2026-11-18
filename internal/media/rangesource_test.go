package media_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
)

const mb = 1 << 20

// bigFile is n bytes, distinct at every offset.
func bigFile(n int) []byte {
	b := make([]byte, n)
	for i := 0; i < n; i += 8 {
		binary.LittleEndian.PutUint64(b[i:], uint64(i)*0x9e3779b97f4a7c15)
	}
	return b
}

// settled waits until o's bodies stop bringing bytes, and returns their
// count.
func settled(t *testing.T, o *mediatest.Opener) int64 {
	t.Helper()
	last, still := int64(-1), 0
	for i := 0; i < 400; i++ {
		n := o.Stats().Bytes
		if n == last {
			if still++; still >= 10 {
				return n
			}
		} else {
			still = 0
		}
		last = n
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the source kept reading: %d bytes", last)
	return 0
}

func readAt(t *testing.T, s *media.RangeSource, off int64, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := s.ReadAt(b, off); err != nil {
		t.Fatalf("ReadAt(%d, %d): %v", off, n, err)
	}
	return b
}

func quiet(string, ...any) {}

// logLines is a log func and what it logged.
func logLines() (func(string, ...any), func() []string) {
	var mu sync.Mutex
	var lines []string
	return func(f string, a ...any) {
			mu.Lock()
			lines = append(lines, fmt.Sprintf(f, a...))
			mu.Unlock()
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), lines...)
		}
}

// The fixture read through the source is the fixture: one GET, the
// answer's size, the first answer's status.
func TestRangeSourceReadsAll(t *testing.T) {
	src, o := mediatest.Source("FRS1", mediatest.GreyGreyRed, false)
	s := media.NewRangeSource(src, media.RangeOptions{Log: quiet})
	defer s.Close()
	info, err := s.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info != (media.Info{Status: 206, Total: int64(len(mediatest.GreyGreyRed)), Seekable: true}) {
		t.Errorf("info %+v", info)
	}
	if got := readAt(t, s, 0, len(mediatest.GreyGreyRed)); !bytes.Equal(got, mediatest.GreyGreyRed) {
		t.Error("the bytes read aren't the file's")
	}
	if _, err := s.ReadSome(context.Background(), make([]byte, 10), info.Total); !errors.Is(err, io.EOF) {
		t.Errorf("a read at the end: %v, want io.EOF", err)
	}
	if st := o.Stats(); len(st.Offsets) != 1 || st.Bytes != info.Total {
		t.Errorf("%d GETs (%v) bringing %d bytes, want one", len(st.Offsets), st.Offsets, st.Bytes)
	}
}

// The GET reads at most ReadAhead past the furthest byte read, and on as
// reads move on: a paused player's source stops there.
func TestRangeSourceReadAhead(t *testing.T) {
	data := bigFile(40 * mb)
	src, o := mediatest.Source("FRS2", data, false)
	s := media.NewRangeSource(src, media.RangeOptions{Log: quiet})
	defer s.Close()
	if !bytes.Equal(readAt(t, s, 0, mb), data[:mb]) {
		t.Fatal("bytes differ")
	}
	n := settled(t, o)
	if n < mb+media.ReadAhead || n > mb+media.ReadAhead+64<<10 {
		t.Errorf("after reading 1 MB the GET brought %d bytes, want 9 MB (1 MB and 8 MB ahead)", n)
	}
	if !bytes.Equal(readAt(t, s, 5*mb, 4096), data[5*mb:5*mb+4096]) {
		t.Fatal("bytes differ")
	}
	if got := len(o.Stats().Offsets); got != 1 {
		t.Errorf("a read of what was read ahead made %d GETs, want the one", got)
	}
	// a read just past what's come waits for the GET, which reads on
	off := int64(n) + 100<<10
	if !bytes.Equal(readAt(t, s, off, 4096), data[off:off+4096]) {
		t.Fatal("bytes differ")
	}
	n2 := settled(t, o)
	if want := off + 4096 + media.ReadAhead; n2 < want || n2 > want+64<<10 {
		t.Errorf("after a read at %d the GET brought %d bytes, want %d", off, n2, want)
	}
	if st := o.Stats(); len(st.Offsets) != 1 {
		t.Errorf("GETs at %v, want one", st.Offsets)
	}
}

// The cache holds at most CacheMax: reading a 40 MB file through keeps
// 16 MB, and every byte comes once.
func TestRangeSourceCacheCap(t *testing.T) {
	data := bigFile(40 * mb)
	src, o := mediatest.Source("FRS3", data, false)
	s := media.NewRangeSource(src, media.RangeOptions{Log: quiet})
	defer s.Close()
	for off := 0; off < len(data); off += mb {
		if !bytes.Equal(readAt(t, s, int64(off), mb), data[off:off+mb]) {
			t.Fatalf("bytes at %d differ", off)
		}
		if c := s.Stats().Cached; c > media.CacheMax {
			t.Fatalf("at %d MB the cache holds %d bytes, over %d", off/mb, c, media.CacheMax)
		}
	}
	settled(t, o)
	if st := o.Stats(); len(st.Offsets) != 1 || st.Bytes != int64(len(data)) {
		t.Errorf("GETs at %v brought %d bytes, want one bringing the file once", st.Offsets, st.Bytes)
	}
}

// At most one GET is open at once, however reads jump about; a read far
// from what's coming starts the GET again at its block.
func TestRangeSourceOneGET(t *testing.T) {
	data := bigFile(40 * mb)
	src, o := mediatest.Source("FRS4", data, false)
	s := media.NewRangeSource(src, media.RangeOptions{Log: quiet})
	defer s.Close()
	r := rand.New(rand.NewSource(1))
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ { // readers at once, as AVFoundation's requests
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			r := rand.New(rand.NewSource(seed))
			for j := 0; j < 10; j++ {
				off := r.Int63n(int64(len(data)) - 64<<10)
				b := make([]byte, 64<<10)
				if _, err := s.ReadAt(b, off); err != nil {
					t.Error(err)
					return
				}
				if !bytes.Equal(b, data[off:off+64<<10]) {
					t.Errorf("bytes at %d differ", off)
					return
				}
			}
		}(r.Int63())
	}
	wg.Wait()
	st, rs := o.Stats(), s.Stats()
	if st.MaxOpen > 1 || rs.MaxInFlight > 1 {
		t.Errorf("%d bodies open at once (the source counted %d), want 1", st.MaxOpen, rs.MaxInFlight)
	}
	for _, off := range st.Offsets {
		if off%media.BlockSize != 0 {
			t.Errorf("a GET at %d, not at a block", off)
		}
	}
	t.Logf("40 random reads by 4 readers: %d GETs, %d restarts, %d bytes", len(st.Offsets), rs.Restarts, st.Bytes)
}

// A seek outside what was read starts the GET again there; back inside
// it, nothing is fetched again.
func TestRangeSourceRestartOnSeek(t *testing.T) {
	data := bigFile(40 * mb)
	src, o := mediatest.Source("FRS5", data, false)
	s := media.NewRangeSource(src, media.RangeOptions{Log: quiet})
	defer s.Close()
	readAt(t, s, 0, 64<<10)
	readAt(t, s, 30*mb+12345, 64<<10)
	readAt(t, s, 100, 64<<10) // still cached
	settled(t, o)
	want := []int64{0, 30 * mb}
	if got := o.Stats().Offsets; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("GETs at %v, want %v", got, want)
	}
	readAt(t, s, 20*mb, 64<<10)
	if got := o.Stats().Offsets; len(got) != 3 || got[2] != 20*mb {
		t.Errorf("GETs at %v, want a third at 20 MB", got)
	}
	if st := s.Stats(); st.Restarts != 2 || st.MaxInFlight != 1 {
		t.Errorf("%+v, want 2 restarts, one GET at a time", st)
	}
}

// moovAtEnd is a file of about n bytes: ftyp, an mdat, then the fixture's
// moov (an original not made for progressive playback).
func moovAtEnd(n int) []byte {
	f := mediatest.MoovAtEnd
	head := f[:40] // ftyp, free
	moov := f[mediatest.MoovAtEndMoov:]
	mdat := make([]byte, n)
	binary.BigEndian.PutUint32(mdat, uint32(n))
	copy(mdat[4:], "mdat")
	return append(append(append([]byte(nil), head...), mdat...), moov...)
}

// A file whose moov is at its end: the demuxer reads the start, jumps to
// the moov once and back; the log line then names the video's sample
// entry, once for the file.
func TestRangeSourceMoovAtEnd(t *testing.T) {
	data := moovAtEnd(20 * mb)
	moovAt := int64(len(data) - (len(mediatest.MoovAtEnd) - mediatest.MoovAtEndMoov))
	logf, lines := logLines()
	src, o := mediatest.Source("FRS6", data, false)
	s := media.NewRangeSource(src, media.RangeOptions{Log: logf})
	readAt(t, s, 0, 64<<10)
	if p := s.Probe(); p.Brand != "isom" || p.Entry != "" || p.MoovAt != -1 || p.NextAt != moovAt {
		t.Errorf("after the start: %+v, want the brand, and the box after the mdat next", p)
	}
	readAt(t, s, moovAt, int(int64(len(data))-moovAt))
	readAt(t, s, 12*mb, 64<<10) // the mdat, past the first GET's read-ahead
	s.Close()
	s.Wait()
	st := o.Stats()
	if len(st.Offsets) != 3 || st.Offsets[0] != 0 || st.Offsets[1] != moovAt/media.BlockSize*media.BlockSize || st.Offsets[2] != 12*mb {
		t.Errorf("GETs at %v, want the start, the moov's block, the mdat", st.Offsets)
	}
	if p := s.Probe(); p.Entry != mediatest.SampleEntry || p.Width != 320 || p.Height != 180 || p.Duration != 3*time.Second {
		t.Errorf("probe %+v", p)
	}
	if l := lines(); len(l) != 1 || !strings.Contains(l[0], `FRS6: http 206, `+fmt.Sprint(len(data))+` bytes, seekable true, ftyp "isom", video "avc1", moov at the end`) {
		t.Errorf("logged %q", l)
	}
	if st.Open != 0 {
		t.Errorf("%d bodies left open after Close", st.Open)
	}
}

// A server ignoring ranges (a 200): the file is read in order from byte
// 0, a read ahead waits for it, a read behind what's cached starts over
// from 0; the log says so.
func TestRangeSource200(t *testing.T) {
	data := bigFile(40 * mb)
	logf, lines := logLines()
	src, o := mediatest.Source("FRS7", data, true)
	s := media.NewRangeSource(src, media.RangeOptions{Log: logf})
	defer s.Close()
	if !bytes.Equal(readAt(t, s, 10*mb, 4096), data[10*mb:10*mb+4096]) {
		t.Fatal("bytes differ")
	}
	info, _ := s.Info(context.Background())
	if info.Seekable || info.Status != 200 || info.Total != int64(len(data)) {
		t.Errorf("info %+v, want a 200, not seekable", info)
	}
	for off := 10 * mb; off < len(data); off += mb {
		readAt(t, s, int64(off), mb)
	}
	if !bytes.Equal(readAt(t, s, 0, 4096), data[:4096]) { // evicted: from 0 again
		t.Fatal("bytes differ")
	}
	settled(t, o)
	// the first asked for its read's block, and got the file from 0
	if st := o.Stats(); len(st.Offsets) != 2 || st.Offsets[0] != 10*mb || st.Offsets[1] != 0 || st.MaxOpen != 1 {
		t.Errorf("GETs at %v (%d open at once), want one from 10 MB answered from 0, then from 0", st.Offsets, st.MaxOpen)
	}
	s.Close()
	if l := lines(); len(l) != 1 || !strings.Contains(l[0], "FRS7: http 200, 41943040 bytes, seekable false") {
		t.Errorf("logged %q", l)
	}
}

// Close ends a read waiting for its bytes at once, and the GET's body.
func TestRangeSourceCloseCancels(t *testing.T) {
	data := bigFile(40 * mb)
	src, o := mediatest.Source("FRS8", data, true)
	o.Delay = 20 * time.Millisecond // 32 KB a read: a slow network
	s := media.NewRangeSource(src, media.RangeOptions{Log: quiet})
	done := make(chan error, 1)
	go func() {
		_, err := s.ReadAt(make([]byte, 10), 30*mb)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	s.Close()
	select {
	case err := <-done:
		if !errors.Is(err, media.ErrClosed) {
			t.Errorf("the waiting read: %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close didn't end the waiting read")
	}
	s.Wait()
	if st := o.Stats(); st.Open != 0 || st.MaxOpen != 1 {
		t.Errorf("%d bodies open after Close (%d at most)", st.Open, st.MaxOpen)
	}
	if _, err := s.ReadSome(context.Background(), make([]byte, 1), 0); !errors.Is(err, media.ErrClosed) {
		t.Errorf("a read after Close: %v", err)
	}
	// a read's own context ends it too
	s2 := media.NewRangeSource(src, media.RangeOptions{Log: quiet})
	defer s2.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := s2.ReadSome(ctx, make([]byte, 1), 35*mb); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a read whose context ended: %v", err)
	}
}

// A GET that fails to open fails the reads; reads right after don't ask
// again.
func TestRangeSourceOpenError(t *testing.T) {
	src, o := mediatest.Source("FRS9", mediatest.GreyGreyRed, false)
	o.Fail = errors.New("slack: not a file URL")
	s := media.NewRangeSource(src, media.RangeOptions{Log: quiet})
	defer s.Close()
	for i := 0; i < 3; i++ {
		if _, err := s.ReadSome(context.Background(), make([]byte, 10), 0); err == nil || err.Error() != o.Fail.Error() {
			t.Errorf("read %d: %v, want the GET's error", i, err)
		}
	}
	if n := len(o.Stats().Offsets); n != 1 {
		t.Errorf("%d GETs, want 1", n)
	}
}

// A file played twice is logged once.
func TestRangeSourceLogOnce(t *testing.T) {
	logf, lines := logLines()
	for i := 0; i < 2; i++ {
		src, _ := mediatest.Source("FRS10", mediatest.GreyGreyRed, false)
		s := media.NewRangeSource(src, media.RangeOptions{Log: logf})
		readAt(t, s, 0, len(mediatest.GreyGreyRed))
		s.Close()
		s.Wait()
	}
	if l := lines(); len(l) != 1 || !strings.Contains(l[0], `media: FRS10: http 206, 24943 bytes, seekable true, ftyp "isom", video "avc1"`) || strings.Contains(l[0], "moov at the end") {
		t.Errorf("logged %q, want one line", l)
	}
}

// The probe reads each fixture's brand, sample entry, duration and size,
// and where its moov is.
func TestProbeFixtures(t *testing.T) {
	for name, tc := range map[string]struct {
		data   []byte
		moovAt int64
	}{
		"faststart":   {mediatest.GreyGreyRed, 32},
		"moov-at-end": {mediatest.MoovAtEnd, mediatest.MoovAtEndMoov},
		"silent":      {mediatest.Silent, 32},
	} {
		p := media.ProbeMP4(tc.data)
		if p != (media.MP4Info{Brand: "isom", Entry: "avc1", MoovAt: tc.moovAt, NextAt: -1, Duration: 3 * time.Second, Width: 320, Height: 180}) {
			t.Errorf("%s: %+v", name, p)
		}
	}
	if p := media.ProbeMP4([]byte("<html>sign in</html>")); p.Entry != "" || p.Brand != "" {
		t.Errorf("a page: %+v", p)
	}
}
