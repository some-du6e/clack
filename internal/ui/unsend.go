package ui

import (
	"context"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// UnsendLast is undoSendLastMessageInChannel (Mod+Z in the empty composer
// within 15 s of a send; composer.tryUnsend): the viewer's latest message
// in the channel (or thread) is deleted and its contents go back into the
// composer. With should_show_unsend_message_confirmation (Advanced >
// Confirmations and warnings, on by default) Slack asks first with its
// Unsend message dialog.
func (a *App) UnsendLast(ch, threadTS string, put func(*composer.Doc)) {
	if a.data == nil {
		return
	}
	svc := a.data.core
	go func() {
		self := svc.S.SelfID()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		m, ok := lastOwnMessage(ctx, svc, ch, threadTS, self)
		if !ok {
			return
		}
		a.Post(func() {
			unsend := func() {
				put(composer.DocForMessage(m, svc.S))
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					if err := svc.Delete(ctx, m.Channel, m.TS); err != nil {
						a.Post(func() { a.Toast("Couldn't unsend that message: " + err.Error()) })
					}
				}()
			}
			if svc.PrefBool("should_show_unsend_message_confirmation", true) {
				popups.ConfirmUnsend(a, m, unsend)
				return
			}
			unsend()
		})
	}()
}

// lastOwnMessage is the unsend selector (boot-shared P5Eq Rw, build
// 35b42de8b870; FMZu Rw before): the viewer's newest message in the
// channel (not a thread reply) or in the thread (not its root), among
// the messages of the conversation's newest slice, dTxY tp(key, null).
func lastOwnMessage(ctx context.Context, svc *core.App, ch, threadTS, self string) (core.Message, bool) {
	msgs, ok := newestMessages(ctx, svc, ch, threadTS)
	if !ok {
		return core.Message{}, false
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.User != self || m.System || m.Pending {
			continue
		}
		if threadTS == "" && m.ThreadTS != "" && m.ThreadTS != m.TS && !m.Broadcast {
			continue
		}
		if threadTS != "" && m.TS == threadTS {
			continue
		}
		if m.Channel == "" {
			m.Channel = ch
		}
		return m, true
	}
	return core.Message{}, false
}

// newestMessages is what Slack's undo send and edit-last selectors look
// through (P5Eq Rw, qszY W4): the conversation's newest slice,
// getPotentiallyOutdatedTimestamps with no start (dTxY tp(key, null)).
// A channel's is its newest page; a thread's is the last slice of a
// history that reached the thread's end, from memory, and nothing when
// it hasn't: the slice the reply box sits on, where the viewer's last
// send went, whichever slice the pane was opened at. Core's thread
// Replies is the slice at the thread's default start (the root, or the
// last read), not that one.
func newestMessages(ctx context.Context, svc *core.App, ch, threadTS string) ([]core.Message, bool) {
	if threadTS == "" {
		p, err := svc.History(ctx, ch, "")
		if err != nil {
			return nil, false
		}
		return p.Messages, true
	}
	return svc.ThreadSliceAt(ch, threadTS, "").Msgs, true
}

// lastOwnEditable is the message Up in an empty composer edits when the
// list has no row editor of its own (editLast): the viewer's newest plain
// message among the conversation's newest slice (Slack's qszY W4,
// getLastEditableMessage). ok is false when the channel's newest page
// didn't load.
func lastOwnEditable(ctx context.Context, svc *core.App, ch, threadTS, self string) (m core.Message, found, ok bool) {
	msgs, ok := newestMessages(ctx, svc, ch, threadTS)
	if !ok {
		return core.Message{}, false, false
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if m := msgs[i]; m.User == self && !m.System && m.Subtype == "" {
			return m, true, true
		}
	}
	return core.Message{}, false, true
}
