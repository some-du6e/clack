package canvasview

import (
	"context"
	"image"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
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
	blocks  []*blk
	title   *richview.View
	list    layout.List
	btns    map[string]*widget.Clickable
	retry   widget.Clickable
	styles  styleSet
	handler *handler
	edit    *editState

	live     atomic.Bool  // files.open sent, watching events
	lastSeen atomic.Int64 // unix ms of the last frame that drew the view
	stop     context.CancelFunc
}

// New returns a view of canvas id. It loads on its first frame.
func New(h Host, id string, mode Mode, act Actions) *View {
	v := &View{h: h, id: id, mode: mode, act: act, btns: map[string]*widget.Clickable{}}
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

// Layout draws the view filling the constraints.
func (v *View) Layout(gtx layout.Context) layout.Dimensions {
	v.lastSeen.Store(time.Now().UnixMilli())
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
	body := image.Rect(0, top, sz.X, sz.Y)
	defer op.Offset(body.Min).Push(gtx.Ops).Pop()
	gtx.Constraints = layout.Exact(body.Size())
	defer clip.Rect{Max: body.Size()}.Push(gtx.Ops).Pop()
	switch {
	case d == nil && err != nil:
		return v.layoutError(gtx, err)
	case d == nil:
		_ = loading
		return v.layoutSkeleton(gtx)
	}
	return v.layoutDoc(gtx, d)
}

// layoutBar is the side panel's / full view's top bar.
func (v *View) layoutBar(gtx layout.Context, d *core.CanvasDoc, size image.Point) {
	th := v.h.Theme()
	c := &th.C
	theme.Fill(gtx, image.Rect(0, size.Y-gtx.Dp(1), size.X, size.Y), c.Border)
	title := "Canvas"
	if d != nil {
		title = d.Info.Title
	}
	type b struct{ key, icon, tip string }
	var bs []b
	switch v.mode {
	case Side:
		if v.act.FullView != nil {
			bs = append(bs, b{"full", "expand", "Open in full view"})
		}
		bs = append(bs, b{"more", "more", "More actions"})
		if v.act.Close != nil {
			bs = append(bs, b{"close", "close", "Close"})
		}
	case Full:
		if v.act.SideView != nil {
			bs = append(bs, b{"side", "panel-right", "Open in side panel"})
		}
		bs = append(bs, b{"more", "more", "More actions"})
	}
	x := size.X - gtx.Dp(12)
	for i := len(bs) - 1; i >= 0; i-- {
		bb := bs[i]
		w := gtx.Dp(28)
		x -= w
		clk := v.btn("bar." + bb.key)
		at(gtx, image.Pt(x, (size.Y-w)/2), image.Pt(w, w), func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, th, clk, bb.icon, 18)
		})
		if clk.Clicked(gtx) {
			v.barAction(bb.key)
		}
		x -= gtx.Dp(4)
	}
	if v.mode == Full && v.act.Share != nil {
		lw := th.Measure(gtx, 13, theme.Bold, "Share")
		w := lw + gtx.Dp(24)
		x -= w + gtx.Dp(4)
		clk := v.btn("bar.share")
		at(gtx, image.Pt(x, (size.Y-gtx.Dp(28))/2), image.Pt(w, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			s := gtx.Constraints.Max
			bg := c.Bg
			if clk.Hovered() {
				bg = c.BgHover
			}
			theme.RRect(gtx, image.Rectangle{Max: s}, gtx.Dp(6), bg)
			theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(6), gtx.Dp(1), c.BorderStrong)
			at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(lw+2, s.Y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
					return th.Label(gtx, 13, theme.Bold, c.Fg, "Share")
				})
			})
			return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: s} })
		})
		if clk.Clicked(gtx) {
			v.act.Share(v.id)
		}
	}
	// icon + title
	ix := gtx.Dp(16)
	isz := gtx.Dp(18)
	at(gtx, image.Pt(ix, (size.Y-isz)/2), image.Pt(isz, isz), func(gtx layout.Context) layout.Dimensions {
		return icons.Get("canvas")(gtx, 18, c.Fg)
	})
	tx := ix + isz + gtx.Dp(8)
	tsize := unit.Sp(18)
	if v.mode == Side {
		tsize = 16
	}
	at(gtx, image.Pt(tx, 0), image.Pt(max(0, x-tx-gtx.Dp(8)), size.Y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, size.Y, func(gtx layout.Context) layout.Dimensions {
			return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Black), Size: tsize, Color: c.Fg, MaxLines: 1}, title)
		})
	})
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

func (v *View) openMore() {
	if v.act.Menu == nil {
		return
	}
	d := v.Doc()
	var items []MenuItem
	if d != nil && d.Info.Permalink != "" {
		link := d.Info.Permalink
		items = append(items, MenuItem{Label: "Copy link to canvas", Icon: "link", Do: func() { copyText(v.h, link) }})
		items = append(items, MenuItem{Label: "Open in browser", Icon: "external", Do: func() {
			if v.act.OpenURL != nil {
				v.act.OpenURL(link)
			}
		}})
	}
	if d != nil && d.Info.Editable {
		items = append(items, MenuItem{Label: "Rename canvas", Icon: "edit", Do: v.startRename})
	}
	if v.act.Share != nil {
		items = append(items, MenuItem{Label: "Share canvas", Icon: "share", Do: func() { v.act.Share(v.id) }})
	}
	if v.mode == Side && v.act.FullView != nil {
		items = append(items, MenuItem{Label: "Open in full view", Icon: "expand", Do: v.act.FullView})
	}
	if v.mode == Full && v.act.SideView != nil {
		items = append(items, MenuItem{Label: "Open in side panel", Icon: "panel-right", Do: v.act.SideView})
	}
	items = append(items, MenuItem{Label: "Refresh", Icon: "refresh", Do: v.Reload})
	v.act.Menu(items)
}

