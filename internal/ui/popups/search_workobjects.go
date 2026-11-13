package popups

import (
	"context"
	"image"
	"image/color"
	"strings"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/openurl"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/workobject"
)

// Work objects on the results page (work_objects_message_search_xp_v2;
// gantry-v2-async search-view UK4f, build 4d0c78085232): a messages
// search also runs core.SearchWorkObjects, and each app whose items
// matched becomes a source (UnifiedSearchRefinementWorkObject: the
// app's name and its result count, after the standard sources in the
// source menu). The app's source shows WorkObjectResults: one
// WorkObjectSearchResult per item (listItem__mBZmP: 12px apart; the
// card at the pane's width, source WORK_OBJECT_SEARCH, no 480px cap),
// with WorkObjectLayoutToggle ("View as detailed" / "View as dense": the
// cards' own layout, or compact). A click opens the item's flexpane, as
// in a message.

// RichStyle, set by the ui root to the message rows' style, draws rich
// content (the work object results' cards) here.
var RichStyle func(h Host) *richview.Style

// WorkObjectOpener is a Host that opens a work object's flexpane.
type WorkObjectOpener interface {
	OpenWorkObjectEntity(e *workobject.Entity, appUnfurlURL, channel, ts, tab string)
}

// srWO is the results page's work object state.
type srWO struct {
	q, sel string // the query the sources are for; the selected source's app id
	seq    int
	srcs   []core.WorkObjectSource
	dense  bool
	views  map[string]*richview.View
	viewOf map[string]string // entity id → the raw it was laid out from

	detail, denseBtn  widget.Clickable
	tipDetail, tipDen TipState
	list              layout.List
}

// woOn is work_objects_message_search_xp_v2 (PEUt YR).
func (v *SearchView) woOn() bool {
	c := v.h.Core()
	return c != nil && c.WorkObjectFlags().Search
}

// searchWO runs searchWorkObjects for a fresh messages search.
func (v *SearchView) searchWO(c *core.App, q string) {
	key := q + "\x00" + v.sortBy // a new sort asks again (getSortParams)
	if !v.woOn() || strings.TrimSpace(q) == "" || key == v.wo.q {
		return
	}
	v.wo.q, v.wo.srcs = key, nil
	v.wo.seq++
	seq := v.wo.seq
	sort, sess := v.sortBy, v.sess
	run(v.h, func(ctx context.Context) ([]core.WorkObjectSource, error) {
		return c.SearchWorkObjects(ctx, q, sort, sess)
	},
		func(srcs []core.WorkObjectSource, err error) {
			if err != nil || seq != v.wo.seq {
				return
			}
			v.wo.srcs = srcs
			if v.wo.sel != "" && v.woSource() == nil {
				v.wo.sel = ""
			}
			v.h.Invalidate()
		})
}

// woSource is the selected source (nil: a standard tab).
func (v *SearchView) woSource() *core.WorkObjectSource {
	if v.wo.sel == "" {
		return nil
	}
	for i := range v.wo.srcs {
		if v.wo.srcs[i].AppID == v.wo.sel {
			return &v.wo.srcs[i]
		}
	}
	return nil
}

// woMenuItems are the source menu's work object rows.
func (v *SearchView) woMenuItems(th *theme.Theme) []MenuItem {
	var out []MenuItem
	for i := range v.wo.srcs {
		s := v.wo.srcs[i]
		name := s.AppName
		if name == "" {
			continue
		}
		sel := v.wo.sel == s.AppID
		out = append(out, MenuItem{Key: core.WorkObjectViewID(s.AppID), Label: name, Checked: sel,
			Widget: srSourceRow(th, name, srCountShort(len(s.Hits)), sel), Do: func() {
				v.wo.sel = s.AppID
				v.wo.list.Position = layout.Position{}
				v.h.Invalidate()
			}})
	}
	return out
}

// woView is a result's card view, rebuilt when the entity changes.
func (v *SearchView) woView(e *workobject.Entity) *richview.View {
	if v.wo.views == nil {
		v.wo.views, v.wo.viewOf = map[string]*richview.View{}, map[string]string{}
	}
	size := ""
	if v.wo.dense {
		size = workobject.Compact
	}
	key := string(e.Raw()) + "|" + size
	if rv := v.wo.views[e.EntityID]; rv != nil && v.wo.viewOf[e.EntityID] == key {
		return rv
	}
	var r richtext.Resolver
	if c := v.h.Core(); c != nil {
		r = c.S
	}
	if r == nil {
		return nil
	}
	wo := richtext.WorkObjectFromEntity(r, e, "")
	d := richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.AttachmentBlock, Att: &richtext.Attachment{WorkObject: wo}}}}
	rv := richview.New(d, richview.Options{WorkObjectSearch: true, WorkObjectSize: size})
	v.wo.views[e.EntityID], v.wo.viewOf[e.EntityID] = rv, key
	return rv
}

