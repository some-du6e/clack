package canvasview

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// View is one open canvas.
type View struct {
	h    Host
	id   string
	mode Mode
	act  Actions

	mu      sync.Mutex
	doc     *core.CanvasDoc
	err     error
	loading bool
	gen     int

	// UI goroutine only
	shown   *core.CanvasDoc // the doc the blocks were built from
	crashed error           // a recovered layout panic: the error state until Try again
	blocks  []*blk
	title   *richview.View
	list    layout.List
	btns    map[string]*widget.Clickable
	retry   widget.Clickable
	styles  styleSet
	handler *handler
	edit    *editState
	phView  *richview.View // the empty canvas's placeholder
	phText  string
	tphView *richview.View // the empty title's placeholder
	toc     *tocState      // the table of contents (toc.go)
	access  accessState    // the View only / Comment only pill (access.go)
	cover   coverState     // the header's Add cover (cover.go)

	queue []func() // post: run on the UI goroutine at the next frame

	live     atomic.Bool  // files.open sent, watching events
	lastSeen atomic.Int64 // unix ms of the last frame that drew the view
	stop     context.CancelFunc
}

// New returns a view of canvas id. It loads on its first frame.
func New(h Host, id string, mode Mode, act Actions) *View {
	v := &View{h: h, id: id, mode: mode, act: act, btns: map[string]*widget.Clickable{}, edit: newEditState(), toc: newTOC()}
	v.list.Axis = layout.Vertical
	v.handler = &handler{v: v}
	return v
}

// ID is the canvas's file id.
func (v *View) ID() string { return v.id }

// Mode is where the view is shown.
func (v *View) Mode() Mode { return v.mode }

// Title is the canvas's title once loaded ("" before).
func (v *View) Title() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.doc == nil {
		return ""
	}
	return v.doc.Info.Title
}

// Doc returns the last fetched document (nil until it arrives).
func (v *View) Doc() *core.CanvasDoc {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.doc
}

// Reload refetches the canvas (keeping what is shown until it arrives).
func (v *View) Reload() { v.load(true) }

func (v *View) load(keep bool) {
	a := v.h.Core()
	if a == nil {
		return
	}
	v.mu.Lock()
	v.gen++
	gen := v.gen
	v.loading = true
	if !keep {
		v.doc, v.err = nil, nil
	}
	v.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		defer func() { // a panic fetching or parsing shows the error state, not a crash
			if r := recover(); r != nil {
				log.Printf("canvasview: panic loading canvas %s (recovered): %v\n%s", v.id, r, debug.Stack())
				v.mu.Lock()
				if gen == v.gen {
					v.loading = false
					v.doc, v.err = nil, fmt.Errorf("canvas: %v", r)
				}
				v.mu.Unlock()
				v.h.Invalidate()
			}
		}()
		d, err := a.Canvas(ctx, v.id)
		if err == nil {
			ids := []string{d.Info.Creator}
			ids = append(ids, d.Info.Editors...)
			a.EnsureUsers(ctx, ids)
		}
		v.mu.Lock()
		if gen == v.gen {
			v.loading = false
			if err == nil || !keep || v.doc == nil {
				v.doc, v.err = d, err
			}
		}
		v.mu.Unlock()
		v.h.Invalidate()
	}()
}

// goLive registers the viewer (files.open) and follows the canvas's
// events until the view stops being drawn for a while (it was closed or
// replaced; the shell has no unmount hook), then sends files.close.
func (v *View) goLive() {
	a := v.h.Core()
	if a == nil || !v.live.CompareAndSwap(false, true) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	evs, unsub := a.Subscribe()
	go func() {
		defer unsub()
		oc, ocancel := context.WithTimeout(ctx, 20*time.Second)
		_ = a.OpenCanvas(oc, v.id)
		ocancel()
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		lastPoll := time.Now()
		var lastUpd int64
		if d := v.Doc(); d != nil {
			lastUpd = d.Info.Updated
		}
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-evs:
				if !ok {
					v.live.Store(false)
					return
				}
				if ev.Type == "canvas" && ev.ID == v.id {
					v.load(true)
				}
				if ev.Type == "canvas_viewers" && ev.ID == v.id {
					v.h.Invalidate()
				}
			case <-tick.C:
				if time.Since(time.UnixMilli(v.lastSeen.Load())) > 6*time.Second {
					cc, ccancel := context.WithTimeout(context.Background(), 20*time.Second)
					_ = a.CloseCanvas(cc, v.id)
					ccancel()
					v.live.Store(false)
					cancel()
					return
				}
				// realtime off: Slack's echo never comes, so poll the header
				if !a.Realtime() && time.Since(lastPoll) >= core.PollInterval {
					lastPoll = time.Now()
					pc, pcancel := context.WithTimeout(ctx, 20*time.Second)
					info, err := a.CanvasInfo(pc, v.id)
					pcancel()
					if err == nil && info.Updated != lastUpd {
						if lastUpd != 0 {
							v.load(true)
						}
						lastUpd = info.Updated
					}
				}
			}
		}
	}()
	v.stop = cancel
}

