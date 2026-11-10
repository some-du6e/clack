package huddleui

import (
	"errors"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// HuddleErrorModal (client-boot OAp3, "huddle_error_modal"): Slack's
// dialog when a huddle can't be joined or ends under you. A join error is
// "Can’t join the huddle" with the reason's text; a huddle that ended for
// everyone is "This huddle has ended"; anything else "Something went
// wrong with your huddle". One button: OK, or Got It for the ones that
// just tell you what happened (room_max_participants, an ended huddle).

// errModal is a HuddleErrorModal's copy.
type errModal struct{ title, body, button string }

// joinErrorModal is HuddleErrorModal{isJoinError} for err.
func joinErrorModal(err error) errModal {
	m := errModal{title: "Can’t join the huddle", body: "You’ll have to go back and give it another try.", button: "OK"}
	var se *slack.Error
	if errors.As(err, &se) {
		switch se.Code {
		case "permission_denied":
			m.body = "You are unable to join the Huddle as you do not have permission to join Huddles."
		case "rooms_call_ended":
			m.body = "The Huddle has ended."
		case "room_max_participants":
			// Slack says "This huddle already has {maxAmount} people, which
			// is the maximum allowed." from a limit Clack isn't sent; it
			// keeps the default text with Slack's Got It
			m.button = "Got It"
		}
	}
	return m
}

// endedModal is HuddleErrorModal for a huddle that went away under you:
// nil (you left) shows nothing.
func endedModal(err error) (errModal, bool) {
	switch {
	case err == nil:
		return errModal{}, false
	case errors.Is(err, huddle.ErrEnded): // HuddleEndedForEveryone
		return errModal{title: "This huddle has ended", body: "This huddle has ended for everyone who was in it.", button: "Got It"}, true
	}
	return errModal{title: "Something went wrong with your huddle", body: "You’ll have to go back and give it another try.", button: "OK"}, true
}

// showErrModal opens m on the main window (any goroutine).
func (c *Controller) showErrModal(m errModal) {
	ph, ok := c.h.(popups.Host)
	if !ok {
		c.h.Toast(m.title)
		return
	}
	c.post(func() {
		th := ph.Theme()
		ok := &popups.ModalButton{Label: m.button, Kind: popups.BtnPrimary, Do: func() bool { return true }}
		ph.ShowOverlay(popups.NewModal(ph, popups.ModalOpts{
			Title:     m.title,
			AutoFocus: ok,
			Class:     "p-huddle_error_modal",
			Body: func(gtx layout.Context) layout.Dimensions {
				return th.Para(gtx, 15, 22, theme.Regular, th.C.Fg, m.body)
			},
			Buttons: []*popups.ModalButton{ok},
		}))
		ph.Invalidate()
	})
}
