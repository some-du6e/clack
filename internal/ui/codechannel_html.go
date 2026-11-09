package ui

// A code channel's html view (Slack's AgentViewTabView, module vxvv):
// Slack fetches client.codeChannels.getView and runs html_content in a
// sandboxed iframe on a per-file origin (HtmlSurfaceIframe, RzYr). Clack
// has no web view and never renders untrusted HTML in-process, so the
// content box shows what Slack's own iframe shows when it can't build its
// sandbox for an agent view, "View is not available in this
// environment.", with the file's "Open in browser" (Slack's file action:
// url_private).

import (
	"context"
	"image"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

type codeHTMLView struct {
	a       *App
	ch      string
	view    core.CodeChannelView
	label   string
	clicks  clickables
	started bool
	done    bool
	err     error
	urlPriv string
	dlURL   string
	agent   string
}

func newCodeHTMLView(a *App, ch string, v core.CodeChannelView, label string) *codeHTMLView {
	return &codeHTMLView{a: a, ch: ch, view: v, label: label, clicks: clickables{}}
}

func (v *codeHTMLView) load() {
	v.started = true
	a := v.a
	if a.data == nil || a.data.core == nil || v.view.FileID == "" {
		v.done, v.err = true, context.Canceled // !fileId: "Unable to load this view."
		return
	}
	ca := a.data.core
	v.agent = ca.AgentName(ca.CodeChannelOf(v.ch))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := ca.CodeChannelHTMLView(ctx, v.ch, v.view.ViewID)
		_, priv, dl, ferr := ca.CodeViewFile(ctx, v.view.FileID)
		a.Post(func() {
			v.done, v.err = true, err
			if ferr == nil {
				v.urlPriv, v.dlURL = priv, dl
			}
			a.Invalidate()
		})
	}()
}

func (v *codeHTMLView) Layout(gtx layout.Context) layout.Dimensions {
	if !v.started {
		v.load()
	}
	a := v.a
	if !v.done {
		return (&codeStatusBody{a: a, loading: true}).Layout(gtx) // "Loading view"
	}
	if v.err != nil {
		return (&codeStatusBody{a: a, text: "Unable to load this view. Please try again later."}).Layout(gtx)
	}
	dark := a.th.Dark
	sz := gtx.Constraints.Max
	// .header__EZe0R: flex-end, gap 12, padding 12; the caption 13px
	// content-ter with margin-right auto
	pad := gtx.Dp(12)
	bh := gtx.Dp(28)
	hh := pad*2 + bh
	if v.agent != "" {
		cap := "HTML file created by @" + v.agent
		at(gtx, image.Pt(pad, pad), image.Pt(sz.X/2, bh), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
				return a.th.Label(gtx, 13, theme.Regular, ccTok(dark, "content-ter"), cap)
			})
		})
	}
	if v.dlURL != "" && v.outlineButton(gtx, "download", "download", "Download", image.Pt(-1, pad), sz.X-pad) {
		openURL(v.dlURL)
	}
	// .content__COVya: 1px otl-ter, radius 12, margin 0 12 12
	box := image.Rect(pad, hh, sz.X-pad, sz.Y-pad)
	theme.Border(gtx, box, gtx.Dp(12), gtx.Dp(1), ccTok(dark, "otl-ter"))
	msg := "View is not available in this environment."
	mw := min(a.th.Measure(gtx, 15, theme.Regular, msg), box.Dx()-gtx.Dp(32))
	cy := box.Min.Y + box.Dy()/2 - gtx.Dp(30)
	at(gtx, image.Pt(box.Min.X+(box.Dx()-mw)/2, cy), image.Pt(mw+1, gtx.Dp(44)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Para(gtx, 15, 22, theme.Regular, ccTok(dark, "content-pry"), msg)
	})
	if v.urlPriv != "" {
		label := "Open in browser"
		bw := gtx.Dp(12)*2 + gtx.Dp(16) + gtx.Dp(4) + a.th.Measure(gtx, 13, theme.Bold, label)
		if v.outlineButton(gtx, "open", "new-window", label, image.Pt(box.Min.X+(box.Dx()-bw)/2, cy+gtx.Dp(22)+gtx.Dp(16)), 0) {
			openURL(v.urlPriv)
		}
	}
	return layout.Dimensions{Size: sz}
}

// outlineButton is <Button type="outline" size="small">: 28px, radius 8,
// 1px otl-sec, 13px bold, a 16px icon 4px before the label. With pos.X <
// 0 it ends at right. It reports a click.
func (v *codeHTMLView) outlineButton(gtx layout.Context, key, icon, label string, pos image.Point, right int) bool {
	a := v.a
	dark := a.th.Dark
	ck := v.clicks.get(key)
	h := gtx.Dp(28)
	lw := a.th.Measure(gtx, 13, theme.Bold, label)
	w := gtx.Dp(12)*2 + gtx.Dp(16) + gtx.Dp(4) + lw
	if pos.X < 0 {
		pos.X = right - w
	}
	at(gtx, pos, image.Pt(w, h), func(gtx layout.Context) layout.Dimensions {
		return clickArea(gtx, ck, func(gtx layout.Context, s image.Point, hov bool) {
			r := image.Rectangle{Max: s}
			theme.RRect(gtx, r, gtx.Dp(8), ccTok(dark, "otl-sec"))
			bg := ccTok(dark, "base-pry")
			if hov {
				bg = ccTok(dark, "base-sec")
			}
			theme.RRect(gtx, r.Inset(gtx.Dp(1)), gtx.Dp(7), bg)
		}, func(gtx layout.Context) layout.Dimensions {
			col := ccTok(dark, "content-pry")
			at(gtx, image.Pt(gtx.Dp(12), (h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return icons.Get(icons.Slack(icon))(gtx, 16, col)
			})
			at(gtx, image.Pt(gtx.Dp(32), 0), image.Pt(lw+1, h), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions { return a.th.Label(gtx, 13, theme.Bold, col, label) })
			})
			return layout.Dimensions{Size: image.Pt(w, h)}
		})
	})
	return clicked(ck)
}
