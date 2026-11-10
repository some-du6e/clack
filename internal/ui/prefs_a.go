package ui

import (
	"context"
	"image"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// prefsA is the state of the rows that make Notifications, VIP, Home,
// Appearance, Messages & media and Language & region real (they used to
// be greyed with naNote).
type prefsA struct {
	kw      *widget.Editor // Notifications > Channel keywords
	kwSent  string         // the global_keywords value last written or read
	kwAt    time.Time      // the last keystroke (Slack debounces 1 s)
	kwDirty bool
	kwMax   bool   // over Slack's 100 keywords: not saved
	bounce  string // MacAppearancePref's local "short"/"long"

	vipQ       *widget.Editor // VIP > "Search to add people, apps and workflows"
	vipGen     int            // the search in flight
	vipHits    []core.User
	vipRecs    []core.VIPRecommendation
	vipRecsSet bool     // asked users.priority.getRecommendations
	vipShown   []string // the six recommendations picked when they came (Slack keeps them)

	emails    []core.EmailToken // Messages & media > Bring emails into Slack
	emailsSet bool              // asked users.emailtoken.get
	emailsGot bool              // and it answered
}

// close flushes a keyword edit still waiting on its debounce (Slack's
// NotificationsKeywordsInput flushes on unmount).
func (r *prefsA) close(p *prefsModal) {
	if r.kwDirty && !r.kwMax {
		r.saveKeywords(p)
	}
}

// maxKeywords is NotificationsKeywordsInput's MAX_KEYWORDS_COUNT.
const maxKeywords = 100

// addKeywordWhitespace / stripKeywordWhitespace are Slack's: the field
// shows "a, b" and the pref holds "a,b".
func addKeywordWhitespace(s string) string   { return strings.ReplaceAll(s, ",", ", ") }
func stripKeywordWhitespace(s string) string { return strings.ReplaceAll(s, ", ", ",") }

// keywordCount is validateCsv's count (itIg XT): comma-separated entries.
func keywordCount(csv string) int {
	n := 0
	for _, k := range strings.Split(csv, ",") {
		if strings.TrimSpace(k) != "" {
			n++
		}
	}
	return n
}

func (r *prefsA) saveKeywords(p *prefsModal) {
	v := stripKeywordWhitespace(r.kw.Text())
	r.kwDirty = false
	r.kwSent = v
	p.setNotify("global_keywords", v)
}

// keywordsField is NotificationsKeywordsInput: a text field holding
// global_keywords; edits are written 1 s after the last keystroke with
// users.prefs.setNotifications (setNotificationsGlobalPrefByApi), unless
// there are more than 100 keywords.
func (p *prefsModal) keywordsField(f *prefsForm) {
	r := &p.ra
	gtx, a := f.gtx, p.a
	c := &a.th.C
	if r.kw == nil {
		r.kw = &widget.Editor{SingleLine: true}
		r.kwSent = p.notifyStr("global_keywords")
		r.kw.SetText(addKeywordWhitespace(r.kwSent))
	}
	focused := gtx.Focused(r.kw)
	if cur := p.notifyStr("global_keywords"); !focused && !r.kwDirty && cur != r.kwSent {
		r.kwSent = cur // changed elsewhere (another client)
		r.kw.SetText(addKeywordWhitespace(cur))
	}
	for {
		e, ok := r.kw.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.ChangeEvent); ok {
			r.kwMax = keywordCount(r.kw.Text()) > maxKeywords
			r.kwDirty, r.kwAt = true, time.Now()
		}
	}
	if r.kwDirty && !r.kwMax {
		if wait := time.Second - time.Since(r.kwAt); wait <= 0 {
			r.saveKeywords(p)
		} else {
			gtx.Execute(op.InvalidateCmd{At: r.kwAt.Add(time.Second)})
		}
	}
	f.record("input", "text.global_keywords", r.kw.Text(), false, true)
	bh := gtx.Dp(36)
	at(gtx, image.Pt(0, f.y), image.Pt(f.w, bh), func(gtx layout.Context) layout.Dimensions {
		rr := image.Rectangle{Max: image.Pt(f.w, bh)}
		pc := p.colors() // .c-input_text: base-pry, otl-pry; :focus otl-hgl-1
		theme.RRect(gtx, rr, gtx.Dp(8), pc.field)
		bc := pc.on(a.th, pc.fieldBorder)
		switch {
		case r.kwMax:
			bc = c.Danger
		case focused:
			bc = pc.focus
		}
		theme.Border(gtx, rr, gtx.Dp(8), gtx.Dp(1), bc)
		at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(f.w-gtx.Dp(24), bh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return a.th.Editor(gtx, r.kw, 15, c.Fg, c.Link)
			})
		})
		return layout.Dimensions{Size: rr.Max}
	})
	f.y += bh + gtx.Dp(8)
	if r.kwMax {
		f.record("text", "", "A maximum of 100 keywords are allowed.", false, true)
		at(gtx, image.Pt(0, f.y), image.Pt(f.w, gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
			return a.th.Label(gtx, 13, theme.Regular, c.Danger, "A maximum of 100 keywords are allowed.")
		})
		f.y += gtx.Dp(24)
	}
}

