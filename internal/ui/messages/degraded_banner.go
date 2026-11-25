package messages

import (
	"image"
	"image/color"
	"math"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// DegradedBanner is Slack's DegradedBanner (boot-shared 7JQN, namespace
// "ms", build 889ac449a32d): while the realtime socket has trouble
// connecting (core.App.ConnectionTrouble, Slack's troubleConnecting) the
// message pane, the Threads view and the Unreads view draw it in their
// top banners.
//
//   - It reads "Last updated less than a minute ago…" and "Load new
//     messages". It keeps the time it mounted (resetLastUpdatedTs), and
//     every 60 s from the mount (setInterval) the text becomes "Last
//     updated {timeAgo}…" with toTimeAgo of that time ("1 minute ago").
//   - "Load new messages" (data-qa connecting_banner_cta) hides the
//     banner for 10 s (temporarilyHide) and runs the view's loads
//     (loadNewMessages: requestOfflineHistory of the open conversation or
//     the Threads view and of an open thread, requestOfflineCounts and
//     reconnectImmediately). When they're done the time restarts from
//     then and the text is "Last updated {timeAgo}…" again.
//   - The CSS: .p-message_pane__degraded_banner under IA4 is 28px,
//     radius 14, padding 0 16px, 13px bold, nowrap, the background
//     rgba(--sk_foreground_low_solid, 1) and the text
//     rgba(--sk_foreground_max, .7); __reload_cta is the same colour,
//     8px after the text, underlined. In the message pane and Threads the
//     top banners are a box centred under the view (left 50%,
//     translate(-50%), width auto), so the banner is a pill as wide as
//     its text (BannerPill). Unreads' have no IA4 rule: 16px in from each
//     side, so it spans the view and its text ellipsizes, under a 27px
//     rgba(--sk_foreground_min_solid) band (BannerSpan).
//   - Screen readers get the offscreen aria-live line, "… Press F6 to
//     navigate to the status banner and click load new messages" in the
//     desktop app (Aria).
type DegradedBanner struct {
	period      time.Time // the trouble period it was mounted for
	on          bool      // mounted: drawn while trouble lasts
	updated     time.Time // lastUpdatedTs
	timeAgo     string    // the timeAgo state ("" before the first update)
	tick        time.Time // the interval's next run
	hiddenUntil time.Time // temporarilyHide's 10 s
	link        widget.Clickable

	mu     sync.Mutex
	loaded bool // the click's loads finished (set off the UI goroutine)

	// what the last frame drew (tests): the text, the banner's and the
	// link's rects (the caller's coordinates), the aria-live line
	Text, Link string
	R, LinkR   image.Rectangle
	Aria       string
}

// BannerFit is how the view's top banners hold the banner.
type BannerFit int

const (
	BannerPill BannerFit = iota // centred, as wide as its text (message pane, Threads)
	BannerSpan                  // 16px in from each side (Unreads)
)

// Degraded banner timings: Slack's setInterval(…, 6e4) and
// temporarilyHide's setTimeout(…, 1e4). Vars for tests.
var (
	DegradedBannerTick = time.Minute
	DegradedBannerHide = 10 * time.Second
)

var nsMS = i18n.NS("ms")

// Reset unmounts the banner: the view left the screen (Slack's remount of
// a view re-entered starts the banner over: resetLastUpdatedTs).
func (b *DegradedBanner) Reset() {
	b.on, b.timeAgo, b.hiddenUntil, b.period = false, "", time.Time{}, time.Time{}
	b.Text, b.Link, b.R, b.LinkR, b.Aria = "", "", image.Rectangle{}, image.Rectangle{}, ""
}

// Layout draws the banner while trouble holds (since: when the trouble
// going on began, zero while there's none), at the top of a view
// width wide (y: the top banners' top, 14px in the message pane, 5px in
// Threads, 23px in Unreads). load runs the click's loads and calls done
// when they're over (it may return at once and call done later, from any
// goroutine). It reports whether the banner is drawn: the unread pill
// hides under it (.p-message_pane__banner + .p-message_pane__banner) and
// the day pills stop sticking (useStickyDayDividers' !isDegradedMode).
func (b *DegradedBanner) Layout(gtx layout.Context, th *theme.Theme, since time.Time, fit BannerFit, y, width int, load func(done func())) bool {
	now := gtx.Now
	if since.IsZero() || b.on && !b.period.Equal(since) {
		// unmounted (the mutex stays: a load may still hold it); a new
		// trouble period, met by a view that was off screen when the last
		// one ended, mounts it afresh
		b.Reset()
		if since.IsZero() {
			return false
		}
	}
	if !b.on {
		b.on, b.period, b.updated, b.timeAgo, b.tick = true, since, now, "", now.Add(DegradedBannerTick)
	}
	b.mu.Lock()
	loaded := b.loaded
	b.loaded = false
	b.mu.Unlock()
	if loaded { // resetLastUpdatedTs, updateTimeAgoMessage
		b.updated = now
		b.timeAgo = i18n.TimeAgo(i18n.Locale(), b.updated, now, false, false)
	}
	for !now.Before(b.tick) { // the interval
		b.timeAgo = i18n.TimeAgo(i18n.Locale(), b.updated, now, false, false)
		b.tick = b.tick.Add(DegradedBannerTick)
	}
	gtx.Execute(op.InvalidateCmd{At: b.tick})
	if b.link.Clicked(gtx) && now.After(b.hiddenUntil) {
		b.hiddenUntil = now.Add(DegradedBannerHide)
		if load != nil {
			load(func() {
				b.mu.Lock()
				b.loaded = true
				b.mu.Unlock()
			})
		}
	}
	if now.Before(b.hiddenUntil) {
		gtx.Execute(op.InvalidateCmd{At: b.hiddenUntil})
		b.Text, b.Link, b.R, b.LinkR = "", "", image.Rectangle{}, image.Rectangle{}
		return false
	}

	var parts []i18n.Part
	if b.timeAgo == "" {
		parts = nsMS.RT("<div>Last updated less than a minute ago…</div><a>Load new messages</a>")
		b.Aria = nsMS.T("Last updated less than a minute ago… Press F6 to navigate to the status banner and click load new messages")
	} else {
		parts = nsMS.RT("<div>Last updated {timeAgo}…</div><a>Load new messages</a>", "timeAgo", b.timeAgo)
		b.Aria = nsMS.T("Last updated {timeAgo}… Press F6 to navigate to the status banner and click load new messages", "timeAgo", b.timeAgo)
	}
	text, link := "", ""
	for _, p := range parts {
		if p.Tag == "a" {
			link += p.Text
		} else {
			text += p.Text
		}
	}
	bg, ink, band := degradedColors(th.Dark)
	ts := textStyle{size: 13, weight: theme.Bold, col: ink, lineH: 28}
	tw, lw := measure(gtx, th, ts, text).X, measure(gtx, th, ts, link).X
	pad, gap, bh := dp(gtx, 16), dp(gtx, 8), dp(gtx, 28)
	var r image.Rectangle
	switch fit {
	case BannerSpan:
		side := dp(gtx, 16)
		// the band over the banner (:not(:empty):before, top -23px, 27px)
		fill(gtx.Ops, image.Rect(side, y-dp(gtx, 23), width-side, y-dp(gtx, 23)+dp(gtx, 27)), band)
		r = image.Rect(side, y, width-side, y+bh)
		if room := r.Dx() - 2*pad - gap - lw; tw > room { // __banner_text ellipsizes (text-overflow: ellipsis)
			text = bannerEllipsis(gtx, th, ts, text, max(0, room))
			tw = min(measure(gtx, th, ts, text).X, max(0, room))
		}
	default:
		bw := pad + tw + gap + lw + pad
		x := (width - bw) / 2
		r = image.Rect(x, y, x+bw, y+bh)
	}
	rrect(gtx.Ops, r, bh/2, bg)
	// justify-content center: the text and the link in the middle
	cw := tw + gap + lw
	x := r.Min.X + (r.Dx()-cw)/2
	at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect{Max: image.Pt(tw, bh)}.Push(gtx.Ops).Pop()
		return lineBox(gtx, ts, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ts, text) })
	})
	lx := x + tw + gap
	b.LinkR = image.Rect(lx, y, lx+lw, y+bh)
	at(gtx, lx, y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(lw, bh))
		return b.link.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			d := lineBox(gtx, ts, func(gtx layout.Context) layout.Dimensions { return label(gtx, th, ts, link) })
			base := d.Size.Y - d.Baseline
			fill(gtx.Ops, image.Rect(0, base+dp(gtx, 1), lw, base+dp(gtx, 2)), ink)
			pointerCursor(gtx, image.Rect(0, 0, lw, bh))
			return layout.Dimensions{Size: image.Pt(lw, bh)}
		})
	})
	b.Text, b.Link, b.R = text, link, r
	return true
}

