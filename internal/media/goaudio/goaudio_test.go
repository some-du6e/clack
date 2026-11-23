package goaudio

// Go audio against mediatest's audio fixtures (3 s: 440 Hz, then 660 Hz,
// then 880 Hz): every format it decodes gives the file's length and its
// tones, at their level, where they are (and where a seek lands); the
// resampler and the time stretch keep a tone's frequency; a player
// renders the whole file at 48 kHz stereo and ends; with no sound card
// (CI) it keeps real time on its clock, at 1x and at Slack's 2x, from a
// seek made before the file loaded; and a chain falls back to it when the
// backend before it can't play the file.

import (
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/mediatest"
)

// goFixtures are the fixtures Go audio decodes: all but the AAC one,
// unless the OS decodes it (Media Foundation's, on Windows).
func goFixtures() []mediatest.AudioFixture {
	var out []mediatest.AudioFixture
	for _, fx := range mediatest.AudioFixtures() {
		if fx.Name != "aac" || DecodesAAC() {
			out = append(out, fx)
		}
	}
	return out
}

// mono decodes the rest of s, its channels averaged.
func mono(t *testing.T, s stream) []float32 {
	t.Helper()
	ch := s.Channels()
	buf := make([]float32, 1024*ch)
	var out []float32
	for i := 0; ; i++ {
		n, err := s.Read(buf)
		for f := 0; f < n; f++ {
			var v float32
			for k := 0; k < ch; k++ {
				v += buf[f*ch+k]
			}
			out = append(out, v/float32(ch))
		}
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if i > 1<<16 {
			t.Fatal("Read never ended")
		}
	}
}

// window is samples [from, to) of mono at rate.
func window(samples []float32, rate int, from, to time.Duration) []float32 {
	a := min(len(samples), int(from.Seconds()*float64(rate)))
	b := min(len(samples), int(to.Seconds()*float64(rate)))
	return samples[a:b]
}

// checkTones checks each second's middle (300-700 ms) holds its tone,
// far louder than the others, at the fixtures' level (a sine of 1/8:
// power 0.0039); off is where the samples start in the file.
func checkTones(t *testing.T, samples []float32, rate int, off time.Duration) {
	t.Helper()
	for i := range mediatest.Tones {
		at := time.Duration(i) * time.Second
		if at+700*time.Millisecond <= off {
			continue
		}
		from, to := max(at+300*time.Millisecond, off)-off, at+700*time.Millisecond-off
		w := window(samples, rate, from, to)
		if len(w) < rate/10 {
			t.Errorf("second %d: %d samples, want at least 100 ms", i+1, len(w))
			continue
		}
		got, ratio := mediatest.Loudest(w, rate)
		p := mediatest.Power(w, rate, mediatest.Tones[i])
		if got != i || ratio < 20 {
			t.Errorf("second %d: the loudest tone is %v Hz, %.1fx the next; want %v Hz", i+1, mediatest.Tones[max(got, 0)], ratio, mediatest.Tones[i])
		}
		if p < 0.0025 || p > 0.0055 {
			t.Errorf("second %d: %v Hz at power %.4f, want the fixture's 0.0039 (a sine of 1/8)", i+1, mediatest.Tones[i], p)
		}
	}
}

func TestDecodesTheFixtures(t *testing.T) {
	for _, fx := range goFixtures() {
		t.Run(fx.Name, func(t *testing.T) {
			s, err := parse(fx.Data, fx.Mime)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			d := time.Duration(s.Frames()) * time.Second / time.Duration(s.Rate())
			tol, slack := 10*time.Millisecond, s.Rate()/100
			switch fx.Name {
			case "mp3": // LAME's delay and padding, and its Xing frame
				tol = 80 * time.Millisecond
			case "aac": // the OS's decoder may keep AAC's 1024 priming frames
				tol, slack = 40*time.Millisecond, s.Rate()/20
			}
			if d < mediatest.AudioDuration-tol || d > mediatest.AudioDuration+tol {
				t.Errorf("duration %v (%d frames at %d Hz), want 3 s", d, s.Frames(), s.Rate())
			}
			samples := mono(t, s)
			if diff := len(samples) - int(s.Frames()); diff < -slack || diff > slack {
				t.Errorf("decoded %d frames, Frames says %d", len(samples), s.Frames())
			}
			t.Logf("%s: %s, %d Hz × %d, %v, %d frames decoded", fx.Name, s.Codec(), s.Rate(), s.Channels(), d, len(samples))
			checkTones(t, samples, s.Rate(), 0)
		})
	}
}

