package popups

// Taut thunks popups dispatches (docs/PLUGINS-taut.md, "Thunks"):
// getContextMenuTemplate (channel menu), getMemberContextMenuTemplate
// (member menu), searchInline (search dropdown's message suggestions),
// submitAddOrRemoveReaction (reacting from the emoji picker) and
// chatDeleteFetcher (Delete message).
//
// Zero cost without plugins: every entry point first loads one atomic
// pointer and asks taut's atomic "is this thunk patched?". Only when a
// plugin wraps the thunk does anything cross into JS, and then always off
// the UI goroutine (run()), never blocking a frame. Menus open at once with
// the native items and take the plugins' template when JS answers (a few
// ms); native items keep their actions by template key.

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"strings"
	"sync/atomic"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

var tautRT atomic.Pointer[taut.Taut]

// SetTaut attaches the plugin runtime (the ui root calls it once taut.New
// succeeds; nil detaches). Popups then routes Slack's thunks through it.
func SetTaut(t *taut.Taut) { tautRT.Store(t) }

// tautFor returns the runtime when a plugin patches thunk, else nil (the
// native fast path).
func tautFor(thunk string) *taut.Taut {
	t := tautRT.Load()
	if t == nil || !t.ThunkPatched(thunk) {
		return nil
	}
	return t
}

// ---- context menu templates ----

// tautTemplate is items as a JSON-safe Slack MenuTemplateItem[] (what the
// native thunk returns to plugins): keys, labels, types, icons, flags and
// submenus. Native actions stay in Go and come back by key.
func tautTemplate(items []MenuItem) []map[string]any {
	keys := menuKeys(items)
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		it := &items[i]
		e := map[string]any{"key": keys[i]}
		switch {
		case it.Separator:
			e["type"] = "separator"
		case it.Header != "":
			e["type"], e["label"] = "header", it.Header
		case it.Note != "":
			e["type"], e["label"] = "custom", it.Note
		default:
			if it.Label != "" {
				e["label"] = it.Label
			} else if it.Widget != nil {
				e["type"] = "custom"
			}
			if it.Icon != "" {
				e["icon"] = it.Icon
			}
			if it.Danger {
				e["danger"] = true
			}
			if it.Disabled {
				e["disabled"] = true
			}
			if it.Checked {
				e["type"], e["checked"] = "checkbox", true
			}
			if len(it.Sub) > 0 {
				e["type"], e["template"] = "submenu", tautTemplate(it.Sub)
			}
		}
		out = append(out, e)
	}
	return out
}

// tautMenuItems decodes a template plugins returned (clicks are JS
// functions) against the native items it was built from.
func tautMenuItems(h Host, raw json.RawMessage, base []MenuItem) ([]MenuItem, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	p, err := slackc.DecodeProps(append(append([]byte(`{"t":`), raw...), '}'))
	if err != nil {
		return nil, false
	}
	list := p.List("t")
	if list == nil {
		return nil, false
	}
	return menuFromTemplate(h.Theme(), list, base), true
}

// tautMenu routes menu m through thunk (Slack's {id} argument) when a
// plugin patches it: base is the native template, and the plugins'
// template replaces m's rows when JS answers. A no-op without plugins.
func tautMenu(h Host, m *Menu, thunk, id string) {
	t := tautFor(thunk)
	if t == nil || m == nil {
		return
	}
	base := append([]MenuItem(nil), m.items...)
	tpl := tautTemplate(base)
	run(h, func(ctx context.Context) (json.RawMessage, error) {
		native := func(ctx context.Context, _ map[string]string) (json.RawMessage, error) {
			return json.Marshal(tpl)
		}
		return taut.Dispatch(ctx, t, thunk, map[string]string{"id": id}, native)
	}, func(raw json.RawMessage, err error) {
		if err != nil || m.Dismissed() {
			return
		}
		if items, ok := tautMenuItems(h, raw, base); ok {
			// the thunk's template is what Slack's MenuFromTemplate renders,
			// so it becomes the menu's base too (a patched MenuFromTemplate
			// is fed m.orig and would put the native items back)
			m.orig = items
			m.tplFrom = nil
			m.setItems(items)
			h.Invalidate()
		}
	})
}

// NewMemberMenu is Slack's member context menu (right-click on a name or
// avatar): View full profile, Copy display name, Copy member ID, Copy link
// to profile. Plugins extend it through getMemberContextMenuTemplate
// (Nicknames' "Set nickname…").
func NewMemberMenu(h Host, anchor image.Point, userID string) *Menu {
	u, ok := lookupUser(h, userID)
	if !ok {
		u = core.User{ID: userID}
	}
	m := NewMenuOpts(h, pfMoreItems(h, u, true, nil), MenuOpts{Anchor: pointRect(anchor)})
	tautMenu(h, m, "getMemberContextMenuTemplate", userID)
	return m
}

// NewProfileOverflowMenu is a member's profile ⋮ menu (the hover card's
// and the full profile's), inside RimetoMemberProfileOverflowMenu: the
// menu AdminBackend and Nicknames add their items to.
func NewProfileOverflowMenu(h Host, anchor image.Point, userID string) *Menu {
	u, ok := lookupUser(h, userID)
	if !ok {
		u = core.User{ID: userID}
	}
	return NewMenuOpts(h, pfMoreItems(h, u, false, nil), pfMenuOpts(userID, MenuOpts{Anchor: pointRect(anchor), AlignRight: true}))
}

// ---- reactions ----

