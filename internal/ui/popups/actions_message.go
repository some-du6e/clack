package popups

import (
	"context"
	"image"
	"math"
	"strings"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Message "More actions" menu (msgmenu.png / ownmenu.png / submenu.png):
// Edit message (own) | sep | Mark unread U | Add to saved Ctrl+Shift+S |
// Add a reminder ▸ | reply notifications | sep | Copy link L | Copy message
// Ctrl+C | sep | Organize ▸ | Forward | Reply in thread | Connect to apps ▸
// | sep | Delete message… (own, danger). Icons only on the first groups,
// like Slack.

// MessageMenuOpts lets the caller take over actions it handles inline.
type MessageMenuOpts struct {
	OnEdit  func() // start inline editing (own messages)
	OnReply func() // open the thread composer
}

// NewMessageMenu builds the More actions menu for m. The menu's right edge
// sits at anchor.X (the kebab button). Show it with h.ShowOverlay.
func NewMessageMenu(h Host, anchor image.Point, m core.Message) *Menu {
	return NewMessageMenuOpts(h, anchor, m, MessageMenuOpts{})
}

// NewMessageMenuOpts is NewMessageMenu with caller hooks.
func NewMessageMenuOpts(h Host, anchor image.Point, m core.Message, o MessageMenuOpts) *Menu {
	return NewMenuOpts(h, actMessageItems(h, m, o), MenuOpts{Anchor: pointRect(anchor), AlignRight: true})
}

func actMessageItems(h Host, m core.Message, o MessageMenuOpts) []MenuItem {
	ch, ts := m.Channel, m.TS
	root := m.ThreadTS
	if root == "" {
		root = ts
	}
	own := m.User != "" && m.User == actSelfID(h)
	link := func() string {
		if m.Permalink != "" {
			return m.Permalink
		}
		return permalink(h, ch, ts, m.ThreadTS)
	}

	var items []MenuItem
	if own {
		items = append(items, MenuItem{Label: "Edit message", Icon: "edit", Hint: "E", Do: func() {
			if o.OnEdit != nil {
				o.OnEdit()
				return
			}
			h.Toast("Press ↑ in the composer to edit your last message")
		}}, Sep())
	}

	remind := func(kind int) func() {
		return func() {
			due := actReminderDue(time.Now(), kind).Unix()
			actDo(h, "Reminder set", func(ctx context.Context, a *core.App) error { return a.Save(ctx, ch, ts, due) })
		}
	}
	follow := MenuItem{Label: "Get notified about new replies", Icon: "bell", Do: func() {
		actDo(h, "You'll be notified about new replies", func(ctx context.Context, a *core.App) error {
			return a.SubscribeThread(ctx, ch, root, true)
		})
	}}
	if own {
		follow = MenuItem{Label: "Turn off notifications for replies", Icon: "bell-off", Do: func() {
			actDo(h, "You won't be notified about new replies", func(ctx context.Context, a *core.App) error {
				return a.SubscribeThread(ctx, ch, root, false)
			})
		}}
	}
	items = append(items,
		MenuItem{Label: "Mark unread", Icon: "mark-unread", Hint: "U", Do: func() {
			actDo(h, "Marked unread", func(ctx context.Context, a *core.App) error {
				_, err := a.MarkUnread(ctx, ch, ts)
				return err
			})
		}},
		MenuItem{Label: "Add to saved", Icon: "bookmark-add", Hint: "Ctrl+Shift+S", Do: func() {
			actDo(h, "Saved for later", func(ctx context.Context, a *core.App) error { return a.Save(ctx, ch, ts, 0) })
		}},
		MenuItem{Label: "Add a reminder", Icon: "remind", Sub: []MenuItem{
			{Label: "In 30 mins", Do: remind(actRem30m)},
			{Label: "In 1 hour", Do: remind(actRem1h)},
			{Label: "In 3 hours", Do: remind(actRem3h)},
			{Label: "Tomorrow at 9:00 AM", Do: remind(actRemTomorrow)},
			{Label: "Monday at 9:00 AM", Do: remind(actRemMonday)},
			{Label: "Custom…", Hint: "M", Do: func() { h.Toast("Custom reminder times aren't supported yet") }},
		}},
		follow,
		Sep(),
		MenuItem{Label: "Copy link", Icon: "link", Hint: "L", DoCtx: func(gtx layout.Context) {
			copyText(gtx, link())
			h.Toast("Link copied")
		}},
		MenuItem{Label: "Copy message", Icon: "text-format", Hint: "Ctrl+C", DoCtx: func(gtx layout.Context) {
			copyText(gtx, actMsgText(m))
		}},
		Sep(),
	)

	pin := MenuItem{Label: "Pin to channel", Icon: "pin", Hint: "P", Do: func() {
		actDo(h, "Message pinned", func(ctx context.Context, a *core.App) error { return a.Pin(ctx, ch, ts) })
	}}
	if m.Pinned {
		pin = MenuItem{Label: "Un-pin from channel", Icon: "unpin", Hint: "P", Do: func() {
			actDo(h, "Message unpinned", func(ctx context.Context, a *core.App) error { return a.Unpin(ctx, ch, ts) })
		}}
	}
	items = append(items,
		MenuItem{Label: "Organize", Sub: []MenuItem{pin}},
		MenuItem{Label: "Forward message…", Do: func() { h.ShowOverlay(NewForwardModal(h, m)) }},
		MenuItem{Label: "Connect to apps", Sub: []MenuItem{{Label: "No apps", Disabled: true}}},
	)
	if own {
		items = append(items, Sep(), MenuItem{Label: "Delete message…", Hint: "delete", Danger: true, Do: func() { ConfirmDelete(h, m) }})
	}
	return items
}

// ConfirmDelete asks before deleting m (delmodal.png) and deletes it on
// confirmation. It shows the modal itself.
func ConfirmDelete(h Host, m core.Message) {
	Confirm(h, "Delete message", "Are you sure you want to delete this message? This cannot be undone.", "Delete", true,
		func(gtx layout.Context) layout.Dimensions { return actMsgPreview(gtx, h, m, true) },
		func() {
			ch, ts := m.Channel, m.TS
			actDo(h, "", func(ctx context.Context, a *core.App) error { return a.Delete(ctx, ch, ts) })
		})
}

// ---- shared helpers (actions_*.go) ----

// actDo runs a core write through do(), toasting when core is missing.
func actDo(h Host, okMsg string, work func(ctx context.Context, a *core.App) error) {
	a := h.Core()
	if a == nil {
		h.Toast("Not connected yet")
		return
	}
	do(h, okMsg, func(ctx context.Context) error { return work(ctx, a) })
}

// actToast returns an action that toasts s (features core can't do yet).
func actToast(h Host, s string) func() { return func() { h.Toast(s) } }

func actSelfID(h Host) string {
	if a := h.Core(); a != nil {
		return a.Self().ID
	}
	return ""
}

// actSnapshot returns the core snapshot without blocking on boot.
func actSnapshot(h Host) (core.Snapshot, bool) {
	a := h.Core()
	if a == nil {
		return core.Snapshot{}, false
	}
	select {
	case <-a.Ready():
		return a.Snapshot(), true
	default:
		return core.Snapshot{}, false
	}
}

// actMsgText is the copyable text of m.
func actMsgText(m core.Message) string {
	if s := richtext.PlainText(m.Doc); strings.TrimSpace(s) != "" {
		return s
	}
	return m.Text
}

// actAuthor resolves the display name and avatar URL of m's author.
func actAuthor(h Host, m core.Message) (name, av string) {
	name, av = m.Name, m.Avatar
	if u, ok := lookupUser(h, m.User); ok {
		if name == "" {
			name = userName(u)
		}
		if av == "" {
			av = u.Avatar
		}
	}
	if name == "" {
		name = "Unknown"
	}
	return name, av
}

// actMsgPreview draws a compact message: avatar 20, name 15/900, time 12px
// muted, then up to 4 lines of text. boxed draws Slack's bordered card
// (1px rgba(29,28,29,.13), radius 8, padding 12 16); otherwise a quote bar.
func actMsgPreview(gtx layout.Context, h Host, m core.Message, boxed bool) layout.Dimensions {
	th := h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	padL, padR, padY := dp(gtx, 16), dp(gtx, 16), dp(gtx, 12)
	if !boxed {
		padL, padR, padY = dp(gtx, 16), 0, dp(gtx, 2)
	}
	innerW := maxInt(1, w-padL-padR)
	name, av := actAuthor(h, m)
	when := dayTime(tsTime(m.TS), time.Now())
	body := actMsgText(m)

	call, d := record(loose(gtx, image.Pt(innerW, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		avPx, gap := dp(gtx, 20), dp(gtx, 8)
		at(gtx, image.Pt(0, dp(gtx, 1)), func(gtx layout.Context) layout.Dimensions { return avatar(gtx, h, av, 20) })
		x := avPx + gap
		tw := maxInt(1, innerW-x)
		nc, nd := record(loose(gtx, image.Pt(tw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Black, cl.Fg, name)
		})
		macroAt(gtx, nc, image.Pt(x, 0))
		if when != "" && nd.Size.X+gap < tw {
			tc, td := record(loose(gtx, image.Pt(tw-nd.Size.X-gap, 1<<20)), func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 12, theme.Regular, cl.Muted, when)
			})
			macroAt(gtx, tc, image.Pt(x+nd.Size.X+gap, (nd.Size.Y-td.Size.Y)/2+dp(gtx, 1)))
		}
		y := maxInt(nd.Size.Y, avPx) + dp(gtx, 2)
		if body == "" {
			return layout.Dimensions{Size: image.Pt(innerW, y)}
		}
		bc, bd := record(loose(gtx, image.Pt(tw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg, body, 4)
		})
		macroAt(gtx, bc, image.Pt(x, y))
		return layout.Dimensions{Size: image.Pt(innerW, y+bd.Size.Y)}
	})
	ht := d.Size.Y + 2*padY
	if boxed {
		stroke(gtx, image.Rect(0, 0, w, ht), dp(gtx, 8), 1, cl.Ring)
	} else {
		rrect(gtx, image.Rect(0, 0, dp(gtx, 4), ht), dp(gtx, 2), theme.WithAlpha(cl.Fg, 0x33))
	}
	macroAt(gtx, call, image.Pt(padL, padY))
	return layout.Dimensions{Size: image.Pt(w, ht)}
}

