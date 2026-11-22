package huddle

// OutputRate and OutputChannels are OpenOutput's PCM: the huddle's
// playback format, 48 kHz interleaved stereo.
const (
	OutputRate     = sampleRate
	OutputChannels = playChannels
)

// OpenOutput opens the speakers Audio & video picked (SelectAudioDevices;
// the system's default otherwise) and pulls interleaved stereo 48 kHz PCM
// from pull, on the sound card's thread, until Close: app audio outside a
// huddle (a message's audio player, internal/media/goaudio). Without a
// sound card (a build without cgo, a machine with no output) it fails, and
// the caller keeps time without one.
func OpenOutput(logf func(string, ...any), pull func(out []int16)) (Device, error) {
	if openDefaultDevice == nil {
		return nil, ErrNoAudio
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	d, err := openDefaultDevice(logf)
	if err != nil {
		return nil, err
	}
	if err := d.Start(nil, pull); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}
