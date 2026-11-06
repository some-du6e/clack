package popups

// corex: Slack calls popups needs that internal/core does not offer yet.
//
// Each helper first looks for a typed core method by runtime interface
// assertion (so backend can land it without a popups change), and only then
// falls back to a direct call through core's Slack client. The fallbacks
// were verified read-only against Hack Club (2026-11): gif.search and
// gif.featured (enterprise host), team.prefs.get custom_status_presets (team
// host), users.prefs.get dnd_* working hours. Remove a fallback once its core
// method exists.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

var errNoCore = errors.New("not connected")

func cxApp(h Host) (*core.App, error) {
	if h == nil || h.Core() == nil {
		return nil, errNoCore
	}
	return h.Core(), nil
}

// ---- star / mute ----

// cxStar stars or unstars a conversation.
func cxStar(ctx context.Context, h Host, ch string, star bool) error {
	a, err := cxApp(h)
	if err != nil {
		return err
	}
	if s, ok := any(a).(interface {
		Star(context.Context, string, bool) error
	}); ok {
		return s.Star(ctx, ch, star)
	}
	m := "stars.remove"
	if star {
		m = "stars.add"
	}
	return a.C.Call(ctx, m, url.Values{"channel": {ch}}, nil)
}

// cxMute mutes or unmutes a conversation.
func cxMute(ctx context.Context, h Host, ch string, mute bool) error {
	a, err := cxApp(h)
	if err != nil {
		return err
	}
	if s, ok := any(a).(interface {
		Mute(context.Context, string, bool) error
	}); ok {
		return s.Mute(ctx, ch, mute)
	}
	// verified in the web client: per-channel notification prefs.
	prefs, _ := json.Marshal([]map[string]string{{"name": "muted", "value": strconv.FormatBool(mute)}})
	return a.C.Call(ctx, "users.prefs.setNotifications", url.Values{
		"global": {"false"}, "channel_ids": {ch}, "prefs": {string(prefs)}, "sync": {"false"},
	}, nil)
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

// starred caches stars.list (team host; verified): channel ids the viewer
// starred. Refreshed in the background when a channel menu opens.
var (
	starMu  sync.Mutex
	starSet map[string]bool
)

// cxStarred reports whether ch is starred (from the last stars.list).
func cxStarred(h Host, ch string) bool {
	a, err := cxApp(h)
	if err == nil {
		if s, ok := any(a).(interface{ Starred(string) bool }); ok {
			return s.Starred(ch)
		}
	}
	starMu.Lock()
	defer starMu.Unlock()
	return starSet[ch]
}

// cxRefreshStars reloads the starred set (call off the UI goroutine).
func cxRefreshStars(ctx context.Context, h Host) error {
	a, err := cxApp(h)
	if err != nil {
		return err
	}
	var r struct {
		Items []struct {
			Type    string `json:"type"`
			Channel string `json:"channel"`
		} `json:"items"`
	}
	if err := a.C.Call(ctx, "stars.list", url.Values{"count": {"200"}}, &r); err != nil {
		return err
	}
	set := make(map[string]bool, len(r.Items))
	for _, it := range r.Items {
		if it.Channel != "" && (it.Type == "channel" || it.Type == "group" || it.Type == "im") {
			set[it.Channel] = true
		}
	}
	starMu.Lock()
	starSet = set
	starMu.Unlock()
	return nil
}

// cxNoteStar records a successful star/unstar locally.
func cxNoteStar(ch string, star bool) {
	starMu.Lock()
	if starSet == nil {
		starSet = map[string]bool{}
	}
	starSet[ch] = star
	starMu.Unlock()
}

// ---- custom emoji ----

// cxCanAddEmoji reports whether core can add custom emoji.
func cxCanAddEmoji(h Host) bool {
	a, err := cxApp(h)
	if err != nil {
		return false
	}
	_, ok := any(a).(interface {
		AddEmoji(context.Context, string, []byte) error
	})
	return ok
}

// cxAddEmoji uploads a custom emoji (emoji.add needs a multipart upload,
// which only core can do).
func cxAddEmoji(ctx context.Context, h Host, name string, img []byte) error {
	a, err := cxApp(h)
	if err != nil {
		return err
	}
	if s, ok := any(a).(interface {
		AddEmoji(context.Context, string, []byte) error
	}); ok {
		return s.AddEmoji(ctx, name, img)
	}
	return errors.New("adding emoji needs core support (emoji.add)")
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
		out = append(out, cxPreset{Emoji: strings.Trim(p[0], ":"), Text: p[1], Dur: p[3]})
	}
	return out, nil
}