// quickReactionsPref is Slack's QuickReactionsPref: the display checkbox
// (user_customized_quick_reactions_display_feature 1/0), then "My most
// frequently used emoji" / "Custom" and the three custom slots, each
// opening the emoji picker (user_customized_quick_reactions_emoji_N, and
// _has_customized once one is picked). The hover toolbar draws them
// (messages/quickreact.go).
func (p *prefsModal) quickReactionsPref(f *prefsForm) {
	on := p.prefInt("user_customized_quick_reactions_display_feature", 0) == 1
	f.option("pref.quick_reactions", "Show one-click reactions on messages", "", on, false, func() {
		v := 0
		if !on {
			v = 1
		}
		p.setPref("user_customized_quick_reactions_display_feature", v)
	})
	if !on {
		return
	}
	freq := p.prefBool("user_customized_quick_reactions_use_frequently_used_emoji", false)
	hint := ""
	if freq {
		hint = "The emoji shown will change based on your usage."
	}
	f.option("pref.quick_reactions=frequently_used_emoji_quick_reactions", "My most frequently used emoji", hint, freq, true,
		func() { p.setPref("user_customized_quick_reactions_use_frequently_used_emoji", true) })
	hint = ""
	if !freq {
		hint = "Click below to choose the emoji you’d like."
	}
	f.option("pref.quick_reactions=custom", "Custom", hint, !freq, true,
		func() { p.setPref("user_customized_quick_reactions_use_frequently_used_emoji", false) })
	if freq {
		return
	}
	var bs []prefsButton
	for i, def := range [3]string{"white_check_mark", "eyes", "raised_hands"} {
		i := i
		name := strings.Trim(p.prefStr("user_customized_quick_reactions_emoji_"+strconv.Itoa(i+1)), ":")
		if name == "" {
			name = def
		}
		bs = append(bs, prefsButton{"quick_reaction_" + strconv.Itoa(i+1), ":" + name + ":", true, func() {
			if mkEmojiPicker == nil {
				return
			}
			p.a.ShowOverlay(mkEmojiPicker(p.a, p.a.PointerPos(), func(n string) {
				kv := map[string]any{"user_customized_quick_reactions_emoji_" + strconv.Itoa(i+1): strings.Trim(n, ":")}
				if !p.prefBool("user_customized_quick_reactions_has_customized", false) {
					kv["user_customized_quick_reactions_has_customized"] = true
				}
				p.setPrefs(kv)
			}))
		}})
	}
	f.buttons(bs...)
}

