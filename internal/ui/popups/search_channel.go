package popups

// Search in channel (⌘F / Ctrl+F in a conversation): Slack's
// SearchInChannelView, a secondary pane (client-search-in-channel-view
// chunk, build 407aff01646b). Its header reads "Search in #channel" (with
// keywords: 'Search "kw" in #channel'); the query runs as
// search.modules.messages for "in:<#C> query", count 10, search_context
// search_in_channel, sort score/desc, timestamp/desc or timestamp/asc.
// Before a search: "Not searching in this channel? Go to full search
// (CTRL G)" on one line (p-explorer_section--align) and "Nothing to show
// yet."; a search with no results: GenericSearchEmptyState (its feedback
// link is the combined modal, module "inChannel"). With results:
// "**N** results found" and "View full search" (no separator), the sort
// select at the right (sort icon + label: Most relevant (default) /
// Newest / Oldest / Edit default), the result rows, "View all N results"
// past ten, and the 40px footer with "Give feedback" at its right
// (searchFeedbackDialog: search.feedback).

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
	"github.com/some-du6e/clack/internal/i18n"
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
	emptyFb  widget.Clickable // the no-results state's "Give feedback"
	sortR    image.Rectangle
	sortBy   string // score | timestamp | oldest
	origin   image.Point
	searched bool // a search ran (Slack's eu: the no-results state replaces "Nothing to show yet.")

	query   string // the submitted keywords
	session core.SearchSession
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
	v := &SearchInChannel{h: h, ch: ch, sortBy: DefaultSearchSort(h.Core())}
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
		v.loading, v.searched = false, false
		return
	}
	v.loading, v.searched = true, true
	sort := v.sortBy
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
		return nsSearch.T("Search in channel")
	}
	if query != "" {
		return `Search "` + query + `" in ` + channel // no Slack string has the query
	}
	return nsSearch.T("Search in {channelName}", "channelName", channel)
}

func (v *SearchInChannel) fullSearch() {
	v.h.Navigate("search?q=" + url.QueryEscape(v.fullQuery()))
}

// fullQuery is the channel's full-search query ("in:<#C> keywords").
func (v *SearchInChannel) fullQuery() string {
	q := "in:<#" + v.ch + ">"
	if v.query != "" {
		q += " " + v.query
	}
	return q
}

// SetOrigin tells the pane where its top-left corner is in window
// coordinates (the sort menu opens under its select).
func (v *SearchInChannel) SetOrigin(p image.Point) { v.origin = p }

// sess is the pane's search session (one per pane).
func (v *SearchInChannel) sess() core.SearchSession {
	if v.session.ID == "" {
		v.session = core.NewSearchSession()
	}
	return v.session
}

// feedbackDialog is the footer's "Give feedback": searchFeedbackDialog
// {module: messages, feedbackLabelOverride: "inChannel"}, i.e.
// search.feedback with the payload Slack builds (no client_version, empty
// module_counts).
func (v *SearchInChannel) feedbackDialog() {
	sess := v.sess()
	sort, dir := srActiveSort(srTabMessages, v.sortBy)
	srFeedbackDialog(v.h, srSearchFeedbackPayload{SearchSessionID: sess.ID, QuerySessionID: sess.QueryID, Query: v.fullQuery(),
		Module: "inChannel", ActiveSort: sort, ActiveSortDir: dir, ActiveFilters: srActiveFilters(v.h.Core(), srTabMessages),
		ModuleCounts: map[string]int{}})
}

// srcSortLabel is the select's label for a sort (MessageEntity's options;
// "Most relevant (default)" unless stick_user_sort).
func srcSortLabel(c *core.App, sortBy string) string {
	switch sortBy {
	case "timestamp":
		return "Newest"
	case "oldest":
		return "Oldest"
	}
	if c != nil && c.PrefBool("stick_user_sort", false) {
		return "Most relevant"
	}
	return "Most relevant (default)"
}