func (v *View) btn(key string) *widget.Clickable {
	b := v.btns[key]
	if b == nil {
		b = new(widget.Clickable)
		v.btns[key] = b
	}
	return b
}

// post runs f on the UI goroutine at the next frame (safe from any goroutine).
func (v *View) post(f func()) {
	v.mu.Lock()
	v.queue = append(v.queue, f)
	v.mu.Unlock()
	v.h.Invalidate()
}

// Layout draws the view filling the constraints. A panic anywhere in the
// canvas's layout (a bug, or a document Clack doesn't expect) is caught
// here: the frame's ops are rolled back to before the view started, the
// panic and its stack go to the log (internal/applog's file), and the
// view shows Slack's "Couldn't load this canvas" with Try again instead
// of taking the app down.
func (v *View) Layout(gtx layout.Context) (dims layout.Dimensions) {
	if v.crashed != nil {
		return v.layoutCrashed(gtx)
	}
	cp := gtx.Ops.Checkpoint()
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if !gtx.Ops.Rollback(cp) {
			panic(r) // the ops were reset under us: nothing safe to draw on
		}
		v.crash(r, debug.Stack())
		dims = v.layoutCrashed(gtx)
	}()
	return v.layout(gtx)
}

// crash records a recovered panic: logged with its stack, the editing
// session dropped, the blocks rebuilt from the document on Try again.
func (v *View) crash(r any, stack []byte) {
	log.Printf("canvasview: panic drawing canvas %s (recovered): %v\n%s", v.id, r, stack)
	v.crashed = fmt.Errorf("canvas: %v", r)
	v.edit = newEditState()
	v.shown, v.blocks, v.title = nil, nil, nil
	// the list was mid-child ("unfinished child" on its next Layout)
	v.list = layout.List{Axis: layout.Vertical, Position: layout.Position{First: v.list.Position.First, Offset: v.list.Position.Offset}}
	v.mu.Lock()
	v.queue = nil
	v.mu.Unlock()
}

// layoutCrashed is the error state after a recovered panic; Try again
// reloads the canvas and lays it out afresh.
func (v *View) layoutCrashed(gtx layout.Context) layout.Dimensions {
	v.lastSeen.Store(time.Now().UnixMilli())
	sz := gtx.Constraints.Max
	gtx.Constraints.Min = sz
	th := v.h.Theme()
	theme.Fill(gtx, image.Rectangle{Max: sz}, th.C.Bg)
	top := 0
	if v.mode != Tab { // the side panel keeps its close button, the full view its bar
		top = gtx.Dp(theme.HeaderH)
		v.layoutBar(gtx, v.Doc(), image.Pt(sz.X, top))
	}
	defer op.Offset(image.Pt(0, top)).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(image.Pt(sz.X, max(0, sz.Y-top)))
	if v.layoutError(g, v.crashed) {
		v.crashed = nil
	}
	return layout.Dimensions{Size: sz}
}

func (v *View) layout(gtx layout.Context) layout.Dimensions {
	v.lastSeen.Store(time.Now().UnixMilli())
	v.mu.Lock()
	q := v.queue
	v.queue = nil
	v.mu.Unlock()
	for _, f := range q {
		f()
	}
	v.mu.Lock()
	started := v.gen > 0
	d, err, loading := v.doc, v.err, v.loading
	v.mu.Unlock()
	if !started {
		v.load(false)
		loading = true
	}
	if !v.live.Load() {
		v.goLive()
	}
	th := v.h.Theme()
	sz := gtx.Constraints.Max
	gtx.Constraints.Min = sz
	theme.Fill(gtx, image.Rectangle{Max: sz}, th.C.Bg)
	v.styles.sync(th, v.h)
	if d != nil && d != v.shown {
		v.rebuild(d)
	}
	top := 0
	if v.mode != Tab {
		top = gtx.Dp(theme.HeaderH)
		v.layoutBar(gtx, d, image.Pt(sz.X, top))
	}
	defer v.layoutAccessPopover(gtx, d) // over everything, in the view's coordinates
	body := image.Rect(0, top, sz.X, sz.Y)
	defer op.Offset(body.Min).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(body.Size())
	defer clip.Rect{Max: body.Size()}.Push(gtx.Ops).Pop()
	switch {
	case d == nil && err != nil:
		v.layoutError(gtx, err)
		return layout.Dimensions{Size: gtx.Constraints.Max}
	case d == nil:
		_ = loading
		return v.layoutSkeleton(gtx)
	}
	return v.layoutDoc(gtx, d)
}