// reactionKey is Slack's _rxn_key for a message.
func reactionKey(ch, ts string) string { return "message-" + ts + "-" + ch }

// React adds (add=true) or removes a reaction as the viewer, through
// Slack's submitAddOrRemoveReaction so plugins (OwlMode) can redirect or
// hold it back. Call on the UI goroutine; it never blocks.
func React(h Host, ch, ts, name string, add bool) {
	name = strings.Trim(name, ":")
	if h == nil || h.Core() == nil || ch == "" || ts == "" || name == "" {
		return
	}
	a := h.Core()
	native := func(ctx context.Context, args tautReactArgs) (any, error) {
		return nil, a.React(ctx, ch, ts, strings.Trim(args.Name, ":"), args.Add)
	}
	args := tautReactArgs{ReactionKey: reactionKey(ch, ts), Name: name, Add: add}
	t := tautFor("submitAddOrRemoveReaction")
	// core.React shows the change at once and rolls it back on failure;
	// the toast says why.
	run(h, func(ctx context.Context) (struct{}, error) {
		if t == nil {
			_, err := native(ctx, args)
			return struct{}{}, err
		}
		_, err := taut.Dispatch(ctx, t, "submitAddOrRemoveReaction", args, native)
		return struct{}{}, tautErr(err)
	}, func(_ struct{}, err error) {
		if err != nil {
			h.Toast(reactionError(add, err))
		}
	})
}

// reactionError is the toast for a failed reaction.
func reactionError(add bool, err error) string {
	verb := "add"
	if !add {
		verb = "remove"
	}
	return "Couldn't " + verb + " the reaction: " + errText(err)
}

type tautReactArgs struct {
	ReactionKey string `json:"reactionKey"`
	Name        string `json:"name"`
	Add         bool   `json:"add"`
}

// tautErr strips the JS error wrapper so toasts read like the plugin's
// own message ("Owl mode held back a reaction as you").
func tautErr(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	if i := strings.LastIndex(s, "Error: "); i >= 0 {
		s = s[i+len("Error: "):]
	}
	return errors.New(strings.TrimSpace(s))
}

// NewReactionPicker is the emoji picker for reacting to a message: it
// carries the message on its EmojiPickerPopover boundary (OwlMode reads
// it) and reacts through React. onPick (optional) also gets the name.
func NewReactionPicker(h Host, anchor image.Point, ch, ts, threadTS string, onPick func(name string)) *EmojiPicker {
	p := NewEmojiPickerAt(h, anchor, func(name string) {
		React(h, ch, ts, name, true)
		if onPick != nil {
			onPick(name)
		}
	})
	p.SetMessage(ch, ts, threadTS)
	return p
}

// ---- search suggestions ----

// tautSearchInline passes the dropdown's message suggestions through
// Slack's searchInline ({query} → {items}) when a plugin patches it
// (Censorship rewrites their text). Runs on the search goroutine.
func tautSearchInline(ctx context.Context, h Host, q string, msgs []core.Message) []core.Message {
	t := tautFor("searchInline")
	if t == nil || len(msgs) == 0 {
		return msgs
	}
	items := make([]any, len(msgs))
	for i := range msgs {
		items[i] = map[string]any(slackMessage(&msgs[i]))
	}
	native := func(ctx context.Context, _ map[string]string) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"items": items})
	}
	raw, err := taut.Dispatch(ctx, t, "searchInline", map[string]string{"query": q}, native)
	if err != nil || len(raw) == 0 {
		return msgs
	}
	var res struct {
		Items []struct {
			Channel string `json:"channel"`
			TS      string `json:"ts"`
			Text    string `json:"text"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return msgs
	}
	byKey := make(map[string]int, len(msgs))
	for i := range msgs {
		byKey[msgs[i].Channel+"/"+msgs[i].TS] = i
	}
	out := make([]core.Message, 0, len(res.Items))
	for _, it := range res.Items {
		i, ok := byKey[it.Channel+"/"+it.TS]
		if !ok {
			continue
		}
		m := msgs[i]
		if it.Text != m.Text {
			m.Text = it.Text
			if a := h.Core(); a != nil {
				m.Doc.Blocks = a.Mrkdwn(it.Text)
			}
		}
		out = append(out, m)
	}
	return out
}

// ---- deleting ----

// DeleteMessage, when set (ui-messages, while the plugin runtime is
// attached), deletes for ConfirmDelete; MessageMenuOpts.Delete overrides
// it per menu. Otherwise popups dispatches Slack's chatDeleteFetcher.
var DeleteMessage func(ctx context.Context, ch, ts string) error

// deleteMessage deletes ch/ts: del, else DeleteMessage, else
// chatDeleteFetcher (plugins may wrap it) over core.Delete.
func deleteMessage(ctx context.Context, h Host, del func(ctx context.Context, ch, ts string) error, ch, ts string) error {
	if del != nil {
		return del(ctx, ch, ts)
	}
	if DeleteMessage != nil {
		return DeleteMessage(ctx, ch, ts)
	}
	a := h.Core()
	if a == nil {
		return errNoCore
	}
	native := func(ctx context.Context, args map[string]string) (any, error) {
		return nil, a.Delete(ctx, args["channel"], args["ts"])
	}
	args := map[string]string{"channel": ch, "ts": ts}
	if t := tautFor("chatDeleteFetcher"); t != nil {
		_, err := taut.Dispatch(ctx, t, "chatDeleteFetcher", args, native)
		return tautErr(err)
	}
	_, err := native(ctx, args)
	return err
}
