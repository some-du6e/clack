package views

// Taut plugin support for the views (docs/PLUGINS.md, PLUGINS-taut.md):
// the views read members, channels and messages through taut's patched
// views (Nicknames, StreamerMode, patchMessageText…) and route Slack's
// named actions through taut.Dispatch so plugin thunk wrappers run.
//
// Zero cost without plugins: the Host's *taut.Taut may be nil, every
// check below (SlicePatched, ThunkPatched) is nil-safe and one atomic
// load, and nothing allocates or reaches JS unless a plugin patches the
// slice or thunk.

import (
	"context"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
)

// tautHost is optionally implemented by the Host: the plugin system the ui
// root runs (nil when plugins are off).
type tautHost interface{ Taut() *taut.Taut }

func tautOf(h Host) *taut.Taut {
	if th, ok := h.(tautHost); ok {
		return th.Taut()
	}
	return nil
}

// memoEntry caches one patched lookup for the current frame (taut's views
// decode JSON per call; a frame may ask for the same member many times).
type memoEntry struct {
	gen uint32
	ok  bool
	mem taut.MemberFields
	ch  taut.ChannelFields
	msg core.Message
}

func (b *base) memo(key string) (*memoEntry, bool) {
	if b.tmemo == nil {
		b.tmemo = map[string]*memoEntry{}
	}
	if len(b.tmemo) > 4000 {
		clear(b.tmemo)
	}
	e := b.tmemo[key]
	if e != nil && e.gen == b.tgen {
		return e, true
	}
	if e == nil {
		e = &memoEntry{}
		b.tmemo[key] = e
	}
	e.gen = b.tgen
	return e, false
}

// viewUser applies plugins' member patches (patchSlice('members'):
// Nicknames, StreamerMode…) to u. Unpatched it returns u unchanged.
func (b *base) viewUser(u core.User) core.User {
	if !b.tt.SlicePatched("members") || u.ID == "" {
		return u
	}
	e, hit := b.memo("u:" + u.ID)
	if !hit {
		e.mem, e.ok = b.tt.MemberView(u.ID)
	}
	if !e.ok {
		return u
	}
	mf := e.mem
	if mf.Name != "" {
		u.Name = mf.Name
	}
	if mf.RealName != "" {
		u.RealName = mf.RealName
	}
	if mf.Avatar != "" {
		u.Avatar, u.Avatar192, u.Avatar512 = mf.Avatar, mf.Avatar, mf.Avatar
	}
	u.StatusText = mf.StatusText
	if mf.StatusEmoji != u.StatusEmoji {
		u.StatusEmoji, u.StatusEmojiURL = mf.StatusEmoji, ""
		if name := strings.Trim(mf.StatusEmoji, ":"); name != "" {
			u.StatusEmojiURL, _, _ = b.h.Core().Emoji(name)
		}
	}
	return u
}

// viewChannelName is the channel's name as plugins present it
// (patchSlice('channels')), or name unchanged.
func (b *base) viewChannelName(id, name string) string {
	if !b.tt.SlicePatched("channels") || id == "" {
		return name
	}
	e, hit := b.memo("c:" + id)
	if !hit {
		e.ch, e.ok = b.tt.ChannelView(id)
	}
	if e.ok && e.ch.Name != "" {
		return e.ch.Name
	}
	return name
}

// viewMsg is message m as plugins present it (patchMessageText,
// patchSlice('messages')): the same pointer when unpatched, else a
// per-frame copy. Never blocks (taut computes the patched value off the UI
// goroutine and invalidates the window).
func (b *base) viewMsg(m *core.Message) *core.Message {
	if m == nil || !b.tt.SlicePatched("messages") || m.TS == "" {
		return m
	}
	e, hit := b.memo("m:" + m.Channel + "/" + m.TS)
	if !hit {
		e.msg, e.ok = b.tt.MessageView(m.Channel, m.TS)
	}
	if !e.ok {
		return m
	}
	return &e.msg
}

// viewsPatched reports whether plugins patch any slice the views draw
// from (members, channels, messages). Cached row text is re-derived each
// frame while it's true.
func (b *base) viewsPatched() bool {
	return b.tt.SlicePatched("members") || b.tt.SlicePatched("channels") || b.tt.SlicePatched("messages")
}

// preview is the one-line preview of m as plugins present it.
func (b *base) preview(m *core.Message) string { return preview(b.viewMsg(m)) }

// ---- thunks (taut.Dispatch; Slack's argument shapes) ----

type draftRef struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id,omitempty"`
}

type deleteDraftArgs struct {
	Draft  draftRef `json:"draft"`
	Reason string   `json:"reason,omitempty"`
}

type clearDraftsArgs struct {
	IDs        []string `json:"ids"`
	SyncDelete bool     `json:"syncDelete"`
}

type channelArgs struct {
	ChannelID string `json:"channelId"`
}

type huddleArgs struct {
	HuddleID string `json:"huddleId,omitempty"`
}

type huddleToastArgs struct {
	Content      string         `json:"content"`
	SVGIconProps map[string]any `json:"svgIconProps,omitempty"`
	Duration     int            `json:"duration,omitempty"`
}

// deleteDraftThunk deletes one draft (Slack's deleteDraftApi).
func deleteDraftThunk(ctx context.Context, tt *taut.Taut, c *core.App, id, ch string) error {
	_, err := taut.Dispatch(ctx, tt, "deleteDraftApi", deleteDraftArgs{Draft: draftRef{ID: id, ChannelID: ch}, Reason: "user"},
		func(ctx context.Context, a deleteDraftArgs) (any, error) { return nil, c.DeleteDraft(ctx, a.Draft.ID) })
	return err
}