// layoutBar is the side panel's / full view's header, Slack's canvas
// flexpane: a .p-view_header (49px, padding 0 16px 0 18px, the title in
// 18px Lato Black, ellipsized; .p-quip_flexpane .p-view_header has no
// bottom shadow) with CanvasActionItems on the right, 6px apart
// (.p-quip_flexpane__action_items--gap). In the full view (the primary
// container) they are the "Edited …" reminder (QuipLastUpdatedReminder,
// once the canvas was edited), the viewers' facepile, the outline Share
// button (CanvasSharePrimaryButton) and More actions (the 28px
// .p-quip_flexpane__menu_trigger). In the side panel (the secondary
// container) Slack drops the reminder and Share; Clack's "Open in full
// view" stands where Slack's "Open in new window" is, and the panel's
// close button ends the row.
func (v *View) layoutBar(gtx layout.Context, d *core.CanvasDoc, size image.Point) {
	th := v.h.Theme()
	c := &th.C
	theme.Fill(gtx, image.Rectangle{Max: size}, c.Bg)
	title := nsCanvas.T("Canvas")
	if d != nil {
		title = d.Info.Title
	}
	gap := gtx.Dp(6)
	x := size.X - gtx.Dp(16)
	icon := func(key, name, tip string) {
		w := gtx.Dp(28)
		x -= w
		clk := v.btn("bar." + key)
		hit := clk.Clicked(gtx) // before Layout, which drains a Clickable's events
		at(gtx, image.Pt(x, (size.Y-w)/2), image.Pt(w, w), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, th, clk, name, 18)
		})
		if hit {
			v.barAction(key)
		}
		x -= gap
	}
	if v.mode == Side && v.act.Close != nil {
		icon("close", "close", nsCanvas.T("Close"))
	}
	icon("more", "more", nsCanvas.T("More actions"))
	if v.mode == Side && v.act.FullView != nil {
		icon("full", "expand", nsCanvas.T("Open in full view"))
	}
	if v.mode == Full && v.act.Share != nil {
		lw := th.Measure(gtx, 13, theme.Bold, nsCanvas.T("Share"))
		w := lw + 2*gtx.Dp(12)
		x -= w
		clk := v.btn("bar.share")
		share := clk.Clicked(gtx)
		at(gtx, image.Pt(x, (size.Y-gtx.Dp(28))/2), image.Pt(w, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			s := gtx.Constraints.Max
			if clk.Hovered() {
				theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(8), c.BgHover)
			}
			// c-button--outline, small: 28px, radius 8, 13px bold;
			// its border fg at .13 in the action items
			theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(8), gtx.Dp(1), theme.WithAlpha(c.Fg, 0x21))
			at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(lw+2, s.Y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 13, theme.Bold, c.Fg, nsCanvas.T("Share"))
				})
			})
			return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: s} })
		})
		if share {
			v.act.Share(v.id)
		}
		x -= gap
	}
	if v.showsAccess(d) { // renderViewOnlyButton: QuipAccessIndicator
		pw := v.accessPillWidth(gtx, v.accessLabel(d.Info))
		x -= pw
		ph := gtx.Dp(28)
		v.layoutAccessPill(gtx, d, image.Rect(x, (size.Y-ph)/2, x+pw, (size.Y-ph)/2+ph), true, image.Point{})
		x -= gap
	}
	if w := v.layoutViewers(gtx, image.Pt(x, (size.Y-gtx.Dp(24))/2), gtx.Dp(24)); w > 0 {
		x -= w - gtx.Dp(12) + gap
	}
	if v.mode == Full && d != nil && d.Info.Updated > d.Info.Created && d.Info.Updated > 0 {
		txt := "Edited " + editedAgo(time.Unix(d.Info.Updated, 0))
		if i18n.Locale() != i18n.EnUS {
			txt = nsCanvas.T("Edited {timeStr}", "timeStr", editedAgo(time.Unix(d.Info.Updated, 0))) // QuipLastUpdatedReminder
		}
		tw := th.Measure(gtx, 13, theme.Regular, txt)
		if tw < x/2 { // the title keeps the room first
			x -= tw
			muted := theme.WithAlpha(c.Fg, 0xb3) // --sk_foreground_max at .7
			at(gtx, image.Pt(x, 0), image.Pt(tw+2, size.Y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, size.Y, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 13, theme.Regular, muted, txt)
				})
			})
			x -= gap
		}
	}
	tx := gtx.Dp(18)
	at(gtx, image.Pt(tx, 0), image.Pt(max(0, x-tx-gtx.Dp(4)), size.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, size.Y, func(gtx layout.Context) layout.Dimensions {
			if d != nil && len(d.Info.TitleSpans) > 0 { // QuipTitleParsedId: emoji drawn
				return v.titleLine(gtx, d.Info.TitleSpans, d.Info.TitleFromText, 18, c.Fg, size.Y)
			}
			return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Black), Size: 18, Color: c.Fg, MaxLines: 1}, title)
		})
	})
}

