package demo

// Image serves the demo's drawn images: path is what follows /img/ on the
// fake (fakeslack.SetImages). STUB: the content agent replaces this file.
func Image(path string) ([]byte, string, bool) { return nil, "", false }

// StandardEmoji draws a Slack standard emoji for its CDN URL
// (emoji.StandardURL) locally. STUB: the content agent replaces this file.
func StandardEmoji(raw string) ([]byte, bool) { return nil, false }
