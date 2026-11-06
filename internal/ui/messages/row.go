package messages

import (
	"context"
	"image"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// row is the cached UI state of one message.
type row struct {
	ts  string
	rev uint32
	m   core.Message

	rv      *richview.View
	handler richHandler

	// resolved author (refreshed when users change)
	uRev      uint64
	name      string
	avatar    string
	statusURL string
	bot       bool

	nameBtn, avatarBtn widget.Clickable
	threadBtn          widget.Clickable
	broadcastBtn       widget.Clickable
	addReact           widget.Clickable
	reacts             []widget.Clickable

	// geometry of the last layout (row-local), for overlays
	reactRects []image.Rectangle
	addRect    image.Rectangle
	height     int
}

// rowCtx carries per-frame inputs to a row layout.
type rowCtx struct {
	h         Host
	th        *theme.Theme
	st        *richview.Style
	c         *colors
	header    bool // first of a group: avatar + name line
	hovered   bool
	thread    bool // inside a thread pane (no thread bar, no "replied to")
	parent    bool // the thread parent row
	now       time.Time
	selfID    string
	usersRev  uint64
	origin    func(image.Point) image.Point
	highlight float32 // 0..1 flash for jump targets
	onReact   func(r *row, name string, add bool)
}

func newRow(e *entry) *row {
	r := &row{ts: e.m.TS}
	r.set(e)
	return r
}

func (r *row) set(e *entry) {
	r.rev = e.rev
	r.m = e.m
	opts := richview.Options{Edited: e.m.Edited, Muted: e.m.System, Dim: e.m.Pending}
	if r.rv == nil {
		r.rv = richview.New(e.m.Doc, opts)
	} else {
		r.rv.Set(e.m.Doc, opts)
	}
	r.uRev = 0
	if len(r.reacts) < len(e.m.Reactions) {
		r.reacts = append(r.reacts, make([]widget.Clickable, len(e.m.Reactions)-len(r.reacts))...)
	}
}

func (r *row) resolve(rc *rowCtx) {
	if r.uRev == rc.usersRev && r.uRev != 0 {
		return
	}
	r.uRev = rc.usersRev
	m := &r.m
	r.name, r.avatar, r.statusURL, r.bot = m.Name, m.Avatar, "", m.IsBot || (m.BotID != "" && m.User == "")
	if m.User != "" && rc.h.Core() != nil {
		if u, ok := rc.h.Core().User(m.User); ok {
			if r.name == "" {
				r.name = u.Name
				if r.name == "" {
					r.name = u.RealName
				}
			}
			if r.avatar == "" {
				r.avatar = u.Avatar
			}
			r.statusURL = u.StatusEmojiURL
			if u.IsBot {
				r.bot = true
			}
		} else {
			ensureUser(rc.h, m.User)
		}
	}
	if r.name == "" {
		if m.User != "" {
			r.name = m.User
		} else {
			r.name = "Unknown"
		}
	}
}

var askedUsers = map[string]bool{}

// ensureUser asks core for a missing user once (UI goroutine only).
func ensureUser(h Host, id string) {
	if askedUsers[id] || h.Core() == nil {
		return
	}
	askedUsers[id] = true
	app := h.Core()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		app.EnsureUsers(ctx, []string{id})
		h.Invalidate()
	}()
}

