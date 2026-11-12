package theme

import (
	"math"
	"strings"

	"gioui.org/font"
	"gioui.org/text"
)

// Letter spacing for the fonts Slack tracks (Preferences > Appearance >
// Font).
//
// Slack's Theme component (boot-shared oWug, build d3de3a487b1c) gives
// .use-theme-font these variables when the font is San Francisco Pro
// (System), Segoe UI (System), Roboto Mono, Noto Sans or Meiryo (its "M"):
//
//	--custom-font-tracking-headline:   -0.36pt
//	--custom-font-tracking-title:      -0.43pt
//	--custom-font-tracking-subtitle:   -0.15pt
//	--custom-font-tracking-large-body: -0.15pt
//	--custom-font-tracking-body:        0.06pt
//	--custom-font-tracking-caption:     0.12pt
//	--custom-font-tracking-micro:       0.17pt
//
// and every other font leaves them unset, so `letter-spacing: var(…,
// inherit)` is normal. Slack's CSS pairs each variable with one type
// size: in client-boot-styles and the webapp's application CSS, 972 of the
// caption rules are 13px, 671 of the body rules 15px (and slack-kit's
// body{font-size:15px}), 146 micro 12px, 241 subtitle and 22 large-body
// 18px, 120 title 22px, 41 headline 28px (Slack Kit's design tokens
// --dt_static_type-size-* and -tracking-* say the same). Text at another
// size inherits its parent's letter-spacing, which for most of the UI is
// body's 0.06pt; code inherits too (no c-mrkdwn__code / __pre rule sets
// it), so Clack's monospace runs take body's. Emoji are images in Slack,
// which letter-spacing doesn't touch.
//
// The spacing itself is Gio patch 12 (third_party/gioui.org
// CLACK_PATCHES.md): Blink's per-cluster letter-spacing, ligatures off.

// Tracking values in CSS pt (1pt = 4/3 px).
const (
	trackHeadline = -0.36
	trackTitle    = -0.43
	trackSubtitle = -0.15 // and large-body
	trackBody     = 0.06
	trackCaption  = 0.12
	trackMicro    = 0.17
	pxPerPt       = 4.0 / 3
)

// fontT is font.Font (gio_patch12*.go).
type fontT = font.Font

// TrackingFor is the letter spacing, in sp (CSS px), Slack's CSS gives text
// of font f at size sp under a tracked font (see above).
func TrackingFor(f font.Font, size float32) float32 {
	tf := string(f.Typeface)
	if strings.HasPrefix(tf, EmojiSlotTypeface) {
		return 0
	}
	pt := float32(trackBody)
	if !strings.HasPrefix(tf, MonoTypeface) {
		pt = trackingPt(size)
	}
	return pt * pxPerPt
}

// trackingPt is the tracking variable of a type size, in pt (body's for a
// size off Slack's scale: it inherits).
func trackingPt(size float32) float32 {
	switch int(math.Round(float64(size))) {
	case 12:
		return trackMicro
	case 13:
		return trackCaption
	case 18:
		return trackSubtitle
	case 22:
		return trackTitle
	case 28:
		return trackHeadline
	}
	return trackBody
}

// TrackingVars are the variables as Slack's Theme writes them, for CSS
// that reads them itself (slackc): nil when the UI font isn't tracked.
func TrackingVars() map[string]string {
	if !UITracking() {
		return nil
	}
	return trackingVars
}

var trackingVars = map[string]string{
	"--custom-font-tracking-headline":   "-0.36pt",
	"--custom-font-tracking-title":      "-0.43pt",
	"--custom-font-tracking-subtitle":   "-0.15pt",
	"--custom-font-tracking-large-body": "-0.15pt",
	"--custom-font-tracking-body":       "0.06pt",
	"--custom-font-tracking-caption":    "0.12pt",
	"--custom-font-tracking-micro":      "0.17pt",
}

// UITracking reports that the default UI font (SetDefaultUIFont) is one
// Slack tracks.
func UITracking() bool {
	u := defaultUI.Load()
	return u != nil && u.Tracking
}

// setTracking gives sh u's letter spacing (none for Lato and untracked
// fonts).
func setTracking(sh *text.Shaper, u *UIFont) {
	if u != nil && u.Tracking {
		shaperSetTracking(sh, TrackingFor)
	}
}

// SetTextScale tells both mode shapers the window's pixels per sp, for
// text laid out without Parameters.PxPerSp (richview, the composer); the
// app calls it every frame with the frame's metric.
func (t *Theme) SetTextScale(pxPerSp float32) {
	if pxPerSp == t.textScale {
		return
	}
	t.textScale = pxPerSp
	for _, sh := range t.shapers {
		if sh != nil {
			shaperSetTrackingScale(sh, pxPerSp)
		}
	}
}
