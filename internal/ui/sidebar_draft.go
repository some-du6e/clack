package ui

import (
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The draft pencil on a sidebar row: Slack's BaseChannelSuffixDraftIcon
// (boot-render PZNh, build 4d0c78085232).
//
//   - Always, the pencil's Tooltip is ChannelSuffixDraftIconTooltipContent:
//     a bold "Draft", the draft's text in curly quotes (DraftRichTextPreview,
//     truncated at 50) and "N file(s)"; under draft_management_in_composer
//     (treatment) a row with several drafts shows a bold "N drafts" instead.
//   - Under draft_management_in_composer (treatment) and
//     sidebar_draft_indicator_menu_and_thread_inclusion (on), with drafts,
//     the pencil is a 20px button (p-channel_sidebar__draft_menu_trigger:
//     radius 4, theme-surf-inv-ter on hover, the edit glyph at 16, "View
//     drafts") that opens the "Drafts" menu at its right, top-aligned:
//     "Jump to draft…", the four newest drafts ("Thread: " before a reply's,
//     "No message (N attachments)" without text), then "View N more drafts"
//     (the Drafts page). A channel draft opens the channel; a reply's opens
//     its thread with the root highlighted.
//
// Which drafts a row counts (thread drafts and DM drafts by person only
// under those experiments) is backend.loadDrafts.

// sbDraftUI is one row's pencil.
type sbDraftUI struct {
	hover gesture.Hover // the plain pencil's tooltip area (the row keeps the click)
	tip   popups.TipState
	btn   widget.Clickable // the menu trigger
}

func (s *sidebarState) draftUI(id string) *sbDraftUI {
	if s.drafts == nil {
		s.drafts = map[string]*sbDraftUI{}
	}
	d := s.drafts[id]
	if d == nil {
		d = &sbDraftUI{}
		s.drafts[id] = d
	}
	return d
}

// sbDraftMgmt is O2By gp (draft_management_in_composer is "treatment").
func (a *App) sbDraftMgmt() bool { return a.exp("draft_management_in_composer") == "treatment" }

// sbDraftMenuOn is gp && le (le: sidebar_draft_indicator_menu_and_thread_inclusion).
func (a *App) sbDraftMenuOn() bool {
	return a.sbDraftMgmt() && a.exp("sidebar_draft_indicator_menu_and_thread_inclusion") == "on"
}

// sbDrafts are the row's drafts, newest first (sortDraftsByTimestamp on
// getDraftLastUpdatedTs).
func (a *App) sbDrafts(id string) []core.Draft {
	if a.data == nil {
		return nil
	}
	ds := append([]core.Draft(nil), a.data.chanDrafts[id]...)
	sort.SliceStable(ds, func(i, j int) bool { return draftTS(ds[i].LastUpdated) > draftTS(ds[j].LastUpdated) })
	return ds
}

func draftTS(ts string) float64 {
	f, _ := strconv.ParseFloat(ts, 64)
	return f
}

// draftFiles is the draft's attachment count.
func draftFiles(d core.Draft) int {
	if d.Files > 0 {
		return d.Files
	}
	return len(d.FileIDs)
}

// draftPreview is DraftRichTextPreview's text: the draft truncated at 50.
func draftPreview(d core.Draft) string {
	t := strings.Join(strings.Fields(d.Text), " ")
	if r := []rune(t); len(r) > 50 {
		return string(r[:50]) + "…"
	}
	return t
}

// draftTipText is ChannelSuffixDraftIconTooltipContent for the row's
// draft (its channel draft: the newest one not in a thread).
func (a *App) draftTipText(id string) string {
	ds := a.sbDrafts(id)
	if a.sbDraftMgmt() && len(ds) > 1 {
		return i18n.T("channel_sidebar", "{count, plural, other {# drafts}}", "count", len(ds))
	}
	lines := []string{i18n.T("channel_sidebar", "Draft")}
	var main *core.Draft
	for i := range ds {
		if ds[i].ThreadTS == "" {
			main = &ds[i]
			break
		}
	}
	if main == nil && len(ds) > 0 {
		main = &ds[0]
	}
	if main != nil {
		if p := draftPreview(*main); p != "" {
			lines = append(lines, i18n.T("general", "“")+p+i18n.T("general", "”"))
		}
		if n := draftFiles(*main); n > 0 {
			lines = append(lines, i18n.T("channel_sidebar", "{numFiles, plural, =1 {# file} other {# files}}", "numFiles", n))
		}
	}
	return strings.Join(lines, "\n")
}

// draftMenuItems is the trigger's menu.
func (a *App) draftMenuItems(id string) []MenuItem {
	ds := a.sbDrafts(id)
	shown := ds
	if len(shown) > 4 {
		shown = shown[:4]
	}
	items := []MenuItem{{Header: true, Label: i18n.T("channel_sidebar", "Jump to draft…")}}
	for _, d := range shown {
		d := d
		label := draftPreview(d)
		if label == "" {
			label = i18n.T("channel_sidebar", "No message ({count, plural, one {# attachment} other {# attachments}})", "count", draftFiles(d))
		}
		if d.ThreadTS != "" {
			label = i18n.T("channel_sidebar", "Thread:") + " " + label
		}
		items = append(items, MenuItem{Key: "sidebar_draft_management_draft_preview", Label: label, Do: func() {
			if d.ThreadTS != "" {
				a.OpenThread(id, d.ThreadTS) // openThread with highlightRoot
				return
			}
			a.Navigate("c/" + id)
		}})
	}
	if more := len(ds) - len(shown); more > 0 {
		items = append(items, MenuItem{Key: "sidebar_draft_management_view_all_drafts",
			Label: i18n.T("channel_sidebar", "{count, plural, one {View # more draft} other {View # more drafts}}", "count", more),
			Do:    func() { a.Navigate("drafts") }})
	}
	return items
}

// layoutSbDraft draws the pencil's interactive layer over row id at slot
// (the row's coordinates): the menu trigger, or the plain pencil's hover
// area, and the tooltip above it. fg is the suffix's colour.
func (a *App) layoutSbDraft(gtx layout.Context, id string, slot image.Rectangle, fg color.NRGBA, menu bool) {
	d := a.sidebar.draftUI(id)
	t := op.Offset(slot.Min).Push(gtx.Ops)
	defer t.Pop()
	sz := slot.Size()
	hovered := false
	if menu {
		if d.btn.Clicked(gtx) { // position "right-top": the trigger's right edge, its top
			org := a.ptrWin
			if h := d.btn.History(); len(h) > 0 {
				org = org.Sub(h[len(h)-1].Position)
			}
			a.menuAt(image.Rectangle{Min: org, Max: org.Add(sz)}, int(popups.Right), 0, a.draftMenuItems(id))
		}
		g := gtx
		g.Constraints = layout.Exact(sz)
		d.btn.Layout(g, func(gtx layout.Context) layout.Dimensions {
			if d.btn.Hovered() {
				theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(4), a.sbPal.SidebarHover)
			}
			is := gtx.Dp(16)
			at(gtx, image.Pt((sz.X-is)/2, (sz.Y-is)/2), image.Pt(is, is), func(gtx layout.Context) layout.Dimensions {
				return icons.Get("edit")(gtx, 16, fg)
			})
			defer clip.Rect{Max: sz}.Push(gtx.Ops).Pop()
			pointer.CursorPointer.Add(gtx.Ops)
			return layout.Dimensions{Size: sz}
		})
		hovered = d.btn.Hovered()
	} else {
		area := clip.Rect{Max: sz}.Push(gtx.Ops)
		pass := pointer.PassOp{}.Push(gtx.Ops)
		d.hover.Add(gtx.Ops)
		pass.Pop()
		area.Pop()
		hovered = d.hover.Update(gtx.Source)
	}
	popups.Tooltip(gtx, a.th, &d.tip, hovered, sz, popups.Above, a.draftTipText(id), "")
}
