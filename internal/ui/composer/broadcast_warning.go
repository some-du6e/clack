package composer

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The "Notify N people?" dialog: Slack's MessagePaneInputBroadcastWarning
// (vfBE, opened by openBroadcastKeywordWarningDialog), "shouty rooster"
// in Slack's code. Build a84662d2d135, 2026-11-08:
//
//   - the keyword it names: @everyone over @channel over the first one
//     used (core.WarnKeyword);
//   - the count: membershipCounts' member_count at first, then
//     conversations.info {timezone_count, display_counts} replaces it with
//     display_counts.display_counts and adds timezone_count; the title
//     counts everyone but the sender (member count − 1), and names the
//     time zones from 2 up;
//   - on open it writes the user pref last_seen_at_channel_warning =
//     Date.now(), which warn_before_at_channel "once" and "daily" read;
//   - the header is a 226px strip (206px plus 10px padding; #96daea, in
//     dark mode sk_foreground_min_solid) with Slack's rooster illustration
//     (ShoutyRooster, 520×291, animated once on open). The rooster is
//     Slack's artwork and Clack commits no Slack assets (docs/NATIVE.md),
//     so Clack draws its own megaphone on the same strip;
//   - admins get "Pssst, admins… You can control this warning in
//     Workspace settings." (the team's admin/settings#permissions);
//   - "Edit Message" (outline, focused) closes it with the message still
//     in the composer; "Send" ("Schedule Message" for a scheduled send)
//     sends it without asking again. There is no "don't ask again": the
//     team pref decides how often it shows.

// warnCopy is the dialog's text for one case.
type warnCopy struct {
	// Title is the whole title, or with Channel the part before the
	// channel entity (Slack's InlineChannelEntity: icon and name), with
	// After after it ("?"; the locale's order: "{channelName}" may come
	// first).
	Title   string
	Channel bool
	After   string
	// Body is mrkdwn with the keyword as a broadcast mention (<!here>),
	// which draws like Slack's highlighted keyword span.
	Body   string
	Button string
}

// messageInputNS is MessagePaneInputBroadcastWarning's I18n (NrP6,
// message-input).
var messageInputNS = i18n.NS("message-input")

// broadcastWarningCopy is the dialog's render(): kw is the keyword shown,
// mpim a group DM, people the member count minus the sender, timezones
// conversations.info's timezone_count (0: not known yet).
func broadcastWarningCopy(kw string, mpim bool, people, timezones int, scheduled bool) warnCopy {
	t := messageInputNS.T
	keyword := "<!" + kw + ">"
	var c warnCopy
	// a title around {channelName}: the text before it and after it
	inChannel := func(src string) {
		seen := false
		for _, p := range messageInputNS.RT(src, "channelName", "#") {
			switch {
			case p.Arg == "channelName":
				seen = true
			case seen:
				c.After += p.Text
			default:
				c.Title += p.Text
			}
		}
		c.Channel = true
	}
	c.Button = t("Send")
	if !scheduled {
		switch {
		case kw == core.KeywordHere && mpim:
			c.Title = t("Notify everyone currently online?")
		case kw == core.KeywordHere:
			inChannel("Notify everyone currently online in {channelName}?")
		case timezones < 2:
			c.Title = t("Notify {member_count, plural, =1 {# person} other {# people}}?", "member_count", people)
		default:
			c.Title = t("Notify {member_count, plural, =1 {# person} other {# people}} across {timezone_count} time zones?", "member_count", people, "timezone_count", timezones)
		}
		if kw == core.KeywordHere {
			c.Body = t("Using {keyword} pings everyone in the channel who’s currently online. It’s a bit noisy! Are you sure you want to send this?", "keyword", keyword)
		} else {
			c.Body = t("Using {keyword} pings everyone in the channel. It’s rather noisy! Are you sure you want to send this?", "keyword", keyword)
		}
		return c
	}
	c.Button = t("Schedule Message")
	switch {
	case kw == core.KeywordHere && mpim:
		c.Title = t("Notify everyone who’ll be online?")
	case kw == core.KeywordHere:
		inChannel("Notify everyone who’ll be online in {channelName}?")
	case timezones < 2 && mpim:
		c.Title = t("Notify everyone?")
	case timezones < 2:
		inChannel("Notify everyone in {channelName}?")
	default:
		c.Title = t("Notify everyone in this channel across {timezone_count} timezones?", "timezone_count", timezones)
	}
	switch {
	case kw == core.KeywordHere && mpim:
		c.Body = t("Using {keyword} will notify everyone who’s online at the time this message is sent. It can be a bit noisy — please confirm that this is what you’d like to do.", "keyword", keyword)
	case kw == core.KeywordHere:
		c.Body = t("Using {keyword} will notify everyone in the channel who’s online at the time this message is sent. It can be a bit noisy — please confirm that this is what you’d like to do.", "keyword", keyword)
	case mpim:
		c.Body = t("Using {keyword} will notify {member_count, plural, =1 {# person} other {# people}} when this message sends. This type of mention can be a bit noisy — please confirm that this is what you’d like to do.", "keyword", keyword, "member_count", people)
	default:
		c.Body = t("Using {keyword} will notify about {member_count, plural, =1 {# person} other {# people}} when this message sends (the number of members can change day by day). This type of mention can be a bit noisy — please confirm that this is what you’d like to do.", "keyword", keyword, "member_count", people)
	}
	return c
}