// timezonePref is Slack's TimezonePref: "Set time zone automatically"
// (on: the detected zone is written to tz at once, and core keeps it in
// step at sign-in, MaybeUpdateTimezone) and the time zone select with
// Slack's labels, disabled while automatic.
func (p *prefsModal) timezonePref(f *prefsForm) {
	auto := p.prefBool("set_tz_automatically", false)
	f.option("pref.set_tz_automatically", "Set time zone automatically", "", auto, false, func() {
		p.setPref("set_tz_automatically", !auto)
		if !auto {
			if z := core.DetectTimezone(); z != "" {
				p.setPref("tz", z)
			}
		}
	})
	tz := p.prefStr("tz")
	label := core.TimezoneLabel(tz)
	if label == "" {
		label = core.TimezoneLabel(core.DefaultTZ)
	}
	labels := make([]string, len(core.SlackTimezones))
	sel := -1
	for i, z := range core.SlackTimezones {
		labels[i] = z.Label
		if z.ID == tz {
			sel = i
		}
	}
	p.selectList(f, "tz", label, !auto, func(anchor image.Rectangle) {
		p.a.ShowOverlay(popups.NewOptionList(p.a, anchor, labels, sel, func(i int) {
			p.setPref("tz", core.SlackTimezones[i].ID)
		}))
	})
}

// selectList is selectBox (Slack's select) for a long list: a click calls
// open with the box's window rectangle (the listbox scrolls).
func (p *prefsModal) selectList(f *prefsForm, key, value string, enabled bool, open func(anchor image.Rectangle)) {
	gtx, a := f.gtx, f.p.a
	c := &a.th.C
	f.record("select", "sel."+key, value, false, enabled)
	b := p.btn("sel." + key)
	bw, bh := f.w, gtx.Dp(36)
	pc := p.colors() // .c-select_button, as selectBox
	fg := c.Fg
	if !enabled {
		fg = pc.disText
	}
	at(gtx, image.Pt(0, f.y), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
			r := image.Rectangle{Max: s}
			theme.RRect(gtx, r, gtx.Dp(8), pc.field)
			theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), pc.on(a.th, pc.fieldBorder))
		}, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(bw-gtx.Dp(40), bh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 15, theme.Regular, fg, value)
				})
			})
			at(gtx, image.Pt(bw-gtx.Dp(28), (bh-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				ic := pc.fieldFg // .c-select_button__icon: content-pry
				if !enabled {
					ic = fg
				}
				return icons.Get("caret-down")(gtx, 16, ic)
			})
			return layout.Dimensions{Size: image.Pt(bw, bh)}
		})
	})
	if clicked(b) && enabled {
		// the box under the pointer, in window coordinates
		ptr, local := a.PointerPos(), image.Pt(bw/2, bh/2)
		if h := b.History(); len(h) > 0 {
			local = h[len(h)-1].Position
		}
		o := ptr.Sub(local)
		open(image.Rect(o.X, o.Y, o.X+bw, o.Y+bh))
	}
	f.y += bh + gtx.Dp(12)
}

// reminderTimePref is Slack's RemindersPreferences: a half-hour time
// select writing reminder_notification_time ("H:mm", unset: 9:00). Slack's
// servers send day-only reminders (/remind … tomorrow) at that time.
func (p *prefsModal) reminderTimePref(f *prefsForm) {
	f.subheading("Set a default time for reminder notifications:")
	mins := reminderMinutes(p.prefStr("reminder_notification_time"))
	label := uipref.Clock(time.Date(2000, 1, 1, 0, mins, 0, 0, time.UTC))
	p.selectList(f, "reminder_notification_time", label, true, func(anchor image.Rectangle) {
		anchor.Max.X = anchor.Min.X + f.gtx.Dp(200) // Slack's width: 200
		p.a.ShowOverlay(popups.NewTimePicker(p.a, anchor, mins, 0, 30, func(m int) {
			p.setPref("reminder_notification_time", strconv.Itoa(m/60)+":"+pad2(m%60))
		}))
	})
}