// A seek lands where it says: the tone there, from the frame asked for,
// at the fixtures' level (silence, whose powers are all zero, would make
// any tone "the loudest").
func TestSeekLandsOnTheTone(t *testing.T) {
	for _, fx := range goFixtures() {
		t.Run(fx.Name, func(t *testing.T) {
			s, err := parse(fx.Data, fx.Mime)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			for _, at := range []time.Duration{1400 * time.Millisecond, 2500 * time.Millisecond, 200 * time.Millisecond} {
				if err := s.SeekFrame(int64(at.Seconds() * float64(s.Rate()))); err != nil {
					t.Fatal(err)
				}
				ch := s.Channels()
				buf := make([]float32, s.Rate()/5*ch) // 200 ms
				n, err := s.Read(buf)
				if err != nil || n < len(buf)/ch/2 {
					t.Fatalf("after a seek to %v: %d frames, %v", at, n, err)
				}
				w := make([]float32, n)
				for f := range w {
					w[f] = buf[f*ch]
				}
				want := int(at / time.Second)
				if got, ratio := mediatest.Loudest(w[n/4:], s.Rate()); got != want || ratio < 10 {
					t.Errorf("after a seek to %v: the loudest tone is %v Hz (%.1fx), want %v Hz", at, mediatest.Tones[max(got, 0)], ratio, mediatest.Tones[want])
				}
				if p := mediatest.Power(w[n/4:], s.Rate(), mediatest.Tones[want]); p < 0.0025 {
					t.Errorf("after a seek to %v: %v Hz at power %.5f, want the fixture's 0.0039 (not silence)", at, mediatest.Tones[want], p)
				}
			}
		})
	}
}

// Without the OS's decoder AAC is the OS backend's: Go audio says it
// doesn't support it (the element's MEDIA_ERR_SRC_NOT_SUPPORTED, so the
// next backend or url_private plays). A page that isn't audio is never
// supported.
func TestAACIsNotSupported(t *testing.T) {
	cases := map[string][]byte{"not audio": []byte("<html>signed out</html>")}
	if !DecodesAAC() {
		cases["aac"] = mediatest.TonesAAC
	}
	for name, b := range cases {
		if _, err := parse(b, "audio/mp4"); !errors.Is(err, errUnsupported) {
			t.Errorf("%s: %v, want errUnsupported", name, err)
		}
	}
}

// sine is n frames of a sine at freq, amp, rate, ch channels.
func sine(n, rate, ch int, freq, amp float64) []float32 {
	out := make([]float32, n*ch)
	for i := 0; i < n; i++ {
		v := float32(amp * math.Sin(2*math.Pi*freq*float64(i)/float64(rate)))
		for k := 0; k < ch; k++ {
			out[i*ch+k] = v
		}
	}
	return out
}

func channel(in []float32, ch, k int) []float32 {
	out := make([]float32, len(in)/ch)
	for i := range out {
		out[i] = in[i*ch+k]
	}
	return out
}

// The resampler turns 16 kHz (and 44.1 kHz) into 48 kHz: three times the
// frames (48000/44100 as many), the same tone at the same level.
func TestResamplerKeepsTheTone(t *testing.T) {
	for _, rate := range []int{16000, 44100} {
		in := sine(rate, rate, 2, 1000, 0.5) // 1 s
		r := newResampler(rate, 2)
		r.push(in)
		out := make([]float32, 2*outRate*2)
		n := r.pull(out, true)
		if want := outRate; n < want-50 || n > want+50 {
			t.Errorf("%d Hz: %d frames out for 1 s, want about %d", rate, n, want)
		}
		l := channel(out[:n*2], 2, 0)
		mid := l[n/4 : 3*n/4]
		p := mediatest.Power(mid, outRate, 1000)
		if p < 0.0625*0.9 || p > 0.0625*1.1 {
			t.Errorf("%d Hz: 1 kHz at power %.4f, want 0.0625 (a sine of 1/2)", rate, p)
		}
		if q := mediatest.Power(mid, outRate, 3000); q > p/1000 {
			t.Errorf("%d Hz: 3 kHz at power %.6f next to 1 kHz's %.4f", rate, q, p)
		}
	}
}

