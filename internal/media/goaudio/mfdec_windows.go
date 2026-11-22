//go:build windows && cgo

package goaudio

/*
#cgo LDFLAGS: -lole32
#include <stdlib.h>
#include "mfdec_windows.h"
*/
import "C"

import (
	"fmt"
	"io"
	"runtime"
	"syscall"
	"unsafe"
)

// Media Foundation's Source Reader decodes what Go can't (AAC: the aac
// renditions and most m4a uploads; mfdec_windows.c).
func init() { osOpen = mfOpen }

// mfStream is a file the Source Reader decodes, on a thread of its own in
// COM's multithreaded apartment: every call goes there.
type mfStream struct {
	calls    chan func()
	d        *C.clackDec
	rate, ch int
	frames   int64
	// seekTo is a seek's target (100 ns units) until the first read
	// after it, which drops the frames before it (the reader lands on the
	// packet holding it); -1 none
	seekTo int64
}

func mfOpen(b []byte, mime string) (stream, error) {
	if len(b) == 0 {
		return nil, errUnsupported
	}
	s := &mfStream{calls: make(chan func()), seekTo: -1}
	opened := make(chan error, 1)
	go s.thread(b, mime, opened)
	if err := <-opened; err != nil {
		return nil, err
	}
	return s, nil
}

func (s *mfStream) thread(b []byte, mime string, opened chan<- error) {
	runtime.LockOSThread() // never unlocked: the thread goes with the goroutine
	C.clack_dec_thread_init()
	defer C.clack_dec_thread_done()
	var hint *C.uint16_t
	if mime != "" {
		if u, err := syscall.UTF16FromString(mime); err == nil {
			hint = (*C.uint16_t)(unsafe.Pointer(&u[0]))
			defer runtime.KeepAlive(u)
		}
	}
	var d *C.clackDec
	var rate, ch C.uint32_t
	var dur C.int64_t
	hr := C.clack_dec_open(unsafe.Pointer(&b[0]), C.uint32_t(len(b)), hint, &d, &rate, &ch, &dur)
	if hr != 0 || d == nil {
		opened <- fmt.Errorf("%w: Media Foundation's source reader: %#x", errUnsupported, uint32(hr))
		return
	}
	if ch > maxChannels {
		C.clack_dec_close(d)
		opened <- fmt.Errorf("%w: %d channels", errUnsupported, int(ch))
		return
	}
	s.d, s.rate, s.ch = d, int(rate), int(ch)
	if dur > 0 {
		s.frames = int64(dur) * int64(rate) / 10_000_000
	}
	opened <- nil
	for f := range s.calls {
		f()
	}
	C.clack_dec_close(d)
}

// do runs f on the decoder's thread.
func (s *mfStream) do(f func()) {
	done := make(chan struct{})
	s.calls <- func() {
		f()
		close(done)
	}
	<-done
}

func (s *mfStream) Rate() int     { return s.rate }
func (s *mfStream) Channels() int { return s.ch }
func (s *mfStream) Frames() int64 { return s.frames }
func (s *mfStream) Codec() string { return "mediafoundation" }

func (s *mfStream) SeekFrame(f int64) error {
	at := max(0, f) * 10_000_000 / int64(s.rate)
	var hr C.int32_t
	s.do(func() { hr = C.clack_dec_seek(s.d, C.int64_t(at)) })
	if hr != 0 {
		return fmt.Errorf("goaudio: Media Foundation's seek: %#x", uint32(hr))
	}
	s.seekTo = at
	return nil
}

func (s *mfStream) Read(buf []float32) (int, error) {
	want := len(buf) / s.ch
	if want == 0 {
		return 0, nil
	}
	got := 0
	var err error
	s.do(func() {
		for tries := 0; got < want && tries < 64; tries++ {
			var n C.uint32_t
			var ts C.int64_t
			var eof C.int
			hr := C.clack_dec_read(s.d, (*C.float)(unsafe.Pointer(&buf[got*s.ch])), C.uint32_t(want-got), &n, &ts, &eof)
			if hr != 0 {
				err = fmt.Errorf("goaudio: Media Foundation's decode: %#x", uint32(hr))
				return
			}
			if eof != 0 {
				if got == 0 {
					err = io.EOF
				}
				return
			}
			k := int(n)
			if s.seekTo >= 0 && k > 0 && ts >= 0 {
				// after a seek: the frames from the target on (the reader
				// may land a packet or more before it)
				drop := int((s.seekTo - int64(ts)) * int64(s.rate) / 10_000_000)
				switch {
				case drop >= k:
					k = 0
				default:
					s.seekTo = -1
					if drop > 0 {
						at := buf[got*s.ch:]
						copy(at, at[drop*s.ch:k*s.ch])
						k -= drop
					}
				}
			}
			got += k
		}
	})
	if got > 0 {
		return got, nil
	}
	if err == nil {
		err = io.EOF // the reader gave nothing in 64 reads
	}
	return 0, err
}

func (s *mfStream) Close() {
	if s.calls != nil {
		close(s.calls)
		s.calls = nil
	}
}
