//go:build cgo

package huddle

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/gen2brain/malgo"
)

func init() { openDefaultDevice = openMalgo }

// malgoDevice is the default microphone and speakers through miniaudio
// (WASAPI, Core Audio, PulseAudio/ALSA, loaded at run time).
type malgoDevice struct {
	ctx       *malgo.AllocatedContext
	cap, play *malgo.Device
	once      sync.Once
	logf      func(string, ...any)
}

func openMalgo(logf func(string, ...any)) (Device, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	return &malgoDevice{ctx: ctx, logf: logf}, nil
}

func samples(b []byte) []int16 {
	if len(b) < 2 {
		return nil
	}
	return unsafe.Slice((*int16)(unsafe.Pointer(&b[0])), len(b)/2)
}

func (d *malgoDevice) Start(capture func([]int16), playback func([]int16)) error {
	var errs []error
	if capture != nil {
		cfg := malgo.DefaultDeviceConfig(malgo.Capture)
		cfg.Capture.Format = malgo.FormatS16
		cfg.Capture.Channels = 1
		cfg.SampleRate = sampleRate
		cfg.PeriodSizeInMilliseconds = 10
		dev, err := malgo.InitDevice(d.ctx.Context, cfg, malgo.DeviceCallbacks{
			Data: func(_, in []byte, _ uint32) { capture(samples(in)) },
		})
		if err == nil {
			err = dev.Start()
		}
		if err != nil {
			errs = append(errs, err)
			d.logf("huddle: no microphone: %v", err)
		} else {
			d.cap = dev
		}
	}
	if playback != nil {
		cfg := malgo.DefaultDeviceConfig(malgo.Playback)
		cfg.Playback.Format = malgo.FormatS16
		cfg.Playback.Channels = playChannels
		cfg.SampleRate = sampleRate
		cfg.PeriodSizeInMilliseconds = 10
		dev, err := malgo.InitDevice(d.ctx.Context, cfg, malgo.DeviceCallbacks{
			Data: func(out, _ []byte, _ uint32) { playback(samples(out)) },
		})
		if err == nil {
			err = dev.Start()
		}
		if err != nil {
			errs = append(errs, err)
			d.logf("huddle: no speakers: %v", err)
		} else {
			d.play = dev
		}
	}
	// Every side that was asked for and failed is reported (the session
	// opens the microphone and the speakers separately, so it can tell the
	// user which one is broken).
	return errors.Join(errs...)
}

func (d *malgoDevice) Close() {
	d.once.Do(func() {
		for _, dev := range []*malgo.Device{d.cap, d.play} {
			if dev != nil {
				_ = dev.Stop()
				dev.Uninit()
			}
		}
		_ = d.ctx.Uninit()
		d.ctx.Free()
	})
}