// adminNote is the admins' line, with the settings link as mrkdwn.
func adminNote(settingsURL string) string {
	link := messageInputNS.T("Workspace settings")
	if settingsURL != "" {
		link = "<" + settingsURL + "|" + link + ">"
	}
	return messageInputNS.T("Pssst, admins… You can control this warning in {workspace_settings_link}.", "workspace_settings_link", link)
}

// plural is ICU's {n, plural, =1 {# one} other {# other}} with en-US
// digit grouping.
func plural(n int, one, other string) string {
	if n == 1 {
		return "1 " + one
	}
	return groupDigits(n) + " " + other
}

func groupDigits(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// confirmBroadcast asks through the dialog and waits for the answer (off
// the UI goroutine): true for Send, false when it was closed.
func (c *Composer) confirmBroadcast(ctx context.Context, ask broadcastAsk) bool {
	ans := make(chan bool, 1)
	c.later(func() {
		openBroadcastWarning(c.h, c.api, ask, func(ok bool) { ans <- ok })
	})
	select {
	case ok := <-ans:
		return ok
	case <-ctx.Done():
		return false
	}
}

// broadcastWarning is the open dialog.
type broadcastWarning struct {
	h     Host
	api   coreAPI
	ask   broadcastAsk
	conv  core.Conv
	kw    string
	admin bool
	url   string
	modal *popups.Modal

	mu                 sync.Mutex
	members, timezones int // members counts the sender (Slack's memberCount)
	sent               bool

	body, note *richview.View
	bodySrc    string
}

// openBroadcastWarning shows the dialog (UI goroutine); done gets the
// answer once.
func openBroadcastWarning(h Host, api coreAPI, ask broadcastAsk, done func(ok bool)) *broadcastWarning {
	d := &broadcastWarning{h: h, api: api, ask: ask, kw: core.WarnKeyword(ask.Used)}
	d.conv, _ = api.conv(ask.Channel)
	d.members = api.memberCount(ask.Channel)
	d.admin, d.url = api.broadcastWarningAdmin()
	cp := d.copy()
	icon := "hash"
	if d.conv.Kind == "private" {
		icon = "lock"
	}
	edit := &popups.ModalButton{Label: messageInputNS.T("Edit Message"), Kind: popups.BtnOutline}
	opts := popups.ModalOpts{
		TitleFunc: func() string { return d.copy().Title },
		Body:      d.layoutBody,
		Hero:      d.layoutHero,
		HeroH:     226,
		// MessagePaneInputBroadcastWarning: "Edit Message" has autoFocus,
		// so Enter or Space right after the composer's Enter goes back to
		// editing; Send needs Tab (or a click)
		AutoFocus: edit,
		Buttons: []*popups.ModalButton{
			edit,
			{Label: cp.Button, Kind: popups.BtnPrimary, Do: func() bool { d.setSent(); return true }},
		},
		OnClose: func() {
			d.mu.Lock()
			sent := d.sent
			d.mu.Unlock()
			done(sent)
		},
	}
	if cp.Channel {
		opts.TitleIcon, opts.TitleAfter = icon, d.conv.Name+cp.After
	}
	d.modal = popups.NewModal(h, opts)
	h.ShowOverlay(d.modal)
	if onBroadcastWarning != nil {
		onBroadcastWarning(d)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = api.markBroadcastWarningSeen(ctx)
		if m, tz, err := api.broadcastAudience(ctx, ask.Channel); err == nil {
			d.mu.Lock()
			if m > 0 {
				d.members = m
			}
			d.timezones = tz
			d.mu.Unlock()
			h.Invalidate()
		}
	}()
	return d
}

// onBroadcastWarning sees each dialog as it opens (tests).
var onBroadcastWarning func(d *broadcastWarning)

func (d *broadcastWarning) setSent() {
	d.mu.Lock()
	d.sent = true
	d.mu.Unlock()
}

// choose answers as the buttons do: Send, or Edit Message.
func (d *broadcastWarning) choose(send bool) {
	if send {
		d.setSent()
	}
	d.modal.Dismiss()
}

func (d *broadcastWarning) copy() warnCopy {
	d.mu.Lock()
	people, tz := d.members-1, d.timezones
	d.mu.Unlock()
	return broadcastWarningCopy(d.kw, d.conv.Kind == "mpim", people, tz, d.ask.Scheduled)
}

func (d *broadcastWarning) layoutBody(gtx layout.Context) layout.Dimensions {
	th := d.h.Theme()
	st := warningStyle(th, d.h.Images())
	if src := d.copy().Body; d.body == nil || src != d.bodySrc {
		d.bodySrc = src
		d.body = richview.New(richtext.Doc{Blocks: richtext.FromMrkdwn(d.api.resolver(), src)}, richview.Options{})
	}
	if d.admin && d.note == nil {
		d.note = richview.New(richtext.Doc{Blocks: richtext.FromMrkdwn(d.api.resolver(), adminNote(d.url))}, richview.Options{})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return d.body.Layout(gtx, st, warningLinks{})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if d.note == nil {
				return layout.Dimensions{}
			}
			return layout.Inset{Top: 16}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return d.note.Layout(gtx, st, warningLinks{})
			})
		}),
	)
}