// sortMenu is the in-channel MessageEntity select: Most relevant, Newest,
// Oldest, then Edit default (Preferences > Advanced). A choice reruns the
// search; with stick_user_sort it is also kept in search_sort.
func (v *SearchInChannel) sortMenu() {
	c := v.h.Core()
	sticky := c != nil && c.PrefBool("stick_user_sort", false)
	var items []MenuItem
	for _, o := range [...]string{"score", "timestamp", "oldest"} {
		val := o
		items = append(items, MenuItem{Label: srcSortLabel(c, val), Checked: v.sortBy == val, Do: func() {
			v.sortBy = val
			v.Search(v.query)
			if sticky {
				pref := srSortPref[val]
				run(v.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.SetPref(ctx, "search_sort", pref) },
					func(struct{}, error) {})
			}
		}})
	}
	items = append(items, Sep(), MenuItem{Label: "Edit default", Do: func() {
		if OpenPreferencesSection != nil {
			OpenPreferencesSection("advanced")
		}
	}})
	items = inAll(nsSearch, items) // MessageEntity's options (the search I18n)
	v.h.ShowOverlay(NewMenuOpts(v.h, items, MenuOpts{Anchor: srClickAnchor(v.h, &v.sortBtn, v.sortR.Add(v.origin)), Width: 240}))
}

