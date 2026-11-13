package views

// LinkDetailsView (the client-link-details-view chunk DEJt, CDN build
// 0e695dd18302): the secondary pane LinkEntity's View link details opens
// (toLinkDetails {url, title, iconUrl}). Its header is the view's "Link"
// title with no bottom border; the body (.container: the pane's height,
// .content: 16px 16px 0) is
//
//   - .metadataContainer (16 under): the h2 title (.title: the 24px
//     favicon, else the link glyph at 20, 8px before the title in h2's
//     28/32 Bold, one line), the URL as a link (.url, 12 under; one line),
//     "First shared by {firstSharer}" (Bold) for the first share's author
//     and "Shared {date}" / "Shared on {date}" (Bold; the relative day
//     lower-cased, uncapitalizedRelativeDay) for its ts;
//   - while search.modules.messages for the URL runs (core.LinkShares,
//     oldest first) a spinner; none found: "No conversations found";
//     else .shareDividerContainer (8px 0 12px: "Conversations {count}",
//     13px Bold content-ter, 10px before an otl-ter rule) and one
//     FileShareDetailsMessageEntity per conversation, 12 apart
//     (padding_bottom_75): .msgEntityItem (1px otl-ter, radius 12, 12px
//     padding), its header (12px/18: "Thread in {channel}" Bold
//     content-sec, the time at the right in content-ter: {time} today,
//     else {date_short_pretty}), the sender (4px 0: the 20px avatar, 8px,
//     the Bold name) and the message (4px 0). A click opens the thread at
//     the message (threadTs ?? shareTs, highlightTs shareTs).

