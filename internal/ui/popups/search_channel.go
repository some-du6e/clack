package popups

// Search in channel (⌘F / Ctrl+F in a conversation): Slack's
// SearchInChannelView, a secondary pane (client-search-in-channel-view
// chunk). Its header reads "Search in #channel" (with keywords:
// 'Search "kw" in #channel'); the query runs as search.modules.messages
// for "in:<#C> query", count 10, search_context search_in_channel, sort
// relevance or newest. Before a search: "Not searching in this channel?
// Go to full search (Ctrl G)" and "Nothing to show yet." With results:
// "**N** results found", "View full search", the result rows, "View all
// N results" past ten, and "Give feedback".

import (
	"context"
	"image"
	"net/url"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/shortcut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// SearchInChannel is the ⌘F pane.
type SearchInChannel struct {
	h        Host
	ch       string
	ed       widget.Editor
	focused  bool
	list     layout.List
	closeBtn widget.Clickable
	fullBtn  widget.Clickable // "Go to full search" / "View full search"
	allBtn   widget.Clickable // "View all N results"
	sortBtn  widget.Clickable
	feedback widget.Clickable
	sortR    image.Rectangle
	newest   bool

	query   string // the submitted keywords
	seq     int
	loading bool
	err     string
	msgs    []core.Message
	total   int
	rows    []widget.Clickable
	convs   map[string]core.ConvRef
}

// NewSearchInChannel opens search in channel ch.
func NewSearchInChannel(h Host, ch string) *SearchInChannel {
	v := &SearchInChannel{h: h, ch: ch}
	v.ed.SingleLine = true
	v.ed.Submit = true
	v.list.Axis = layout.Vertical
	return v
}

// Channel is the searched conversation.
func (v *SearchInChannel) Channel() string { return v.ch }

// Search runs q in the channel (what Enter in the box does).
func (v *SearchInChannel) Search(q string) {
	q = strings.TrimSpace(q)
	v.query = q
	v.seq++
	seq := v.seq
	v.msgs, v.total, v.err = nil, 0, ""
	v.list.Position = layout.Position{}
	c := v.h.Core()
	if q == "" || c == nil {
		v.loading = false
		return
	}
	v.loading = true
	sort := "score"
	if v.newest {
		sort = "timestamp"
	}
	ch := v.ch
	run(v.h, func(ctx context.Context) (core.SearchResult, error) {
		return c.SearchMessages(ctx, core.SearchOpts{Query: q, InChannel: ch, Sort: sort})
	}, func(res core.SearchResult, err error) {
		if seq != v.seq {
			return
		}
		v.loading = false
		if err != nil {
			v.err = errText(err)
			return
		}
		v.msgs, v.total, v.convs = res.Messages, res.Total, res.Convs
		for len(v.rows) < len(v.msgs) {
			v.rows = append(v.rows, widget.Clickable{})
		}
	})
}

func (v *SearchInChannel) channelLabel() string {
	c := v.h.Core()
	if c == nil {
		return ""
	}
	ref := c.ConvRef(v.ch)
	switch ref.Kind {
	case "im", "mpim":
		return ref.Name
	}
	if ref.Name == "" {
		return ""
	}
	return "#" + ref.Name
}

// srcTitle is SearchInChannelView's header.
func srcTitle(query, channel string) string {
	if channel == "" {
		return "Search in channel"
	}
	if query != "" {
		return `Search "` + query + `" in ` + channel
	}
	return "Search in " + channel
}

func (v *SearchInChannel) fullSearch() {
	q := "in:<#" + v.ch + ">"
	if v.query != "" {
		q += " " + v.query
	}
	v.h.Navigate("search?q=" + url.QueryEscape(q))
}

// Layout fills the pane.
func (v *SearchInChannel) Layout(gtx layout.Context) layout.Dimensions {
	drain()
	th := v.h.Theme()
	cl := colorsOf(th)
	sz := gtx.Constraints.Max
	gtx.Constraints.Min = image.Point{}
	if !v.focused {
		v.focused = true
		gtx.Execute(key.FocusCmd{Tag: &v.ed})
	}
	for {
		e, ok := v.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			v.Search(v.ed.Text())
		}
	}
	for {
		e, ok := gtx.Event(key.Filter{Focus: &v.ed, Name: key.NameEscape})
		if !ok {
			break
		}
		if ke, ok := e.(key.Event); ok && ke.State == key.Press {
			v.h.CloseSecondary()
		}
	}
	if v.closeBtn.Clicked(gtx) {
		v.h.CloseSecondary()
	}
	if v.fullBtn.Clicked(gtx) || v.allBtn.Clicked(gtx) {
		v.fullSearch()
	}
	if v.feedback.Clicked(gtx) {
		v.h.Toast("Feedback isn't supported yet")
	}
	if v.sortBtn.Clicked(gtx) {
		items := []MenuItem{
			{Label: "Most relevant", Checked: !v.newest, Do: func() { v.newest = false; v.Search(v.query) }},
			{Label: "Newest", Checked: v.newest, Do: func() { v.newest = true; v.Search(v.query) }},
		}
		v.h.ShowOverlay(NewMenuOpts(v.h, items, MenuOpts{Anchor: v.sortR, Width: 200}))
	}
	for i := range v.msgs {
		if i < len(v.rows) && v.rows[i].Clicked(gtx) {
			RecordRecentConversation(v.msgs[i].Channel)
			v.h.Navigate(msgRoute(v.msgs[i]))
		}
	}

	pad := dp(gtx, 16)
	// header
	hh := dp(gtx, 49)
	at(gtx, image.Pt(pad, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, sz.X-pad-dp(gtx, 52)), hh)}
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 18, theme.Black, cl.Fg, srcTitle(v.query, v.channelLabel()))
		})
	})
	at(gtx, image.Pt(sz.X-dp(gtx, 12)-dp(gtx, 28), (hh-dp(gtx, 28))/2), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &v.closeBtn, "close", 28, 20, cl.Fg2, cl.BgHover, 8)
	})
	fill(gtx, image.Rect(0, hh, sz.X, hh+maxInt(1, dp(gtx, 1))), cl.Divider)

	// the search box
	y := hh + dp(gtx, 12)
	bh := dp(gtx, 36)
	box := image.Rect(pad, y, sz.X-pad, y+bh)
	rrect(gtx, box, dp(gtx, 8), cl.Bg)
	bc := cl.BtnBorder
	if gtx.Focused(&v.ed) {
		bc = cl.Focus
	}
	stroke(gtx, box, dp(gtx, 8), maxInt(1, dp(gtx, 1)), bc)
	at(gtx, image.Pt(box.Min.X+dp(gtx, 10), y+(bh-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "search", 16, cl.Fg2)
	})
	tx := box.Min.X + dp(gtx, 34)
	tw := maxInt(1, box.Max.X-tx-dp(gtx, 10))
	at(gtx, image.Pt(tx, y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, bh)}
			if v.ed.Len() == 0 {
				call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = image.Point{}
					return text(gtx, th, 15, theme.Regular, cl.Placeholder, "Search in channel")
				})
				call.Add(gtx.Ops)
			}
			return th.Editor(gtx, &v.ed, unit.Sp(15), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
		})
	})
	y += bh + dp(gtx, 16)

	// body
	bodyH := maxInt(0, sz.Y-y)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(sz.X, bodyH))
		defer clipRRect(gtx, image.Rect(0, 0, sz.X, bodyH), 0).Pop()
		return v.body(gtx, th, cl, sz.X, y)
	})
	return layout.Dimensions{Size: sz}
}