// reminderMinutes parses "H:mm" (Slack's default "9:00").
func reminderMinutes(v string) int {
	h, m, ok := strings.Cut(v, ":")
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if !ok || err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 9 * 60
	}
	return hh*60 + mm
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// skinTonePicker is EmojiSkinTonePickerRadioGroup: six radios side by
// side, each with :hand: in its tone (:wave: while hovered), the tone's
// name as its label for assistive tech (skinTones), preferred_skin_tone
// "1"…"6". Slack hides the radio itself (.p-emoji_skin_tone_picker_radio_button
// {opacity:0}): each tone is its label box, .…__container{border:1px solid
// rgb(var(--sk_foreground_low_solid)); border-radius:6px; padding:8px;
// margin-right:12px}, :hover rgba(var(--sk_foreground_min),.04), and
// --selected{border-color:#1264a3} (a literal, the same in dark).
func (p *prefsModal) skinTonePicker(f *prefsForm) {
	gtx, a := f.gtx, p.a
	pc := p.colors()
	tone := p.prefStr("preferred_skin_tone")
	if tone == "" {
		tone = "1"
	}
	es := gtx.Dp(26)
	h := es + 2*gtx.Dp(8) + 2*max(1, gtx.Dp(1))
	cell := h + gtx.Dp(12)
	hover := hex8(0x1d1c1d0a)
	if a.th.Dark {
		hover = hex8(0xe8e8e80a)
	}
	hover = pc.on(a.th, hover)
	for i, l := range skinTones {
		v := strconv.Itoa(i + 1)
		key := "pref.preferred_skin_tone=" + v
		on := tone == v
		f.record("radio", key, l, on, true)
		b := p.btn(key)
		name := "hand"
		if b.Hovered() && !on {
			name = "wave"
		}
		if i > 0 {
			name += "::skin-tone-" + v
		}
		url := ""
		if co := a.Core(); co != nil {
			url, _, _ = co.Emoji(name)
		}
		at(gtx, image.Pt(i*cell, f.y), image.Pt(h, h), func(gtx layout.Context) layout.Dimensions {
			return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
				r := image.Rectangle{Max: s}
				if hov {
					theme.RRect(gtx, r, gtx.Dp(6), hover)
				}
				bc := pc.on(a.th, pc.hr)
				if on {
					bc = hex8(0x1264a3ff)
				}
				theme.Border(gtx, r, gtx.Dp(6), max(1, gtx.Dp(1)), bc)
			}, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt((h-es)/2, (h-es)/2), image.Pt(es, es), func(gtx layout.Context) layout.Dimensions {
					if url != "" {
						if img, ok := a.images.Get(url, es*2); ok {
							return theme.Image(gtx, img, image.Pt(es, es), 0)
						}
					}
					return layout.Dimensions{Size: image.Pt(es, es)}
				})
				return layout.Dimensions{Size: image.Pt(h, h)}
			})
		})
		if clicked(b) && !on {
			p.setPref("preferred_skin_tone", v)
			a.Invalidate()
		}
	}
	f.y += h + gtx.Dp(12)
}