import (
	"context"
	"image"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var nsChannelLinks = i18n.NS("channel_links")

// LinkDetails is the link details pane.
type LinkDetails struct {
	base
	link, title, favURL string
	data                async[[]core.LinkShare]
	closeBtn, urlBtn    widget.Clickable
	cards               map[string]*widget.Clickable
	list                widget.List
	// Close runs on the header's ✕ (the ui root closes the pane).
	Close func()
}

// NewLinkDetails builds the pane for link (title and icon as LinkEntity
// passes them).
func NewLinkDetails(h Host, link, title, icon string) *LinkDetails {
	v := &LinkDetails{base: newBase(h), link: link, title: title, favURL: icon, cards: map[string]*widget.Clickable{}}
	v.list.Axis = layout.Vertical
	if a := h.Core(); a != nil {
		v.data.load(h, false, func(ctx context.Context) ([]core.LinkShare, error) { return a.LinkShares(ctx, link) })
	}
	return v
}

// URL is the link the pane shows.
func (v *LinkDetails) URL() string { return v.link }

// Shares are the conversations found so far and whether the search answered.
func (v *LinkDetails) Shares() ([]core.LinkShare, bool) {
	s, ok, _ := v.data.get()
	return s, ok && !v.data.busy()
}

// Layout draws the pane.
func (v *LinkDetails) Layout(gtx C) D {
	v.frame()
	sz := gtx.Constraints.Max
	fill(gtx, image.Rectangle{Max: sz}, v.p.Bg)
	if v.closeBtn.Clicked(gtx) && v.Close != nil {
		v.Close()
	}
	if v.urlBtn.Clicked(gtx) {
		if o, ok := v.h.(interface{ OpenURL(string) }); ok {
			o.OpenURL(v.link)
		}
	}
	shares, ok, _ := v.data.get()
	for _, s := range shares {
		if c := v.cards[s.Channel+"/"+s.Message.TS]; c != nil && c.Clicked(gtx) {
			ts := s.Message.ThreadTS
			if ts == "" {
				ts = s.Message.TS
			}
			v.h.OpenThread(s.Channel, ts)
		}
	}
	// the header: "Link", ✕ (hideBottomBorder)
	hh := gtx.Dp(theme.HeaderH)
	dirAt(gtx, image.Pt(gtx.Dp(20), 0), func(gtx C) D {
		gtx.Constraints = layout.Exact(image.Pt(max(0, sz.X-gtx.Dp(76)), hh))
		return layout.W.Layout(gtx, func(gtx C) D { return v.label(gtx, 18, wBlack, v.p.Text, nsChannelLinks.T("Link")) })
	})
	bs := gtx.Dp(28)
	dirAt(gtx, image.Pt(sz.X-gtx.Dp(8)-bs, (hh-bs)/2), func(gtx C) D {
		return v.iconButton(gtx, &v.closeBtn, "close", 28, 18, v.p.Sec)
	})
	body := gtx
	body.Constraints = layout.Exact(image.Pt(sz.X, max(0, sz.Y-hh)))
	dirAt(gtx, image.Pt(0, hh), func(gtx C) D {
		gtx = body
		items := v.rows(shares, ok && !v.data.busy())
		return v.scrollList(gtx, &v.list, len(items), func(gtx C, i int) D {
			return layout.Inset{Left: 16, Right: 16}.Layout(gtx, items[i])
		})
	})
	return D{Size: sz}
}

// rows are the body's pieces in order.
func (v *LinkDetails) rows(shares []core.LinkShare, done bool) []W {
	out := []W{func(gtx C) D {
		return layout.Inset{Top: 16, Bottom: 16}.Layout(gtx, func(gtx C) D { return v.metadata(gtx, shares) })
	}}
	switch {
	case !done:
		out = append(out, func(gtx C) D { return v.skeleton(gtx, 2, 20) })
	case len(shares) == 0:
		out = append(out, func(gtx C) D {
			return v.para(gtx, 15, 22, wReg, v.p.Text, 0, nsChannelLinks.T("No conversations found"))
		})
	default:
		out = append(out, v.divider(len(shares)))
		for i := range shares {
			s := shares[i]
			out = append(out, func(gtx C) D {
				return layout.Inset{Bottom: 12}.Layout(gtx, func(gtx C) D { return v.card(gtx, &s) })
			})
		}
	}
	return out
}

func (v *LinkDetails) metadata(gtx C, shares []core.LinkShare) D {
	title := v.title
	if title == "" {
		title = v.link
	}
	ch := []layout.FlexChild{
		layout.Rigid(func(gtx C) D {
			// h2: margin 0 0 1rem
			return layout.Inset{Bottom: 16}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return v.favicon(gtx) }),
					hgap(8),
					layout.Flexed(1, func(gtx C) D {
						return v.para(gtx, 28, 32, wBold, v.p.Text, 1, title)
					}),
				)
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 12}.Layout(gtx, func(gtx C) D {
				return v.urlBtn.Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					d := v.para(gtx, 15, 22, wReg, v.p.Link, 1, v.link)
					if v.urlBtn.Hovered() {
						fill(gtx, image.Rect(0, d.Size.Y-gtx.Dp(3), d.Size.X, d.Size.Y-gtx.Dp(2)), v.p.Link)
					}
					return d
				})
			})
		}),
	}
	if len(shares) > 0 {
		first := shares[0].Message
		if first.User != "" {
			ch = append(ch, layout.Rigid(func(gtx C) D {
				return v.para(gtx, 15, 22, wBold, v.p.Text, 1, nsChannelLinks.T("First shared by {firstSharer}", "firstSharer", v.userName(first.User)))
			}))
		}
		if t := tsTime(first.TS); !t.IsZero() {
			now := time.Now()
			day := cfCalendarDay(t, now)
			s := nsChannelLinks.T("Shared on {date}", "date", day)
			if sameDay(t, now) || sameDay(t, now.AddDate(0, 0, -1)) {
				s = nsChannelLinks.T("Shared {date}", "date", strings.ToLower(day))
			}
			ch = append(ch, layout.Rigid(func(gtx C) D { return v.para(gtx, 15, 22, wBold, v.p.Text, 1, s) }))
		}
	}
	return col(gtx, ch...)
}

// favicon is the 24px icon, else the link glyph at 20.
func (v *LinkDetails) favicon(gtx C) D {
	s := gtx.Dp(24)
	if v.favURL != "" && v.h.Images() != nil {
		if img, ok := v.h.Images().Get(v.favURL, s*2); ok {
			return themeImage(gtx, img, image.Pt(s, s), 0)
		}
	}
	dirAt(gtx, image.Pt((s-gtx.Dp(20))/2, (s-gtx.Dp(20))/2), func(gtx C) D { return v.icon(gtx, "link", 20, v.p.Text) })
	return D{Size: image.Pt(s, s)}
}

