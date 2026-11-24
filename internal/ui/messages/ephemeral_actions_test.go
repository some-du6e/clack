package messages

import (
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// Slack's message actions on an ephemeral ("Only visible to you")
// message and its relatives (popups/actions_rules.go has each rule):
// the toolbar keeps ⋮ alone, its menu Copy message and Delete message…
// (which removes the message here alone), and a key does nothing where
// the button isn't. Karim saw the whole toolbar, Reply in thread
// included, on one in a thread pane (2026-11-23).

const (
	tEph      = "C0EPHACTS"
	tEphKeys  = "C0EPHKEYS"
	tEphAppDM = "D0EPHAPP1"
	tEphApp   = "UEPHAPP01"

	tsEphPlain = "1700000501.000100"
	tsEph      = "1700000502.000100"
	tsEphTomb  = "1700000503.000100"
	tsEphRoot  = "1700000504.000100"
	tsEphR1    = "1700000504.000200"
	tsEphR2    = "1700000504.000300" // an ephemeral reply
	tsEphMine  = "1700000505.000100"
	tsEphDM    = "1700000506.000100"
)

// newEphEnv is a ctxEnv over #eph: an ordinary message, Slackbot's
// ephemeral one, a tombstone kept for its reply, a thread root whose
// second reply is ephemeral, and one of the viewer's own; plus a DM from
// an app whose reply there is ephemeral. f records the API calls.
func newEphEnv(t *testing.T) (*ctxEnv, *fakeSlack) {
	t.Helper()
	app, f := newTestApp(t)
	app.S.PutUsers([]slack.User{{ID: tEphApp, Name: "pollbot", RealName: "Poll Bot", IsBot: true, Profile: slack.Profile{DisplayName: "Poll Bot"}}})
	app.S.PutConv(slack.Conversation{ID: tEph, Name: "eph", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutConv(slack.Conversation{ID: tEphKeys, Name: "ephkeys", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.PutConv(slack.Conversation{ID: tEphAppDM, IsIM: true, User: tEphApp, IsMember: true})
	app.S.SetLatest(tEph, []slack.Message{
		{Type: "message", TS: tsEphPlain, User: "UALICE", Text: "a synthetic note"},
		{Type: "message", TS: tsEph, User: tBot, Text: "only you can see this synthetic notice", IsEphemeral: true},
		{Type: "message", TS: tsEphTomb, User: tBot, Subtype: "tombstone", Text: "This message was deleted.", ThreadTS: tsEphTomb,
			ReplyCount: 1, ReplyUsers: []string{"UBOB"}, LatestReply: "1700000503.000200"},
		{Type: "message", TS: tsEphRoot, User: "UBOB", Text: "a synthetic question", ThreadTS: tsEphRoot, ReplyCount: 2,
			ReplyUsers: []string{"UALICE"}, LatestReply: tsEphR2},
		{Type: "message", TS: tsEphMine, User: tSelf, Text: "a synthetic note of mine"},
	}, false)
	app.S.SetThread(tEph, tsEphRoot, []slack.Message{
		{Type: "message", TS: tsEphRoot, User: "UBOB", Text: "a synthetic question", ThreadTS: tsEphRoot, ReplyCount: 2},
		{Type: "message", TS: tsEphR1, User: "UALICE", Text: "a synthetic answer", ThreadTS: tsEphRoot},
		{Type: "message", TS: tsEphR2, User: tBot, Text: "only you can see this synthetic reply", ThreadTS: tsEphRoot, IsEphemeral: true},
	})
	app.S.SetLatest(tEphKeys, []slack.Message{
		{Type: "message", TS: tsEphPlain, User: "UALICE", Text: "a synthetic note"},
		{Type: "message", TS: tsEph, User: tBot, Text: "only you can see this synthetic notice", IsEphemeral: true},
	}, false)
	app.S.SetLatest(tEphAppDM, []slack.Message{{Type: "message", TS: tsEphDM, User: tEphApp, Text: "only you can see this synthetic app reply", IsEphemeral: true}}, false)
	e := &ctxEnv{t: t, h: &ctxHost{coreHost: coreHost{app: app, th: theme.New(false)}}, now: time.Now()}
	e.v = NewChannelView(e.h, tEph)
	e.v.Preview = true
	until(t, 5*time.Second, "the rows laid out", e.frame, func() bool {
		for _, ts := range []string{tsEphPlain, tsEph, tsEphTomb, tsEphRoot, tsEphMine} {
			if e.row(ts) == nil {
				return false
			}
		}
		return true
	})
	for k := 0; k < 3; k++ {
		e.frame()
	}
	return e, f
}

// ephHover puts the pointer on row ts and reports whether the toolbar is
// drawn for it, and how many quick reactions it has.
func (e *ctxEnv) ephHover(t *testing.T, ts string) (drawn bool, quick int) {
	t.Helper()
	s := e.v.s
	now := e.now.Add(time.Second)
	e.moveTo(rowPoint(t, s, ts), now)
	e.frameAt(now.Add(20 * time.Millisecond))
	e.frameAt(now.Add(40 * time.Millisecond))
	if s.tbFor < 0 || s.tbKey != ts {
		return false, 0
	}
	return true, len(s.quick.names)
}

// The toolbar on an ephemeral message is ⋮ alone, in the channel, in the
// thread pane and in an app's DM; an ordinary message keeps every button.
// A tombstone has one too, with the thread button its replies give it.
func TestEphemeralToolbarLikeSlack(t *testing.T) {
	t.Cleanup(uipref.Reset)
	uipref.Set(store.Prefs{MorePrefs: store.MorePrefs{QuickReactions: true}})
	e, _ := newEphEnv(t)
	app := e.h.app
	s := e.v.s
	for _, c := range []struct{ ts, want string }{
		{tsEphPlain, "Add reaction…|Reply in thread|Share message…|Add to-do|Add to list…|More actions"},
		{tsEph, "More actions"},
		// no reactions or share (isActionable); its replies give it the
		// thread button, and to-do and lists have no tombstone case
		{tsEphTomb, "Reply to thread|Add to-do|Add to list…|More actions"},
	} {
		if got, _ := toolbarFor(t, s, app, c.ts); got != c.want {
			t.Errorf("channel, %s: toolbar %q, want %q", c.ts, got, c.want)
		}
	}
	// drawn: the ephemeral message's card without quick reactions, the
	// tombstone's too (canAddReaction), the ordinary one's with them
	for _, c := range []struct {
		ts    string
		quick bool
	}{{tsEphPlain, true}, {tsEph, false}, {tsEphTomb, false}} {
		drawn, quick := e.ephHover(t, c.ts)
		if !drawn {
			t.Errorf("%s: no toolbar drawn", c.ts)
			continue
		}
		if (quick > 0) != c.quick {
			t.Errorf("%s: %d quick reactions, want them %v", c.ts, quick, c.quick)
		}
	}

	// the thread pane: never a thread button; the ephemeral reply has ⋮
	// alone
	tv := NewThreadView(e.h, tEph, tsEphRoot)
	tf := expFrame(tv, 500)
	until(t, 5*time.Second, "the thread", tf, func() bool { return tv.s.rows[tsEphR1] != nil && tv.s.rows[tsEphR2] != nil })
	for _, c := range []struct{ ts, want string }{
		{tsEphR1, "Add reaction…|Share message…|Add to-do|Add to list…|More actions"},
		{tsEphR2, "More actions"},
	} {
		if got, _ := toolbarFor(t, tv.s, app, c.ts); got != c.want {
			t.Errorf("thread pane, %s: toolbar %q, want %q", c.ts, got, c.want)
		}
	}

	// an app's "Only visible to you" reply in its DM: the same
	dv := NewChannelView(e.h, tEphAppDM)
	dv.Preview = true
	df := expFrame(dv, 900)
	until(t, 5*time.Second, "the DM's row", df, func() bool { return dv.s.rows[tsEphDM] != nil })
	if got, _ := toolbarFor(t, dv.s, app, tsEphDM); got != "More actions" {
		t.Errorf("app DM: toolbar %q, want More actions", got)
	}
}

// A notice this client made itself (LocalEphemeral, Slack's
// addEphemeralMessage with its fake "x" ts: isTempMsg) has no toolbar at
// all, as a pending send has none.
func TestLocalNoticeHasNoToolbar(t *testing.T) {
	e, _ := newEphEnv(t)
	ts := e.h.app.LocalEphemeral(tEph, "", "a synthetic local notice")
	until(t, 5*time.Second, "the notice's row", e.frame, func() bool { return e.row(ts) != nil })
	if drawn, _ := e.ephHover(t, tsEphPlain); !drawn {
		t.Fatal("no toolbar on the ordinary message")
	}
	if drawn, _ := e.ephHover(t, ts); drawn {
		t.Errorf("the local notice has a toolbar: %q", strings.Join(e.v.s.tbLabel[:], "|"))
	}
}

// ephItem is the menu item (or submenu item) whose action is action.
func ephItem(items []popups.MenuItem, action string) (popups.MenuItem, bool) {
	for _, it := range items {
		if it.Action == action || it.Key == action && action != "" {
			return it, true
		}
		if sub, ok := ephItem(it.Sub, action); ok {
			return sub, true
		}
	}
	return popups.MenuItem{}, false
}

// ephKeyed is the menu item (or submenu item) that takes key k.
func ephKeyed(items []popups.MenuItem, k string) (string, bool) {
	for _, it := range items {
		if slices.Contains(it.Keys, k) && !it.Disabled {
			return it.Label, true
		}
		if l, ok := ephKeyed(it.Sub, k); ok {
			return l, true
		}
	}
	return "", false
}

// Each action of the right-click menu (the toolbar's group, then the ⋮
// menu) on an ephemeral message, by Slack's rule for it: there or not,
// still there on an ordinary message, and its key bound to nothing where
// the row is gone (Slack's menu binds an item's key only while the item
// is there). The ⋮ menu has the same rows below the toolbar's group.
func TestEphemeralMenuActionsLikeSlack(t *testing.T) {
	e, _ := newEphEnv(t)
	s := e.v.s
	at := image.Pt(400, 300)
	menus := func(ts string) (ctx, more []popups.MenuItem) {
		r := s.rows[ts]
		if r == nil {
			t.Fatalf("no row %s", ts)
		}
		ctx = popups.NewMessageContextMenu(e.h, at, r.src, popups.InChannel, popups.MessageMenuOpts{}).Items()
		more = popups.NewMessageMenuOpts(e.h, at, r.src, popups.MessageMenuOpts{}).Items()
		return ctx, more
	}
	ephCtx, ephMore := menus(tsEph)
	plainCtx, plainMore := menus(tsEphPlain)
	mineCtx, mineMore := menus(tsEphMine)
	for _, a := range []struct {
		name, action string
		keys         []string
		onEph        bool // Slack keeps it on an ephemeral message
		toolbar      bool // the toolbar's group: the right-click menu only
		own          bool // on the viewer's own messages only
	}{
		{"add reaction (canAddReaction)", "add_reaction", popups.KeyReact, false, true, false},
		{"reply in thread (canStartOrOpenThread)", "comment", popups.KeyComment, false, true, false},
		{"forward (canBeShared)", "share_message", popups.KeyShare, false, true, false},
		{"add to-do (addToLaterTodos)", "add_to_later_todos", popups.KeyLaterTodos, false, true, false},
		{"mark unread (canMarkUnread)", "mark_unread", popups.KeyMarkUnread, false, false, false},
		{"save (later)", "later", popups.KeySaveTodos, false, false, false},
		{"reminder (canAddReminder)", "remind", popups.KeyRemind, false, false, false},
		{"follow (canStartOrOpenThread)", "subscribe", nil, false, false, false},
		{"copy link (canCopyLink)", "copy_link", popups.KeyCopyLink, false, false, false},
		{"copy message (canCopyMessageText)", "copy_text", popups.KeyCopyText, true, false, false},
		{"organize (isActionable)", "organize_actions_menu", nil, false, false, false},
		{"pin (canPinMessage)", "pin", popups.KeyPin, false, false, false},
		{"connect to apps (isActionable)", "connect_to_apps_actions_menu", nil, false, false, false},
		{"delete (canDeleteMessageHelper)", "delete_message", popups.KeyDelete(), true, false, true},
	} {
		t.Run(a.name, func(t *testing.T) {
			normalCtx, normalMore := plainCtx, plainMore
			if a.own {
				normalCtx, normalMore = mineCtx, mineMore
			}
			lists := [][2][]popups.MenuItem{{ephCtx, normalCtx}}
			if !a.toolbar {
				lists = append(lists, [2][]popups.MenuItem{ephMore, normalMore})
			}
			for k, l := range lists {
				which := [...]string{"right-click menu", "⋮ menu"}[k]
				if _, ok := ephItem(l[1], a.action); !ok {
					t.Errorf("%s: no %s on an ordinary message", which, a.action)
				}
				_, ok := ephItem(l[0], a.action)
				if ok != a.onEph {
					t.Errorf("%s: %s on the ephemeral message %v, want %v", which, a.action, ok, a.onEph)
				}
				if a.onEph {
					continue
				}
				for _, key := range a.keys {
					if label, ok := ephKeyed(l[0], key); ok {
						t.Errorf("%s: key %q runs %q on the ephemeral message", which, key, label)
					}
				}
			}
		})
	}
	// with no toolbar group the right-click menu starts at the ⋮ menu's
	// rows, no separator above them
	if len(ephCtx) == 0 || ephCtx[0].Separator {
		t.Errorf("ephemeral right-click menu starts %q", labels(popups.NewMessageContextMenu(e.h, at, s.rows[tsEph].src, popups.InChannel, popups.MessageMenuOpts{})))
	}
}

// Delete message… on an ephemeral message asks as for any message, then
// removes it here alone: Slack's delete only animates it out
// (animateAndDeleteMessageApi returns before chat.delete).
func TestEphemeralDeleteIsLocal(t *testing.T) {
	e, f := newEphEnv(t)
	app := e.h.app
	it, ok := ephItem(popups.NewMessageMenuOpts(e.h, image.Pt(400, 300), e.v.s.rows[tsEph].src, popups.MessageMenuOpts{}).Items(), "delete_message")
	if !ok || it.Do == nil {
		t.Fatal("no Delete message… on the ephemeral message")
	}
	it.Do()
	var modal *popups.Modal
	for _, o := range e.h.overlays {
		if m, ok := o.(*popups.Modal); ok {
			modal = m
		}
	}
	if modal == nil {
		t.Fatal("Delete message… asked nothing")
	}
	if !modal.Press("Delete") {
		t.Fatal("the dialog has no Delete button")
	}
	until(t, 5*time.Second, "the message gone", e.frame, func() bool {
		_, found := app.S.Find(tEph, tsEph)
		return !found && e.row(tsEph) == nil
	})
	time.Sleep(200 * time.Millisecond) // a chat.delete would have gone out by now
	if calls := f.called("chat.delete"); len(calls) != 0 {
		t.Errorf("chat.delete went out for the ephemeral message: %v", calls)
	}
	if _, found := app.S.Find(tEph, tsEphPlain); !found {
		t.Error("the ordinary message went too")
	}
}

// V (Add to list…) on the keyboard-selected message: nothing on an
// ephemeral message (shouldShowAddToList), its list on an ordinary one.
func TestEphemeralAddToListKeyIsNoOp(t *testing.T) {
	e, _ := newEphEnv(t)
	lh := &listAddHost{ctxHost: e.h}
	e.v = NewChannelView(lh, tEphKeys)
	e.v.Preview = true
	until(t, 5*time.Second, "the rows laid out", e.frame, func() bool { return e.row(tsEph) != nil && e.row(tsEphPlain) != nil })
	if !e.v.FocusNewest(func() {}) {
		t.Fatal("no message to select")
	}
	for k := 0; k < 3; k++ { // the focus frame holds that frame's events
		e.frame()
	}
	if e.v.s.kb.ts != tsEph {
		t.Fatalf("selected %s, want the ephemeral message", e.v.s.kb.ts)
	}
	press := func(n key.Name) {
		e.r.Queue(key.Event{Name: n, State: key.Press}, key.Event{Name: n, State: key.Release})
		e.frame()
		e.frame()
	}
	press("V")
	if len(lh.added) != 0 {
		t.Fatalf("V on the ephemeral message added %v to a list", lh.added)
	}
	press(key.NameUpArrow)
	if e.v.s.kb.ts != tsEphPlain {
		t.Fatalf("↑ selected %s, want the ordinary message", e.v.s.kb.ts)
	}
	press("V")
	if len(lh.added) != 1 || lh.added[0].TS != tsEphPlain {
		t.Errorf("V on the ordinary message: %v", lh.added)
	}
}
