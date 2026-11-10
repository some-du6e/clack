//go:build cgo

package huddle

// #include <stdlib.h>
import "C"

import (
	"errors"
	"sync"
	"unsafe"

	"github.com/gen2brain/malgo"
)

func init() {
	openDefaultDevice = openMalgo
	listAudioDevices = listMalgo
}

func listMalgo(capture bool) ([]AudioDevice, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = ctx.Uninit(); ctx.Free() }()
	kind := malgo.Playback
	if capture {
		kind = malgo.Capture
	}
	infos, err := ctx.Devices(kind)
	if err != nil {
		return nil, err
	}
	out := make([]AudioDevice, 0, len(infos))
	for i := range infos {
		out = append(out, AudioDevice{ID: infos[i].ID.String(), Name: infos[i].Name(), Default: infos[i].IsDefault != 0})
	}
	return out, nil
}

// deviceID finds the selected device (id from AudioDevice.ID) and returns
// a C copy of its miniaudio id for DeviceConfig (free it after InitDevice);
// nil is the default device.
func (d *malgoDevice) deviceID(kind malgo.DeviceType, id string) unsafe.Pointer {
	if id == "" {
		return nil
	}
	infos, err := d.ctx.Devices(kind)
	if err != nil {
		return nil
	}
	for i := range infos {
		if infos[i].ID.String() == id {
			return infos[i].ID.Pointer()
		}
	}
	d.logf("huddle: the chosen audio device is gone, using the default")
	return nil
}

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
	micID, outID := SelectedAudioDevices()
	if capture != nil {
		cfg := malgo.DefaultDeviceConfig(malgo.Capture)
		cfg.Capture.DeviceID = d.deviceID(malgo.Capture, micID)
		cfg.Capture.Format = malgo.FormatS16
		cfg.Capture.Channels = 1
		cfg.SampleRate = sampleRate
		cfg.PeriodSizeInMilliseconds = 10
		dev, err := malgo.InitDevice(d.ctx.Context, cfg, malgo.DeviceCallbacks{
			Data: func(_, in []byte, _ uint32) { capture(samples(in)) },
		})
		if cfg.Capture.DeviceID != nil {
			C.free(cfg.Capture.DeviceID)
		}
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
		cfg.Playback.DeviceID = d.deviceID(malgo.Playback, outID)
		cfg.Playback.Format = malgo.FormatS16
		cfg.Playback.Channels = playChannels
		cfg.SampleRate = sampleRate
		cfg.PeriodSizeInMilliseconds = 10
		dev, err := malgo.InitDevice(d.ctx.Context, cfg, malgo.DeviceCallbacks{
			Data: func(out, _ []byte, _ uint32) { playback(samples(out)) },
		})
		if cfg.Playback.DeviceID != nil {
			C.free(cfg.Playback.DeviceID)
		}
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