// layout draws the row at full width and returns its size.
func (r *row) layout(gtx layout.Context, rc *rowCtx) layout.Dimensions {
	r.resolve(rc)
	c := rc.c
	w := gtx.Constraints.Max.X
	padX := dp(gtx, mt.padX)
	cx := dp(gtx, mt.contentX)
	cw := w - cx - padX
	if cw < dp(gtx, 80) {
		cw = dp(gtx, 80)
	}
	m := &r.m

	// Record content first; the background depends on the height.
	rec := op.Record(gtx.Ops)
	var opac paint.OpacityStack
	if m.Pending {
		opac = paint.PushOpacity(gtx.Ops, 0.5)
	}
	y := dp(gtx, mt.padTop) - dp(gtx, 4)

	if m.Pinned && !rc.thread {
		y += r.labelLine(gtx, rc, cx, y, "pin", "Pinned")
	}
	if m.Broadcast && !rc.thread && m.ThreadTS != "" && m.ThreadTS != m.TS {
		y += r.broadcastLine(gtx, rc, cx, y)
	}

	if rc.header {
		// avatar
		ar := image.Rectangle{Min: image.Pt(padX, y+dp(gtx, 4)), Max: image.Pt(padX+dp(gtx, mt.avatar), y+dp(gtx, 4)+dp(gtx, mt.avatar))}
		at(gtx, ar.Min.X, ar.Min.Y, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(ar.Size())
			return r.avatarBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				drawAvatar(gtx, rc.h, r.avatar, image.Rectangle{Max: ar.Size()}, c)
				return layout.Dimensions{Size: ar.Size()}
			})
		})
		if r.avatarBtn.Clicked(gtx) || r.nameBtn.Clicked(gtx) {
			if m.User != "" {
				rc.h.OpenProfile(rc.origin(image.Pt(cx, y+dp(gtx, 22))), m.User)
			}
		}
		y += r.nameLine(gtx, rc, cx, y, cw)
	} else if rc.hovered {
		// hover timestamp in the gutter, right-aligned to the avatar column
		ts := shortClock(tsTime(m.TS))
		s := textStyle{size: 12, col: c.Text2, lineH: 22}
		sz := measure(gtx, rc.th, s, ts)
		at(gtx, padX+dp(gtx, mt.avatar)-sz.X, y, func(gtx layout.Context) layout.Dimensions { return label(gtx, rc.th, s, ts) })
	}

	// body
	r.handler.h, r.handler.m = rc.h, m
	r.handler.anchor = rc.origin
	body := at(gtx, cx, y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(cw, 1<<20)}
		return r.rv.Layout(gtx, rc.st, &r.handler)
	})
	y += body.Size.Y
	if body.Size.Y == 0 && !rc.header {
		y += dp(gtx, mt.nameLine)
	}
	y += dp(gtx, mt.bodyMarginBottom)

	// reactions
	if len(m.Reactions) > 0 {
		y += dp(gtx, mt.reactTop) - dp(gtx, 4)
		y += r.reactions(gtx, rc, cx, y, cw)
		y += dp(gtx, mt.reactBottom)
	} else {
		r.reactRects = r.reactRects[:0]
		r.addRect = image.Rectangle{}
	}

	// thread bar
	if m.ReplyCount > 0 && !rc.thread && (m.ThreadTS == "" || m.ThreadTS == m.TS) {
		y += r.threadBar(gtx, rc, cx, y, cw)
		y += dp(gtx, 8)
	}
	if m.Pending {
		opac.Pop()
	}
	content := rec.Stop()

	h := y + dp(gtx, mt.padBottom) - dp(gtx, 8)
	if rc.header {
		if mh := dp(gtx, mt.padTop) + dp(gtx, mt.avatar) + dp(gtx, mt.padBottom); h < mh {
			h = mh
		}
	}
	size := image.Pt(w, h)
	bg := c.Bg
	bg.A = 0
	switch {
	case m.Pinned && rc.hovered:
		bg = c.PinnedHover
	case m.Pinned:
		bg = c.Pinned
	case rc.hovered:
		bg = c.Hover
	}
	fill(gtx.Ops, image.Rectangle{Max: size}, bg)
	if rc.highlight > 0 {
		hl := c.Highlight
		hl.A = uint8(float32(hl.A) * rc.highlight)
		fill(gtx.Ops, image.Rectangle{Max: size}, hl)
	}
	content.Add(gtx.Ops)
	r.height = h
	return layout.Dimensions{Size: size}
}

