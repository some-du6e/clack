package popups

// Archiving a channel: Slack's ArchiveChannelModal (boot-shared Oc3D, its
// contents pOUs; web client 66e72b57ade1), which /archive opens when
// isSlashCommandEnabled allows it. "Archive this channel?", what
// archiving does, and Cancel / Archive Channel (danger, focused):
// submitArchive (core.Archive: conversations.archive {channel}) closes
// every modal once it succeeds; a failure shows under the text, by code
// (ArchiveErrorText). The details' Settings draw the same modal
// themselves (views, details_managing.go).
//
// Not here: the solution-template clean-up flow Oc3D shows instead where
// templates are allowed (7LFL g; not on this org), and what follows an
// externally shared channel's archive (6QxH a).

import (
	"context"
	"image"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// Archive strings (channel_details).
const (
	arTitle  = "Archive this channel?"
	arIntro  = "When you archive a channel, it’s archived for everyone. That means…"
	arItem1  = "No one will be able to send messages to the channel"
	arItem2  = "Any apps installed in the channel will be disabled"
	arItem3  = "If there are external people in this channel, they will be removed. They’ll still have access to the chat history."
	arOutro  = "You’ll still be able to find the channel’s contents via search. And you can always unarchive the channel in the future, if you’d like."
	arButton = "Archive Channel"
)

// ArchiveErrorText is ArchiveChannelModalContents' error text for an
// archive's error code (andLeave false): admin is the viewer being a
// workspace admin (Bn/N Zo), private the channel's type.
func ArchiveErrorText(code string, admin, private bool) string {
	switch code {
	case "last_ra_channel":
		if admin {
			return nsChDetails.T("Sorry, you can’t archive this channel because it is the only channel one of the guest account members belongs to. If you first disable the guest account, you will then be able to archive the channel.")
		}
		return nsChDetails.T("Sorry, you can’t archive this channel because it is the only channel one of the guest account members belongs to.")
	case "restricted_action", "user_is_restricted":
		if private {
			return nsChDetails.T("You don’t have permission to archive private channels. Talk to your Workspace Owner.")
		}
		return nsChDetails.T("You don’t have permission to archive channels. Talk to your Workspace Owner.")
	case "already_archived":
		return nsChDetails.T("This channel was already archived.")
	case "cant_archive_general":
		return nsChDetails.T("This channel cannot be archived.")
	}
	return nsChDetails.T("Archiving failed with error: {error}", "error", code)
}

// ArchiveChannelModal is "Archive this channel?" for one channel.
type ArchiveChannelModal struct {
	h       Host
	ch      string
	private bool
	modal   *Modal
	pending bool
	err     string
}

// NewArchiveChannelModal builds the modal for channel ch; show it with
// h.ShowOverlay.
func NewArchiveChannelModal(h Host, ch string) *ArchiveChannelModal {
	m := &ArchiveChannelModal{h: h, ch: ch}
	if a := h.Core(); a != nil {
		if c, ok := a.Conv(ch); ok {
			m.private = c.Kind == "private"
		}
	}
	archive := &ModalButton{Label: nsChDetails.T(arButton), Kind: BtnDanger, Enabled: func() bool { return !m.pending },
		Do: func() bool { m.Archive(); return false }}
	m.modal = NewModal(h, ModalOpts{Title: nsChDetails.T(arTitle), Body: m.layout, AutoFocus: archive,
		Buttons: []*ModalButton{{Label: nsChDetails.T("Cancel"), Kind: BtnOutline}, archive}})
	return m
}

func (m *ArchiveChannelModal) Layout(gtx layout.Context) layout.Dimensions {
	return m.modal.Layout(gtx)
}
func (m *ArchiveChannelModal) Dismissed() bool { return m.modal.Dismissed() }
func (m *ArchiveChannelModal) Dismiss()        { m.modal.Dismiss() }

// Title is the modal's title; Error is the archive's failure ("" for
// none).
func (m *ArchiveChannelModal) Title() string { return m.modal.Title() }
func (m *ArchiveChannelModal) Error() string { return m.err }

// Archive is Archive Channel: submitArchive, then the modal closes (Oc3D's
// onSubmit: closeAll), or the error shows.
func (m *ArchiveChannelModal) Archive() {
	a := m.h.Core()
	if a == nil || m.pending {
		return
	}
	m.pending, m.err = true, ""
	ch := m.ch
	run(m.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.Archive(ctx, ch) },
		func(_ struct{}, err error) {
			m.pending = false
			if err == nil {
				m.modal.Dismiss()
				return
			}
			admin := false
			if u, ok := a.User(a.S.SelfID()); ok {
				admin = u.IsAdmin
			}
			m.err = ArchiveErrorText(slackErrID(err), admin, m.private)
			m.h.Invalidate()
		})
}

// layout is renderContent: the paragraph, the list (16px in, a bullet
// each), the closing paragraph, then the error alert.
func (m *ArchiveChannelModal) layout(gtx layout.Context) layout.Dimensions {
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	line := func(x int, s string) int {
		d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(w-x, 1), 1<<20)}
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, s, 0)
		})
		return d.Size.Y
	}
	y += line(0, nsChDetails.T(arIntro))
	y += dp(gtx, 8)
	for _, it := range []string{arItem1, arItem2, arItem3} {
		at(gtx, image.Pt(dp(gtx, 8), y), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, cl.Fg, "•")
		})
		y += line(dp(gtx, 24), nsChDetails.T(it))
	}
	y += dp(gtx, 16)
	y += line(0, nsChDetails.T(arOutro))
	if m.err != "" { // margin_top_100: 16px
		y += dp(gtx, 16)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return renameAlert(gtx, th, cl, m.err, w)
		})
		y += d.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}
