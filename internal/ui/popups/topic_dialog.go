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
// (the dialog stays open). The input is a Texty input over the stored
// topic (composer.TextyInput: its mentions as pills, not raw mrkdwn).
// handleSave sends only a changed value (`t !== U`), and what it sends is
// submitChannelTopic's (boot-shared s0VR) cleaned text; unchanged, Save
// just closes.

import (
	"context"
	"image"
	"image/color"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// TopicInput is the dialog's Texty input over the stored text
// (composer.TextyInput; popups can't import the composer).
type TopicInput interface {
	Layout(gtx layout.Context, th *theme.Theme, col color.NRGBA) layout.Dimensions
	Tag() event.Tag
	Text() string     // what it shows
	SetText(s string) // tests
	Changed() bool    // value() differs from the untouched input's
	Submit() string   // the text Slack sends for it
	OverLimit() int   // characters past maxLength (getNumCharsOverLimit)
	// LayoutWarning draws TextyLengthWarning's pill at the origin, the
	// box's inner top-left corner, while OverLimit > 0
	LayoutWarning(gtx layout.Context, th *theme.Theme)
}

// TopicDialog is "Edit topic" for one conversation.
type TopicDialog struct {
	h       Host
	ch      string
	kind    string // channel | private | im | mpim | app
	name    string // #name, or the DM's display name
	modal   *Modal
	in      TopicInput
	err     string
	pending bool
	focused bool
}

// NewTopicDialog opens "Edit topic" for ch (kind and name as the header
// shows them, topic the current topic); input makes the Texty input over
// a text with a placeholder.
func NewTopicDialog(h Host, ch, kind, name, topic string, input func(text, placeholder string) TopicInput) *TopicDialog {
	d := &TopicDialog{h: h, ch: ch, kind: kind, name: name}
	d.in = input(topic, nsChDetails.T("Add a topic"))
	// useChannelTopicInput's modal (the channel_details I18n)
	save := &ModalButton{Label: nsChDetails.T("Save"), Kind: BtnPrimary, Enabled: func() bool { return !d.pending }, Do: func() bool { return d.Save() }}
	d.modal = NewModal(h, ModalOpts{Title: nsChDetails.T("Edit topic"), Body: d.layout, PadX: 24, HeadH: 68,
		Buttons:    []*ModalButton{{Label: nsChDetails.T("Cancel"), Kind: BtnOutline}, save},
		FocusStops: func() []event.Tag { return []event.Tag{d.in.Tag()} }}).OldDialog()
	return d
}

func (d *TopicDialog) Layout(gtx layout.Context) layout.Dimensions { return d.modal.Layout(gtx) }
func (d *TopicDialog) Dismissed() bool                             { return d.modal.Dismissed() }
func (d *TopicDialog) Dismiss()                                    { d.modal.Dismiss() }

// SetText replaces the input's text; Text is what it shows; Error is the
// error under it.
func (d *TopicDialog) SetText(s string) { d.in.SetText(s) }
func (d *TopicDialog) Text() string     { return d.in.Text() }
func (d *TopicDialog) Error() string    { return d.err }

// Save is the dialog's go button: conversations.setTopic when the value
// changed (handleSave), closing once Slack took it. It reports whether
// the dialog closes now.
func (d *TopicDialog) Save() bool {
	if !d.in.Changed() {
		return true
	}
	t := d.in.Submit()
	a := d.h.Core()
	if a == nil || d.pending {
		return false
	}
	d.pending, d.err = true, ""
	ch := d.ch
	run(d.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.SubmitChannelTopic(ctx, ch, t) },
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
		gtx.Execute(key.FocusCmd{Tag: d.in.Tag()})
	}
	m := gtx
	m.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 24), 1<<20)}
	call, td := record(m, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Min.Y = gtx.Sp(22)
		return d.in.Layout(gtx, th, cl.Fg)
	})
	ih := td.Size.Y + dp(gtx, 16)
	r := image.Rect(0, 0, w, ih)
	rad := dp(gtx, 8)
	if gtx.Focused(d.in.Tag()) {
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
	b := border1(gtx)
	at(gtx, image.Pt(b, b), func(gtx layout.Context) layout.Dimensions {
		d.in.LayoutWarning(gtx, th)
		return layout.Dimensions{}
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