// srClickAnchor is a clicked widget's window rectangle: the pointer's
// window position minus the press position inside the widget (the panes
// don't know where the root put them). fallback when the host can't tell.
func srClickAnchor(h Host, c *widget.Clickable, fallback image.Rectangle) image.Rectangle {
	pp, ok := h.(interface{ PointerPos() image.Point })
	hist := c.History()
	if !ok || len(hist) == 0 {
		return fallback
	}
	o := pp.PointerPos().Sub(hist[len(hist)-1].Position)
	return image.Rectangle{Min: o, Max: o.Add(fallback.Size())}
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
		v.feedbackDialog()
	}
	if v.emptyFb.Clicked(gtx) {
		// GenericSearchEmptyState{moduleName: messages, feedbackLabelOverride: "inChannel"}
		q, sess := v.fullQuery(), v.sess()
		c := v.h.Core()
		sort, dir := srActiveSort(srTabMessages, v.sortBy)
		openSearchFeedback(v.h, srFeedbackOpts{feedbackType: "search", query: q, payload: func() any {
			return srSearchFeedbackPayload{SearchSessionID: sess.ID, QuerySessionID: sess.QueryID, Query: q, Module: "inChannel",
				ActiveSort: sort, ActiveSortDir: dir, ActiveFilters: srActiveFilters(c, srTabMessages),
				ClientVersion: core.SearchFeedbackClientVersion, ModuleCounts: map[string]int{}}
		}})
	}
	if v.sortBtn.Clicked(gtx) {
		v.sortMenu()
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
		return nsSearch.T("Go to full search (⌘G)")
	}
	return nsSearch.T("Go to full search (CTRL G)")
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
		if v.searched {
			// GenericSearchEmptyState (Slack's eu: a search ran)
			srNothing(gtx, th, cl, pad, dp(gtx, 32), inner, &v.emptyFb)
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}
		// "Not searching in this channel? Go to full search (CTRL G)": a
		// p-explorer_section--align label, 13px #1d1c1d@.7 regular, the link
		// 4px after it on the same line
		lh := dp(gtx, 20)
		sec := linearAlpha(theme.WithAlpha(cl.Fg, 0xb3), cl.Bg)
		d := at(gtx, image.Pt(pad, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(inner, lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, sec, nsSearch.T("Not searching in this channel?"))
			})
		})
		at(gtx, image.Pt(pad+d.Size.X+dp(gtx, 4), 0), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &v.fullBtn, nil, func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, theme.Regular, cl.Link, srcFullLabel())
				})
			})
		})
		y := lh + dp(gtx, 16) + dp(gtx, 28)
		title := nsSearch.T("Nothing to show yet.")
		tw := textW(gtx, th, 15, theme.Bold, title)
		at(gtx, image.Pt((w-tw)/2, y), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, title)
		})
		y += dp(gtx, 26)
		at(gtx, image.Pt(pad, y), func(gtx layout.Context) layout.Dimensions {
			return paraCentered(gtx, th, 13, 18, cl.Fg2,
				i18n.Plain(nsSearch.RT("Type a keyword or <strong>from: @name</strong> to find that message you’re looking for.")), inner)
		})
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	// stickyResultHeader: "<strong>N</strong> results found" and "View
	// full search", the sort select at the right
	lh := dp(gtx, 28)
	headH := lh + dp(gtx, 8)
	v.resultsHeader(gtx, th, cl, w, pad, lh, top)
	// stickyResultFooter: p-explorer_sections--footer, 40px on ctr-sec,
	// the unstyled "Give feedback" (content-ter) at its right
	bodyH := gtx.Constraints.Max.Y
	fh := dp(gtx, 40)
	fy := maxInt(headH, bodyH-fh)
	at(gtx, image.Pt(0, fy), func(gtx layout.Context) layout.Dimensions { return v.footer(gtx, th, cl, w, fh) })
	n := len(v.msgs)
	if v.total > 10 {
		n++
	}
	at(gtx, image.Pt(0, headH), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, maxInt(0, fy-headH)))
		defer clipRRect(gtx, image.Rectangle{Max: gtx.Constraints.Max}, 0).Pop()
		return v.list.Layout(gtx, n, func(gtx layout.Context, i int) layout.Dimensions {
			if i < len(v.msgs) {
				return v.row(gtx, th, cl, i, w)
			}
			// "View all N results" (search-escape-hatch), centred
			label := nsSearch.T("View all {totalCountStr} results", "totalCountStr", srCountGrouped(v.total))
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
		})
	})
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// resultsHeader is the results label row.
func (v *SearchInChannel) resultsHeader(gtx layout.Context, th *theme.Theme, cl *Colors, w, pad, lh, top int) {
	sec := linearAlpha(theme.WithAlpha(cl.Fg, 0xb3), cl.Bg)
	x := pad
	var d layout.Dimensions
	if i18n.Locale() == i18n.EnUS {
		d = at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Bold, sec, srCountGrouped(v.total))
			})
		})
		x += d.Size.X
		unit := " results found"
		if v.total == 1 {
			unit = " result found"
		}
		d = at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, sec, unit)
			})
		})
		x += d.Size.X
	} else {
		// "<strong>{count}</strong> results found" (0qtz) in the locale's order
		for _, p := range nsSearch.RT("<strong>{count}</strong> results found", "count", i18n.FormatNumber(i18n.Locale(), float64(v.total))) {
			wt := theme.Regular
			if p.Tag == "strong" {
				wt = theme.Bold
			}
			txt := p.Text
			d = at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 13, wt, sec, txt)
				})
			})
			x += d.Size.X
		}
	}
	x += dp(gtx, 6) // title-label margin-right 4 + padding-right 2
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &v.fullBtn, nil, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Link, nsSearch.T("View full search"))
			})
		})
	})
	// the sort select: the sort icon, and its label when the pane is at
	// least 350 wide (FilterSortSelectedOptionContent inChannel)
	label := ""
	if w >= dp(gtx, 350) {
		label = nsSearch.T(srcSortLabel(v.h.Core(), v.sortBy))
	}
	call, pd := record(gtx, func(gtx layout.Context) layout.Dimensions {
		return Pill(gtx, th, &v.sortBtn, "sort", label, true, v.sortBy != "score")
	})
	sx := w - pad - pd.Size.X
	sy := (lh - pd.Size.Y) / 2
	v.sortR = image.Rectangle{Min: image.Pt(sx, top+sy), Max: image.Pt(sx, top+sy).Add(pd.Size)}
	at(gtx, image.Pt(sx, sy), func(gtx layout.Context) layout.Dimensions {
		call.Add(gtx.Ops)
		return pd
	})
}

// footer is the sticky feedback footer.
func (v *SearchInChannel) footer(gtx layout.Context, th *theme.Theme, cl *Colors, w, fh int) layout.Dimensions {
	bg := cl.BgAlt
	if th != nil && th.Dark {
		bg = rgb(0x212428)
	}
	fill(gtx, image.Rect(0, 0, w, fh), bg)
	fw := textW(gtx, th, 13, theme.Regular, nsSearch.T("Give feedback"))
	at(gtx, image.Pt(w-dp(gtx, 16)-fw, 0), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &v.feedback, nil, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, fh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 13, theme.Regular, cl.Fg3, nsSearch.T("Give feedback"))
			})
		})
	})
	return layout.Dimensions{Size: image.Pt(w, fh)}
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
