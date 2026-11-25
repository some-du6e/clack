package fakeslack

// The GIF picker's message: what Slack's GIF picker posts (client-boot
// @2402313, build e277bbacc0bc), in the shape #emojibot's 1795559136.610559
// has (Karim's Copy Image report, 2026-11-24; its shape read with
// conversations.history, none of its text kept): the text as a rich_text
// block, then one attachment, fallback "shared a GIF", holding one image
// block. The block has Giphy's rendition for image_url (an animated WebP,
// with a query), the GIF's alt_text, title {plain_text "GIF", emoji},
// image_width, image_height and image_bytes, the server's "<w>x<h>px
// image" fallback, and is_animated.
//
// GIFPickerMessage builds it with a test's text and image, and
// /img/<w>x<h>/gp<seed>.webp serves an animated WebP like Giphy's: three
// solid frames 80 ms apart, looping, the first GIFPickerFirst and the
// others GIFPickerLater, so a copy shows which frame it took.

import (
	"encoding/json"
	"fmt"
	"image/color"
	"strings"

	"github.com/some-du6e/clack/internal/imgcache/webptest"
	"github.com/some-du6e/clack/internal/slack"
)

// GIFPickerFirst is GIFPickerWebP's first frame's colour, GIFPickerLater
// its other frames'.
var (
	GIFPickerFirst = color.NRGBA{R: 220, G: 30, B: 60, A: 255}
	GIFPickerLater = color.NRGBA{R: 30, G: 160, B: 90, A: 255}
)

// GIFPickerWebP is a w×h animated WebP of three solid frames, the first
// GIFPickerFirst.
func GIFPickerWebP(w, h int) []byte {
	return webptest.Anim(w, h, 0,
		webptest.Solid(0, 0, w, h, 80, GIFPickerFirst),
		webptest.Solid(0, 0, w, h, 80, GIFPickerLater),
		webptest.Solid(0, 0, w, h, 80, GIFPickerLater))
}

// gifPickerImage serves /img/ paths <w>x<h>/gp<seed>.webp.
func gifPickerImage(path string) ([]byte, string, bool) {
	dims, seed, ok := strings.Cut(path, "/")
	if !ok || !strings.HasPrefix(seed, "gp") || !strings.HasSuffix(seed, ".webp") || strings.Contains(seed, "/") {
		return nil, "", false
	}
	var w, h int
	if n, _ := fmt.Sscanf(dims, "%dx%d", &w, &h); n != 2 || w <= 0 || h <= 0 || w > 2048 || h > 2048 {
		return nil, "", false
	}
	return GIFPickerWebP(w, h), "image/webp", true
}

// GIFPickerText is the text GIFPickerMessage posts (the real one's is the
// GIF's search term).
const GIFPickerText = "test-gif"

// GIFPickerMessage is the GIF picker's message at ts from user, its image
// imageURL (w×h, image_bytes size) with alt text alt.
func GIFPickerMessage(ts, user, imageURL, alt string, w, h, size int) slack.Message {
	block, _ := json.Marshal([]map[string]any{{
		"type": "image", "block_id": "0f4c2a6e-6c1b-4cf5-9a3e-6f0b3c2d1e7a",
		"image_url": imageURL, "alt_text": alt,
		"title":       map[string]any{"type": "plain_text", "text": "GIF", "emoji": true},
		"image_width": w, "image_height": h, "image_bytes": size,
		"fallback":    fmt.Sprintf("%dx%dpx image", w, h),
		"is_animated": true,
	}})
	text, _ := json.Marshal([]map[string]any{{
		"type": "rich_text", "block_id": "gp0",
		"elements": []any{map[string]any{"type": "rich_text_section",
			"elements": []any{map[string]any{"type": "text", "text": GIFPickerText}}}},
	}})
	return slack.Message{Type: "message", TS: ts, User: user, Text: GIFPickerText, Blocks: text,
		Attachments: []slack.Attachment{{ID: 1, Fallback: "shared a GIF", Blocks: block}}}
}