// titleLine draws a title's spans on one line in Lato Black, ellipsized
// (QuipTitleParsedId's truncate: lines 1): text as text, custom emoji as
// images, 16px from the title string (FormatTextWithEmoji's small
// c-emoji, small) or 22px from title_blocks (rich text's medium c-emoji),
// centred on the line.
func (v *View) titleLine(gtx layout.Context, spans []richtext.Span, fromText bool, size unit.Sp, col color.NRGBA, h int) layout.Dimensions {
	th := v.h.Theme()
	maxW := gtx.Constraints.Max.X
	es := gtx.Dp(22)
	if fromText {
		es = gtx.Dp(16)
	}
	x := 0
	for _, s := range spans {
		if x >= maxW || s.Kind == richtext.Break {
			break
		}
		if s.Kind == richtext.Emoji && s.URL != "" {
			if x+es > maxW {
				break
			}
			url := s.URL
			at(gtx, image.Pt(x, (h-es)/2), image.Pt(es, es), func(gtx layout.Context) layout.Dimensions {
				if img, ok := v.h.Images().Get(url, es*2); ok {
					return theme.Image(gtx, img, image.Pt(es, es), 0)
				}
				return layout.Dimensions{Size: image.Pt(es, es)}
			})
			x += es
			continue
		}
		txt := strings.ReplaceAll(s.Text, "\n", " ")
		if txt == "" {
			continue
		}
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(maxW-x, h)}
		m := op.Record(gtx.Ops)
		dim := th.Text(g, theme.LabelOpts{Font: theme.Font(theme.Black), Size: size, Color: col, MaxLines: 1}, txt)
		call := m.Stop()
		func() {
			defer op.Offset(image.Pt(x, (h-dim.Size.Y)/2)).Push(gtx.Ops).Pop()
			call.Add(gtx.Ops)
		}()
		x += dim.Size.X
	}
	return layout.Dimensions{Size: image.Pt(min(x, maxW), h)}
}

// editedAgo is QuipLastUpdatedReminder's time: always relative
// (alwaysAgo), "just now" under a minute, "minutes" shortened to "min".
func editedAgo(t time.Time) string {
	if l := i18n.Locale(); l != i18n.EnUS {
		return i18n.TimeAgo(l, t, time.Now(), false, true) // toTimeAgo, alwaysAgo
	}
	d := time.Since(t)
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit + " ago"
		}
		return itoa(n) + " " + unit + "s ago"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + " min ago"
	case d < 24*time.Hour:
		return plural(int(d/time.Hour), "hour")
	case d < 30*24*time.Hour:
		return plural(int(d/(24*time.Hour)), "day")
	case d < 365*24*time.Hour:
		return plural(int(d/(30*24*time.Hour)), "month")
	}
	return plural(int(d/(365*24*time.Hour)), "year")
}

func (v *View) barAction(key string) {
	switch key {
	case "full":
		if v.act.FullView != nil {
			v.act.FullView()
		}
	case "side":
		if v.act.SideView != nil {
			v.act.SideView()
		}
	case "close":
		if v.act.Close != nil {
			v.act.Close()
		}
	case "more":
		v.openMore()
	}
}

// openMore is the canvas's More options menu, Slack's items Clack can
// carry out, under Slack's labels: Copy link, Share this canvas, and the
// split view / full view switch ("Open in split view" in a channel tab).
func (v *View) openMore() {
	if v.act.Menu == nil {
		return
	}
	d := v.Doc()
	var items []MenuItem
	if d != nil && d.Info.Permalink != "" {
		link := d.Info.Permalink
		items = append(items, MenuItem{Label: nsCanvas.T("Copy link"), Icon: "link", Do: func() { copyText(v.h, link) }})
	}
	if v.act.Share != nil {
		items = append(items, MenuItem{Label: nsCanvas.T("Share this canvas"), Icon: "share-message", Do: func() { v.act.Share(v.id) }})
	}
	var view []MenuItem
	switch {
	case v.mode == Tab && v.act.SideView != nil:
		view = append(view, MenuItem{Label: nsCanvas.T("Open in split view"), Icon: "panel-right", Do: v.act.SideView})
	case v.mode == Side && v.act.FullView != nil:
		view = append(view, MenuItem{Label: nsCanvas.T("Open in full view"), Icon: "expand", Do: v.act.FullView})
	case v.mode == Full && v.act.SideView != nil:
		view = append(view, MenuItem{Label: nsCanvas.T("Open in side panel"), Icon: "panel-right", Do: v.act.SideView})
	}
	if len(items) > 0 && len(view) > 0 {
		items = append(items, MenuItem{Divider: true})
	}
	items = append(items, view...)
	if len(items) == 0 {
		return
	}
	v.act.Menu(items)
}

// copyText is set by the shell (clipboard writes need the window).
var copyText = func(h Host, s string) { h.Toast(nsCanvas.T("Link copied")) }

// SetCopy installs the shell's clipboard writer.
func SetCopy(f func(h Host, s string)) { copyText = f }

// layoutError is Slack's canvas error state; it reports whether Try
// again was clicked (the canvas is reloading).
func (v *View) layoutError(gtx layout.Context, err error) (retried bool) {
	th := v.h.Theme()
	sz := gtx.Constraints.Max
	msg := "Couldn't load this canvas"
	if s := err.Error(); strings.Contains(s, "not a canvas") {
		msg = "This file isn't a canvas"
	} else if strings.Contains(s, "file_not_found") || strings.Contains(s, "file_deleted") {
		msg = "This canvas was deleted or you don't have access to it"
	}
	y := sz.Y / 3
	w := th.Measure(gtx, 15, theme.Bold, msg)
	at(gtx, image.Pt((sz.X-w)/2, y), image.Pt(w+2, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
		return th.Label(gtx, 15, theme.Bold, th.C.Fg, msg)
	})
	retry := v.retry.Clicked(gtx) // before Layout, which drains a Clickable's events
	lw := th.Measure(gtx, 13, theme.Bold, nsCanvas.T("Try again"))
	bw := lw + gtx.Dp(24)
	at(gtx, image.Pt((sz.X-bw)/2, y+gtx.Dp(36)), image.Pt(bw, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
		s := gtx.Constraints.Max
		theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(6), gtx.Dp(1), th.C.BorderStrong)
		at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(lw+2, s.Y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 13, theme.Bold, th.C.Fg, nsCanvas.T("Try again"))
			})
		})
		return v.retry.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: s} })
	})
	if retry {
		v.load(false)
		return true
	}
	return false
}

