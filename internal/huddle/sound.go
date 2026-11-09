package huddle

import (
	"errors"
	"sync"
	"time"
)

// PlaySound plays pcm (interleaved stereo, 48 kHz) once on the system's
// default speakers, for app sounds outside a huddle (the incoming invite's
// ring). It returns when the sound ended or stop closed. Off the UI
// goroutine.
func PlaySound(pcm []int16, stop <-chan struct{}, logf func(string, ...any)) error {
	if openDefaultDevice == nil {
		return errors.New("huddle: no audio output (built without cgo)")
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	dev, err := openDefaultDevice(logf)
	if err != nil {
		return err
	}
	defer dev.Close()
	var (
		mu   sync.Mutex
		pos  int
		once sync.Once
	)
	done := make(chan struct{})
	err = dev.Start(nil, func(out []int16) {
		mu.Lock()
		n := copy(out, pcm[min(pos, len(pcm)):])
		pos += n
		fin := pos >= len(pcm)
		mu.Unlock()
		clear(out[n:])
		if fin {
			once.Do(func() { close(done) })
		}
	})
	if err != nil {
		return err
	}
	select {
	case <-done:
		time.Sleep(60 * time.Millisecond) // the device's last period
	case <-stop:
	}
	return nil
}

// ResampleStereo converts interleaved stereo PCM at rate Hz to 48 kHz
// (linear interpolation; the app's sounds are short).
func ResampleStereo(pcm []int16, rate int) []int16 {
	if rate == sampleRate || rate <= 0 || len(pcm) < 4 {
		return pcm
	}
	in := len(pcm) / 2
	out := int(int64(in) * sampleRate / int64(rate))
	res := make([]int16, out*2)
	for i := 0; i < out; i++ {
		x := float64(i) * float64(rate) / sampleRate
		j := int(x)
		f := x - float64(j)
		if j >= in-1 {
			j, f = in-2, 1
		}
		for ch := 0; ch < 2; ch++ {
			a, b := float64(pcm[2*j+ch]), float64(pcm[2*(j+1)+ch])
			res[2*i+ch] = int16(a + (b-a)*f)
		}
	}
	return res
}
