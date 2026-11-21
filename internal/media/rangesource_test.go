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
	for i := 0; i+8 <= n; i += 8 {
		binary.LittleEndian.PutUint64(b[i:], uint64(i)*0x9e3779b97f4a7c15)
	}
	return b
}

// settled waits until o's bodies stop bringing bytes, and returns their
// count.
func settled(t *testing.T, o *mediatest.Opener) int64 {
	t.Helper()
	last, still := int64(-1), 0
	for i := 0; i < 600; i++ {
		n := o.Stats().Bytes
		if n == last {
			if still++; still >= 20 {
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

// source is a RangeSource of src logging into logf, with a set of logged
// files of its test's own (a test run again in the same process logs
// again).
func source(src media.Source, logf func(string, ...any), logged *sync.Map) *media.RangeSource {
	if logged == nil {
		logged = new(sync.Map)
	}
	return media.NewRangeSource(src, media.RangeOptions{Log: logf, Logged: logged})
}

// The fixture read through the source is the fixture: one GET, the
// answer's size, the first answer's status.
func TestRangeSourceReadsAll(t *testing.T) {
	src, o := mediatest.Source("FRS1", mediatest.GreyGreyRed, false)
	s := source(src, quiet, nil)
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
	s := source(src, quiet, nil)
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
	s := source(src, quiet, nil)
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

// Scattered seeks on a slow link leave blocks partly filled: each counts
// whole (it holds its 256 KB), so the cache stays within CacheMax.
func TestRangeSourceCacheCountsBlocks(t *testing.T) {
	data := bigFile(100 * mb)
	src, o := mediatest.Source("FRS4", data, false)
	o.Delay = time.Millisecond
	s := source(src, quiet, nil)
	defer s.Close()
	r := rand.New(rand.NewSource(2))
	for i := 0; i < 120; i++ {
		off := r.Int63n(int64(len(data)) - 4096)
		if !bytes.Equal(readAt(t, s, off, 4096), data[off:off+4096]) {
			t.Fatalf("bytes at %d differ", off)
		}
		if c := s.Stats().Cached; c > media.CacheMax+media.BlockSize*2 {
			t.Fatalf("after %d seeks the blocks hold %d bytes, over %d", i, c, media.CacheMax)
		}
	}
}

// Two readers far apart, each reading in order (a demuxer's video and its
// audio, AVFoundation's request from 0 and its request for the moov),
// each keep a GET: the GETs don't change hands with every read.
func TestRangeSourceTwoReaders(t *testing.T) {
	data := bigFile(40 * mb)
	src, o := mediatest.Source("FRS5", data, false)
	o.Delay = 100 * time.Microsecond
	s := source(src, quiet, nil)
	defer s.Close()
	var wg sync.WaitGroup
	for _, base := range []int64{0, 20 * mb} {
		wg.Add(1)
		go func(base int64) {
			defer wg.Done()
			b := make([]byte, 64<<10)
			for off := base; off < base+6*mb; off += int64(len(b)) {
				if _, err := s.ReadAt(b, off); err != nil {
					t.Error(err)
					return
				}
				if !bytes.Equal(b, data[off:off+int64(len(b))]) {
					t.Errorf("bytes at %d differ", off)
					return
				}
			}
		}(base)
	}
	wg.Wait()
	st, rs := o.Stats(), s.Stats()
	t.Logf("two readers of 6 MB each, 20 MB apart: %d GETs at %v, %d restarts, %d bytes", len(st.Offsets), st.Offsets, rs.Restarts, st.Bytes)
	if len(st.Offsets) > 2 || rs.Restarts != 0 || st.MaxOpen > 2 {
		t.Errorf("%d GETs, %d restarts, %d open at once: want one GET each", len(st.Offsets), rs.Restarts, st.MaxOpen)
	}
}

// Three readers far apart share two GETs: one changes hands at most once
// a tenure, never at each read.
func TestRangeSourceThreeReaders(t *testing.T) {
	data := bigFile(60 * mb)
	src, o := mediatest.Source("FRS6", data, false)
	o.Delay = 200 * time.Microsecond
	s := source(src, quiet, nil)
	defer s.Close()
	var wg sync.WaitGroup
	start := time.Now()
	for _, base := range []int64{0, 20 * mb, 40 * mb} {
		wg.Add(1)
		go func(base int64) {
			defer wg.Done()
			b := make([]byte, 64<<10)
			for off := base; off < base+3*mb; off += int64(len(b)) {
				if _, err := s.ReadAt(b, off); err != nil {
					t.Error(err)
					return
				}
			}
		}(base)
	}
	wg.Wait()
	took := time.Since(start)
	st, rs := o.Stats(), s.Stats()
	t.Logf("three readers of 3 MB, 20 MB apart, in %v: %d GETs, %d restarts, %d bytes, %d open at most", took, len(st.Offsets), rs.Restarts, st.Bytes, st.MaxOpen)
	// a restart needs a GET to have brought 1 MB or had 300 ms: 9 MB read
	// allow 9 by bytes, and the time allows took/300ms
	if bound := int64(9+took/(300*time.Millisecond)) + 3; int64(len(st.Offsets)) > bound || st.MaxOpen > 2 {
		t.Errorf("%d GETs (%d open at once), want at most %d", len(st.Offsets), st.MaxOpen, bound)
	}
}

// A seek outside what was read gets a GET of its own; back inside it,
// nothing is fetched again; a third place takes the least recently read
// GET's place.
func TestRangeSourceRestartOnSeek(t *testing.T) {
	data := bigFile(40 * mb)
	src, o := mediatest.Source("FRS7", data, false)
	s := source(src, quiet, nil)
	defer s.Close()
	readAt(t, s, 0, 64<<10)
	readAt(t, s, 30*mb+12345, 64<<10)
	readAt(t, s, 100, 64<<10) // still cached
	settled(t, o)
	want := []int64{0, 30 * mb}
	if got := o.Stats().Offsets; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("GETs at %v, want %v", got, want)
	}
	time.Sleep(350 * time.Millisecond) // the GETs had their tenure
	readAt(t, s, 20*mb, 64<<10)
	if got := o.Stats().Offsets; len(got) != 3 || got[2] != 20*mb {
		t.Errorf("GETs at %v, want a third at 20 MB", got)
	}
	if st := s.Stats(); st.Restarts != 1 || st.MaxInFlight != 2 {
		t.Errorf("%+v, want 1 restart, two GETs at once at most", st)
	}
}

// A GET started for a read in a block partly cached starts at the
// block's first missing byte, not at its start.
func TestRangeSourceStartsAtMissingByte(t *testing.T) {
	data := bigFile(4 * mb)
	src, o := mediatest.Source("FRS8", data, false)
	o.Cut = 100 << 10 // the first body ends 100 KB into the first block
	s := source(src, quiet, nil)
	defer s.Close()
	readAt(t, s, 0, 50<<10)
	o.Change(func(o *mediatest.Opener) { o.Cut = 0 })
	readAt(t, s, 120<<10, 50<<10)
	if got := o.Stats().Offsets; len(got) != 2 || got[1] != 100<<10 {
		t.Errorf("GETs at %v, want the second at the first missing byte, 102400", got)
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
	src, o := mediatest.Source("FRS9", data, false)
	s := source(src, logf, nil)
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
	if l := lines(); len(l) != 1 || !strings.Contains(l[0], `FRS9: http 206, `+fmt.Sprint(len(data))+` bytes, seekable true, ftyp "isom", video "avc1", moov at the end`) {
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
	src, o := mediatest.Source("FRS10", data, true)
	s := source(src, logf, nil)
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
	time.Sleep(350 * time.Millisecond)                    // its tenure
	if !bytes.Equal(readAt(t, s, 0, 4096), data[:4096]) { // evicted: from 0 again
		t.Fatal("bytes differ")
	}
	settled(t, o)
	// the first asked for its read's block, and got the file from 0
	if st := o.Stats(); len(st.Offsets) != 2 || st.Offsets[0] != 10*mb || st.Offsets[1] != 0 || st.MaxOpen != 1 {
		t.Errorf("GETs at %v (%d open at once), want one from 10 MB answered from 0, then from 0", st.Offsets, st.MaxOpen)
	}
	s.Close()
	if l := lines(); len(l) != 1 || !strings.Contains(l[0], "FRS10: http 200, 41943040 bytes, seekable false") {
		t.Errorf("logged %q", l)
	}
}

// Close ends a read waiting for its bytes at once, and the GET's body.
func TestRangeSourceCloseCancels(t *testing.T) {
	data := bigFile(40 * mb)
	src, o := mediatest.Source("FRS11", data, true)
	o.Delay = 20 * time.Millisecond // 32 KB a read: a slow network
	s := source(src, quiet, nil)
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
	s2 := source(src, quiet, nil)
	defer s2.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := s2.ReadSome(ctx, make([]byte, 1), 35*mb); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a read whose context ended: %v", err)
	}
}

// A GET that fails to open fails the reads; reads right after don't ask
// again; the failure is the file's log line, with the status and no URL.
func TestRangeSourceOpenError(t *testing.T) {
	logf, lines := logLines()
	src, o := mediatest.Source("FRS12", mediatest.GreyGreyRed, false)
	o.Fail, o.FailStatus = errors.New("slack: not a file URL (https://files.slack.com/files-pri/T1-F1/secret.mp4)"), 403
	s := source(src, logf, nil)
	defer s.Close()
	for i := 0; i < 3; i++ {
		if _, err := s.ReadSome(context.Background(), make([]byte, 10), 0); err == nil || err.Error() != o.Fail.Error() {
			t.Errorf("read %d: %v, want the GET's error", i, err)
		}
	}
	if n := len(o.Stats().Offsets); n != 1 {
		t.Errorf("%d GETs, want 1", n)
	}
	if l := lines(); len(l) != 1 || l[0] != "media: FRS12: http 403, the file wasn't served: the request failed" {
		t.Errorf("logged %q", l)
	}
}

// A server that cuts every body at the same byte: the read waiting past
// it fails after the GET that brought nothing new, not a GET a round
// trip; then the source holds off (1 s, then 2 s).
func TestRangeSourceShortBodies(t *testing.T) {
	data := bigFile(4 * mb)
	src, o := mediatest.Source("FRS13", data, false)
	o.Cut = 3 * mb
	s := source(src, quiet, nil)
	defer s.Close()
	readAt(t, s, 0, 64<<10)
	settled(t, o)
	t0 := time.Now()
	_, err := s.ReadAt(make([]byte, 4096), 3*mb+100<<10)
	if !errors.Is(err, media.ErrShort) || time.Since(t0) > time.Second {
		t.Errorf("the read past the cut: %v after %v, want ErrShort at once", err, time.Since(t0))
	}
	if n := len(o.Stats().Offsets); n != 2 {
		t.Errorf("%d GETs, want 2 (the one cut, the one bringing nothing)", n)
	}
	if _, err := s.ReadSome(context.Background(), make([]byte, 10), 3*mb+200<<10); !errors.Is(err, media.ErrShort) || len(o.Stats().Offsets) != 2 {
		t.Errorf("a read within the hold: %v, %d GETs; want the error, no GET", err, len(o.Stats().Offsets))
	}
	time.Sleep(1100 * time.Millisecond)
	s.ReadSome(context.Background(), make([]byte, 10), 3*mb+200<<10)
	time.Sleep(1200 * time.Millisecond) // within the doubled hold
	s.ReadSome(context.Background(), make([]byte, 10), 3*mb+200<<10)
	if n := len(o.Stats().Offsets); n != 3 {
		t.Errorf("%d GETs after 2.3 s, want 3 (the second failure holds 2 s)", n)
	}
}

// A server saying when to come back (Retry-After) is left alone that long.
func TestRangeSourceRetryAfter(t *testing.T) {
	src, o := mediatest.Source("FRS14", mediatest.GreyGreyRed, false)
	o.Fail = mediatest.RetryAfter{D: 3 * time.Second}
	s := source(src, quiet, nil)
	defer s.Close()
	s.ReadSome(context.Background(), make([]byte, 10), 0)
	time.Sleep(1500 * time.Millisecond)
	if _, err := s.ReadSome(context.Background(), make([]byte, 10), 0); err == nil || len(o.Stats().Offsets) != 1 {
		t.Errorf("1.5 s into a 3 s Retry-After: %v, %d GETs; want the error, no new GET", err, len(o.Stats().Offsets))
	}
}

// A later answer for another version of the file (a new validator), or
// the whole file after ranges, fails the source: its bytes aren't mixed in.
func TestRangeSourceChanged(t *testing.T) {
	for name, change := range map[string]func(o *mediatest.Opener){
		"etag":  func(o *mediatest.Opener) { o.ETag = `"v2"` },
		"200":   func(o *mediatest.Opener) { o.Whole = true },
		"total": func(o *mediatest.Opener) { o.Data = o.Data[:30*mb] },
	} {
		t.Run(name, func(t *testing.T) {
			data := bigFile(40 * mb)
			src, o := mediatest.Source("FRS15"+name, data, false)
			o.ETag = `"v1"`
			s := source(src, quiet, nil)
			defer s.Close()
			readAt(t, s, 0, 4096)
			o.Change(change)
			if _, err := s.ReadAt(make([]byte, 4096), 25*mb); !errors.Is(err, media.ErrChanged) {
				t.Errorf("a read after the file changed: %v, want ErrChanged", err)
			}
			if _, err := s.ReadAt(make([]byte, 10), 100); !errors.Is(err, media.ErrChanged) {
				t.Errorf("even of what's cached: %v, want ErrChanged", err)
			}
		})
	}
}

// A file the answers didn't size: a read past its end is the end (the
// 416 names the size), not an error.
func TestRangeSourceUnknownTotal(t *testing.T) {
	data := bigFile(mb)
	src, o := mediatest.Source("FRS16", data, false)
	o.UnknownTotal = true
	s := source(src, quiet, nil)
	defer s.Close()
	got := readAt(t, s, 0, mb)
	if !bytes.Equal(got, data) {
		t.Fatal("bytes differ")
	}
	if _, err := s.ReadSome(context.Background(), make([]byte, 10), mb); !errors.Is(err, io.EOF) {
		t.Errorf("a read at the end: %v, want io.EOF", err)
	}
	if _, err := s.ReadSome(context.Background(), make([]byte, 10), 2*mb); !errors.Is(err, io.EOF) {
		t.Errorf("a read past the end: %v, want io.EOF", err)
	}
	// the first read past the end: its GET's 416 says where the end is
	s2 := source(src, quiet, nil)
	defer s2.Close()
	if _, err := s2.ReadSome(context.Background(), make([]byte, 10), 2*mb); !errors.Is(err, io.EOF) {
		t.Errorf("a first read past the end: %v, want io.EOF (the 416's size)", err)
	}
	if n, err := s2.ReadSome(context.Background(), make([]byte, 10), 0); n == 0 || err != nil {
		t.Errorf("then a read at 0: %d, %v", n, err)
	}
}

// A file played twice is logged once.
func TestRangeSourceLogOnce(t *testing.T) {
	logf, lines := logLines()
	logged := new(sync.Map)
	for i := 0; i < 2; i++ {
		src, _ := mediatest.Source("FRS17", mediatest.GreyGreyRed, false)
		s := source(src, logf, logged)
		readAt(t, s, 0, len(mediatest.GreyGreyRed))
		s.Close()
		s.Wait()
	}
	if l := lines(); len(l) != 1 || !strings.Contains(l[0], `media: FRS17: http 206, 24943 bytes, seekable true, ftyp "isom", video "avc1"`) || strings.Contains(l[0], "moov at the end") {
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

// Two readers to the end of one file, the second starting past the
// first's GET (AVFoundation's 0-end and X-end requests): once the first
// GET reaches the bytes the second brought, it ends, and its reader takes
// them from the cache, so the file comes once between them. Round 4 step
// 5: each kept its GET to the end and the file came twice.
func TestRangeSourceOverlappingReadersShare(t *testing.T) {
	data := bigFile(6 * mb)
	src, o := mediatest.Source("FRSO", data, false)
	o.Delay = time.Millisecond // a network: the GETs take a while
	s := media.NewRangeSource(src, media.RangeOptions{Ahead: 512 << 10, Near: 128 << 10, Log: quiet, Logged: new(sync.Map)})
	defer s.Close()
	read := func(off int64, step time.Duration, done chan<- error) {
		buf := make([]byte, 64<<10)
		for cur := off; cur < int64(len(data)); {
			n, err := s.ReadSome(context.Background(), buf, cur)
			if n > 0 && !bytes.Equal(buf[:n], data[cur:cur+int64(n)]) {
				done <- fmt.Errorf("the bytes at %d differ", cur)
				return
			}
			cur += int64(n)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				done <- err
				return
			}
			time.Sleep(step)
		}
		done <- nil
	}
	a, b := make(chan error, 1), make(chan error, 1)
	go read(0, 3*time.Millisecond, a) // the one behind
	readAt(t, s, 0, 1)                // its GET is open
	go read(2*mb, 0, b)               // past its read-ahead: a GET of its own
	if err := <-b; err != nil {
		t.Fatal(err)
	}
	if err := <-a; err != nil {
		t.Fatal(err)
	}
	st, rs := o.Stats(), s.Stats()
	t.Logf("GETs at %v brought %d bytes of %d; %d merged", st.Offsets, st.Bytes, len(data), rs.Merged)
	if len(st.Offsets) < 2 {
		t.Skip("one GET served both: the second read came within the first's read-ahead")
	}
	if lim := int64(len(data)) * 6 / 5; st.Bytes > lim || rs.Merged == 0 {
		t.Errorf("two readers to the end brought %d bytes of a %d-byte file (%d GETs merged): each read it", st.Bytes, len(data), rs.Merged)
	}
}

// A reader dropped where it read to, and another beginning a little behind
// it (AVFoundation drops its first request to the end and asks again from
// where the asset's bytes end): the second reads again bytes the first
// read, then the first GET's read-ahead, and every byte comes from the
// network once. The least recently used going first, that read-ahead
// (brought before the second reader began, and read by nothing since) went
// before the blocks read again, and a GET brought it again (macOS runs on
// 222f3fc9: two or three 256 KB blocks of TestAVFReadsAhead's file twice).
func TestRangeSourceKeepsReadAhead(t *testing.T) {
	const block = 64 << 10
	data := bigFile(4 * mb)
	src, o := mediatest.Source("FRSK", data, false)
	s := media.NewRangeSource(src, media.RangeOptions{Block: block, Cache: 16 * block, Ahead: 8 * block, Near: 2 * block, Log: quiet, Logged: new(sync.Map)})
	defer s.Close()
	readAt(t, s, 0, 5*block) // the first, dropped there
	if n := settled(t, o); n < 13*block {
		t.Fatalf("its GET stopped at %d, want its read-ahead of 8 blocks past the 5 read", n)
	}
	buf := make([]byte, block)
	for cur := int64(3 * block); cur < int64(len(data)); { // the second, slower than the network
		n, err := s.ReadSome(context.Background(), buf, cur)
		if n > 0 && !bytes.Equal(buf[:n], data[cur:cur+int64(n)]) {
			t.Fatalf("the bytes at %d differ", cur)
		}
		cur += int64(n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	st := o.Stats()
	t.Logf("GETs at %v, their spans %v: %d bytes of %d", st.Offsets, st.Spans, st.Bytes, len(data))
	if again := st.Again(); again > 0 {
		t.Errorf("%d bytes came twice (GETs at %v): the read-ahead went before the blocks read again", again, st.Offsets)
	}
}