// layoutSkeleton is the loading state: a title bar and text lines.
func (v *View) layoutSkeleton(gtx layout.Context) layout.Dimensions {
	th := v.h.Theme()
	sz := gtx.Constraints.Max
	x0, w := columnFor(gtx, sz.X, true) // p-quip_loading_skeleton's 96/94 column
	y := gtx.Dp(32)
	theme.RRect(gtx, image.Rect(x0, y, x0+w*55/100, y+gtx.Dp(28)), gtx.Dp(4), th.C.Skeleton)
	y += gtx.Dp(56)
	for i, p := range []int{92, 80, 86, 40, 0, 70, 88, 64} {
		if p > 0 {
			theme.RRect(gtx, image.Rect(x0, y, x0+w*p/100, y+gtx.Dp(12)), gtx.Dp(3), th.C.Skeleton)
		}
		y += gtx.Dp(22)
		_ = i
	}
	return layout.Dimensions{Size: sz}
}

// column is the document's content column. Slack's canvas
// (.parts-screen-children-wrapper) is at most 990px wide, centred, with
// 96px/94px side padding: an 800px column (Quip's MAX_DOCUMENT_WIDTH).
// Narrower panes use Quip's smaller paddings (p-quip_loading_skeleton's
// breakpoints: under 500 "medium" 51px, under 350 28px, under 200 16px).
//
// The fixed padding is 94px a side (horizontalPaddingOverrides: fixed 94,
// medium 51). With the table of contents on, Quip reserves its 64px
// gutter (setExternalGutterReservedWidth), and canvasDocumentMargins
// makes the left margin max(padding, 64+32): 96px, the measured 96/94.
func (v *View) column(gtx layout.Context, w int) (x, cw int) {
	return columnFor(gtx, w, v.tocOn())
}

func columnFor(gtx layout.Context, w int, toc bool) (x, cw int) {
	pad := 0
	switch {
	case w < gtx.Dp(200):
		pad = gtx.Dp(16)
	case w < gtx.Dp(350):
		pad = gtx.Dp(28)
	case w < gtx.Dp(500):
		pad = gtx.Dp(51) // --document-padding-medium-horizontal
	}
	gutter := 0
	if toc {
		gutter = gtx.Dp(64) + gtx.Dp(32)
	}
	if pad > 0 {
		left := max(pad, gutter)
		return left, max(1, w-left-pad)
	}
	left, right := max(gtx.Dp(94), gutter), gtx.Dp(94)
	wrap := min(w, gtx.Dp(800)+left+right) // .parts-screen-children-wrapper's max-width
	x0 := (w - wrap) / 2
	return x0 + left, max(1, wrap-left-right)
}

// docTop is the space above the title: the canvas's header-image
// container, 100px tall when the canvas has no cover image.
const docTop = 100

// docBottom is the wrapper's padding-bottom (120px), the room the bottom
// toolbar floats in.
const docBottom = 120

