package core

import (
	"encoding/json"

	"github.com/some-du6e/clack/internal/slack"
)

// Locked threads (Slack build 2cd9270e786c, 2026-11-12). An admin's
// "Turn off replies" sets the root's is_locked; the thread pane's
// ThreadFooter (boot-shared `352G`) then draws
// MessageInputLockedThreadRoadblock instead of the reply box.
//
//   - isThreadLocked (client-boot `rZb1`): no thread or channel is
//     unlocked; else the root from the message store
//     (getMessageByTimestamp, `iR00` Ld) through isLocked (`p6HR` ln:
//     `!!e?.is_locked`).
//   - The socket's message_locked (boot-render `yYRM` messageLocked) needs
//     a string channel and thread_ts and a boolean is_locked, then
//     locallyUpdateThreadLock (boot-shared `h/rh`) merges is_locked and
//     locked_by_team_id into the stored root when both the channel and
//     the root are known.
//   - The roadblock's copy (useThreadLockedCopy, `352G`) reads
//     feat_flag_message_hide_replies: "on" says replies and reactions are
//     off, any other group (or none) just replies.

// ExpHideReplies is the experiment useThreadLockedCopy reads (`"on" ===
// _Z(…)`).
const ExpHideReplies = "feat_flag_message_hide_replies"

func init() { experimentReads[ExpHideReplies] = on }

// ThreadLocked is Slack's isThreadLocked: whether ts's root in ch has its
// replies turned off.
func (a *App) ThreadLocked(ch, ts string) bool {
	if ch == "" || ts == "" {
		return false
	}
	m, ok := a.S.Find(ch, ts)
	return ok && m.IsLocked
}

// HideRepliesOn is useThreadLockedCopy's feat_flag_message_hide_replies
// read: the locked roadblock says reactions are off too.
func (a *App) HideRepliesOn() bool { return a.experiment(ExpHideReplies) == "on" }

// onMessageLocked is messageLocked plus locallyUpdateThreadLock.
func (a *App) onMessageLocked(ch string, raw json.RawMessage) {
	var e struct {
		ThreadTS       string `json:"thread_ts"`
		IsLocked       *bool  `json:"is_locked"`
		LockedByTeamID string `json:"locked_by_team_id"`
	}
	if json.Unmarshal(raw, &e) != nil || e.IsLocked == nil || e.ThreadTS == "" {
		return
	}
	if ch == "" { // handle's chanID(ev.Channel)
		return
	}
	if _, ok := a.S.Conv(ch); !ok {
		return
	}
	locked := *e.IsLocked
	if upd, ok := a.S.Mutate(ch, e.ThreadTS, func(x *slack.Message) {
		x.IsLocked, x.LockedByTeamID = locked, e.LockedByTeamID
	}); ok {
		a.emitUpdate(ch, &upd)
	}
}