// clearDraftsThunk deletes several drafts (Slack's clearDrafts). It tries
// every id and returns the first error.
func clearDraftsThunk(ctx context.Context, tt *taut.Taut, c *core.App, ids []string) error {
	_, err := taut.Dispatch(ctx, tt, "clearDrafts", clearDraftsArgs{IDs: ids, SyncDelete: true},
		func(ctx context.Context, a clearDraftsArgs) (any, error) {
			var first error
			for _, id := range a.IDs {
				if err := c.DeleteDraft(ctx, id); err != nil && first == nil {
					first = err
				}
			}
			return nil, first
		})
	return err
}

// joinChannelThunk joins a channel (Slack's joinChannel).
func joinChannelThunk(ctx context.Context, tt *taut.Taut, c *core.App, id string) error {
	_, err := taut.Dispatch(ctx, tt, "joinChannel", channelArgs{ChannelID: id},
		func(ctx context.Context, a channelArgs) (any, error) {
			_, err := c.Join(ctx, a.ChannelID)
			return nil, err
		})
	return err
}

// huddleThunks are the Slack huddle bookkeeping actions plugins wrap
// (HuddleActivity…). Natively they are no-ops: Clack has no huddle media.
var huddleThunks = [...]string{"startEventLog", "stopHuddle", "stopEventLog", "enqueueHuddleToast"}

// huddleStarter is implemented by a Host that runs native huddles (the ui
// root, through internal/ui/huddleui).
type huddleStarter interface{ StartHuddle(channel string) }

// startHuddle starts or joins the huddle in args.ChannelID natively, through
// Slack's startHuddle thunk when a plugin wraps it (BetterHuddles), else
// directly. Without a native huddle Host it is huddleAttempt.
func startHuddle(h Host, tt *taut.Taut, args startHuddleArgs) {
	hs, ok := h.(huddleStarter)
	if !ok {
		huddleAttempt(h, tt, args)
		return
	}
	if args.ChannelID == "" { // Slack's "New huddle" asks who to call
		if tt.ThunkPatched("startHuddle") {
			huddleAttempt(h, tt, args) // a plugin may take it
			return
		}
		h.Toast("Open a channel or DM and click Huddle in its header to start one")
		return
	}
	if !tt.ThunkPatched("startHuddle") {
		hs.StartHuddle(args.ChannelID)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := taut.Dispatch(ctx, tt, "startHuddle", args, func(ctx context.Context, a startHuddleArgs) (any, error) {
			if a.ChannelID == "" {
				return nil, nil
			}
			ch := a.ChannelID
			if p, ok := h.(interface{ Post(func()) }); ok { // back on the UI goroutine
				p.Post(func() { hs.StartHuddle(ch) })
			} else {
				hs.StartHuddle(ch)
			}
			return nil, nil
		})
		if err != nil {
			h.Toast("Couldn't start the huddle: " + err.Error())
		}
		h.Invalidate()
	}()
}

// startHuddleArgs is Slack's startHuddle argument (BetterHuddles reads
// channelId, muted, isStandaloneHuddle and fromJoinPreview).
type startHuddleArgs struct {
	ChannelID          string `json:"channelId,omitempty"`
	HuddleID           string `json:"huddleId,omitempty"`
	Muted              bool   `json:"muted"`
	FromJoinPreview    bool   `json:"fromJoinPreview,omitempty"`
	IsStandaloneHuddle bool   `json:"isStandaloneHuddle,omitempty"`
}

// huddleAttempt is a start/join click: Slack's startHuddle thunk with
// args (the conversation, the huddle when joining one; a new huddle from
// the page header is standalone). Clack can't join (no Chime media), so natively it runs like a
// huddle that ends at once: startEventLog, stopHuddle and stopEventLog,
// then enqueueHuddleToast says why. A plugin wrapping startHuddle
// (BetterHuddles) can take the join over instead. Without a plugin
// wrapping any of them it's just the toast, inline.
func huddleAttempt(h Host, tt *taut.Taut, args startHuddleArgs) {
	patched := tt.ThunkPatched("startHuddle")
	for _, n := range huddleThunks {
		patched = patched || tt.ThunkPatched(n)
	}
	if !patched {
		h.Toast(joinUnsupported)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := taut.Dispatch(ctx, tt, "startHuddle", args, func(ctx context.Context, a startHuddleArgs) (any, error) {
			noop := func(ctx context.Context, a huddleArgs) (any, error) { return nil, nil }
			for _, n := range huddleThunks[:3] {
				_, _ = taut.Dispatch(ctx, tt, n, huddleArgs{HuddleID: a.HuddleID}, noop)
			}
			return taut.Dispatch(ctx, tt, "enqueueHuddleToast",
				huddleToastArgs{Content: joinUnsupported, SVGIconProps: map[string]any{"type": "headphones"}, Duration: 5000},
				func(ctx context.Context, a huddleToastArgs) (any, error) {
					if a.Content != "" {
						h.Toast(a.Content)
					}
					return nil, nil
				})
		})
		if err != nil {
			h.Toast(joinUnsupported)
		}
		h.Invalidate()
	}()
}

// fetchBackgrounds dispatches maybeFetchBackgrounds for a live huddle
// (Slack loads huddle themes; a no-op natively). Only when patched.
func fetchBackgrounds(tt *taut.Taut, roomID string) {
	if !tt.ThunkPatched("maybeFetchBackgrounds") {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = taut.Dispatch(ctx, tt, "maybeFetchBackgrounds", huddleArgs{HuddleID: roomID},
			func(ctx context.Context, a huddleArgs) (any, error) { return nil, nil })
	}()
}