// ---- reminder / snooze time math (pure; tested) ----

const (
	actRem30m = iota
	actRem1h
	actRem3h
	actRemTomorrow
	actRemMonday
)

// actReminderDue is the due time of a reminder preset picked at now.
func actReminderDue(now time.Time, kind int) time.Time {
	switch kind {
	case actRem30m:
		return now.Add(30 * time.Minute)
	case actRem1h:
		return now.Add(time.Hour)
	case actRem3h:
		return now.Add(3 * time.Hour)
	case actRemTomorrow:
		return actTomorrow9(now)
	}
	return actNextMonday9(now)
}

// actTomorrow9 is 9:00 AM local on the day after now.
func actTomorrow9(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d+1, 9, 0, 0, 0, now.Location())
}

// actNextMonday9 is 9:00 AM on the next Monday strictly after today.
func actNextMonday9(now time.Time) time.Time {
	days := (8 - int(now.Weekday())) % 7
	if days == 0 {
		days = 7
	}
	y, m, d := now.Date()
	return time.Date(y, m, d+days, 9, 0, 0, 0, now.Location())
}

// actMinutesUntil rounds t-now up to whole minutes (at least 1).
func actMinutesUntil(now, t time.Time) int {
	n := int(math.Ceil(t.Sub(now).Minutes()))
	if n < 1 {
		n = 1
	}
	return n
}
