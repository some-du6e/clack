package ui

import (
	"image"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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
