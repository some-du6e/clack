package popups

// corex: Slack calls popups needs that internal/core does not offer yet.
//
// Each helper first looks for a typed core method by runtime interface
// assertion (so backend can land it without a popups change), and only then
// falls back to a direct call through core's Slack client. The fallbacks
// were verified read-only against Hack Club (2026-11): gif.search and
// gif.featured (enterprise host), team.prefs.get custom_status_presets (team
// host), users.prefs.get dnd_* working hours. Star/mute use core directly.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
)

var errNoCore = errors.New("not connected")

func cxApp(h Host) (*core.App, error) {
	if h == nil || h.Core() == nil {
		return nil, errNoCore
	}
	return h.Core(), nil
}

// ---- star / mute (core.Star / core.Starred / core.Mute) ----

// cxStar stars or unstars a conversation.
func cxStar(ctx context.Context, h Host, ch string, star bool) error {
	a, err := cxApp(h)
	if err != nil {
		return err
	}
	return a.Star(ctx, ch, star)
}

// cxMute mutes or unmutes a conversation.
func cxMute(ctx context.Context, h Host, ch string, mute bool) error {
	a, err := cxApp(h)
	if err != nil {
		return err
	}
	return a.Mute(ctx, ch, mute)
}

// cxMuted reports a conversation's muted state as core knows it.
func cxMuted(h Host, ch string) bool {
	a, err := cxApp(h)
	if err != nil {
		return false
	}
	if c, ok := a.Conv(ch); ok {
		return c.Muted
	}
	return false
}

// cxStarred reports whether ch is starred (core keeps the stars.list set).
func cxStarred(h Host, ch string) bool {
	a, err := cxApp(h)
	if err != nil {
		return false
	}
	return a.Starred(ch)
}

// cxRefreshStars is a no-op kept for callers: core tracks stars itself
// (stars.list at boot plus star_added / star_removed events).
func cxRefreshStars(ctx context.Context, h Host) error { return nil }

// cxNoteStar is a no-op: core updates its starred set on success.
func cxNoteStar(ch string, star bool) {}

// ---- custom emoji ----

// cxCanAddEmoji reports whether core can add custom emoji (it can).
func cxCanAddEmoji(h Host) bool { return h != nil && h.Core() != nil }

// cxAddEmoji uploads a custom emoji via core.AddEmoji (emoji.add) and turns
// Slack's error codes into the dialog's wording.
func cxAddEmoji(ctx context.Context, h Host, name string, img []byte) error {
	a, err := cxApp(h)
	if err != nil {
		return err
	}
	err = a.AddEmoji(ctx, name, img)
	var se *slack.Error
	if errors.As(err, &se) {
		switch se.Code {
		case "error_name_taken", "error_name_taken_i18n":
			return errors.New("that name is already taken")
		case "too_large", "error_too_big":
			return errors.New("that image is too large")
		case "invalid_name", "error_invalid_name":
			return errors.New("that name isn't allowed")
		case "error_bad_upload", "error_bad_format":
			return errors.New("Slack couldn't read that image")
		case "not_allowed", "restricted_action", "missing_scope", "no_permission":
			return errors.New("you aren't allowed to add emoji here")
		}
	}
	return err
}

// ---- GIFs (Slack's GIF tab: gif.featured / gif.search, Giphy media) ----

// Gif is one GIF result.
type Gif struct {
	ID, Title, Page string // Page: giphy.com page URL
	Preview         string // small animated webp (200w) for the grid
	Still           string // first-frame gif (fallback)
	URL             string // full webp
	W, H            int
}

type cxGifResp struct {
	Gifs []struct {
		ID    string `json:"id"`
		URL   string `json:"url"`
		Desc  string `json:"content_description"`
		Media map[string]struct {
			URL  string `json:"url"`
			Dims []int  `json:"dims"`
		} `json:"media_formats"`
	} `json:"gifs"`
	Meta struct {
		Next string `json:"next_cursor"`
	} `json:"response_metadata"`
}