// nameLine draws "Name [APP] [status] 3:04 PM" and returns its height.
func (r *row) nameLine(gtx layout.Context, rc *rowCtx, x, y, w int) int {
	c := rc.c
	lh := dp(gtx, mt.nameLine)
	nameS := textStyle{size: 15, weight: theme.Black, col: c.Text, lineH: 22}
	nd := at(gtx, x, y, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w*2/3, lh)}
		return r.nameBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			d := label(gtx, rc.th, nameS, r.name)
			if r.nameBtn.Hovered() {
				base := d.Size.Y - d.Baseline
				fill(gtx.Ops, image.Rect(0, base+dp(gtx, 2), d.Size.X, base+dp(gtx, 3)), c.Text)
			}
			return d
		})
	})
	baseline := y + nd.Size.Y - nd.Baseline
	xx := x + nd.Size.X
	if r.bot {
		xx += dp(gtx, mt.badgeGap)
		s := textStyle{size: 10, weight: theme.Bold, col: c.BadgeText}
		sz := measure(gtx, rc.th, s, "APP")
		bw := sz.X + dp(gtx, 6)
		bh := dp(gtx, 14)
		top := baseline - dp(gtx, 11)
		rrect(gtx.Ops, image.Rect(xx, top, xx+bw, top+bh), dp(gtx, 2), c.BadgeBg)
		at(gtx, xx+dp(gtx, 3), top+(bh-sz.Y)/2, func(gtx layout.Context) layout.Dimensions { return label(gtx, rc.th, s, "APP") })
		xx += bw
	}
	if r.statusURL != "" {
		s := dp(gtx, mt.statusEmoji)
		xx += dp(gtx, 4)
		top := baseline - s + dp(gtx, 3)
		drawEmoji(gtx, rc.h, r.statusURL, image.Rect(xx, top, xx+s, top+s))
		xx += s
	}
	xx += dp(gtx, mt.tsGap)
	tsS := textStyle{size: 12, col: c.Text2}
	tsText := clock(tsTime(r.m.TS))
	// Align baselines.
	m := op.Record(gtx.Ops)
	d := label(gtx, rc.th, tsS, tsText)
	call := m.Stop()
	t := op.Offset(image.Pt(xx, baseline-(d.Size.Y-d.Baseline))).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()
	return lh
}

// labelLine draws a small gray label with an icon above the name ("Pinned").
func (r *row) labelLine(gtx layout.Context, rc *rowCtx, x, y int, icon, txt string) int {
	c := rc.c
	h := dp(gtx, 18)
	is := dp(gtx, 12)
	at(gtx, x, y+(h-is)/2+dp(gtx, 4), func(gtx layout.Context) layout.Dimensions {
		return icons.Get(icon)(gtx, unit.Dp(12), c.Text2)
	})
	at(gtx, x+is+dp(gtx, 4), y+dp(gtx, 4), func(gtx layout.Context) layout.Dimensions {
		return label(gtx, rc.th, textStyle{size: 13, col: c.Text2, lineH: 18}, txt)
	})
	return h + dp(gtx, 4)
}

// broadcastLine draws "replied to a thread: …" for thread replies also sent
// to the channel.
func (r *row) broadcastLine(gtx layout.Context, rc *rowCtx, x, y int) int {
	c := rc.c
	h := dp(gtx, 18)
	s := textStyle{size: 13, col: c.Text2, lineH: 18}
	pre := "replied to a thread: "
	pd := at(gtx, x, y+dp(gtx, 4), func(gtx layout.Context) layout.Dimensions { return label(gtx, rc.th, s, pre) })
	at(gtx, x+pd.Size.X, y+dp(gtx, 4), func(gtx layout.Context) layout.Dimensions {
		return r.broadcastBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return label(gtx, rc.th, textStyle{size: 13, col: c.Link, lineH: 18}, "View thread")
		})
	})
	if r.broadcastBtn.Clicked(gtx) {
		rc.h.OpenThread(r.m.Channel, r.m.ThreadTS)
	}
	return h + dp(gtx, 4)
}

