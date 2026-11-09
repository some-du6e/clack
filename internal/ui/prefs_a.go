package ui

import (
	"image"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
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
		theme.RRect(gtx, rr, gtx.Dp(8), c.Bg)
		bc := c.BorderStrong
		switch {
		case r.kwMax:
			bc = c.Danger
		case focused:
			bc = c.Link
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
	fg := c.Fg
	if !enabled {
		fg = c.FgSubtle
	}
	at(gtx, image.Pt(0, f.y), image.Pt(bw, bh), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, b, func(gtx layout.Context, s image.Point, hov bool) {
			r := image.Rectangle{Max: s}
			theme.RRect(gtx, r, gtx.Dp(8), c.Bg)
			bc := c.BorderStrong
			if hov && enabled {
				bc = c.Fg
			}
			theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), bc)
		}, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(bw-gtx.Dp(40), bh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
					return a.th.Label(gtx, 15, theme.Regular, fg, value)
				})
			})
			at(gtx, image.Pt(bw-gtx.Dp(28), (bh-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("caret-down")(gtx, 16, fg)
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
