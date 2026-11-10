package listview

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// styles are the richview styles of the description and comments.
type styles struct {
	shaper     *text.Shaper
	dark       bool
	gen        uint64
	desc, body *richview.Style
}

func (s *styles) sync(th *theme.Theme, h Host) {
	if s.desc != nil && s.shaper == th.Shaper && s.dark == th.Dark && s.gen == th.Gen {
		return
	}
	s.shaper, s.dark, s.gen = th.Shaper, th.Dark, th.Gen
	var imgs richview.Images
	if im := h.Images(); im != nil {
		imgs = im
	}
	mk := func(size, line float32) *richview.Style {
		st := richview.NewStyle(th.Shaper, imgs, richview.LightPalette())
		st.Face = theme.Typeface
		st.EmojiURL = th.EmojiURL
		st.TextSize, st.LineHeight = size, line
		st.Embolden = theme.BodyEmboldenFor(theme.LightPalette.Fg)
		if th.Dark {
			st.SetPalette(richview.DarkPalette())
			st.Embolden = theme.BodyEmboldenFor(theme.DarkPalette.Fg)
		}
		if p, ok := richview.Themed(th); ok {
			st.SetPalette(p)
		}
		return st
	}
	s.desc = mk(15, 22)
	s.body = mk(15, 22)
}

// handler opens what richview text links to.
type handler struct {
	h   Host
	act *Actions
}

func (r *handler) OpenURL(u string) {
	if r.act.OpenURL != nil {
		r.act.OpenURL(u)
	}
}
func (r *handler) OpenUser(id string, anchor image.Point) {
	if id != "" {
		r.h.OpenProfile(anchor, id)
	}
}
func (r *handler) OpenChannel(id string) {
	if id != "" {
		r.h.Navigate("c/" + id)
	}
}
func (r *handler) OpenUsergroup(id string, anchor image.Point) {}
func (r *handler) OpenImage(img richtext.Image)                {}
func (r *handler) OpenFile(f *richtext.File) {
	if f.Permalink != "" {
		r.OpenURL(f.Permalink)
	}
}
func (r *handler) Button(b richtext.Button) {
	if b.URL != "" {
		r.OpenURL(b.URL)
	}
}

// emoji draws a :name: emoji (a workspace image or a standard one) at
// size dp.
func (v *View) emoji(gtx layout.Context, name string, size unit.Dp) layout.Dimensions {
	return drawEmoji(gtx, v.h, name, size)
}

func drawEmoji(gtx layout.Context, h Host, name string, size unit.Dp) layout.Dimensions {
	th := h.Theme()
	px := gtx.Dp(size)
	sz := image.Pt(px, px)
	name = strings.Trim(name, ":")
	if a := h.Core(); a != nil {
		if url, txt, ok := a.Emoji(name); ok {
			if url != "" && h.Images() != nil {
				if img, ok := h.Images().Get(url, px); ok {
					return theme.Image(gtx, img, sz, 0)
				}
				return layout.Dimensions{Size: sz}
			}
			if txt != "" {
				gtx.Constraints = layout.Exact(sz)
				return th.Text(gtx, theme.LabelOpts{Size: unit.Sp(float32(size) * 0.85), Color: th.C.Fg, MaxLines: 1}, txt)
			}
		}
	}
	return layout.Dimensions{Size: sz}
}

func avatar(gtx layout.Context, h Host, url string, size unit.Dp) layout.Dimensions {
	s := gtx.Dp(size)
	r := float32(size) * 0.2222
	if r < 4 {
		r = 4
	}
	rad := gtx.Dp(unit.Dp(r))
	sz := image.Pt(s, s)
	if url != "" && h.Images() != nil {
		if img, ok := h.Images().Get(url, s); ok {
			return theme.Image(gtx, img, sz, rad)
		}
	}
	return h.Theme().Placeholder(gtx, sz, rad)
}