// The time stretch (WSOLA) plays 2 s in 1 s at 2x and in 4 s at 0.5x,
// keeping a 440 Hz tone at 440 Hz and its level.
func TestStretchKeepsThePitch(t *testing.T) {
	in := sine(2*outRate, outRate, 2, 440, 0.5)
	for _, rate := range []float64{2, 0.5, 1.5} {
		s := newStretcher()
		s.rate = rate
		var out []float32
		buf := make([]float32, 4*wsolaH*2)
		for i := 0; i < len(in); i += 4800 * 2 { // 100 ms at a time
			s.push(in[i:min(len(in), i+4800*2)])
			for {
				n := s.pull(buf, false)
				out = append(out, buf[:n*2]...)
				if n == 0 {
					break
				}
			}
		}
		for {
			n := s.pull(buf, true)
			out = append(out, buf[:n*2]...)
			if n == 0 {
				break
			}
		}
		frames := len(out) / 2
		want := int(2 * outRate / rate)
		if frames < want-2*wsolaN || frames > want+2*wsolaN {
			t.Errorf("%vx: %d frames out of 2 s, want about %d", rate, frames, want)
		}
		l := channel(out, 2, 0)
		mid := l[len(l)/4 : 3*len(l)/4]
		p := mediatest.Power(mid, outRate, 440)
		if p < 0.0625*0.6 || p > 0.0625*1.4 {
			t.Errorf("%vx: 440 Hz at power %.4f, want about 0.0625", rate, p)
		}
		for _, f := range []float64{220, 880, 440 * rate} {
			if f == 440 {
				continue
			}
			if q := mediatest.Power(mid, outRate, f); q > p/20 {
				t.Errorf("%vx: %v Hz at power %.5f next to 440 Hz's %.4f: the pitch moved", rate, f, q, p)
			}
		}
	}
}

// rec records a player's events.
type rec struct {
	mu  sync.Mutex
	evs []media.Event
	c   chan media.Event
}

func watch(p media.Player) *rec {
	r := &rec{c: make(chan media.Event, 1024)}
	go func() {
		for ev := range p.Events() {
			r.mu.Lock()
			r.evs = append(r.evs, ev)
			r.mu.Unlock()
			select {
			case r.c <- ev:
			default:
			}
		}
	}()
	return r
}

// wait is the next event of kind k within d (an error fails the test).
func (r *rec) wait(t *testing.T, k media.EventKind, d time.Duration) media.Event {
	t.Helper()
	tm := time.NewTimer(d)
	defer tm.Stop()
	for {
		select {
		case ev := <-r.c:
			if ev.Kind == k {
				return ev
			}
			if ev.Kind == media.Error && k != media.Error {
				t.Fatalf("error %d waiting for %v; events %v", ev.State.Err, k, r.kinds())
			}
		case <-tm.C:
			t.Fatalf("no %v within %v; events %v", k, d, r.kinds())
		}
	}
}

func (r *rec) kinds() []media.EventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []media.EventKind
	for _, ev := range r.evs {
		if ev.Kind != media.TimeUpdate {
			out = append(out, ev.Kind)
		}
	}
	return out
}

func inOrder(got, want []media.EventKind) bool {
	i := 0
	for _, k := range got {
		if i < len(want) && k == want[i] {
			i++
		}
	}
	return i == len(want)
}

func source(id string, fx mediatest.AudioFixture) media.Source {
	src, _ := mediatest.Source(id, fx.Data, false)
	src.Ext, src.Mime = fx.Ext, fx.Mime
	return src
}

func fixture(name string) mediatest.AudioFixture {
	for _, fx := range mediatest.AudioFixtures() {
		if fx.Name == name {
			return fx
		}
	}
	panic("no fixture " + name)
}

// noDevice is a machine with no sound card: the player keeps time on its
// clock.
func noDevice(func([]int16)) (io.Closer, error) {
	return nil, errors.New("no audio device (the test's)")
}