// cxGifs runs gif.search (query != "") or gif.featured, returning results
// and the next cursor.
func cxGifs(ctx context.Context, h Host, query, cursor string, limit int) ([]Gif, string, error) {
	a, err := cxApp(h)
	if err != nil {
		return nil, "", err
	}
	method := "gif.featured"
	p := url.Values{"limit": {strconv.Itoa(limit)}}
	if q := strings.TrimSpace(query); q != "" {
		method = "gif.search"
		p.Set("query", q)
	}
	if cursor != "" {
		p.Set("cursor", cursor)
	}
	var r cxGifResp
	if err := a.C.Call(ctx, method, p, &r); err != nil {
		return nil, "", err
	}
	out := make([]Gif, 0, len(r.Gifs))
	for _, g := range r.Gifs {
		v := Gif{ID: g.ID, Title: g.Desc, Page: g.URL}
		if m, ok := g.Media["tinygif"]; ok {
			v.Preview = m.URL
			if len(m.Dims) == 2 {
				v.W, v.H = m.Dims[0], m.Dims[1]
			}
		}
		if m, ok := g.Media["tinygifpreview"]; ok {
			v.Still = m.URL
		}
		if m, ok := g.Media["gif"]; ok {
			v.URL = m.URL
			if v.W == 0 && len(m.Dims) == 2 {
				v.W, v.H = m.Dims[0], m.Dims[1]
			}
		}
		if v.Preview == "" {
			v.Preview = v.URL
		}
		if v.Preview != "" {
			out = append(out, v)
		}
	}
	return out, r.Meta.Next, nil
}

// cxSendGif posts a GIF as an image block (with the Giphy page as a
// fallback text link). Prefers core.SendGif when backend adds it.
func cxSendGif(ctx context.Context, h Host, ch, threadTS string, g Gif) error {
	a, err := cxApp(h)
	if err != nil {
		return err
	}
	if s, ok := any(a).(interface {
		SendGif(context.Context, string, string, string, string) error
	}); ok {
		return s.SendGif(ctx, ch, threadTS, g.ID, g.URL)
	}
	title := g.Title
	if title == "" {
		title = "GIF"
	}
	img := g.URL
	if img == "" {
		img = g.Preview
	}
	blocks, _ := json.Marshal([]map[string]any{{
		"type":      "image",
		"image_url": img,
		"alt_text":  title,
		"title":     map[string]any{"type": "plain_text", "text": title},
	}})
	_, err = a.Send(ctx, core.SendOpts{Channel: ch, ThreadTS: threadTS, Text: title, Blocks: blocks})
	return err
}

// ---- status presets (team.prefs.get custom_status_presets) ----

// cxPreset is a workspace status preset.
type cxPreset struct {
	Emoji, Text string
	Canonical   string // text_canonical (index 2; Slack's jk80 reads it as canonical)
	Dur         string // no_expiration | all_day | 30_minutes | 1_hour | 4_hours | this_week | …
}

// cxStatusPresets returns the workspace's "For <team>" presets.
func cxStatusPresets(ctx context.Context, h Host) ([]cxPreset, error) {
	a, err := cxApp(h)
	if err != nil {
		return nil, err
	}
	var r struct {
		Prefs struct {
			Presets [][]string `json:"custom_status_presets"`
		} `json:"prefs"`
	}
	if err := a.C.Call(ctx, "team.prefs.get", nil, &r); err != nil {
		return nil, err
	}
	out := make([]cxPreset, 0, len(r.Prefs.Presets))
	for _, p := range r.Prefs.Presets {
		if len(p) < 4 || p[1] == "" {
			continue
		}
		out = append(out, cxPreset{Emoji: strings.Trim(p[0], ":"), Text: p[1], Canonical: p[2], Dur: p[3]})
	}
	return out, nil
}
