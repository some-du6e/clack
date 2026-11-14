package huddleui

import (
	"image"
	"testing"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/chanent"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// withEnt is v with the channel's entity e in the window's line and the
// card (the states a lookup goes through, drawn without one).
func withEnt(v inviteView, e chanent.Ent) inviteView {
	swap := func(ss []span) []span {
		out := append([]span(nil), ss...)
		for i := range out {
			if out[i].Ent != nil {
				ent := e
				out[i].Ent, out[i].Text = &ent, e.Text
			}
		}
		return out
	}
	v.Line, v.CardText = swap(v.Line), swap(v.CardText)
	if len(v.Line) == 3 {
		v.Text = v.Line[0].Text + e.Title() + v.Line[2].Text
	}
	return v
}

// TestHuddleShotsInvitePrivate (CLACK_SHOTS, the huddles-audit-shots
// workflow): the invite window and the sidebar card for a private channel
// (lock-filled, the bare name), one still loading (the hash and the
// MissingText bar) and one the viewer can't see (the Private channel
// chip), light and dark: invite-{window,card}-{private,loading,gone}-{light,dark}.png.
func TestHuddleShotsInvitePrivate(t *testing.T) {
	const priv, gone = "C0SHOTPRIV1", "C0SHOTGONE1"
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		e := newShotEnv(t, dark)
		e.h.app.S.PutConv(slack.Conversation{ID: priv, Name: "secret-plans", IsGroup: true, IsPrivate: true, IsMember: true})
		e.h.app.S.SetChannelAccess(gone, richtext.ChannelHidden)
		c := e.c
		th := theme.New(dark)
		var dec, soon, join widget.Clickable
		var st cardState
		for _, cs := range []struct {
			name, ch string
			ent      *chanent.Ent
		}{
			{"private", priv, nil},
			{"loading", priv, &chanent.Ent{Text: "#", Bar: chanent.BarWidth(priv), Disp: chanent.UnknownName()}},
			{"gone", gone, nil},
		} {
			inv := &incoming{Channel: cs.ch, Call: "R1", Sender: huddle.FakeRemoteUser, status: "pending", card: true}
			v := c.view(inv)
			if cs.ent != nil {
				v = withEnt(v, *cs.ent)
			}
			if _, ent := lineText(v.Line); ent == nil {
				t.Fatalf("%s: the window's line has no channel entity (%q)", cs.name, v.Text)
			}
			render(t, "invite-window-"+cs.name+"-"+mode, 380, 280, func(gtx C) {
				layoutInviteWindow(gtx, th, c, v, &dec, &soon, &join)
			})
			render(t, "invite-card-"+cs.name+"-"+mode, 260, 240, func(gtx C) {
				theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, hudFrame)
				layoutCard(gtx, th, c, v, &st, gtx.Constraints.Max.X, false)
			})
		}
	}
}