// vipList is PrefsSectionVIP's list half: the member search (members,
// Slackbot, bots and workflows; not you, agents or current VIPs; picking
// one is users.priority.add), then "Added to your VIP list" with Remove,
// and "Recommended" (users.priority.getRecommendations, up to six who
// aren't VIPs yet) with Add and Slack's reason line.
func (p *prefsModal) vipList(f *prefsForm) {
	r := &p.ra
	a := p.a
	c := a.Core()
	if r.vipQ == nil {
		r.vipQ = &widget.Editor{SingleLine: true}
	}
	if !r.vipRecsSet && c != nil {
		r.vipRecsSet = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			recs, err := c.VIPRecommendations(ctx)
			a.Post(func() {
				if err == nil {
					r.vipRecs = recs
					r.vipShown = nil
				}
				a.Invalidate()
			})
		}()
	}
	var vips []string
	if c != nil {
		vips = c.VIPs()
	}
	isVIP := map[string]bool{}
	for _, id := range vips {
		isVIP[id] = true
	}
	for {
		e, ok := r.vipQ.Update(f.gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.ChangeEvent); ok {
			p.searchVIPs(strings.TrimSpace(r.vipQ.Text()))
		}
	}
	p.textField(f, "vip_search", r.vipQ, "Search to add people, apps and workflows")
	if strings.TrimSpace(r.vipQ.Text()) != "" {
		self := a.model().SelfID
		n := 0
		for _, u := range r.vipHits {
			if u.ID == self || isVIP[u.ID] || u.Agent || n == 8 {
				continue
			}
			n++
			u := u
			f.listRow("vip_add."+u.ID, u.Avatar, vipName(u), "Add", func() {
				r.vipQ.SetText("")
				r.vipHits = nil
				p.addVIP(u.ID, vipName(u))
			})
		}
	}
	name := func(id string) (string, string) {
		if c != nil {
			if u, ok := c.User(id); ok {
				return vipName(u), u.Avatar
			}
		}
		ensureUserAsync(a, id)
		return id, ""
	}
	if r.vipShown == nil && r.vipRecs != nil { // the first six who aren't VIPs, as Slack picks them
		r.vipShown = []string{}
		for _, rec := range r.vipRecs {
			if !isVIP[rec.User] && len(r.vipShown) < 6 {
				r.vipShown = append(r.vipShown, rec.User)
			}
		}
	}
	reason := map[string]string{}
	for _, rec := range r.vipRecs {
		reason[rec.User] = rec.Reason
	}
	var recs []string
	for _, id := range r.vipShown {
		if !isVIP[id] {
			recs = append(recs, id)
		}
	}
	if len(vips) == 0 && len(recs) == 0 {
		f.subheading("Added to your VIP list")
		f.muted("No VIPs added yet", false)
		return
	}
	if len(vips) > 0 {
		f.subheading("Added to your VIP list")
		for _, id := range vips {
			id := id
			n, av := name(id)
			f.listRow("vip."+id, av, n, "Remove", func() { a.removeVIP(id, nil) })
		}
	}
	if len(vips) > 0 && len(recs) > 0 {
		f.hr()
	}
	if len(recs) > 0 {
		f.subheading("Recommended")
		for _, id := range recs {
			id := id
			n, av := name(id)
			f.listRow("vip_rec."+id, av, n, "Add", func() { p.addVIP(id, n) })
			if d := vipReason(reason[id]); d != "" {
				f.muted(d, true)
			}
		}
	}
}

// vipReason is 5Ta/'s description for a recommendation.
func vipReason(r string) string {
	switch r {
	case "frequent_collaborator":
		return "Frequent collaborator"
	case "frequently_used":
		return "Frequently used"
	case "manager":
		return "Your manager"
	case "skip_level":
		return "Your manager’s manager"
	}
	return ""
}

func vipName(u core.User) string {
	if u.RealName != "" {
		return u.RealName
	}
	return u.Name
}

// searchVIPs runs the member search for q (the newest answer wins).
func (p *prefsModal) searchVIPs(q string) {
	r := &p.ra
	r.vipGen++
	gen := r.vipGen
	c := p.a.Core()
	if q == "" || c == nil {
		r.vipHits = nil
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		us, err := c.SearchUsers(ctx, q, 20)
		p.a.Post(func() {
			if err == nil && gen == r.vipGen {
				r.vipHits = us
				p.a.Invalidate()
			}
		})
	}()
}

// addVIP is the priority thunk's add: users.priority.add, then Slack's
// toast "Added <name> to VIPs".
func (p *prefsModal) addVIP(id, name string) {
	a := p.a
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := errNoCore
		if c := a.Core(); c != nil {
			err = c.AddVIP(ctx, id)
		}
		a.Post(func() {
			if err != nil {
				a.Toast("Hmm, something went wrong. Try again?")
				return
			}
			a.Toast("Added " + name + " to VIPs")
			a.Invalidate()
		})
	}()
}

