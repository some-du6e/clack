package ui

import (
	"context"
	"image"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/canvasview"
	"github.com/some-du6e/clack/internal/ui/listview"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/views"
)

// tabPeekBodyMore is the rest of useTabPeek's switch (tab_slug_peek.go).
func (a *App) tabPeekBodyMore(p *slugPeek, ch string, t core.SlugTab) tabPeekBody {
	switch t.Type {
	case "canvas":
		return &canvasTabPeek{a: a, ch: ch, tab: t}
	case "files":
		return &filesTabPeek{v: views.NewChannelFilesPeek(a, ch)}
	case "list":
		return &listTabPeek{a: a, p: p, ch: ch, tab: t}
	case "workflows":
		return a.workflowsPeek(p, ch)
	}
	// "channel", "pins" and the types without a case render an empty
	// fragment. "bookmarks" (PinsTabPeekV2: the Pins tab's rows) and
	// "folder" (FolderTabPeek: the bookmark folder's rows) are those tabs'
	// own views in preview, and Clack has neither view yet (the Pins and
	// bookmark-folder tabs open their bookmarks menu), so they show
	// nothing either.
	return nil
}

// unmounter is a body with work when the peek goes away.
type unmounter interface{ unmount() }

// closed is the peek's unmount.
func (p *slugPeek) closed() {
	if u, ok := p.body.(unmounter); ok {
		u.unmount()
	}
}

// ---- CanvasTabPeek ----

// canvasTabPeek is CanvasTabPeek (q5Cv): nothing without the tab's
// data.fileId or for an empty canvas (useQuipDoc's isEmpty); else
// ChannelTabPeek at its default height around .container__GF3bq (a
// column filling it, the canvas in .content__JjzTO: flex 1, clipped)
// with the canvas embed in preview (QuipEmbedWithEntityId isPreview:
// editing disabled, no toolbar, no live watcher; canvasview.Peek). The
// embed marks the canvas read when it unmounts after being up 6s
// (markFileRead, reason "mark-read-preview": files.markRead when the
// file's created time isn't after now). A knowledge canvas's "Auto-
// generated from Structured Intelligence" banner (channel_knowledge_canvas)
// isn't drawn: no such canvases on the account.
type canvasTabPeek struct {
	a    *App
	ch   string
	tab  core.SlugTab
	v    *canvasview.View
	open time.Time
}

func (b *canvasTabPeek) height(gtx layout.Context) (int, bool) {
	if b.tab.FileID == "" {
		return 0, false
	}
	if b.v == nil {
		b.v = b.a.newCanvasView(b.tab.FileID, canvasview.Peek)
	}
	if empty, known := b.v.Empty(); known && empty {
		return 0, false
	}
	if b.open.IsZero() {
		b.open = gtx.Now
	}
	return peekDefault, true
}

func (b *canvasTabPeek) layout(gtx layout.Context) layout.Dimensions {
	gtx.Constraints.Min = gtx.Constraints.Max
	return b.v.Layout(gtx)
}

func (b *canvasTabPeek) unmount() {
	if b.open.IsZero() || time.Since(b.open) < 6*time.Second {
		return
	}
	c := b.a.Core()
	if c == nil {
		return
	}
	d := b.v.Doc()
	if d == nil {
		return
	}
	id, created := b.tab.FileID, d.Info.Created
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = c.MarkFileReadPreview(ctx, id, created, time.Now())
	}()
}

// ---- FilesTabPeek ----

// filesTabPeek is FilesTabPeek: the Files & links tab in preview
// (views/channel_files_peek.go), its contentHeight from its rows.
type filesTabPeek struct{ v *views.ChannelFiles }

func (b *filesTabPeek) height(gtx layout.Context) (int, bool) { return b.v.PeekHeight(gtx), true }

func (b *filesTabPeek) layout(gtx layout.Context) layout.Dimensions {
	gtx.Constraints.Min = gtx.Constraints.Max
	return b.v.Layout(gtx)
}