// The header strip's colours (p-message_pane_modal_rooster__header--animation)
// and Clack's megaphone on it.
var (
	heroBg     = color.NRGBA{R: 0x96, G: 0xda, B: 0xea, A: 0xff}
	heroBgDark = color.NRGBA{R: 34, G: 37, B: 41, A: 0xff} // rgba(var(--sk_foreground_min_solid)) in dark mode
	heroDisc   = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	heroInk    = color.NRGBA{R: 0x92, G: 0x1d, B: 0x21, A: 0xff}
)

// layoutHero draws the header strip: a white disc with a megaphone in
// the middle.
func (d *broadcastWarning) layoutHero(gtx layout.Context) layout.Dimensions {
	sz := gtx.Constraints.Max
	bg := heroBg
	if d.h.Theme().Dark {
		bg = heroBgDark
	}
	theme.Fill(gtx, image.Rectangle{Max: sz}, bg)
	r := gtx.Dp(70)
	c := image.Pt(sz.X/2, sz.Y/2)
	theme.RRect(gtx, image.Rect(c.X-r, c.Y-r, c.X+r, c.Y+r), r, heroDisc)
	isz := gtx.Dp(88)
	st := op.Offset(image.Pt(c.X-isz/2, c.Y-isz/2)).Push(gtx.Ops)
	g := gtx
	g.Constraints = layout.Exact(image.Pt(isz, isz))
	icons.Get("megaphone-filled")(g, 88, heroInk)
	st.Pop()
	return layout.Dimensions{Size: sz}
}

// warningLinks opens the settings link; mentions do nothing here.
type warningLinks struct{}

func (warningLinks) OpenURL(u string) {
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
		_ = openurl.Open(u)
	}
}
func (warningLinks) OpenUser(string, image.Point)      {}
func (warningLinks) OpenChannel(string)                {}
func (warningLinks) OpenUsergroup(string, image.Point) {}
func (warningLinks) OpenImage(richtext.Image)          {}
func (warningLinks) OpenFile(*richtext.File)           {}
func (warningLinks) Button(richtext.Button)            {}

var (
	warnStyleMu sync.Mutex
	warnStyles  = map[*theme.Theme]*warnStyleEntry{}
)

type warnStyleEntry struct {
	st         *richview.Style
	dark, init bool
}

// warningStyle is a richview.Style for modal text (blockkit's styleFor).
func warningStyle(th *theme.Theme, images *imgcache.Cache) *richview.Style {
	warnStyleMu.Lock()
	defer warnStyleMu.Unlock()
	e := warnStyles[th]
	if e == nil {
		var imgs richview.Images
		if images != nil {
			imgs = images
		}
		e = &warnStyleEntry{st: richview.NewStyle(th.Shaper, imgs, richview.LightPalette())}
		e.st.Face = theme.Typeface
		e.st.EmojiURL = th.EmojiURL
		warnStyles[th] = e
	}
	if th.Dark != e.dark || !e.init {
		e.dark, e.init = th.Dark, true
		e.st.Embolden = theme.BodyEmboldenFor(theme.LightPalette.Fg)
		e.st.LightTextFade = 0
		if th.Dark {
			e.st.SetPalette(richview.DarkPalette())
			e.st.Embolden = theme.BodyEmboldenFor(theme.DarkPalette.Fg)
			e.st.LightTextFade = 0.235
		} else {
			e.st.SetPalette(richview.LightPalette())
		}
	}
	return e.st
}