func newPlayer(t *testing.T, b *Backend, src media.Source) (media.Player, *rec) {
	t.Helper()
	if b.Logf == nil {
		b.Logf = t.Logf
	}
	p, err := b.New(src, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := watch(p)
	t.Cleanup(p.Close)
	return p, r
}

// fastSink is a sound card that pulls as fast as the player renders,
// keeping every frame.
type fastSink struct {
	mu     sync.Mutex
	frames []int16
	stop   chan struct{}
	done   chan struct{}
}

func (s *fastSink) open(pull func([]int16)) (io.Closer, error) {
	s.stop, s.done = make(chan struct{}), make(chan struct{})
	go func() {
		defer close(s.done)
		buf := make([]int16, 4800*2)
		for {
			select {
			case <-s.stop:
				return
			case <-time.After(time.Millisecond):
			}
			pull(buf)
			s.mu.Lock()
			s.frames = append(s.frames, buf...)
			s.mu.Unlock()
		}
	}()
	return s, nil
}

func (s *fastSink) Close() error {
	close(s.stop)
	<-s.done
	return nil
}

// The player renders each format whole at 48 kHz stereo: 3 s of frames
// once it plays, with the tones where they are, then ends (the element's
// events in order); the decoded duration is the metadata's.
func TestPlayerRendersTheFile(t *testing.T) {
	for _, fx := range goFixtures() {
		t.Run(fx.Name, func(t *testing.T) {
			sink := &fastSink{}
			p, r := newPlayer(t, &Backend{Output: sink.open}, source("FGA"+fx.Name, fx))
			md := r.wait(t, media.LoadedMetadata, 10*time.Second)
			if d := md.State.Duration; d < 2920*time.Millisecond || d > 3080*time.Millisecond {
				t.Errorf("loadedmetadata's duration %v, want 3 s", d)
			}
			r.wait(t, media.LoadedData, 5*time.Second)
			sink.mu.Lock()
			silent := len(sink.frames)
			sink.mu.Unlock()
			p.Play()
			r.wait(t, media.Ended, 10*time.Second)
			if st := p.State(); !st.Ended || !st.Paused || st.Pos != st.Duration {
				t.Errorf("after ended: %+v", st)
			}
			if !inOrder(r.kinds(), []media.EventKind{media.DurationChange, media.LoadedMetadata, media.LoadedData, media.Play, media.Playing, media.Pause, media.Ended}) {
				t.Errorf("events %v", r.kinds())
			}
			sink.mu.Lock()
			out := append([]int16(nil), sink.frames[silent:]...)
			sink.mu.Unlock()
			// what played: the frames up to the last one that isn't silent
			last := len(out) - 1
			for last >= 0 && out[last] == 0 {
				last--
			}
			played := (last + 2) / 2
			if d := time.Duration(played) * time.Second / outRate; d < 2920*time.Millisecond || d > 3080*time.Millisecond {
				t.Errorf("%v of sound rendered, want 3 s", d)
			}
			l := make([]float32, played)
			for i := range l {
				l[i] = float32(out[2*i]) / 32768
			}
			if fx.Name != "mp3" { // LAME's delay moves its tones by up to 50 ms: the windows allow it
				for i := 0; i < played; i++ {
					if out[2*i] != out[2*i+1] {
						t.Fatalf("frame %d: left %d, right %d; a mono file plays on both", i, out[2*i], out[2*i+1])
					}
				}
			}
			checkTones(t, l, outRate, 0)
		})
	}
}

// With no sound card (CI) the player keeps real time on its clock: the
// position runs with the wall clock and the file ends 3 s after it
// played; paused, the position holds.
func TestPlayerKeepsTimeWithoutASoundCard(t *testing.T) {
	p, r := newPlayer(t, &Backend{Output: noDevice}, source("FGAT", fixture("opus-frag")))
	r.wait(t, media.LoadedData, 10*time.Second)
	p.Play()
	r.wait(t, media.Playing, 5*time.Second)
	t0 := time.Now()
	time.Sleep(time.Second)
	pos, wall := p.State().Pos, time.Since(t0)
	if d := pos - wall; d < -250*time.Millisecond || d > 100*time.Millisecond {
		t.Errorf("after %v of playing the position is %v", wall, pos)
	}
	p.Pause()
	r.wait(t, media.Pause, 5*time.Second)
	held := p.State().Pos
	time.Sleep(300 * time.Millisecond)
	if d := p.State().Pos - held; d != 0 {
		t.Errorf("paused, the position moved %v", d)
	}
	p.Play()
	t1 := time.Now()
	r.wait(t, media.Ended, 8*time.Second)
	left, took := mediatest.AudioDuration-held, time.Since(t1)
	if took < left-200*time.Millisecond || took > left+1500*time.Millisecond {
		t.Errorf("the last %v played in %v", left, took)
	}
}

// At Slack's 2x (media_playback_speed) the clip plays in half the time,
// from a seek made before the file loaded.
func TestPlayerRateAndEarlySeek(t *testing.T) {
	p, r := newPlayer(t, &Backend{Output: noDevice}, source("FGAR", fixture("webm")))
	p.SetRate(2)
	p.Seek(time.Second)
	p.Play()
	ev := r.wait(t, media.Playing, 10*time.Second)
	t0 := time.Now()
	// the position the playing event carried: where it started, not where
	// it got to by the time this goroutine looked
	if pos := ev.State.Pos; pos != time.Second {
		t.Errorf("playing from %v, want the seek's 1 s", pos)
	}
	r.wait(t, media.Ended, 8*time.Second)
	if took := time.Since(t0); took < 800*time.Millisecond || took > 2500*time.Millisecond {
		t.Errorf("2 s at 2x took %v, want about 1 s", took)
	}
}

// Without the OS's decoder AAC fails as MEDIA_ERR_SRC_NOT_SUPPORTED
// before any metadata.
func TestPlayerAACFails(t *testing.T) {
	if DecodesAAC() {
		t.Skip("this build decodes AAC (Media Foundation's source reader): TestPlayerRendersTheFile/aac plays it")
	}
	p, r := newPlayer(t, &Backend{Output: noDevice}, source("FGAE", fixture("aac")))
	p.Play()
	ev := r.wait(t, media.Error, 10*time.Second)
	if ev.State.Err != media.ErrSrcNotSupported {
		t.Errorf("error %d, want %d (src not supported)", ev.State.Err, media.ErrSrcNotSupported)
	}
	for _, k := range r.kinds() {
		if k == media.LoadedMetadata {
			t.Errorf("loadedmetadata before the error: %v", r.kinds())
		}
	}
}

// A chain whose first backend can't play the file (the fake plays only
// video) hands it to Go audio with what was asked meanwhile (the rate,
// the play): it plays to the end at 2x, and no error reaches the page. A
// file neither plays fails as the last one did. Go audio takes no video.
func TestChainFallsBackToGoAudio(t *testing.T) {
	fake := media.NewFake(media.FakeOptions{Log: func(string, ...any) {}})
	ch := media.NewChain(nil, fake, &Backend{Output: noDevice, Logf: t.Logf})
	if !ch.Takes(media.Source{Mime: "video/mp4", Ext: "mp4"}) || !ch.Takes(media.Source{Mime: "audio/mp4", Ext: "m4a"}) {
		t.Error("the chain doesn't take video and audio")
	}
	if (&Backend{}).Takes(media.Source{Mime: "video/mp4", Ext: "mp4"}) {
		t.Error("Go audio takes a video")
	}
	if media.NewChain(nil, &Backend{}).Takes(media.Source{Mime: "video/mp4", Ext: "mp4"}) {
		t.Error("a chain of Go audio alone takes a video")
	}
	p, err := ch.New(source("FGAC", fixture("opus-mp4")), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	r := watch(p)
	p.SetRate(2)
	p.Play()
	md := r.wait(t, media.LoadedMetadata, 10*time.Second)
	if d := md.State.Duration; d < 2950*time.Millisecond || d > 3050*time.Millisecond {
		t.Errorf("duration %v", d)
	}
	t0 := time.Now()
	r.wait(t, media.Ended, 10*time.Second)
	if took := time.Since(t0); took > 2800*time.Millisecond {
		t.Errorf("3 s at 2x took %v", took)
	}
	for _, k := range r.kinds() {
		if k == media.Error {
			t.Errorf("an error reached the page: %v", r.kinds())
		}
	}
	if b, ok := p.(interface{ Backend() string }); !ok || !strings.Contains(b.Backend(), "goaudio") {
		t.Errorf("played by %v", p)
	}

	if DecodesAAC() {
		return // Go audio plays the AAC one too (Media Foundation's decoder)
	}
	q, err := ch.New(source("FGAD", fixture("aac")), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(q.Close)
	rq := watch(q)
	q.Play()
	if ev := rq.wait(t, media.Error, 10*time.Second); ev.State.Err != media.ErrSrcNotSupported {
		t.Errorf("error %d, want %d", ev.State.Err, media.ErrSrcNotSupported)
	}
}