// divider is .shareDividerContainer.
func (v *LinkDetails) divider(n int) W {
	return func(gtx C) D {
		return layout.Inset{Top: 8, Bottom: 12}.Layout(gtx, func(gtx C) D {
			muted := v.lt(rgb(0x5e5d60), rgb(0x9a9b9e)) // --dt_color-content-ter
			label := nsChannelLinks.T("Conversations {count}", "count", strconv.Itoa(n))
			lw := v.th.Measure(gtx, 13, wBold, label)
			w := gtx.Constraints.Max.X
			d := v.label(gtx, 13, wBold, muted, label)
			y := d.Size.Y / 2
			x := lw + gtx.Dp(10)
			if x < w {
				fill(gtx, image.Rect(x, y, w, y+max(1, gtx.Dp(1))), theme.Over(v.p.CardLine, v.p.Bg))
			}
			return D{Size: image.Pt(w, d.Size.Y)}
		})
	}
}

// card is FileShareDetailsMessageEntity.
func (v *LinkDetails) card(gtx C, s *core.LinkShare) D {
	key := s.Channel + "/" + s.Message.TS
	c := v.cards[key]
	if c == nil {
		c = new(widget.Clickable)
		v.cards[key] = c
	}
	return c.Layout(gtx, func(gtx C) D {
		pointerCursor(gtx)
		w := gtx.Constraints.Max.X
		bw := max(1, gtx.Dp(1))
		pad := gtx.Dp(12)
		inner := gtx
		inner.Constraints = layout.Constraints{Max: image.Pt(max(0, w-2*pad-2*bw), 1<<20)}
		macro := op.Record(gtx.Ops)
		d := v.cardBody(inner, s)
		call := macro.Stop()
		h := d.Size.Y + 2*pad + 2*bw
		r := image.Rect(0, 0, w, h)
		line := theme.Over(v.p.CardLine, v.p.Bg)
		rrect(gtx, r, gtx.Dp(12), line)
		rrect(gtx, r.Inset(bw), gtx.Dp(12)-bw, v.p.Bg)
		if c.Hovered() {
			rrect(gtx, r.Inset(bw), gtx.Dp(12)-bw, v.p.Hover)
		}
		func() {
			defer op.Offset(image.Pt(bw+pad, bw+pad)).Push(gtx.Ops).Pop()
			call.Add(gtx.Ops)
		}()
		return D{Size: image.Pt(w, h)}
	})
}

func (v *LinkDetails) cardBody(gtx C, s *core.LinkShare) D {
	m := &s.Message
	where := s.Name
	if where == "" {
		where = v.userName(m.User)
	} else if !s.IM && !s.MPIM {
		where = "#" + where
	}
	t := tsTime(m.TS)
	when := ""
	if !t.IsZero() {
		now := time.Now()
		if sameDay(t, now) {
			when = clock(t)
		} else {
			when = i18n.ShortDatePretty(i18n.Locale(), t, now, false)
		}
	}
	text := linkPlain(m.Text)
	return col(gtx,
		layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return row(gtx, layout.Middle,
				layout.Flexed(1, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.W.Layout(gtx, func(gtx C) D {
						return v.para(gtx, 12, 18, wBold, v.p.Sec, 1, i18n.T("file-details", "Thread in {channel}", "channel", where))
					})
				}),
				layout.Rigid(func(gtx C) D {
					return v.para(gtx, 12, 18, wReg, v.lt(rgb(0x5e5d60), rgb(0x9a9b9e)), 1, when)
				}),
			)
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Middle,
					layout.Rigid(func(gtx C) D { return v.avatar(gtx, v.avatarOf(m.User), 20) }),
					hgap(8),
					layout.Flexed(1, func(gtx C) D { return v.para(gtx, 15, 22, wBold, v.p.Text, 1, v.userName(m.User)) }),
				)
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
				return v.para(gtx, 15, 22, wReg, v.p.Text, 0, text)
			})
		}),
	)
}

func (v *LinkDetails) avatarOf(id string) string {
	if a := v.h.Core(); a != nil {
		if u, ok := a.User(id); ok {
			return u.Avatar
		}
	}
	return ""
}

var linkMarkup = regexp.MustCompile(`<((?:https?|mailto):[^|>]+)(?:\|([^>]+))?>`)

// linkPlain is the message's text with its <url|label> links as their
// labels (or URLs).
func linkPlain(s string) string {
	return linkMarkup.ReplaceAllStringFunc(s, func(m string) string {
		p := linkMarkup.FindStringSubmatch(m)
		if p[2] != "" {
			return p[2]
		}
		return p[1]
	})
}