// cellOpts say how a cell is drawn.
type cellOpts struct {
	primary bool // the primary column: bold text
	wrap    bool
	size    unit.Sp // text size (15 in the grid, 13 on board cards)
	self    string
}

// momentLayout converts a list column's moment.js date format to Go's.
func momentLayout(f string) string {
	if f == "" || f == "default" {
		return "Jan 2, 2006" // Slack's "MMM D, YYYY"
	}
	r := strings.NewReplacer("YYYY", "2006", "YY", "06", "MMMM", "January", "MMM", "Jan", "MM", "01", "M", "1",
		"dddd", "Monday", "ddd", "Mon", "DD", "02", "D", "2")
	return r.Replace(f)
}

// dateText is a date cell's text: one day, or a range "start to end".
func dateText(c *lists.Column, f *lists.Field) string {
	if f == nil || len(f.Date) == 0 {
		return ""
	}
	lay := momentLayout(c.Options.DateFormat)
	var out []string
	for _, d := range f.Date {
		for _, part := range strings.Split(d, ":") {
			if t, err := time.Parse("2006-01-02", part); err == nil {
				out = append(out, t.Format(lay))
			}
		}
	}
	return strings.Join(out, " to ")
}

// timeText is a created/edited time (unix seconds).
func timeText(f *lists.Field) string {
	if f == nil || len(f.Timestamp) == 0 || f.Timestamp[0] <= 0 {
		return ""
	}
	return time.Unix(int64(f.Timestamp[0]), 0).Format("Jan 2, 2006 at 3:04 PM")
}

func numberText(c *lists.Column, f *lists.Field) string {
	if f == nil || len(f.Number) == 0 {
		return ""
	}
	p := -1
	if c.Options.Precision != nil {
		p = *c.Options.Precision
	}
	s := strconv.FormatFloat(f.Number[0], 'f', p, 64)
	if c.Type == lists.Currency && c.Options.Currency != "" {
		s = c.Options.Currency + " " + s
	}
	return s
}

// CellText is a cell's value as plain text (board cards, tests, copy).
func CellText(a *core.App, c *lists.Column, f *lists.Field) string {
	if f == nil {
		return ""
	}
	switch c.Type {
	case lists.Text, lists.RichText:
		return f.PlainText()
	case lists.Number, lists.Currency:
		return numberText(c, f)
	case lists.Select, lists.MultiSelect:
		var out []string
		for _, s := range f.Select {
			if ch, ok := c.Choice(s); ok {
				out = append(out, ch.Label)
			}
		}
		return strings.Join(out, ", ")
	case lists.Date, lists.DueDate, lists.TodoDueDate:
		return dateText(c, f)
	case lists.CreatedTime, lists.LastEditedTime:
		return timeText(f)
	case lists.User, lists.Assignee, lists.TodoAssignee, lists.CreatedBy, lists.LastEditedBy, lists.Vote:
		var out []string
		for _, u := range f.User {
			if a != nil {
				if us, ok := a.User(u); ok {
					out = append(out, us.Name)
					continue
				}
			}
			out = append(out, u)
		}
		return strings.Join(out, ", ")
	case lists.Channel:
		var out []string
		for _, ch := range f.Channel {
			if a != nil {
				if cv, ok := a.Conv(ch); ok {
					out = append(out, "#"+cv.Name)
					continue
				}
			}
			out = append(out, ch)
		}
		return strings.Join(out, ", ")
	case lists.Email:
		return strings.Join(f.Email, ", ")
	case lists.Phone:
		return strings.Join(f.Phone, ", ")
	case lists.Link:
		var out []string
		for _, l := range f.Link {
			out = append(out, firstNonEmpty(l.DisplayName, l.OriginalURL))
		}
		return strings.Join(out, ", ")
	case lists.Rating:
		if len(f.Rating) > 0 {
			return strconv.Itoa(int(f.Rating[0]))
		}
	case lists.Checkbox, lists.Completed, lists.TodoCompleted:
		if f.Checkbox != nil && *f.Checkbox {
			return "✓"
		}
	case lists.Message:
		if len(f.Message) > 0 {
			return "Message"
		}
	}
	return ""
}