// layoutDoc draws the title and blocks in a scrolling list, the floating
// actions, and (for editors) Slack's bottom toolbar.
func (v *View) layoutDoc(gtx layout.Context, d *core.CanvasDoc) layout.Dimensions {
	sz := gtx.Constraints.Max
	x0, cw := v.column(gtx, sz.X)
	v.edit.events(gtx, v)
	defer v.layoutFloatingActions(gtx)
	defer v.edit.layoutInsertionToolbar(gtx, v)
	// The rows are this snapshot of the blocks. Handlers that run during
	// the list's layout (a click that starts or ends an edit, Escape in
	// the editor) add and remove blocks; that shows from the next frame.
	// Indexing v.blocks itself with a count taken before them read past
	// its end when a click dropped the empty new-block row (alpha.24's
	// crash on opening a short canvas: a click under the text, then one on
	// it).
	blocks := append([]*blk(nil), v.blocks...)
	defer func() {
		if !sameBlks(blocks, v.blocks) {
			v.h.Invalidate()
		}
	}()
	// A canvas's title is its document's title section (Quip's
	// extractTitleMetadata): a heading that opens the document is drawn
	// once, as the title; it is not repeated as the first block.
	var title *blk
	rows := blocks
	if titleSection(blocks) {
		title, rows = blocks[0], blocks[1:]
	}
	ph := v.placeholder(rows)
	n := len(rows) + 2     // header, blocks, bottom space
	defer v.layoutTOC(gtx) // over the blocks, under the toolbars
	dims := v.list.Layout(gtx, n, func(gtx layout.Context, i int) (dims layout.Dimensions) {
		gtx.Constraints.Min.X = sz.X
		switch {
		case i == 0:
			dims = v.layoutHeader(gtx, d, x0, cw, title)
			v.toc.rowLaidOut(i, 0, dims.Size.Y)
			return dims
		case i == n-1:
			below := image.Pt(sz.X, max(gtx.Dp(docBottom), sz.Y/3))
			v.edit.belowArea(gtx, v, below)
			v.toc.rowLaidOut(i, 0, below.Y)
			return layout.Dimensions{Size: below}
		}
		b := rows[i-1]
		var prev *blk
		if i >= 2 {
			prev = rows[i-2]
		}
		defer op.Offset(image.Pt(x0, 0)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Constraints{Min: image.Pt(cw, 0), Max: image.Pt(cw, sz.Y*8)}
		top := gap(gtx, prev, b, true)
		var dim layout.Dimensions
		if ph != "" && b == rows[0] && !v.edit.editing(b) {
			dim = v.layoutPlaceholder(g, b, top, ph)
		} else {
			dim = v.layoutBlock(g, b, top)
		}
		dim.Size.X = sz.X
		v.toc.rowLaidOut(i, top, dim.Size.Y)
		return dim
	})
	v.toc.listDone(v.list.Position)
	return dims
}

// titlePlaceholder is Slack's title placeholder (titlePlaceholder in
// client-boot-shared's canvas options), which Quip's TitleSectionOverlay
// draws over an empty title section.
const titlePlaceholder = "Your canvas title"

// placeholderRow is the block the empty-canvas placeholder is drawn on,
// or nil.
func (v *View) placeholderRow() *blk {
	rows := v.blocks
	if titleSection(rows) {
		rows = rows[1:]
	}
	if v.placeholder(rows) == "" {
		return nil
	}
	return rows[0]
}

// textPlaceholders are Slack's empty-canvas placeholders; a canvas gets
// the one its file id picks (getIndexFromFileId in client-boot-shared).
var textPlaceholders = [...]string{
	"Go ahead, start writing!",
	"What\u2019s on the docket for today?",
	"Once upon a time\u2026",
	"Time to document your genius!",
	"Words go here ",
}

// placeholderFor is Slack's getIndexFromFileId: the id's last two
// characters, as a+=16*a+(c-65), modulo the count.
func placeholderFor(id string) string {
	a := 0
	for i := max(0, len(id)-2); i < len(id); i++ {
		a += 16*a + int(id[i]) - 65
	}
	if a < 0 {
		a = -a
	}
	return nsCanvas.T(textPlaceholders[a%len(textPlaceholders)])
}

// placeholder is the text Slack overlays on an empty canvas (Quip's
// TextSectionOverlay): shown to editors only, when the document is its
// title plus one plain paragraph that is empty or unedited default
// content. Otherwise "".
func (v *View) placeholder(rows []*blk) string {
	if !v.edit.canEdit(v) || len(rows) != 1 {
		return ""
	}
	b := rows[0].b
	if b.Kind != canvas.Paragraph || !(b.Default || strings.TrimSpace(b.Text()) == "") {
		return ""
	}
	return v.textPlaceholder()
}

// textPlaceholder is the canvas's textPlaceholder. Slack's QuipEmbed
// passes getIndexFromFileId the Quip document's id (doc.id, which it also
// hands on as quipThreadId), not the Slack file's: files.info
// quip_thread_id, else the file id.
func (v *View) textPlaceholder() string {
	if v.shown != nil && v.shown.Info.QuipThreadID != "" {
		return placeholderFor(v.shown.Info.QuipThreadID)
	}
	return placeholderFor(v.id)
}

// layoutPlaceholder draws b's line with the placeholder over it
// (.editor-default-section-text-overlay: 15px in #77757a, #9a9b9e dark);
// a click on it edits b.
func (v *View) layoutPlaceholder(gtx layout.Context, b *blk, top int, text string) layout.Dimensions {
	w := gtx.Constraints.Max.X
	defer op.Offset(image.Pt(0, top)).Push(gtx.Ops).Pop()
	if v.phView == nil || v.phText != text {
		v.phText = text
		v.phView = richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: text}}}}}, richview.Options{})
	}
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(w, gtx.Constraints.Max.Y)}
	h := max(gtx.Dp(bodyLine), v.phView.Layout(g, v.styles.ph, v.handler).Size.Y)
	v.edit.textArea(gtx, v, b, image.Rect(0, 0, w, h))
	return layout.Dimensions{Size: image.Pt(w, top+h)}
}

// titleSection reports whether the document opens with its title
// section: a heading with text, or an H1 at all. Quip names a canvas
// after its first heading (else the start of its first paragraph), and
// Slack draws that heading where it is, once; the export has no separate
// title. Quip's isTitleSection takes the first section when it is the
// title style or, in a document with a title, an H1, even an empty one
// (TitleSectionOverlay then shows "Your canvas title" over it).
func titleSection(bs []*blk) bool {
	if len(bs) == 0 || bs[0].b == nil {
		return false
	}
	b := bs[0].b
	switch b.Kind {
	case canvas.Heading1:
		return true
	case canvas.Heading2, canvas.Heading3:
	default:
		return false
	}
	for _, s := range b.Spans {
		if s.Kind != richtext.Text || strings.TrimSpace(s.Text) != "" {
			return true
		}
	}
	return false
}