// srcFullLabel is the escape hatch: Slack writes "(⌘G)" on macOS and
// "(CTRL G)" elsewhere.
func srcFullLabel() string {
	if shortcut.Mac() {
		return "Go to full search (" + shortcut.Format("Mod+G") + ")"
	}
	return "Go to full search (CTRL G)"
}

func (v *SearchInChannel) body(gtx layout.Context, th *theme.Theme, cl *Colors, w, top int) layout.Dimensions {
	pad := dp(gtx, 16)
	inner := maxInt(1, w-2*pad)
	if v.loading || v.err != "" {
		if v.err != "" {
			at(gtx, image.Pt(pad, 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(inner, dp(gtx, 20))}
				return text(gtx, th, 13, theme.Regular, cl.Fg3, "Search failed: "+v.err)
			})
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if len(v.msgs) == 0 {
		y := 0
		if v.query == "" {
			// "Not searching in this channel? Go to full search (CTRL G)"
			at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(inner, dp(gtx, 20))}
				return text(gtx, th, 13, theme.Bold, cl.Fg2, "Not searching in this channel?")
			})
			y += dp(gtx, 20)
			at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, &v.fullBtn, nil, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Link, srcFullLabel())
				})
			})
			y += dp(gtx, 44)
			title := "Nothing to show yet."
			tw := textW(gtx, th, 15, theme.Bold, title)
			at(gtx, image.Pt((w-tw)/2, y), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Bold, cl.Fg, title)
			})
			y += dp(gtx, 26)
			at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
				return paraCentered(gtx, th, 13, 18, cl.Fg2,
					"Type a keyword or from: @name to find that message you’re looking for.", inner)
			})
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
		tw := textW(gtx, th, 18, theme.Black, srNothingTitle)
		at(gtx, image.Pt((w-tw)/2, dp(gtx, 32)), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 18, theme.Black, cl.Fg, srNothingTitle)
		})
		at(gtx, image.Pt(pad, dp(gtx, 64)), func(gtx layout.Context) layout.Dimensions {
			return paraCentered(gtx, th, 15, 22, cl.Fg2, srNothingDesc, inner)
		})
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	// "<strong>N</strong> results found" · View full search, sort at the right
	lh := dp(gtx, 20)
	n := 3 + len(v.msgs)
	if v.total > 10 {
		n++
	}
	return v.list.Layout(gtx, n, func(gtx layout.Context, i int) layout.Dimensions {
		switch {
		case i == 0:
			x := pad
			d := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Bold, cl.Fg, srCountGrouped(v.total))
				})
			})
			x += d.Size.X
			unit := " results found · "
			if v.total == 1 {
				unit = " result found · "
			}
			d = at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Fg2, unit)
				})
			})
			x += d.Size.X
			at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, &v.fullBtn, nil, func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 13, theme.Regular, cl.Link, "View full search")
					})
				})
			})
			label := "Most relevant"
			if v.newest {
				label = "Newest"
			}
			sw := textW(gtx, th, 13, theme.Bold, label) + dp(gtx, 16)
			sx := w - pad - sw
			v.sortR = image.Rect(sx, top, sx+sw, top+lh)
			at(gtx, image.Pt(sx, 0), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, &v.sortBtn, nil, func(gtx layout.Context) layout.Dimensions {
					vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 13, theme.Bold, cl.Link, label)
					})
					at(gtx, image.Pt(sw-dp(gtx, 12), (lh-dp(gtx, 12))/2), func(gtx layout.Context) layout.Dimensions {
						return icon(gtx, "chevron-down", 12, cl.Link)
					})
					return layout.Dimensions{Size: image.Pt(sw, lh)}
				})
			})
			return layout.Dimensions{Size: image.Pt(w, lh+dp(gtx, 12))}
		case i == 1:
			return layout.Dimensions{Size: image.Pt(w, 0)}
		case i-2 < len(v.msgs):
			return v.row(gtx, th, cl, i-2, w)
		case v.total > 10 && i-2 == len(v.msgs):
			label := "View all " + srCountGrouped(v.total) + " results"
			lw := textW(gtx, th, 15, theme.Bold, label)
			bw, bh := lw+dp(gtx, 24), dp(gtx, 36)
			return at(gtx, image.Pt((w-bw)/2, dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, &v.allBtn, nil, func(gtx layout.Context) layout.Dimensions {
					r := image.Rect(0, 0, bw, bh)
					stroke(gtx, r, dp(gtx, 8), maxInt(1, dp(gtx, 1)), cl.BtnBorder)
					at(gtx, image.Pt(dp(gtx, 12), 0), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, bh, func(gtx layout.Context) layout.Dimensions {
							return text(gtx, th, 15, theme.Bold, cl.Fg, label)
						})
					})
					return layout.Dimensions{Size: image.Pt(bw, bh+dp(gtx, 16))}
				})
			})
		}
		// footer
		fw := textW(gtx, th, 13, theme.Regular, "Give feedback")
		return at(gtx, image.Pt((w-fw)/2, dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &v.feedback, nil, func(gtx layout.Context) layout.Dimensions {
				d := text(gtx, th, 13, theme.Regular, cl.Link, "Give feedback")
				return layout.Dimensions{Size: image.Pt(d.Size.X, d.Size.Y+dp(gtx, 24))}
			})
		})
	})
}

