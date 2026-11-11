package huddleui

import (
	"image"
	"strings"
	"sync"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Sharing your screen, the way Slack's huddle UI does it (build
// 4d0c78085232):
//
//   - HuddleToolbarScreenshareButton (boot-shared Q9Yv) after the camera,
//     in the window's toolbar and the docked bar: the screen icon, or
//     screen-off while you share; "Share your screen", "Stop sharing
//     screen", or "Screen sharing limit reached" once two others share
//     (useIsScreenshareLimitReached: getRemotePeersWithScreenshareEnabled-
//     Count > 1), when it takes no click; lit while you share or the
//     picker is open.
//   - Its click (0S5z useHandleScreenShareButtonClicked): Screen Recording
//     denied (maybeCheckSystemMediaAccess "screen") raises the
//     noSystemScreenShare alert and asks the OS (getScreenThumbnails); the
//     limit does nothing; while sharing it's toggleScreenShare, which
//     stops; otherwise ScreenPickerModal in the window clicked
//     (screenpicker.go). On Wayland Slack skips its picker for the
//     desktop portal's own.
//   - toggleScreenShare (ah2Y): the share starts with the picked source and
//     "Share system audio"; BIqb's border goes around it on macOS and
//     Windows and comes off when it stops.
//   - HuddleMiniPanel (boot-render zuQH) shows while you share
//     (minipanel.go).
//   - The alerts (client-boot vAUZ, boot-shared NoKD): noSystemScreenShare
//     and ScreenShareNotReadableError draw as the huddle's warning banner;
//     screenShareError and ScreenShareNotFoundError have no text in
//     useWarningBannerAlertText, so nothing shows for them.

// The alerts that have text.
const (
	alertNoScreenShare = "noSystemScreenShare"
	alertShareBusy     = "ScreenShareNotReadableError"
)

// shareUI is the controller's screen share state.
type shareUI struct {
	mu      sync.Mutex
	picking bool   // ScreenPickerModal is open
	alert   string // the open alert ("" none)
	last    huddle.ShareState
	shown   string // the source the border is around ("" none)
	panel   *miniPanel
	picker  *screenPicker // the open picker
}

// shareSupported is huddle.ShareSupported (a var for tests).
var shareSupported = huddle.ShareSupported

// waylandShare: Slack's picker is skipped on Wayland
// (window.slackPlatform.isWayland), where the desktop portal picks.
var waylandShare = func() bool { return false }

// shareLimitReached is useIsScreenshareLimitReached.
func shareLimitReached(st huddle.State) bool {
	n := 0
	for _, p := range st.Participants {
		if !p.Self && p.Sharing {
			n++
		}
	}
	return n > 1
}

// shareCtl is the toolbar's screen share button (useScreenShareProps'
// states, Q9Yv's icon and tooltip).
func (c *Controller) shareCtl(st huddle.State) ctl {
	k := ctl{id: "share", icon: "screen", tip: nsHuddles.T("Share your screen")}
	switch {
	case st.Share.State.Sharing():
		k.icon, k.tip, k.on = "screen-off", nsHuddles.T("Stop sharing screen"), true
	case shareLimitReached(st):
		k.dim, k.tip = true, nsHuddles.T("Screen sharing limit reached")
	}
	c.share.mu.Lock()
	if c.share.picking {
		k.on = true
	}
	c.share.mu.Unlock()
	return k
}

// shareClick is useHandleScreenShareButtonClicked. ph is the window the
// button is in (the picker opens there), run its goroutine.
func (c *Controller) shareClick(s *huddle.Session, st huddle.State, ph popups.Host, run func(func())) {
	if s == nil {
		return
	}
	if !huddle.ScreenAccessAllowed() { // maybeCheckSystemMediaAccess("screen") said denied
		c.setShareAlert(alertNoScreenShare)
		huddle.RequestScreenAccess()
		return
	}
	if shareLimitReached(st) && !st.Share.State.Sharing() {
		return
	}
	if st.Share.State.Sharing() {
		c.stopShare(s)
		return
	}
	if waylandShare() { // the portal shows its own picker
		s.StartShare(huddle.ShareSource{ID: "screen:0:0"}, false)
		return
	}
	if ph == nil {
		return
	}
	c.share.mu.Lock()
	if c.share.picking {
		c.share.mu.Unlock()
		return
	}
	c.share.picking = true
	c.share.mu.Unlock()
	run(func() { c.openScreenPicker(s, ph) })
	c.changed()
}

// startShare is toggleScreenShare with a picked source.
func (c *Controller) startShare(s *huddle.Session, src huddle.ShareSource, audio bool) {
	if s == nil {
		return
	}
	c.setShareAlert("")
	huddle.ShowShareBorder(src) // (0, r.X)(mediaSourceId)
	c.share.mu.Lock()
	c.share.shown = src.ID
	c.share.mu.Unlock()
	s.StartShare(src, audio)
	c.changed()
}

// stopShare is toggleScreenShare while sharing: the border comes off.
func (c *Controller) stopShare(s *huddle.Session) {
	huddle.HideShareBorder()
	c.share.mu.Lock()
	c.share.shown = ""
	c.share.mu.Unlock()
	s.StopShare()
	c.changed()
}

// shareTick follows the share's state on every session change: the
// alerts Slack raises for a failed start (ContentShareManager's errors),
// the border, and the presenter's mini panel.
func (c *Controller) shareTick(s *huddle.Session, st huddle.State) {
	sh := &c.share
	sh.mu.Lock()
	prev := sh.last
	sh.last = st.Share.State
	shown := sh.shown
	sh.mu.Unlock()
	if st.Share.State != prev {
		switch st.Share.State {
		case huddle.ShareDenied: // NotAllowedError → onCheckSystemMediaAccess("screen")
			c.setShareAlert(alertNoScreenShare)
			huddle.RequestScreenAccess()
		case huddle.ShareNotReadable:
			c.setShareAlert(alertShareBusy)
		}
	}
	if !st.Share.State.Sharing() && shown != "" {
		huddle.HideShareBorder()
		sh.mu.Lock()
		sh.shown = ""
		sh.mu.Unlock()
	}
	c.syncMiniPanel(s, st)
}

// shareEnded tidies up when the huddle ends.
func (c *Controller) shareEnded() {
	huddle.HideShareBorder()
	c.share.mu.Lock()
	c.share.shown, c.share.alert, c.share.picking, c.share.last = "", "", false, huddle.ShareOff
	p := c.share.panel
	c.share.panel = nil
	c.share.mu.Unlock()
	if p != nil {
		p.close()
	}
}

func (c *Controller) setShareAlert(a string) {
	c.share.mu.Lock()
	c.share.alert = a
	c.share.mu.Unlock()
	c.changed()
}

func (c *Controller) shareAlert() string {
	c.share.mu.Lock()
	defer c.share.mu.Unlock()
	return c.share.alert
}

// alertParts is useWarningBannerAlertText's content for a share alert:
// the text's runs (bold for <strong>, the link for <a>) and where the
// link goes. Slack names itself in it; Clack says its own name.
func alertParts(alert string) (parts []i18n.Part, link string) {
	switch alert {
	case alertNoScreenShare:
		if shareGOOS == "darwin" {
			parts = nsHuddles.RT("<strong>Allow screen recording access in <a>system preferences</a></strong>. If you’ve already granted permissions, quit and reopen Slack.")
			link = screenRecordingSettings
		} else {
			parts = nsHuddles.RT("Please check your system preferences to give screen share access to Slack.")
		}
	case alertShareBusy:
		parts = nsHuddles.RT("You may be screensharing in another app. Try closing other apps or restarting Slack.")
	}
	for i := range parts {
		parts[i].Text = strings.ReplaceAll(parts[i].Text, "Slack", AppName)
	}
	return parts, link
}

// shareGOOS is runtime.GOOS (tests draw the macOS text anywhere).
var shareGOOS = micGOOS

// screenRecordingSettings is the alert's link on macOS: Privacy &
// Security › Screen Recording (what huddle.ScreenAccessSettingsURL is
// there).
const screenRecordingSettings = "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture"

// alertState is one surface's banner widgets.
type alertState struct {
	link, close widget.Clickable
}

// layoutShareAlert is HuddlesAlertBanner for a share alert at width w:
// the warning icon, the text (bold where Slack's <strong> is, the link
// underlined), a close button; it returns its size (nothing when there is
// no alert).
func (c *Controller) layoutShareAlert(gtx C, th *theme.Theme, st *alertState, w int) D {
	alert := c.shareAlert()
	if alert == "" {
		return D{}
	}
	parts, link := alertParts(alert)
	if st.close.Clicked(gtx) {
		c.setShareAlert("")
		return D{}
	}
	if st.link.Clicked(gtx) && link != "" {
		_ = openurl.OpenSettings(link)
	}
	pad, gap := gtx.Dp(10), gtx.Dp(8)
	ic, xs := gtx.Dp(16), gtx.Dp(20)
	tw := max(gtx.Dp(40), w-2*pad-ic-gap-xs-gap)
	var spans []span
	linkText := ""
	for _, p := range parts {
		bold := p.Tag == "strong" || p.Tag == "b" || p.Tag == "a"
		if p.Tag == "a" {
			linkText = p.Text
		}
		spans = append(spans, span{p.Text, bold})
	}
	lines := wrapSpans(gtx, th, 13, spans, tw)
	lineH := gtx.Sp(18)
	h := pad + max(len(lines)*lineH, ic) + pad
	theme.RRect(gtx, image.Rectangle{Max: image.Pt(w, h)}, gtx.Dp(8), hudWarnBg)
	at(gtx, image.Pt(pad, pad+gtx.Dp(1)), image.Pt(ic, ic), func(gtx C) D {
		iconAt(gtx, image.Pt(ic, ic), "warning", 16, hudWarn)
		return D{Size: image.Pt(ic, ic)}
	})
	x0 := pad + ic + gap
	for i, ln := range lines {
		x := x0
		y := pad + i*lineH
		for _, sp := range ln {
			wt := theme.Regular
			col := theme.WithAlpha(hudWhite, 0xd9)
			if sp.Bold {
				wt, col = theme.Bold, hudWhite
			}
			sz := labelSize(gtx, th, 13, wt, sp.Text)
			at(gtx, image.Pt(x, y), sz, func(gtx C) D { return th.Label(gtx, 13, wt, col, sp.Text) })
			if j := strings.Index(sp.Text, linkText); linkText != "" && sp.Bold && j >= 0 {
				lx := x + labelSize(gtx, th, 13, wt, sp.Text[:j]).X
				lw := labelSize(gtx, th, 13, wt, linkText)
				at(gtx, image.Pt(lx, y), lw, func(gtx C) D {
					return button(gtx, &st.link, lw, nil, func(gtx C) D {
						theme.Fill(gtx, image.Rect(0, lw.Y-gtx.Dp(2), lw.X, lw.Y-gtx.Dp(1)), hudWhite)
						return D{Size: lw}
					})
				})
			}
			x += sz.X
		}
	}
	at(gtx, image.Pt(w-pad-xs, pad-gtx.Dp(2)), image.Pt(xs, xs), func(gtx C) D {
		return button(gtx, &st.close, image.Pt(xs, xs), func(gtx C, hov bool) {
			if hov {
				theme.RRect(gtx, image.Rectangle{Max: image.Pt(xs, xs)}, gtx.Dp(4), hudBtn)
			}
		}, func(gtx C) D { iconAt(gtx, image.Pt(xs, xs), "close", 12, hudWhite); return D{} })
	})
	return D{Size: image.Pt(w, h)}
}

// shareAlertHeight measures layoutShareAlert at width w.
func (c *Controller) shareAlertHeight(gtx C, th *theme.Theme, w int) int {
	if c.shareAlert() == "" {
		return 0
	}
	var st alertState
	gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
	m := op.Record(gtx.Ops)
	d := c.layoutShareAlert(gtx, th, &st, w)
	m.Stop()
	return d.Size.Y
}
