package slick

import (
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/webaudio"
)

// withPageAudio gives the plugins' page the client's audio host
// (taut.Options.Audio): decodeAudioData and MediaRecorder.
func withPageAudio(o *taut.Options) { o.Audio = webaudio.Host{} }