// layoutHeader: the canvas title at the top of the document, below the
// header-image band (with Add cover for editors, cover.go); Slack draws
// no byline under it. The title is the document's own opening heading,
// tb, when it has one (drawn and edited as that block, in the title
// section's 28px/34px black); otherwise the file's title.
func (v *View) layoutHeader(gtx layout.Context, d *core.CanvasDoc, x0, cw int, tb *blk) layout.Dimensions {
	y := gtx.Dp(docTop)
	if v.edit != nil && v.edit.renaming {
		y += v.edit.layoutRename(gtx, v, image.Pt(x0, y), cw)
	} else if tb != nil {
		func() {
			defer op.Offset(image.Pt(x0, y)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Min: image.Pt(cw, 0), Max: image.Pt(cw, gtx.Constraints.Max.Y)}
			y += v.layoutTitleBlock(g, tb)
		}()
	} else {
		if v.title == nil {
			sp := d.Info.TitleSpans
			if len(sp) == 0 {
				sp = []richtext.Span{{Kind: richtext.Text, Text: d.Info.Title}}
			}
			sp = withStyle(sp, richtext.Bold)
			v.title = richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: sp}}}, richview.Options{})
		}
		func() {
			defer op.Offset(image.Pt(x0, y)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Constraints{Max: image.Pt(cw, gtx.Constraints.Max.Y)}
			dim := v.title.Layout(g, v.styles.title, v.handler)
			y += dim.Size.Y
			if d.Info.Editable && v.edit.canEdit(v) {
				r := image.Rect(0, 0, cw, dim.Size.Y)
				v.edit.titleArea(gtx, v, r)
			}
		}()
	}
	v.layoutCover(gtx, gtx.Constraints.Min.X, x0, gtx.Dp(docTop), y) // over the title: its hover area passes presses on
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, y)}
}

// layoutTitleBlock draws the document's opening heading as the title
// section (data-section-style 48: 28px/34px, weight 900), or its editor.
func (v *View) layoutTitleBlock(gtx layout.Context, b *blk) int {
	w := gtx.Constraints.Max.X
	if v.edit.editing(b) {
		return v.edit.layoutEditor(gtx, v, b, 0, w)
	}
	g := gtx
	g.Constraints = layout.Constraints{Max: image.Pt(w, gtx.Constraints.Max.Y)}
	h := gtx.Dp(titleLine)
	switch {
	case emptyTitle(b):
		// Quip's TitleSectionOverlay: the placeholder in the title
		// section's own type (.editor-default-section-text-overlay's
		// colour), for editors; an empty line for everyone else
		if v.edit.canEdit(v) {
			if v.tphView == nil {
				v.tphView = richview.New(richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph,
					Spans: []richtext.Span{{Kind: richtext.Text, Text: nsCanvas.T(titlePlaceholder), Style: richtext.Bold}}}}}, richview.Options{})
			}
			h = max(h, v.tphView.Layout(g, v.styles.titlePh, v.handler).Size.Y)
		}
	case b.rv != nil:
		h = b.rv.Layout(g, v.styles.title, v.handler).Size.Y
	default:
		return 0
	}
	if v.edit.canEdit(v) {
		v.edit.textArea(gtx, v, b, image.Rect(0, 0, w, h))
	}
	return h
}

// emptyTitle: the title section has no text (whitespace counts as
// empty, as isEmpty({checkForWhitespace: true}) does).
func emptyTitle(b *blk) bool {
	if b == nil || b.b == nil {
		return false
	}
	for _, s := range b.b.Spans {
		if s.Kind != richtext.Text || strings.TrimSpace(s.Text) != "" {
			return false
		}
	}
	return true
}

// layoutFloatingActions is the channel tab's floating More options (⋮)
// at the canvas's top right (floatingActions: a 32px tile, 2px padding,
// radius 8, a 1px ring and a 1px-3px shadow; the 28px button inside), with
// the facepile of the other viewers to its left. The side panel and full
// view have their bar instead.
func (v *View) layoutFloatingActions(gtx layout.Context) {
	if v.mode != Tab {
		return
	}
	th := v.h.Theme()
	c := &th.C
	sz := gtx.Constraints.Max
	tile := gtx.Dp(32)
	// the access indicator (borderless) shares the tile, before More options
	d := v.Doc()
	pill := 0
	if v.showsAccess(d) {
		pill = v.accessPillWidth(gtx, v.accessLabel(d.Info))
	}
	r := image.Rect(sz.X-gtx.Dp(24)-tile-pill, gtx.Dp(24), sz.X-gtx.Dp(24), gtx.Dp(24)+tile)
	theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(1))), gtx.Dp(8), gtx.Dp(3), theme.WithAlpha(c.PanelShadow, 0x14))
	theme.RRect(gtx, r, gtx.Dp(8), c.Bg)
	theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), theme.WithAlpha(c.Fg, 0x21))
	if pill > 0 {
		p := r.Min.Add(image.Pt(gtx.Dp(2), gtx.Dp(2)))
		v.layoutAccessPill(gtx, d, image.Rectangle{Min: p, Max: p.Add(image.Pt(pill, gtx.Dp(28)))}, false, image.Point{})
	}
	clk := v.btn("float.more")
	hit := clk.Clicked(gtx) // before Layout, which drains a Clickable's events
	at(gtx, image.Pt(r.Max.X-gtx.Dp(2)-gtx.Dp(28), r.Min.Y+gtx.Dp(2)), image.Pt(gtx.Dp(28), gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, th, clk, "kebab", 20)
	})
	if hit {
		v.openMore()
	}
	v.layoutViewers(gtx, image.Pt(r.Min.X-gtx.Dp(8)+gtx.Dp(12), r.Min.Y+gtx.Dp(6)), gtx.Dp(20))
}

