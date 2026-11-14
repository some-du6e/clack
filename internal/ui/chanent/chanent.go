// Package chanent names a channel the way Slack's channel entities do,
// from what core's store has for it (core.InlineChannel; Slack's rules
// are in internal/core/channel_lookup.go). Activity rows (views) and the
// huddle invite surfaces (huddleui) draw it; neither ever shows a raw
// channel id.
//
//   - InlineChannelEntity (client-boot X3qV): InlineChannelIcon (N4L8,
//     getChannelIconType, 1em, margin-right 2px) then ChannelName (g5tH,
//     getDisplayNameForChannel without the hash): the hash for a public
//     channel, lock-filled for a private one, archive or archive-filled
//     and " (archived)" for an archived one, canvas or lists for a
//     document's conversation. Clack draws the public channel's hash as
//     text, "#name"; the other icons are drawn.
//   - The unknown stub while its lookup runs: the hash, then MissingText
//     (sSUQ: an 8px base-ter pill, 80, 120 or 160px wide; tooltip
//     "Loading…").
//   - A channel the viewer can't see: BaseMissingChannel's chip (1x8o),
//     no hash: the lock, 4px, channel_token's "Private channel".
//   - getDisplayNameForChannel's text (W+Ke SB, default options): "#name"
//     for a public channel, a private one's bare name, formatChannelName's
//     "unknown-channel" (ZuZy, message) when there's no name.
package chanent

import (
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
)

// Ent is one channel as the entities draw it.
type Ent struct {
	// Text is the name as drawn: "#name", a private channel's bare name,
	// "name (archived)", "Private channel"; "#" before the loading bar.
	Text string
	// Icon is the Slack icon before Text ("" for a public channel, whose
	// hash is in Text; "lock" for the Private channel chip).
	Icon string
	// Bar is the unknown stub's MissingText width (0: not loading).
	Bar unit.Dp
	// Chip: BaseMissingChannel's "Private channel" chip.
	Chip bool
	// Disp is getDisplayNameForChannel's text.
	Disp string
	// Exists: the store has the channel (isExistent), named or not; the
	// huddle invite's notification names only one that does.
	Exists bool
}

// Loading reports whether e is the unknown stub (the MissingText bar).
func (e Ent) Loading() bool { return e.Bar > 0 }

// Title is e as text: its name as drawn, "" while it loads.
func (e Ent) Title() string {
	if e.Loading() {
		return ""
	}
	return e.Text
}

// barWidths are MissingText's sizes (small, medium, large).
var barWidths = [...]unit.Dp{80, 120, 160}

// BarWidth is MissingText's width "random" for channel id: one of its
// sizes, the same for the channel each time (Slack picks one per mount).
func BarWidth(id string) unit.Dp {
	h := uint32(2166136261) // FNV-1a
	for i := 0; i < len(id); i++ {
		h ^= uint32(id[i])
		h *= 16777619
	}
	return barWidths[h%uint32(len(barWidths))]
}

// UnknownName is formatChannelName's name for a channel without one.
func UnknownName() string { return i18n.T("message", "unknown-channel") }

// PrivateLabel is BaseMissingChannel's text for a channel the viewer
// can't see.
func PrivateLabel() string { return i18n.T("channel_token", "Private channel") }

// Of is channel id as the entities draw what the store has for it (ic);
// view, when set, is the name as plugins present it.
func Of(id string, ic core.InlineChannel, view func(name string) string) Ent {
	switch ic.State {
	case core.ChannelUnknown:
		return Ent{Text: "#", Bar: BarWidth(id), Disp: UnknownName()}
	case core.ChannelNonExistent:
		return Ent{Text: PrivateLabel(), Icon: "lock", Chip: true, Disp: UnknownName()}
	}
	name := UnknownName()
	if ic.State == core.ChannelKnown {
		name = ic.Name
		if view != nil {
			name = view(name)
		}
	}
	e := Ent{Text: name, Disp: name, Exists: true}
	if !ic.Private {
		e.Disp = "#" + name
	}
	switch {
	case ic.Doc == "canvas":
		e.Icon = "canvas"
	case ic.Doc == "list":
		e.Icon = "lists"
	case ic.Archived:
		e.Icon, e.Text = "archive", name+" "+i18n.T("search", "(archived)")
		if ic.Private {
			e.Icon = "archive-filled"
		}
	case ic.Private:
		e.Icon = "lock-filled"
	default:
		e.Text = "#" + name
	}
	return e
}