// ---- working hours (users.prefs dnd_*) ----

// cxHours are notification working hours: notifications are allowed from
// Start to End (Slack stores the inverse, dnd_end_hour → dnd_start_hour).
type cxHours struct {
	Enabled    bool
	Days       string // every_day | weekdays | custom
	Start, End string // "H:MM" 24h
	// automatic statuses
	HuddleStatus, FocusStatus, OutsideStatus bool
}

// cxWorkingHours reads the working-hours prefs.
func cxWorkingHours(ctx context.Context, h Host) (cxHours, error) {
	a, err := cxApp(h)
	if err != nil {
		return cxHours{}, err
	}
	raw, err := a.C.Prefs(ctx)
	if err != nil {
		return cxHours{}, err
	}
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(raw[k], &s)
		return s
	}
	boolean := func(k string, def bool) bool {
		var b bool
		if v, ok := raw[k]; ok && json.Unmarshal(v, &b) == nil {
			return b
		}
		return def
	}
	hr := cxHours{
		Enabled: boolean("dnd_enabled", true), Days: str("dnd_days"),
		Start: str("dnd_end_hour"), End: str("dnd_start_hour"),
		HuddleStatus: boolean("huddles_set_status", true), FocusStatus: boolean("focus_mode_set_status", true),
		OutsideStatus: boolean("outside_working_hours_set_status", false),
	}
	if hr.Days == "" {
		hr.Days = "every_day"
	}
	return hr, nil
}

// cxSetPrefs writes user prefs (users.prefs.set). Prefers core.SetPrefs.
func cxSetPrefs(ctx context.Context, h Host, prefs map[string]string) error {
	a, err := cxApp(h)
	if err != nil {
		return err
	}
	if s, ok := any(a).(interface {
		SetPrefs(context.Context, map[string]string) error
	}); ok {
		return s.SetPrefs(ctx, prefs)
	}
	b, _ := json.Marshal(prefs)
	return a.C.Call(ctx, "users.prefs.set", url.Values{"prefs": {string(b)}}, nil)
}

// cxSetWorkingHours saves working hours and the automatic-status toggles.
func cxSetWorkingHours(ctx context.Context, h Host, hr cxHours) error {
	return cxSetPrefs(ctx, h, map[string]string{
		"dnd_enabled":                      strconv.FormatBool(hr.Enabled),
		"dnd_days":                         hr.Days,
		"dnd_end_hour":                     hr.Start,
		"dnd_start_hour":                   hr.End,
		"huddles_set_status":               strconv.FormatBool(hr.HuddleStatus),
		"focus_mode_set_status":            strconv.FormatBool(hr.FocusStatus),
		"outside_working_hours_set_status": strconv.FormatBool(hr.OutsideStatus),
	})
}

// ---- scheduled status (Out of office) ----

// cxCanScheduleStatus reports whether core can schedule a status that
// starts later (the web client's "Schedule for later").
func cxCanScheduleStatus(h Host) bool {
	a, err := cxApp(h)
	if err != nil {
		return false
	}
	_, ok := any(a).(interface {
		ScheduleStatus(context.Context, string, string, int64, int64) error
	})
	return ok
}

// cxScheduleStatus sets a status from start to end (unix seconds). A start
// in the past or now is a normal status with an expiration.
func cxScheduleStatus(ctx context.Context, h Host, text, emoji string, start, end int64, now int64) error {
	a, err := cxApp(h)
	if err != nil {
		return err
	}
	if start <= now+60 {
		return a.SetStatus(ctx, text, emoji, end)
	}
	if s, ok := any(a).(interface {
		ScheduleStatus(context.Context, string, string, int64, int64) error
	}); ok {
		return s.ScheduleStatus(ctx, text, emoji, start, end)
	}
	return errors.New("scheduling a status for later needs core support")
}