// drawCell draws one cell's value in a w×h box (vertically centred).
func (v *View) drawCell(gtx layout.Context, c *lists.Column, f *lists.Field, w, h int, o cellOpts) {
	drawCell(gtx, v.h, c, f, w, h, o)
}

func drawCell(gtx layout.Context, hst Host, c *lists.Column, f *lists.Field, w, h int, o cellOpts) {
	th := hst.Theme()
	col := &th.C
	a := hst.Core()
	if o.size == 0 {
		o.size = 15
	}
	defer clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops).Pop()
	label := func(s string, colr theme.LabelOpts) {
		at(gtx, image.Pt(0, 0), image.Pt(w, h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.X = w
				return th.Text(gtx, colr, s)
			})
		})
	}
	plain := theme.LabelOpts{Font: theme.Font(theme.Regular), Size: o.size, Color: col.Fg, MaxLines: 1}
	if o.primary {
		plain.Font = theme.Font(theme.Bold)
	}
	if o.wrap {
		plain.MaxLines = 2
	}
	switch c.Type {
	case lists.Text, lists.RichText:
		label(f.PlainText(), plain)
	case lists.Number, lists.Currency:
		label(numberText(c, f), plain)
	case lists.Email, lists.Phone:
		lo := plain
		label(CellText(a, c, f), lo)
	case lists.Link:
		lo := plain
		lo.Color = col.Link
		label(CellText(a, c, f), lo)
	case lists.Date, lists.DueDate, lists.TodoDueDate:
		label(dateText(c, f), plain)
	case lists.CreatedTime, lists.LastEditedTime:
		label(timeText(f), plain)
	case lists.Select, lists.MultiSelect:
		if f == nil {
			return
		}
		x := 0
		for _, s := range f.Select {
			ch, ok := c.Choice(s)
			if !ok {
				continue
			}
			pw := optionPill(gtx, th, x, h, ch, o.size)
			x += pw + gtx.Dp(4)
			if x > w {
				break
			}
		}
	case lists.User, lists.Assignee, lists.TodoAssignee, lists.CreatedBy, lists.LastEditedBy:
		if f == nil {
			return
		}
		people(gtx, hst, f.User, c.ShowsMemberName() && !(c.Multi() && len(f.User) > 1), w, h, o.size)
	case lists.Channel:
		lo := plain
		lo.Color = col.Link
		label(CellText(a, c, f), lo)
	case lists.Checkbox, lists.Completed:
		checked := f != nil && f.Checkbox != nil && *f.Checkbox
		at(gtx, image.Pt(0, (h-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
			return checkbox(gtx, th, checked)
		})
	case lists.TodoCompleted:
		checked := f != nil && f.Checkbox != nil && *f.Checkbox
		ic, colr := "check-circle", col.FgSubtle
		if checked {
			ic, colr = "check-circle-filled", shade(th, "jade-50")
		}
		at(gtx, image.Pt(0, (h-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get(ic)(gtx, 20, colr)
		})
	case lists.Rating:
		n := 0
		if f != nil && len(f.Rating) > 0 {
			n = int(f.Rating[0])
		}
		emo := firstNonEmpty(c.Options.Emoji, ":star:")
		for i := 0; i < max(1, c.Options.Max); i++ {
			x := i * gtx.Dp(26)
			if x+gtx.Dp(16) > w {
				break
			}
			func() {
				if i >= n {
					defer paint.PushOpacity(gtx.Ops, 0.25).Pop()
				}
				at(gtx, image.Pt(x, (h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return drawEmoji(gtx, hst, emo, 16)
				})
			}()
		}
	case lists.Vote:
		n := 0
		mine := false
		if f != nil {
			n = len(f.User)
			for _, u := range f.User {
				mine = mine || u == o.self
			}
		}
		if n == 0 {
			return
		}
		emo := firstNonEmpty(c.Options.Emoji, ":+1:")
		s := strconv.Itoa(n)
		tw := th.Measure(gtx, 13, theme.Bold, s)
		pw := gtx.Dp(8+16+4+8) + tw
		ph := gtx.Dp(24)
		at(gtx, image.Pt(0, (h-ph)/2), image.Pt(pw, ph), func(gtx layout.Context) layout.Dimensions {
			bg, br := col.BgInset, col.Border
			if mine {
				bg, br = theme.WithAlpha(col.Link, 0x1a), col.Link
			}
			theme.RRect(gtx, image.Rect(0, 0, pw, ph), ph/2, bg)
			theme.Border(gtx, image.Rect(0, 0, pw, ph), ph/2, gtx.Dp(1), br)
			at(gtx, image.Pt(gtx.Dp(8), (ph-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
				return drawEmoji(gtx, hst, emo, 16)
			})
			return at(gtx, image.Pt(gtx.Dp(28), 0), image.Pt(tw+2, ph), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, ph, func(gtx layout.Context) layout.Dimensions {
					tc := col.Fg
					if mine {
						tc = col.Link
					}
					return th.Label(gtx, 13, theme.Bold, tc, s)
				})
			})
		})
	case lists.Attachment, lists.Canvas:
		if f == nil || len(f.Attachment) == 0 {
			return
		}
		ic := "attach"
		if c.Type == lists.Canvas {
			ic = "canvas"
		}
		x := 0
		for _, id := range f.Attachment {
			name := fileTitle(hst, id)
			tw := th.Measure(gtx, 13, theme.Bold, name)
			pw := min(gtx.Dp(8+16+6+8)+tw, w-x)
			if pw < gtx.Dp(40) {
				break
			}
			ph := gtx.Dp(26)
			at(gtx, image.Pt(x, (h-ph)/2), image.Pt(pw, ph), func(gtx layout.Context) layout.Dimensions {
				theme.RRect(gtx, image.Rect(0, 0, pw, ph), gtx.Dp(6), col.BgInset)
				theme.Border(gtx, image.Rect(0, 0, pw, ph), gtx.Dp(6), gtx.Dp(1), col.Border)
				at(gtx, image.Pt(gtx.Dp(8), (ph-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
					return icons.Get(ic)(gtx, 16, col.FgMuted)
				})
				return at(gtx, image.Pt(gtx.Dp(30), 0), image.Pt(max(0, pw-gtx.Dp(36)), ph), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, ph, func(gtx layout.Context) layout.Dimensions {
						return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Bold), Size: 13, Color: col.Fg, MaxLines: 1}, name)
					})
				})
			})
			x += pw + gtx.Dp(4)
		}
	case lists.Message:
		if f == nil || len(f.Message) == 0 {
			return
		}
		at(gtx, image.Pt(0, (h-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("message")(gtx, 16, col.FgMuted)
		})
		lo := plain
		lo.Color = col.Link
		at(gtx, image.Pt(gtx.Dp(22), 0), image.Pt(max(0, w-gtx.Dp(22)), h), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
				return th.Text(gtx, lo, messageLabel(hst, f.Message[0]))
			})
		})
	default:
		label(CellText(a, c, f), plain)
	}
}