// ---- ListsTabPeek ----

// listTabPeek is ListsTabPeek: ChannelTabPeek at its default height whose
// click opens the tab (openListTab {tabId, channelId}), around
// .container__RNLr2 with the list in its peek context (viewId: the tab's
// listsChannelTabDefaultRefineViewId; isViewOnly, hideRefinements,
// hideTitleHeader, disablePointerEvents: listview.Peek under a click
// catcher), or, without a file id, the list's empty state (UyfM).
type listTabPeek struct {
	a     *App
	p     *slugPeek
	ch    string
	tab   core.SlugTab
	v     *listview.View
	click widget.Clickable
}

func (b *listTabPeek) height(gtx layout.Context) (int, bool) { return peekDefault, true }

func (b *listTabPeek) layout(gtx layout.Context) layout.Dimensions {
	sz := gtx.Constraints.Max
	gtx.Constraints.Min = sz
	if b.click.Clicked(gtx) {
		b.p.done = true
		b.a.openChannelTab(b.ch, b.tab.ID)
	}
	if b.tab.FileID != "" {
		if b.v == nil {
			b.v = b.a.newListView(b.tab.FileID, listview.Peek)
			if b.tab.DefaultView != "" {
				b.v.SelectView(b.tab.DefaultView)
			}
		}
		b.v.Layout(gtx)
	} else {
		b.a.listPeekMissing(gtx)
	}
	// disablePointerEvents: the container takes every click
	b.click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
		pointer.CursorPointer.Add(gtx.Ops)
		return layout.Dimensions{Size: sz}
	})
	return layout.Dimensions{Size: sz}
}

// listPeekMissing is UyfM's UnknownRecordError, the list tab peek
// without a list: "Something went wrong" over "We’re not quite sure
// what, but please try again in a bit." (EmptyState with thinking_face).
// Its "You can also try reloading" line reloads the web client, which
// Clack has no counterpart for, so it isn't drawn.
func (a *App) listPeekMissing(gtx layout.Context) {
	sz := gtx.Constraints.Max
	c := &a.th.C
	title := i18nT("recordchannels", "Something went wrong")
	desc := i18nT("recordchannels", "We\u2019re not quite sure what, but please try again in a bit.")
	es := gtx.Dp(40)
	y := sz.Y/2 - gtx.Dp(60)
	at(gtx, image.Pt((sz.X-es)/2, y), image.Pt(es, es), func(gtx layout.Context) layout.Dimensions {
		return a.emojiByName(gtx, "thinking_face", es)
	})
	y += es + gtx.Dp(16)
	tw := min(a.th.Measure(gtx, 18, theme.Black, title), sz.X-gtx.Dp(32))
	at(gtx, image.Pt((sz.X-tw)/2, y), image.Pt(tw+1, gtx.Dp(24)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Label(gtx, 18, theme.Black, c.Fg, title)
	})
	y += gtx.Dp(32)
	dw := min(a.th.Measure(gtx, 15, theme.Regular, desc), sz.X-gtx.Dp(32))
	at(gtx, image.Pt((sz.X-dw)/2, y), image.Pt(dw+1, gtx.Dp(66)), func(gtx layout.Context) layout.Dimensions {
		return a.th.Para(gtx, 15, 22, theme.Regular, c.Fg, desc)
	})
}

func i18nT(ns, s string) string { return i18n.T(ns, s) }

// emojiByName draws the emoji name s px square (nothing until its image
// is in).
func (a *App) emojiByName(gtx layout.Context, name string, s int) layout.Dimensions {
	if c := a.Core(); c != nil {
		if u, _, ok := c.Emoji(name); ok && u != "" {
			if im, ok := a.images.Get(u, s); ok {
				theme.Image(gtx, im, image.Pt(s, s), 0)
			}
		}
	}
	return layout.Dimensions{Size: image.Pt(s, s)}
}
