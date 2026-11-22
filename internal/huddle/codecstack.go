package huddle

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// The Opus codec is libopus transpiled to Go (kazzmir/opus-go, see
// opusguard.go), and its C keeps addresses as uintptr. The decoder takes
// the address of its Go stack locals and passes them down the call chain
// that way: celt_decode_with_ec's range decoder and the intensity,
// dual_stereo and balance that clt_compute_allocation writes for it,
// opus_decode_frame's dec, silk_Decode's sample counts (opuscc, v1.4.0).
// The wrapper does the same with the caller's PCM and packet buffers
// (libc.PtrInt16, &pcm[0], &packet[0]). A uintptr isn't a pointer to Go:
// when a goroutine's stack grows, the runtime copies it and fixes its
// pointers, not those. The callees then write into the old copy while the
// caller reads the new one, so the frame decodes to noise. A goroutine
// waiting for its next packet sits on a shallow stack, the GC shrinks it,
// and the next frame's deep call chain grows it again: in a huddle that's a
// burst of static every few GC cycles, on whatever the others say, and on
// your own voice where another of your clients plays it back. (Upstream
// issue kazzmir/opus-go#79 reports the same; its master turns the
// decoder's locals into typed pointers.)
//
// libopus itself doesn't care which thread runs it. So every call into
// the transpiled code runs on one goroutine whose stack can't move:
// codecGoroutine first grows it past what any codec call needs and keeps
// that much in use (pinStack's frames, live until the goroutine ends), so
// a codec call never makes it grow and the GC never finds it idle enough
// to shrink (a stack shrinks only below a quarter in use). The buffers a
// call reads and writes are captured by its closure, which puts them on
// the heap, and the heap doesn't move. Chromium runs the same libopus in
// C, where none of this can happen.

const (
	pinFrames = 64   // pinStack's depth
	pinPad    = 4096 // bytes per frame: 64 × 4 KiB in use, a 512 KiB stack
)

type codecCallReq struct {
	f    func()
	done chan struct{}
	err  any // what f panicked with (a libopus assertion: opusguard.go)
}

var codecWorker struct {
	once  sync.Once
	calls chan *codecCallReq
	moves atomic.Int64 // calls after which the goroutine's stack had moved (never, by design)
	count atomic.Int64 // calls run
}

// codecCall runs f on the codec goroutine and waits for it. A panic in f
// (a libopus assertion) comes back as a panic here, on the caller's
// goroutine, where audio.go's recovers expect it. f must not call
// codecCall.
func codecCall(f func()) {
	codecWorker.once.Do(func() {
		codecWorker.calls = make(chan *codecCallReq)
		ready := make(chan struct{})
		go codecGoroutine(codecWorker.calls, ready)
		<-ready
	})
	r := &codecCallReq{f: f, done: make(chan struct{})}
	codecWorker.calls <- r
	<-r.done
	if r.err != nil {
		panic(r.err)
	}
}

func codecGoroutine(calls <-chan *codecCallReq, ready chan<- struct{}) {
	pinStack(pinFrames, func() {
		// probe's address is where the stack is: had a call moved the
		// stack, probe would be somewhere else afterwards
		var probe byte
		base := uintptr(unsafe.Pointer(&probe))
		close(ready)
		for r := range calls {
			runCodecReq(r)
			codecWorker.count.Add(1)
			if at := uintptr(unsafe.Pointer(&probe)); at != base {
				codecWorker.moves.Add(1)
				base = at
			}
			close(r.done)
		}
	})
}

func runCodecReq(r *codecCallReq) {
	defer func() { r.err = recover() }()
	r.f()
}

// pinStack nests depth frames of pinPad bytes each, then runs run in the
// innermost one. The pads are read after the call returns, so they stay
// live and in use the whole time run runs.
//
//go:noinline
func pinStack(depth int, run func()) byte {
	var pad [pinPad]byte
	pad[depth%pinPad] = byte(depth)
	var keep byte
	if depth > 0 {
		keep = pinStack(depth-1, run)
	} else {
		run()
	}
	return keep ^ pad[(depth*131)%pinPad]
}

// codecStackMoves is how many codec calls found the codec goroutine's
// stack moved (tests: it must stay 0), out of how many ran.
func codecStackMoves() (moves, calls int64) {
	return codecWorker.moves.Load(), codecWorker.count.Load()
}