// srCountGrouped formats a count with thousands separators.
func srCountGrouped(n int) string {
	s := srResultsLabel(n)
	return s[:strings.IndexByte(s, ' ')]
}

// row is one result: avatar, sender, time, two lines of text.
func (v *SearchInChannel) row(gtx layout.Context, th *theme.Theme, cl *Colors, i, w int) layout.Dimensions {
	m := &v.msgs[i]
	pad := dp(gtx, 16)
	name, av := srAuthor(v.h, m)
	terms := srParse(v.query).Highlights()
	segs := srHighlight(srFlatten(srBody(m)), terms)
	cw := w - 2*pad
	tx := dp(gtx, 12) + dp(gtx, 36) + dp(gtx, 8)
	toks, lines, over := srWrapFit(gtx, th, segs, maxInt(1, cw-tx-dp(gtx, 12)), 2, textW(gtx, th, 15, theme.Regular, "…"))
	lh := dp(gtx, 22)
	h := dp(gtx, 12) + lh + lines*lh + dp(gtx, 12)
	h = maxInt(h, dp(gtx, 64))
	return at(gtx, image.Pt(pad, 0), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &v.rows[i], nil, func(gtx layout.Context) layout.Dimensions {
			r := image.Rect(0, 0, cw, h)
			bg := cl.Bg
			if v.rows[i].Hovered() {
				bg = cl.BgHover
			}
			rrect(gtx, r, dp(gtx, 12), bg)
			stroke(gtx, r, dp(gtx, 12), maxInt(1, dp(gtx, 1)), cl.Divider)
			at(gtx, image.Pt(dp(gtx, 12), dp(gtx, 12)), func(gtx layout.Context) layout.Dimensions {
				return avatar(gtx, v.h, av, 36)
			})
			nd := at(gtx, image.Pt(tx, dp(gtx, 10)), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, cw-tx-dp(gtx, 120)), lh)}
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Black, cl.Fg, name)
				})
			})
			when := dayTime(tsTime(m.TS), time.Now())
			at(gtx, image.Pt(tx+nd.Size.X+dp(gtx, 8), dp(gtx, 10)), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Fg2, when)
				})
			})
			at(gtx, image.Pt(tx, dp(gtx, 10)+lh), func(gtx layout.Context) layout.Dimensions {
				srDrawToks(gtx, th, cl, toks, lh, cl.Fg)
				if over {
					x := srEndX(toks, lines-1)
					at(gtx, image.Pt(x, (lines-1)*lh), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
							return text(gtx, th, 15, theme.Regular, cl.Fg, "…")
						})
					})
				}
				return layout.Dimensions{}
			})
			return layout.Dimensions{Size: image.Pt(cw, h+dp(gtx, 8))}
		})
	})
}
