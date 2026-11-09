package demo

// The demo's images, all drawn on request (art_raster.go) and cached:
// people's avatars (art_avatar.go), the team icon, the custom and standard
// emoji (art_emoji.go) and the pictures people share (art_files.go). The
// URLs Build writes are the fake's /img/demo/…; Image answers them.

import (
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
)

// Image paths (what follows /img/ on the fake).
const (
	imgAvatar = "demo/avatar/" // <handle>/<px>.png
	imgEmoji  = "demo/emoji/"  // <name>.png
	imgFile   = "demo/file/"   // <key>/<w>x<h>.png
	imgTeam   = "demo/team/"   // <px>.png
)

// maxPx bounds a requested size: nothing the demo shows is bigger, and a
// stray huge request shouldn't allocate a gigabyte.
const maxPx = 2048

// Image serves the demo's drawn images: path is what follows /img/ on the
// fake (fakeslack.SetImages).
func Image(path string) ([]byte, string, bool) {
	var b []byte
	switch {
	case strings.HasPrefix(path, imgAvatar):
		handle, px, ok := cutPx(strings.TrimPrefix(path, imgAvatar))
		if !ok {
			return nil, "", false
		}
		av, okp := avatarSpec(handle)
		if !okp {
			return nil, "", false
		}
		b = cached(path, func() []byte { return drawAvatar(av, px) })
	case strings.HasPrefix(path, imgTeam):
		px, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(path, imgTeam), ".png"))
		if err != nil || px <= 0 || px > maxPx {
			return nil, "", false
		}
		b = cached(path, func() []byte { return drawTeam(px) })
	case strings.HasPrefix(path, imgEmoji):
		name := strings.TrimSuffix(strings.TrimPrefix(path, imgEmoji), ".png")
		b = cached(path, func() []byte {
			out, _ := drawCustomEmoji(name, emojiPx)
			return out
		})
	case strings.HasPrefix(path, imgFile):
		key, size, ok := strings.Cut(strings.TrimPrefix(path, imgFile), "/")
		ws, hs, ok2 := strings.Cut(strings.TrimSuffix(size, ".png"), "x")
		w, err1 := strconv.Atoi(ws)
		h, err2 := strconv.Atoi(hs)
		if !ok || !ok2 || err1 != nil || err2 != nil || w <= 0 || h <= 0 || w > maxPx || h > maxPx {
			return nil, "", false
		}
		b = cached(path, func() []byte {
			out, _ := drawFile(key, w, h)
			return out
		})
	}
	if len(b) == 0 {
		return nil, "", false
	}
	return b, "image/png", true
}

// cutPx splits "<name>/<px>.png".
func cutPx(s string) (string, int, bool) {
	name, rest, ok := strings.Cut(s, "/")
	px, err := strconv.Atoi(strings.TrimSuffix(rest, ".png"))
	if !ok || err != nil || px <= 0 || px > maxPx {
		return "", 0, false
	}
	return name, px, true
}

// emojiPx is the size emoji are drawn at: Slack's own are 64px
// ("apple-medium"), and the client scales them down to the line.
const emojiPx = 64

var (
	stdOnce  sync.Once
	stdNames map[string]string // image stem ("1f44d") -> a name art_emoji draws
)

// StandardEmoji draws a Slack standard emoji for its CDN URL
// (emoji.StandardURL) locally: the demo is offline, so the common ones
// have simple drawings of their own and everything else a neutral face.
func StandardEmoji(raw string) ([]byte, bool) {
	if !strings.HasPrefix(raw, emoji.CDN) {
		return nil, false
	}
	_, file, ok := strings.Cut(strings.TrimPrefix(raw, emoji.CDN), "/")
	if !ok || !strings.HasSuffix(file, ".png") {
		return nil, false
	}
	stem := strings.ToLower(strings.TrimSuffix(file, ".png"))
	stdOnce.Do(func() {
		stdNames = map[string]string{}
		for _, n := range standardDrawn {
			if img, _, ok := emoji.Standard(n); ok {
				stdNames[strings.ToLower(img)] = n
			}
		}
	})
	name, ok := stdNames[stem]
	if !ok {
		// a skin tone or a gender variant draws as its base emoji
		name, ok = stdNames[baseStem(stem)]
	}
	if !ok {
		name = "" // the fallback, coloured by the stem
	}
	b := cached("std/"+stem, func() []byte { return drawStandardEmoji(name, stem, emojiPx) })
	return b, len(b) > 0
}

// baseStem drops skin tone modifiers, variation selectors and ZWJ tails
// ("1f44d-1f3fd" -> "1f44d", "1f937-200d-2640-fe0f" -> "1f937").
func baseStem(stem string) string {
	var keep []string
	for _, p := range strings.Split(stem, "-") {
		switch {
		case p == "200d":
			return strings.Join(keep, "-")
		case p == "fe0f", len(p) == 5 && p >= "1f3fb" && p <= "1f3ff":
			continue
		}
		keep = append(keep, p)
	}
	return strings.Join(keep, "-")
}