// woToggle is WorkObjectLayoutToggle: a 28px base-ter track with the two
// small icon buttons (table: detailed; menu: dense) and the selected
// one's base-pry mask (1px otl-sec, radius 8).
func (v *SearchView) woToggle(gtx layout.Context, th *theme.Theme, cl *Colors, x, y int) {
	h := dp(gtx, 28)
	bw := dp(gtx, 28)
	w := 2 * bw
	r := image.Rect(x, y, x+w, y+h)
	rrect(gtx, r, dp(gtx, 8), woTok(th, "base-ter"))
	mx := x
	if v.wo.dense {
		mx += bw
	}
	m := image.Rect(mx, y, mx+bw, y+h)
	rrect(gtx, m, dp(gtx, 8), cl.Bg)
	theme.Border(gtx, m, dp(gtx, 8), border1(gtx), woTok(th, "otl-sec"))
	for i, b := range []struct {
		c    *widget.Clickable
		icon string
	}{{&v.wo.detail, "table"}, {&v.wo.denseBtn, "menu"}} {
		bx := x + i*bw
		at(gtx, image.Pt(bx, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(bw, h))
			return clickBox(gtx, b.c, nil, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt((bw-dp(gtx, 16))/2, (h-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
					return icon(gtx, b.icon, 16, cl.Fg2)
				})
				return layout.Dimensions{Size: image.Pt(bw, h)}
			})
		})
	}
	if v.wo.detail.Clicked(gtx) && v.wo.dense {
		v.wo.dense = false
		v.h.Invalidate()
	}
	if v.wo.denseBtn.Clicked(gtx) && !v.wo.dense {
		v.wo.dense = true
		v.h.Invalidate()
	}
}

// woTok is Slack's --dt_color-<name> in the theme's mode.
func woTok(th *theme.Theme, name string) color.NRGBA {
	c, ok := css.ParseColor(css.SlackVars(th.Dark)["--dt_color-"+name])
	if !ok {
		return colorsOf(th).Fg2
	}
	return theme.Over(c, colorsOf(th).Bg)
}

// woResults lays out the selected source's results.
func (v *SearchView) woResults(gtx layout.Context, th *theme.Theme, cl *Colors, px int, sz image.Point) layout.Dimensions {
	src := v.woSource()
	if src == nil || RichStyle == nil {
		return layout.Dimensions{Size: sz}
	}
	st := RichStyle(v.h)
	cw := sz.X - 2*px
	hits := src.Hits
	v.wo.list.Axis = layout.Vertical
	return v.wo.list.Layout(gtx, len(hits), func(gtx layout.Context, i int) layout.Dimensions {
		rv := v.woView(hits[i].Entity)
		if rv == nil {
			return layout.Dimensions{}
		}
		top := dp(gtx, 12) // listItem: margin-top 12px
		d := at(gtx, image.Pt(px, top), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(cw, 0), Max: image.Pt(cw, gtx.Dp(2000))}
			return rv.Layout(gtx, st, srWOHandler{v})
		})
		return layout.Dimensions{Size: image.Pt(sz.X, top+d.Size.Y)}
	})
}

// srWOHandler routes a result card's clicks.
type srWOHandler struct{ v *SearchView }

func (h srWOHandler) OpenURL(u string)                  { _ = openurl.Open(u) }
func (h srWOHandler) OpenUser(id string, a image.Point) { h.v.h.OpenProfile(a, id) }
func (h srWOHandler) OpenChannel(id string)             { h.v.h.Navigate("c/" + id) }
func (h srWOHandler) OpenUsergroup(string, image.Point) {}
func (h srWOHandler) OpenImage(img richtext.Image)      { _ = openurl.Open(img.URL) }
func (h srWOHandler) OpenFile(f *richtext.File)         {}
func (h srWOHandler) Button(b richtext.Button) {
	if b.URL != "" {
		_ = openurl.Open(b.URL)
	}
}

// WorkObject is a result card's click: the flexpane (Details, Comments
// or Conversations), Open opens the item.
func (h srWOHandler) WorkObject(ev richview.WorkObjectEvent) {
	if ev.Att == nil || ev.Att.WorkObject == nil {
		return
	}
	e := ev.Att.WorkObject.E
	o, _ := h.v.h.(WorkObjectOpener)
	tab := ""
	switch ev.Action {
	case richview.WOClick, richview.WOPreview:
		tab = "details"
	case richview.WOComments:
		tab = "comments"
	case richview.WOConversations:
		tab = "threads"
	case richview.WOOpen:
		_ = openurl.Open(richtext.SafeURL(e.ExternalURL))
		return
	default:
		return
	}
	if o != nil {
		o.OpenWorkObjectEntity(e, "", "", "", tab)
	}
}