// messageLabel names a message cell ("Message in #general").
func messageLabel(h Host, m lists.MessageValue) string {
	if a := h.Core(); a != nil && m.ChannelID != "" {
		if c, ok := a.Conv(m.ChannelID); ok && c.Name != "" && c.Kind != "im" && c.Kind != "mpim" {
			return "Message in #" + c.Name
		}
	}
	return "Message"
}

// optionPill draws a select option's pill at x; returns its width.
func optionPill(gtx layout.Context, th *theme.Theme, x, h int, ch lists.Choice, size unit.Sp) int {
	ts := size - 2
	if ts < 12 {
		ts = 12
	}
	tw := th.Measure(gtx, ts, theme.Regular, ch.Label)
	pw := tw + gtx.Dp(16)
	ph := gtx.Dp(24)
	at(gtx, image.Pt(x, (h-ph)/2), image.Pt(pw, ph), func(gtx layout.Context) layout.Dimensions {
		theme.RRect(gtx, image.Rect(0, 0, pw, ph), gtx.Dp(6), optionBg(th, ch.Color))
		return at(gtx, image.Pt(gtx.Dp(8), 0), image.Pt(tw+2, ph), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, ph, func(gtx layout.Context) layout.Dimensions {
				return th.Label(gtx, ts, theme.Regular, th.C.Fg, ch.Label)
			})
		})
	})
	return pw
}