// reactions draws the reaction pills and the add button; returns height.
func (r *row) reactions(gtx layout.Context, rc *rowCtx, x, y, w int) int {
	c := rc.c
	m := &r.m
	if len(r.reacts) < len(m.Reactions) {
		r.reacts = append(r.reacts, make([]widget.Clickable, len(m.Reactions)-len(r.reacts))...)
	}
	r.reactRects = r.reactRects[:0]
	ph := dp(gtx, mt.reactH)
	gap := dp(gtx, mt.reactGap)
	es := dp(gtx, mt.reactEmoji)
	cx, cy := x, y
	countS := textStyle{size: 11, weight: theme.Bold, col: c.Text, lineH: 16}
	for i := range m.Reactions {
		re := &m.Reactions[i]
		cnt := strconv.Itoa(re.Count)
		cs := countS
		if re.Self {
			cs.col = c.ReactSelfText
		}
		tw := measure(gtx, rc.th, cs, cnt).X
		pw := dp(gtx, 6) + es + dp(gtx, 4) + tw + dp(gtx, 8)
		if cx > x && cx+pw > x+w {
			cx = x
			cy += ph + gap
		}
		rect := image.Rect(cx, cy, cx+pw, cy+ph)
		btn := &r.reacts[i]
		if btn.Clicked(gtx) && rc.onReact != nil {
			rc.onReact(r, re.Name, !re.Self)
		}
		at(gtx, cx, cy, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(rect.Size())
			return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				rr := image.Rectangle{Max: rect.Size()}
				rad := ph / 2
				hov := btn.Hovered()
				switch {
				case re.Self && hov:
					rrect(gtx.Ops, rr, rad, c.ReactSelfHoverBg)
					border(gtx.Ops, rr, rad, dp(gtx, 1), c.ReactSelfBorder)
				case re.Self:
					rrect(gtx.Ops, rr, rad, c.ReactSelfBg)
					border(gtx.Ops, rr, rad, dp(gtx, 1), c.ReactSelfBorder)
				case hov:
					rrect(gtx.Ops, rr, rad, c.Bg)
					border(gtx.Ops, rr, rad, dp(gtx, 1), c.ReactHoverBorder)
				default:
					rrect(gtx.Ops, rr, rad, c.ReactBg)
				}
				ex := dp(gtx, 6)
				er := image.Rect(ex, (ph-es)/2, ex+es, (ph-es)/2+es)
				if !drawEmoji(gtx, rc.h, re.URL, er) {
					at(gtx, ex, 0, func(gtx layout.Context) layout.Dimensions {
						return label(gtx, rc.th, textStyle{size: 11, col: c.Text2, lineH: 24}, ":"+re.Name+":")
					})
				}
				at(gtx, ex+es+dp(gtx, 4), (ph-dp(gtx, 16))/2, func(gtx layout.Context) layout.Dimensions {
					return label(gtx, rc.th, cs, cnt)
				})
				return layout.Dimensions{Size: rect.Size()}
			})
		})
		r.reactRects = append(r.reactRects, rect)
		cx += pw + gap
	}
	// add-reaction button, visible on hover
	aw := dp(gtx, 32)
	if cx > x && cx+aw > x+w {
		cx = x
		cy += ph + gap
	}
	r.addRect = image.Rect(cx, cy, cx+aw, cy+ph)
	if rc.hovered {
		at(gtx, cx, cy, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(r.addRect.Size())
			return r.addReact.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				rr := image.Rectangle{Max: r.addRect.Size()}
				if r.addReact.Hovered() {
					rrect(gtx.Ops, rr, ph/2, c.Bg)
					border(gtx.Ops, rr, ph/2, dp(gtx, 1), c.ReactHoverBorder)
				} else {
					rrect(gtx.Ops, rr, ph/2, c.ReactBg)
				}
				is := dp(gtx, 16)
				col := c.Text2
				if r.addReact.Hovered() {
					col = c.Text
				}
				at(gtx, (rr.Dx()-is)/2, (rr.Dy()-is)/2, func(gtx layout.Context) layout.Dimensions {
					return icons.Get("add-reaction")(gtx, unit.Dp(16), col)
				})
				return layout.Dimensions{Size: rr.Size()}
			})
		})
	}
	if r.addReact.Clicked(gtx) {
		rr := r
		rc.h.OpenEmojiPicker(rc.origin(image.Pt(cx, cy)), func(name string) {
			if rc.onReact != nil {
				rc.onReact(rr, name, true)
			}
		})
	}
	return cy + ph - y
}

