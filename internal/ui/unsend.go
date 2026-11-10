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
				put(composer.DocForMessage(m))
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

// lastOwnMessage is FMZu Rw: the viewer's newest message in the channel
// (not a thread reply) or in the thread (not its root).
func lastOwnMessage(ctx context.Context, svc *core.App, ch, threadTS, self string) (core.Message, bool) {
	var msgs []core.Message
	if threadTS == "" {
		p, err := svc.History(ctx, ch, "")
		if err != nil {
			return core.Message{}, false
		}
		msgs = p.Messages
	} else {
		rs, err := svc.Replies(ctx, ch, threadTS)
		if err != nil {
			return core.Message{}, false
		}
		msgs = rs
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