// copyText is set by the shell (clipboard writes need the window).
var copyText = func(h Host, s string) { h.Toast("Link copied") }

// SetCopy installs the shell's clipboard writer.
func SetCopy(f func(h Host, s string)) { copyText = f }

func (v *View) layoutError(gtx layout.Context, err error) layout.Dimensions {
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
	lw := th.Measure(gtx, 13, theme.Bold, "Try again")
	bw := lw + gtx.Dp(24)
	at(gtx, image.Pt((sz.X-bw)/2, y+gtx.Dp(36)), image.Pt(bw, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
		s := gtx.Constraints.Max
		theme.Border(gtx, image.Rectangle{Max: s}, gtx.Dp(6), gtx.Dp(1), th.C.BorderStrong)
		at(gtx, image.Pt(gtx.Dp(12), 0), image.Pt(lw+2, s.Y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, s.Y, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, 13, theme.Bold, th.C.Fg, "Try again")
			})
		})
		return v.retry.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: s} })
	})
	if v.retry.Clicked(gtx) {
		v.load(false)
	}
	return layout.Dimensions{Size: sz}
}

// layoutSkeleton is the loading state: a title bar and text lines.
func (v *View) layoutSkeleton(gtx layout.Context) layout.Dimensions {
	th := v.h.Theme()
	sz := gtx.Constraints.Max
	x0, w := v.column(gtx, sz.X)
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

// column is the document's content column: Slack centres a canvas at a
// readable width with generous side padding (less in the side panel).
func (v *View) column(gtx layout.Context, w int) (x, cw int) {
	pad := gtx.Dp(48)
	maxW := gtx.Dp(760)
	if v.mode == Side || w < gtx.Dp(560) {
		pad = gtx.Dp(24)
	}
	cw = w - 2*pad
	if cw > maxW {
		cw = maxW
	}
	if cw < gtx.Dp(120) {
		cw = max(1, w-gtx.Dp(16))
	}
	return (w - cw) / 2, cw
}

// layoutDoc draws the title, byline and blocks in a scrolling list.
func (v *View) layoutDoc(gtx layout.Context, d *core.CanvasDoc) layout.Dimensions {
	sz := gtx.Constraints.Max
	x0, cw := v.column(gtx, sz.X)
	n := len(v.blocks) + 2 // header, blocks, bottom space
	v.edit.events(gtx, v)
	return v.list.Layout(gtx, n, func(gtx layout.Context, i int) layout.Dimensions {
		gtx.Constraints.Min.X = sz.X
		switch {
		case i == 0:
			return v.layoutHeader(gtx, d, x0, cw)
		case i == n-1:
			return layout.Dimensions{Size: image.Pt(sz.X, gtx.Dp(96))}
		}
		b := v.blocks[i-1]
		defer op.Offset(image.Pt(x0, 0)).Push(gtx.Ops).Pop()
		g := gtx
		g.Constraints = layout.Constraints{Min: image.Pt(cw, 0), Max: image.Pt(cw, sz.Y*8)}
		dim := v.layoutBlock(g, b, i-1 == 0)
		dim.Size.X = sz.X
		return dim
	})
}

// layoutHeader: the canvas title (Slack's large title at the top of the
// document) and the byline: the author's avatar and name, last edited.
func (v *View) layoutHeader(gtx layout.Context, d *core.CanvasDoc, x0, cw int) layout.Dimensions {
	th := v.h.Theme()
	c := &th.C
	y := gtx.Dp(36)
	if v.mode == Side {
		y = gtx.Dp(24)
	}
	if v.edit != nil && v.edit.renaming {
		h := v.edit.layoutRename(gtx, v, image.Pt(x0, y), cw)
		y += h
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
	y += gtx.Dp(10)
	// byline
	a := v.h.Core()
	name := ""
	avatar := ""
	if a != nil {
		if u, ok := a.User(d.Info.Creator); ok {
			name, avatar = u.Name, u.Avatar
		}
	}
	line := ""
	if name != "" {
		line = "Created by " + name
	}
	if d.Info.Updated > 0 {
		if line != "" {
			line += "  ·  "
		}
		line += "Last edited " + ago(time.Unix(d.Info.Updated, 0))
	}
	ah := gtx.Dp(20)
	x := x0
	if avatar != "" {
		if img, ok := v.h.Images().Get(avatar, ah); ok {
			at(gtx, image.Pt(x, y), image.Pt(ah, ah), func(gtx layout.Context) layout.Dimensions {
				return theme.Image(gtx, img, image.Pt(ah, ah), gtx.Dp(5))
			})
		} else {
			theme.RRect(gtx, image.Rect(x, y, x+ah, y+ah), gtx.Dp(5), c.Skeleton)
		}
		x += ah + gtx.Dp(8)
	}
	fw := v.layoutViewers(gtx, image.Pt(x0+cw, y), ah)
	at(gtx, image.Pt(x, y), image.Pt(max(1, x0+cw-fw-x), ah), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, ah, func(gtx layout.Context) layout.Dimensions {
			return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Regular), Size: 13, Color: c.FgMuted, MaxLines: 1}, line)
		})
	})
	y += ah + gtx.Dp(20)
	if d.Info.Editable && v.edit != nil && v.edit.toolbarShown() {
		y += v.edit.layoutToolbar(gtx, v, image.Pt(x0, y), cw)
	}
	return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, y)}
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
