package popups

import (
	"strings"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/shortcut"
)

// SplitViewer is the ui root's split view: Slack's openInSplitView thunk
// (Fndw / 9ciC) puts a channel ("c/<id>") or thread ("c/<id>/t/<ts>")
// route in the window's one Split view container.
//
// SplitViewOK is false where Slack has no split view: Simplified layout
// mode (isBBModeEnabled; Slack's menus build no split rows there).
// InSplitView reports that the menu or control being built belongs to the
// split view itself (useShouldShowSplitViewButton, eXbF, leaves its
// "Open in split view" out there).
type SplitViewer interface {
	OpenSplit(route string)
	SplitViewOK() bool
	InSplitView() bool
}

// SecondaryRight, set by the ui root, is the window x where the secondary
// pane ends: the window's right inset, or the split view's column while
// one shows. Nil: the right inset.
var SecondaryRight func() int

// SplitKey is the "Open in split view" rows' key (Slack's dataQa
// channel_ctx_open_split). A row keyed SplitKey+":hidden" is one Slack
// leaves out; StripSplitView drops it.
const SplitKey = "channel_ctx_open_split"

// SplitHint is the shortcut Slack's split rows show: the modifier click
// that opens the same view in the split view (shouldSplit, module 5Nta:
// the platform's Cmd key with Alt), "⌘ Opt Click" on macOS and
// "Ctrl Alt Click" elsewhere (getTilesTemplate,
// get-activity-item-menu-template).
func SplitHint() string { return shortcut.ClickHint("Mod+Alt") }

// SplitViewItem is Slack's "Open in split view" row for route: the
// move-to-split-view icon, the modifier-click hint when hint is set, and
// openInSplitView. ok is false when h has no split view (the row then
// does nothing) or Slack wouldn't offer one (Simplified layout mode).
func SplitViewItem(h Host, route string, hint bool) (MenuItem, bool) {
	it := MenuItem{Key: SplitKey, Label: "Open in split view", Icon: "move-to-split-view"}
	if hint {
		it.Hint = SplitHint()
	}
	sv, ok := h.(SplitViewer)
	if !ok {
		return it, false
	}
	it.Do = func() { sv.OpenSplit(route) }
	return it, sv.SplitViewOK()
}

// channelSplitItem is getTilesTemplate's split row in the channel menu
// (boot-render 26v2): "Open in split view" with its hint, or, with the
// channel_ia_header_refinements experiment, the "Open in…" submenu whose
// "Split view" row has no hint (for channels, private channels, group
// DMs and DMs). Slack offers it for ids that look like a channel's
// (looksLikeChannelId, jh4t) and never in Simplified layout mode.
func channelSplitItem(h Host, convID, kind string) MenuItem {
	it, _ := SplitViewItem(h, "c/"+convID, true)
	if !looksLikeChannelID(convID) {
		it.Key = SplitKey + ":hidden"
		return it
	}
	if a := h.Core(); a != nil && a.Experiment("channel_ia_header_refinements") == "on" {
		switch kind {
		case "channel", "private", "im", "mpim", "":
			sub := it
			sub.Label, sub.Hint = "Split view", ""
			return MenuItem{Key: SplitKey, Label: "Open in…", Sub: []MenuItem{sub}}
		}
	}
	return it
}

// looksLikeChannelID is Slack's looksLikeChannelId (module jh4t):
// /^(C|G|D|Z)[A-Z0-9]{8,}$/.
func looksLikeChannelID(id string) bool {
	if len(id) < 9 || !strings.ContainsRune("CGDZ", rune(id[0])) {
		return false
	}
	for _, r := range id[1:] {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// StripSplitView drops the rows of m Slack wouldn't build: hidden split
// rows, and with all every split row (no split view: Simplified layout
// mode). A separator left doubled or dangling goes too. It runs before the
// menu is first drawn (the ui root's ShowOverlay).
func StripSplitView(m *Menu, all bool) {
	if m == nil {
		return
	}
	drop := func(it MenuItem) bool {
		return it.Key == SplitKey+":hidden" || all && it.Key == SplitKey
	}
	n := 0
	for _, it := range m.items {
		if drop(it) {
			n++
		}
	}
	if n == 0 {
		return
	}
	var out []MenuItem
	for _, it := range m.items {
		if drop(it) {
			continue
		}
		if it.Separator && (len(out) == 0 || out[len(out)-1].Separator) {
			continue
		}
		out = append(out, it)
	}
	for len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	m.items, m.orig = out, out
	m.clicks = make([]widget.Clickable, len(out))
}