// textField is an editable 36px field with a placeholder.
func (p *prefsModal) textField(f *prefsForm, key string, ed *widget.Editor, placeholder string) {
	gtx, a := f.gtx, p.a
	c := &a.th.C
	focused := gtx.Focused(ed)
	f.record("input", "text."+key, ed.Text(), false, true)
	bh := gtx.Dp(36)
	at(gtx, image.Pt(0, f.y), image.Pt(f.w, bh), func(gtx layout.Context) layout.Dimensions {
		rr := image.Rectangle{Max: image.Pt(f.w, bh)}
		pc := p.colors() // .c-input_text
		theme.RRect(gtx, rr, gtx.Dp(8), pc.field)
		bc := pc.on(a.th, pc.fieldBorder)
		if focused {
			bc = pc.focus
		}
		theme.Border(gtx, rr, gtx.Dp(8), gtx.Dp(1), bc)
		at(gtx, image.Pt(gtx.Dp(10), (bh-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("search")(gtx, 16, c.FgMuted)
		})
		at(gtx, image.Pt(gtx.Dp(34), 0), image.Pt(f.w-gtx.Dp(44), bh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				if ed.Len() == 0 && !focused {
					at(gtx, image.Point{}, image.Pt(gtx.Constraints.Max.X, bh), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
							return a.th.Label(gtx, 15, theme.Regular, p.colors().subtext, placeholder)
						})
					})
				}
				return a.th.Editor(gtx, ed, 15, c.Fg, c.Link)
			})
		})
		return layout.Dimensions{Size: rr.Max}
	})
	f.y += bh + gtx.Dp(8)
}

// emailForwarding is EmailForwardingPref: users.emailtoken.get on open;
// each address read-only with Copy ("Send to Slack <address>") and
// "Disable this address" (a confirm, then users.emailtoken.delete); with
// none, "Get a Forwarding Address" (users.emailtoken.create).
func (p *prefsModal) emailForwarding(f *prefsForm) {
	r := &p.ra
	a := p.a
	c := a.Core()
	run := func(do func(ctx context.Context) error) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			err := do(ctx)
			a.Post(func() {
				if err != nil {
					a.Toast("Hmm, something went wrong. Try again?")
				}
				a.Invalidate()
			})
		}()
	}
	if !r.emailsSet && c != nil {
		r.emailsSet = true
		run(func(ctx context.Context) error {
			ts, err := c.EmailTokens(ctx)
			a.Post(func() {
				if r.emails == nil { // a create that landed first wins
					r.emails = ts
				}
				r.emailsGot = true
			})
			return err
		})
	}
	if len(r.emails) == 0 {
		f.button("email_forwarding", "Get a Forwarding Address", c != nil, func() {
			run(func(ctx context.Context) error {
				t, err := c.CreateEmailToken(ctx)
				if err == nil {
					a.Post(func() { r.emails = []core.EmailToken{t} })
				}
				return err
			})
		})
		return
	}
	for _, t := range r.emails {
		t := t
		f.textBox("email_forwarding_address."+t.ID.String(), t.Address, "")
		f.button("email_copy."+t.ID.String(), "Copy", true, func() {
			a.copyText("Send to Slack <" + t.Address + ">")
			a.Toast("Copied!")
		})
		f.linkDo("email_disable."+t.ID.String(), "Disable this address", func() {
			a.ShowOverlay(popups.Confirm(a, "This address will be permanently deleted",
				"The address "+t.Address+" will no longer work. If you’d like, you can always request a new forwarding address.",
				"Disable Address", true, nil, func() {
					run(func(ctx context.Context) error {
						err := c.DeleteEmailToken(ctx, t.ID.String())
						if err == nil {
							a.Post(func() {
								var keep []core.EmailToken
								for _, x := range r.emails {
									if x.ID != t.ID {
										keep = append(keep, x)
									}
								}
								r.emails = keep
							})
						}
						return err
					})
				}))
		})
	}
}

// surpriseRand drives Surprise me (tests seed it).
var surpriseRand = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x5eed))

// importTheme is ThemeImportHexButton: the Import theme modal; a valid
// string becomes the account theme (theme.ImportThemeString) and turns
// window_background_gradients off, like Slack's Apply.
func (p *prefsModal) importTheme() {
	p.a.ShowOverlay(popups.NewThemeImportModal(p.a, p.applyThemeString))
}

func (p *prefsModal) applyThemeString(s string) bool {
	a := p.a
	t, ok := theme.ImportThemeString(a.accountTheme(), s)
	if !ok {
		return false
	}
	a.setAccountTheme(t)
	a.setWindowGradients(false)
	return true
}