// layoutViewers draws the facepile of the other people viewing the canvas
// right-aligned to end (top-left y), ah tall; it returns its width.
func (v *View) layoutViewers(gtx layout.Context, end image.Point, ah int) int {
	a := v.h.Core()
	if a == nil {
		return 0
	}
	ids := a.CanvasViewers(v.id)
	if len(ids) == 0 {
		return 0
	}
	th := v.h.Theme()
	shown := ids
	if len(shown) > 3 {
		shown = shown[:3]
	}
	step := ah - gtx.Dp(4)
	ring := gtx.Dp(2)
	w := step*(len(shown)-1) + ah
	more := ""
	if n := len(ids) - len(shown); n > 0 {
		more = "+" + itoa(n)
		w += gtx.Dp(6) + th.Measure(gtx, 12, theme.Bold, more)
	}
	x := end.X - w
	for i := len(shown) - 1; i >= 0; i-- { // the first viewer on top
		px := x + i*step
		r := image.Rect(px-ring, end.Y-ring, px+ah+ring, end.Y+ah+ring)
		theme.RRect(gtx, r, gtx.Dp(7), th.C.Bg)
		u, _ := a.User(shown[i])
		if img, ok := v.h.Images().Get(u.Avatar, ah); ok && u.Avatar != "" {
			at(gtx, image.Pt(px, end.Y), image.Pt(ah, ah), func(gtx layout.Context) layout.Dimensions {
				return theme.Image(gtx, img, image.Pt(ah, ah), gtx.Dp(5))
			})
		} else {
			theme.RRect(gtx, image.Rect(px, end.Y, px+ah, end.Y+ah), gtx.Dp(5), th.C.Skeleton)
		}
	}
	if more != "" {
		mx := x + step*(len(shown)-1) + ah + gtx.Dp(6)
		at(gtx, image.Pt(mx, end.Y), image.Pt(end.X-mx+2, ah), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, ah, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 12, theme.Bold, th.C.FgMuted, more)
			})
		})
	}
	return w + gtx.Dp(12)
}

// ago is Slack's relative time for "Last edited".
func ago(t time.Time) string {
	if l := i18n.Locale(); l != i18n.EnUS {
		return i18n.TimeAgo(l, t, time.Now(), false, true)
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		n := int(d / time.Minute)
		if n == 1 {
			return "1 minute ago"
		}
		return itoa(n) + " minutes ago"
	case d < 24*time.Hour:
		n := int(d / time.Hour)
		if n == 1 {
			return "1 hour ago"
		}
		return itoa(n) + " hours ago"
	case d < 48*time.Hour:
		return "yesterday"
	case time.Now().Year() == t.Year():
		return t.Format("January 2")
	}
	return t.Format("January 2, 2006")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func sameBlks(a, b []*blk) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func withStyle(sp []richtext.Span, st richtext.Style) []richtext.Span {
	out := make([]richtext.Span, len(sp))
	for i, s := range sp {
		s.Style |= st
		out[i] = s
	}
	return out
}

// ---- small drawing helpers ----

func at(gtx layout.Context, p, size image.Point, w layout.Widget) layout.Dimensions {
	defer op.Offset(p).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Exact(size)
	return w(g)
}

func vcenter(gtx layout.Context, h int, w layout.Widget) layout.Dimensions {
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	d := w(g)
	call := m.Stop()
	defer op.Offset(image.Pt(0, (h-d.Size.Y)/2)).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(d.Size.X, h)}
}

func iconButton(gtx layout.Context, th *theme.Theme, clk *widget.Clickable, icon string, isz unit.Dp) layout.Dimensions {
	s := gtx.Constraints.Max
	return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if clk.Hovered() {
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), th.C.BgHover)
		}
		p := gtx.Dp(isz)
		at(gtx, image.Pt((s.X-p)/2, (s.Y-p)/2), image.Pt(p, p), func(gtx layout.Context) layout.Dimensions {
			return icons.Get(icon)(gtx, isz, th.C.FgMuted)
		})
		return layout.Dimensions{Size: s}
	})
}