// threadBar draws the reply summary; returns its height.
func (r *row) threadBar(gtx layout.Context, rc *rowCtx, x, y, w int) int {
	c := rc.c
	m := &r.m
	bh := dp(gtx, mt.threadH)
	bw := w + dp(gtx, 5)
	if mx := dp(gtx, 600); bw > mx {
		bw = mx
	}
	bx := x - dp(gtx, 5)
	if r.threadBtn.Clicked(gtx) {
		rc.h.OpenThread(m.Channel, m.TS)
	}
	at(gtx, bx, y-dp(gtx, 2), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(bw, bh))
		return r.threadBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			rr := image.Rectangle{Max: image.Pt(bw, bh)}
			hov := r.threadBtn.Hovered()
			if hov {
				rrect(gtx.Ops, rr, dp(gtx, 8), c.ThreadBg)
				border(gtx.Ops, rr, dp(gtx, 8), dp(gtx, 1), c.ThreadBorder)
			}
			pad := dp(gtx, 6)
			as := dp(gtx, mt.threadAvatar)
			ax := pad
			users := m.ReplyUsers
			if len(users) > 5 {
				users = users[:5]
			}
			for _, id := range users {
				url := ""
				if rc.h.Core() != nil {
					if u, ok := rc.h.Core().User(id); ok {
						url = u.Avatar
					} else {
						ensureUser(rc.h, id)
					}
				}
				drawAvatar(gtx, rc.h, url, image.Rect(ax, (bh-as)/2, ax+as, (bh-as)/2+as), c)
				ax += as + dp(gtx, 4)
			}
			ax += dp(gtx, 4)
			n := "1 reply"
			if m.ReplyCount != 1 {
				n = strconv.Itoa(m.ReplyCount) + " replies"
			}
			ty := (bh - dp(gtx, 18)) / 2
			nd := at(gtx, ax, ty, func(gtx layout.Context) layout.Dimensions {
				return label(gtx, rc.th, textStyle{size: 13, weight: theme.Bold, col: c.Link, lineH: 18}, n)
			})
			ax += nd.Size.X + dp(gtx, 8)
			desc := ""
			if hov {
				desc = "View thread"
			} else if m.LatestReply != "" {
				desc = "Last reply " + ago(tsTime(m.LatestReply), rc.now)
			}
			at(gtx, ax, ty, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.X = bw - ax - dp(gtx, 28)
				return label(gtx, rc.th, textStyle{size: 13, col: c.Text2, lineH: 18}, desc)
			})
			if hov {
				is := dp(gtx, 16)
				at(gtx, bw-pad-is, (bh-is)/2, func(gtx layout.Context) layout.Dimensions {
					return icons.Get("chevron-right")(gtx, unit.Dp(16), c.Text2)
				})
			}
			return layout.Dimensions{Size: rr.Size()}
		})
	})
	return bh - dp(gtx, 2)
}

// reactionTip is the tooltip text for a reaction pill.
func reactionTip(h Host, re *core.Reaction, selfID string) string {
	names := make([]string, 0, 4)
	for i, id := range re.Users {
		if i >= 3 {
			break
		}
		if id == selfID {
			names = append(names, "you")
			continue
		}
		n := id
		if h.Core() != nil {
			if u, ok := h.Core().User(id); ok && u.Name != "" {
				n = u.Name
			}
		}
		names = append(names, n)
	}
	others := re.Count - len(names)
	var who string
	switch {
	case len(names) == 0:
		who = strconv.Itoa(re.Count) + " people"
	case others > 0:
		who = strings.Join(names, ", ") + " and " + strconv.Itoa(others) + " others"
	case len(names) == 1:
		who = names[0]
	default:
		who = strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
	if len(names) > 0 && names[0] == "you" {
		who = "You" + who[3:]
	}
	return who + " reacted with :" + re.Name + ":"
}

// skeleton draws a loading placeholder row.
func skeleton(gtx layout.Context, c *colors, i int) layout.Dimensions {
	w := gtx.Constraints.Max.X
	h := dp(gtx, 60)
	padX := dp(gtx, mt.padX)
	a := dp(gtx, mt.avatar)
	rrect(gtx.Ops, image.Rect(padX, dp(gtx, 8), padX+a, dp(gtx, 8)+a), avatarRadius(a), c.Skeleton)
	cx := dp(gtx, mt.contentX)
	widths := []int{120, 90, 160}
	nw := dp(gtx, float32(widths[i%3]))
	rrect(gtx.Ops, image.Rect(cx, dp(gtx, 10), cx+nw, dp(gtx, 22)), dp(gtx, 6), c.Skeleton)
	bw := (w - cx - padX) * (60 + (i*23)%35) / 100
	rrect(gtx.Ops, image.Rect(cx, dp(gtx, 30), cx+bw, dp(gtx, 42)), dp(gtx, 6), c.Skeleton)
	return layout.Dimensions{Size: image.Pt(w, h)}
}