// bannerEllipsis is text cut to fit w with "…" after it, as
// text-overflow: ellipsis draws it (whole glyphs only).
func bannerEllipsis(gtx layout.Context, th *theme.Theme, ts textStyle, text string, w int) string {
	rs := []rune(text)
	lo, hi := 0, len(rs) // the longest prefix whose ellipsized form fits
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if measure(gtx, th, ts, string(rs[:mid])+"…").X <= w {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return ""
	}
	return strings.TrimRight(string(rs[:lo]), " ") + "…"
}

// degradedColors are the banner's background (sk_foreground_low_solid),
// its text (sk_foreground_max at .7) and Unreads' band
// (sk_foreground_min_solid).
func degradedColors(dark bool) (bg, ink, band color.NRGBA) {
	return skColor(dark, "--sk_foreground_low_solid", 1, rgb(0xdddddd)), skColor(dark, "--sk_foreground_max", .7, rgb(0x1d1c1d)),
		skColor(dark, "--sk_foreground_min_solid", 1, rgb(0xf8f8f8))
}

// skColor is rgba(var(name), a): Slack's sk_ variable for the theme
// (def when the table lacks it).
func skColor(dark bool, name string, a float64, def color.NRGBA) color.NRGBA {
	if v, ok := css.SlackVars(dark)[name]; ok {
		if c, ok := css.ParseColor("rgb(" + v + ")"); ok {
			def = c
		}
	}
	def.A = uint8(math.Round(float64(def.A) * a))
	return def
}
