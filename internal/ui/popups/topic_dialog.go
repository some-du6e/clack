package popups

// The channel header's topic "Edit": Slack's ChannelTopicDialog
// (client-channel-details-view ulnE openChannelTopicDialog, build
// a84662d2d135): a DeprecatedBaseDialog titled "Edit topic" with "Save",
// whose body is useChannelTopicInput (boot-shared SHFG): the topic input
// (max 250, Enter adds a line) and a hint under it,
//
//   - channels: "Let people know what **#name** is focused on right now
//     (ex. a project milestone). Topics are always visible in the
//     header."
//   - DMs: "Add a topic to your conversation with **name**. This will be
//     visible to both of you at the top of your DM."
//   - group DMs: "… This will be visible to all participants at the top of
//     your conversation."
//
// or, after conversations.setTopic failed, getChannelTopicError's text
// (the dialog stays open). Save with the text unchanged just closes.

import (
	"context"
	"image"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// TopicDialog is "Edit topic" for one conversation.
type TopicDialog struct {
	h       Host
	ch      string
	kind    string // channel | private | im | mpim | app
	name    string // #name, or the DM's display name
	topic0  string
	modal   *Modal
	ed      widget.Editor
	err     string
	pending bool
	focused bool
}

// NewTopicDialog opens "Edit topic" for ch (kind and name as the header
// shows them, topic the current topic).
func NewTopicDialog(h Host, ch, kind, name, topic string) *TopicDialog {
	d := &TopicDialog{h: h, ch: ch, kind: kind, name: name, topic0: topic}
	d.ed.SetText(topic)
	d.ed.SetCaret(len([]rune(topic)), len([]rune(topic)))
	d.ed.MaxLen = 250
	// useChannelTopicInput's modal (the channel_details I18n)
	save := &ModalButton{Label: nsChDetails.T("Save"), Kind: BtnPrimary, Enabled: func() bool { return !d.pending }, Do: func() bool { return d.Save() }}
	d.modal = NewModal(h, ModalOpts{Title: nsChDetails.T("Edit topic"), Body: d.layout, PadX: 24, HeadH: 68,
		Buttons:    []*ModalButton{{Label: nsChDetails.T("Cancel"), Kind: BtnOutline}, save},
		FocusStops: func() []event.Tag { return []event.Tag{&d.ed} }}).OldDialog()
	return d
}

func (d *TopicDialog) Layout(gtx layout.Context) layout.Dimensions { return d.modal.Layout(gtx) }
func (d *TopicDialog) Dismissed() bool                             { return d.modal.Dismissed() }
func (d *TopicDialog) Dismiss()                                    { d.modal.Dismiss() }

// SetText types into the input; Text reads it; Error is the error under it.
func (d *TopicDialog) SetText(s string) { d.ed.SetText(s) }
func (d *TopicDialog) Text() string     { return d.ed.Text() }
func (d *TopicDialog) Error() string    { return d.err }

// Save is the dialog's go button: conversations.setTopic when the text
// changed (handleSave), closing once Slack took it. It reports whether
// the dialog closes now.
func (d *TopicDialog) Save() bool {
	t := d.ed.Text()
	if t == d.topic0 {
		return true
	}
	a := d.h.Core()
	if a == nil || d.pending {
		return false
	}
	d.pending, d.err = true, ""
	ch := d.ch
	run(d.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.SetTopic(ctx, ch, t) },
		func(_ struct{}, err error) {
			d.pending = false
			if err != nil {
				d.err = TopicErrorText(err)
				d.h.Invalidate()
				return
			}
			d.modal.Dismiss()
		})
	return false
}

// TopicErrorText is Slack's getChannelTopicError (boot-shared) for a
// conversations.setTopic error.
func TopicErrorText(err error) string {
	s := err.Error()
	has := func(code string) bool { return strings.Contains(s, code) }
	switch {
	case has("too_long"):
		return "Channel topics can only include up to 250 characters — please make this topic shorter."
	case has("restricted_action"), has("user_is_restricted"):
		return "It looks like you don’t have permission to change the channel topic. Please check with your workspace admin."
	case has("method_not_supported_for_channel_type"):
		return "It’s not possible to set a topic here, unfortunately."
	case has("method_not_supported_for_dm"):
		return "Regrettably, direct messages do not have topics."
	}
	return "It looks like setting the channel topic didn’t work, for some reason. Try again?"
}

// hint is useChannelTopicInput's hint for the conversation's kind.
func (d *TopicDialog) hint() []cnRun {
	// one string each, {channelName} drawn bold (rtRuns)
	switch d.kind {
	case "im", "app":
		return rtRuns(nsChDetails.RT("Add a topic to your conversation with {channelName}. This will be visible to both of you at the top of your DM.", "channelName", d.name), "")
	case "mpim":
		return rtRuns(nsChDetails.RT("Add a topic to your conversation with {channelName}. This will be visible to all participants at the top of your conversation.", "channelName", d.name), "")
	}
	return rtRuns(nsChDetails.RT("Let people know what {channelName} is focused on right now (ex. a project milestone). Topics are always visible in the header.", "channelName", d.name), "")
}

// layout is the input (15/22, padding 8 12, radius 8, the focus ring)
// and the 13/18 hint or error 6px under it.
func (d *TopicDialog) layout(gtx layout.Context) layout.Dimensions {
	th := d.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	if !d.focused {
		d.focused = true
		gtx.Execute(key.FocusCmd{Tag: &d.ed})
	}
	m := gtx
	m.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 24), 1<<20)}
	call, td := record(m, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Min.Y = gtx.Sp(22)
		return th.Editor(gtx, &d.ed, 15, cl.Fg, theme.WithAlpha(cl.Link, 0x40))
	})
	ih := td.Size.Y + dp(gtx, 16)
	r := image.Rect(0, 0, w, ih)
	rad := dp(gtx, 8)
	if gtx.Focused(&d.ed) {
		ring := dp(gtx, 5)
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		one := maxInt(1, dp(gtx, 1))
		rrect(gtx, r.Inset(-one), rad+one, cl.Focus)
		rrect(gtx, r, rad, cl.ModalBg)
	} else {
		rrect(gtx, r, rad, cl.InputBorder)
		rrect(gtx, r.Inset(border1(gtx)), rad-border1(gtx), cl.ModalBg)
	}
	at(gtx, image.Pt(dp(gtx, 12), dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
		call.Add(gtx.Ops)
		return td
	})
	y := ih + dp(gtx, 6)
	hd := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		if d.err != "" {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, cl.Danger, d.err, 0)
		}
		return cnRuns(gtx, th, cl, d.hint(), 13, 18, cl.Fg3, w)
	})
	return layout.Dimensions{Size: image.Pt(w, y+hd.Size.Y)}
}
