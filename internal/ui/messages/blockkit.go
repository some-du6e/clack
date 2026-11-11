package messages

// Live Block Kit elements and ephemeral messages (internal/ui/blockkit):
// the body's richview gets blockkit's element host, so buttons, selects,
// pickers, checkboxes and legacy attachment actions work; ephemeral
// messages get Slack's "Only visible to you" label (c-message_kit__labels,
// eye icon), except in your own DM and Slackbot's.

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/blockkit"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

type bkState struct {
	rev  uint32
	ts   string
	done bool
	el   *blockkit.Message
}

// elements is the row's element host (nil: nothing interactive), looked
// up once per message revision.
func (r *row) elements(h Host) *blockkit.Message {
	if !r.bk.done || r.bk.rev != r.rev || r.bk.ts != r.m.TS {
		r.bk = bkState{rev: r.rev, ts: r.m.TS, done: true, el: blockkit.MessageElements(h, &r.m)}
	}
	return r.bk.el
}

// withElements hands the element host to the body's richview.
func (r *row) withElements(h Host) {
	el := r.elements(h)
	if el == nil {
		return
	}
	o := r.rvOpts()
	o.Elements = el
	r.rv.SetOptions(o)
}

// ephemeral reports whether m shows the "Only visible to you" label.
func ephemeral(rc *rowCtx, m *core.Message) bool {
	if !blockkit.Ephemeral(rc.h, m.Channel, m.TS) {
		return false
	}
	if c := rc.h.Core(); c != nil {
		if cv, ok := c.Conv(m.Channel); ok && cv.Kind == "im" && (cv.User == rc.selfID || cv.User == "USLACKBOT") {
			return false
		}
	}
	return true
}

// hostElements is the element host of a message a plugin drew with
// Slack's Blocks / MessageBlocks component: the channel row's own
// (blockkit.MessageElements, one per message), so its buttons and menus
// send blocks.actions from the message as the row's do. nil when msg
// isn't a message Slack could act on (no channel or ts) or has nothing
// interactive.
func hostElements(h Host, app *core.App, msg slackc.Props) richview.Elements {
	if h == nil || app == nil {
		return nil
	}
	ch := msg.StrOr("channel", "")
	raw, err := json.Marshal(jsonable(msg))
	if err != nil || ch == "" {
		return nil
	}
	var sm slack.Message
	if json.Unmarshal(raw, &sm) != nil || sm.TS == "" {
		return nil
	}
	m := app.MessageFromSlack(ch, &sm)
	if el := blockkit.MessageElements(h, &m); el != nil {
		return el
	}
	return nil
}
