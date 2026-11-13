package huddleui

import (
	"context"
	"errors"
	"image"
	"net/url"
	"sort"
	"sync"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The huddle's theme behind the stage: Slack's HuddleBackground
// (gantry-v2-async-client-boot-shared w33Y), build 274f7c7f1b69.
//
//   - The theme is the room's background_id (client-boot qY8a, the
//     updateHuddle reducer in HtDl): everyone in the huddle sees the one
//     somebody chose with ⋯ › Choose a huddle theme. With none set the
//     stage shows no picture: there is no client-side default.
//   - maybeFetchBackgrounds (5BO1) asks huddles.listBackgrounds once per
//     huddle (hasFetchedBackgrounds; a failure clears it so the next try
//     asks again) and AUOM's K4 turns the id into the theme's url (the
//     preview_url only in the mini panel).
//   - The picture is .p-huddle_background: object-fit cover, in the
//     compact window's .p-huddle_background__container--compact_mode
//     (border-radius 8px, margin 0 4px, height calc(100% - 56px)), and it
//     fades in when it has loaded (huddle-background-fade-in: .65s
//     ease-out, opacity 0→1, scale 1.1→1). A theme being swapped keeps
//     the old picture until the new one has loaded.

// bgTheme is one huddles.listBackgrounds entry.
type bgTheme struct {
	ID, Name, URL, PreviewURL string
	Category                  string // category_name
}

// bgCatalog is the themes, fetched once per huddle.
type bgCatalog struct {
	mu      sync.Mutex
	room    string // the huddle the fetch ran for
	busy    bool
	retryAt time.Time // after a failed fetch
	byID    map[string]bgTheme
}

// bgRetry is how long a failed huddles.listBackgrounds waits before the
// next frame asks again (Slack asks on the next mount).
const bgRetry = 30 * time.Second

// themeURL is the picture for the room's theme ("" while the themes
// aren't known, or with none set). It starts the fetch when it's due.
//
// Slack fetches the themes whenever HuddleBackground mounts; Clack asks
// only once a theme is set, the one time it needs them (it has no "Choose
// a huddle theme" dialog to list them in).
func (c *Controller) themeURL(room, id string) string {
	if room == "" || id == "" {
		return ""
	}
	b := &c.bg
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.room != room {
		b.room, b.byID, b.retryAt = room, nil, time.Time{}
	}
	if b.byID == nil && !b.busy && time.Now().After(b.retryAt) {
		if cl := c.client(); cl != nil {
			b.busy = true
			go c.fetchThemes(room)
		}
	}
	return b.byID[id].URL
}

// fetchThemes is maybeFetchBackgrounds: huddles.listBackgrounds.
func (c *Controller) fetchThemes(room string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _ = c.loadThemes(ctx, room)
}

// themes are the huddle themes for plugins' maybeFetchBackgrounds: the
// fetched ones, else a fetch now.
func (c *Controller) themes(ctx context.Context, room string) ([]bgTheme, error) {
	b := &c.bg
	b.mu.Lock()
	var have []bgTheme
	if b.room == room && b.byID != nil {
		for _, t := range b.byID {
			have = append(have, t)
		}
	}
	b.mu.Unlock()
	if have != nil {
		sort.Slice(have, func(i, j int) bool { return have[i].ID < have[j].ID })
		return have, nil
	}
	return c.loadThemes(ctx, room)
}

// loadThemes asks huddles.listBackgrounds and keeps the answer for room.
func (c *Controller) loadThemes(ctx context.Context, room string) ([]bgTheme, error) {
	var res struct {
		Backgrounds []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			URL        string `json:"url"`
			PreviewURL string `json:"preview_url"`
			Category   string `json:"category_name"`
		} `json:"backgrounds"`
	}
	cl := c.client()
	if cl == nil {
		return nil, errors.New("huddle: no Slack client")
	}
	err := cl.Call(ctx, "huddles.listBackgrounds", url.Values{}, &res)
	var out []bgTheme
	for _, t := range res.Backgrounds {
		out = append(out, bgTheme{ID: t.ID, Name: t.Name, URL: t.URL, PreviewURL: t.PreviewURL, Category: t.Category})
	}
	b := &c.bg
	b.mu.Lock()
	b.busy = false
	if b.room == room {
		if err != nil {
			b.retryAt = time.Now().Add(bgRetry)
		} else {
			b.byID = map[string]bgTheme{}
			for _, t := range out {
				b.byID[t.ID] = t
			}
		}
	}
	b.mu.Unlock()
	if err == nil {
		c.changed()
	}
	return out, err
}

// bgShown is what the window draws: the picture last loaded (k in
// HuddleBackground) and when, for the fade.
type bgShown struct {
	url string
	at  time.Time
}

// bgFade is huddle-background-fade-in's .65s.
const bgFade = 650 * time.Millisecond

// themeSize is the decode size for a theme picture covering r: the next
// 512px step (one cache entry across small resizes), at most 2048.
func themeSize(r image.Rectangle) int {
	n := max(r.Dx(), r.Dy())
	return min(2048, (n+511)/512*512)
}

// layoutTheme draws the room's theme in r (HuddleBackground). It reports
// whether a picture was drawn.
func (w *window) layoutTheme(gtx C, room, id string, r image.Rectangle, rad int) bool {
	want := w.c.themeURL(room, id)
	imgs := w.c.h.Images()
	px := themeSize(r)
	if want == "" {
		w.theme = bgShown{}
	} else if want != w.theme.url {
		if _, ok := imgs.Get(want, px); ok { // the new picture replaces the old once loaded
			w.theme = bgShown{url: want, at: gtx.Now}
		}
	}
	if w.theme.url == "" || r.Empty() {
		return false
	}
	img, ok := imgs.Get(w.theme.url, px)
	if !ok {
		return false
	}
	t := float32(1)
	if age := gtx.Now.Sub(w.theme.at); age < bgFade {
		t = float32(age) / float32(bgFade)
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(16 * time.Millisecond)})
	}
	e := 1 - (1-t)*(1-t) // CSS ease-out, close enough at .65s
	defer op.Offset(r.Min).Push(gtx.Ops).Pop()
	defer clip.UniformRRect(image.Rectangle{Max: r.Size()}, rad).Push(gtx.Ops).Pop()
	defer paint.PushOpacity(gtx.Ops, e).Pop()
	sc := 1.1 - 0.1*e
	ctr := layout.FPt(r.Size()).Div(2)
	defer op.Affine(f32.Affine2D{}.Scale(ctr, f32.Pt(sc, sc))).Push(gtx.Ops).Pop()
	theme.Image(gtx, img, r.Size(), 0)
	return true
}