// people draws avatars (20px) and, with names, the first person's name.
func people(gtx layout.Context, h Host, ids []string, names bool, w, hh int, size unit.Sp) {
	th := h.Theme()
	a := h.Core()
	x := 0
	for i, id := range ids {
		url, name := "", id
		if a != nil {
			if u, ok := a.User(id); ok {
				url, name = u.Avatar, u.Name
			}
		}
		at(gtx, image.Pt(x, (hh-gtx.Dp(20))/2), image.Pt(gtx.Dp(20), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
			return avatar(gtx, h, url, 20)
		})
		x += gtx.Dp(20)
		if names {
			x += gtx.Dp(6)
			nw := w - x
			at(gtx, image.Pt(x, 0), image.Pt(max(0, nw), hh), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = max(0, nw)
					return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Regular), Size: size, Color: th.C.Fg, MaxLines: 1}, name)
				})
			})
			return
		}
		x += gtx.Dp(4)
		if x+gtx.Dp(20) > w && i < len(ids)-1 {
			break
		}
	}
}

// checkbox is Slack's list checkbox: a jade disc with a white check when
// checked (p-field_renderer__checkbox--checked), a ring when not.
func checkbox(gtx layout.Context, th *theme.Theme, checked bool) layout.Dimensions {
	s := gtx.Constraints.Max
	ctr := image.Pt(s.X/2, s.Y/2)
	if checked {
		theme.Circle(gtx, ctr, s.X, shade(th, "jade-50"))
		at(gtx, image.Pt(gtx.Dp(3), gtx.Dp(3)), image.Pt(s.X-gtx.Dp(6), s.Y-gtx.Dp(6)), func(gtx layout.Context) layout.Dimensions {
			return icons.Get("check")(gtx, 14, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		})
		return layout.Dimensions{Size: s}
	}
	// a 1.5px ring in content-ter (#1d1c1d80)
	stroke := float32(gtx.Metric.PxPerDp) * 1.5
	d := float32(s.X) - stroke
	off := stroke / 2
	ell := clip.Ellipse{Min: image.Pt(int(off), int(off)), Max: image.Pt(int(off+d+0.5), int(off+d+0.5))}
	paint.FillShape(gtx.Ops, th.C.FgSubtle, clip.Stroke{Path: ell.Path(gtx.Ops), Width: stroke}.Op())
	return layout.Dimensions{Size: s}
}

// fileTitle is an attachment's or canvas's title, read once with
// files.info (core caches nothing here, so the view keeps it).
func fileTitle(h Host, id string) string {
	fileTitlesMu.Lock()
	t, ok := fileTitles[id]
	fileTitlesMu.Unlock()
	if ok {
		if t == "" {
			return "File"
		}
		return t
	}
	fileTitlesMu.Lock()
	fileTitles[id] = ""
	fileTitlesMu.Unlock()
	if a := h.Core(); a != nil {
		go func() {
			ctx, cancel := ctxTimeout(20 * time.Second)
			defer cancel()
			if t, err := a.FileTitle(ctx, id); err == nil {
				fileTitlesMu.Lock()
				fileTitles[id] = t
				fileTitlesMu.Unlock()
				h.Invalidate()
			}
		}()
	}
	return "File"
}

var (
	fileTitlesMu sync.Mutex
	fileTitles   = map[string]string{}
)

func ctxTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
