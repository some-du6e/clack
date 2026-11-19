//go:build js

package huddleui

import "errors"

// decodeMP3 has no decoder in the browser demo (cmd/clack-web), which
// can't play a sound anyway: without cgo there is no audio output
// (huddle.PlaySound). go-mp3 (Apache-2.0) stays out of the page.
func decodeMP3([]byte) ([]int16, error) {
	return nil, errors.New("huddleui: no MP3 decoder in the browser demo")
}
