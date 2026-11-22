package views

// Later's ways into a canvas's file channel (canvas-mention round 6).
// Slack's To-dos and Saved rows and the rail peek's rows run
// jumpToSavedMessage (vh7T → jumpToMessage, isSpaceship): the canvas,
// never the file channel's conversation or its thread view. The menu's
// Open in Home is R9 toMessage (e6), which RedirectFileChannel makes the
// thread view on the message, and Open in split view is
// openInSplitView(R9) at the message (ta). Made-up messages only.

import (
	"encoding/json"
	"image"
	"net/url"
	"slices"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// lfHost is w26Host with Later's file channel hooks (SavedJumper) and the
// split view, recording what the page asks for.
type lfHost struct {
	*w26Host
	jumps  []string // JumpToSaved: "ch thread ts side|full"
	homes  []string // OpenSavedInHome: "ch/ts"
	splits []string // OpenSplit's routes
}

func (h *lfHost) JumpToSaved(ch, thread, ts string, where FileWhere) bool {
	if !h.Core().IsFileChannelID(ch) {
		return false
	}
	h.jumps = append(h.jumps, ch+" "+thread+" "+ts+" "+map[FileWhere]string{FileSide: "side", FileFull: "full", FileInPane: "pane"}[where])
	return true
}

func (h *lfHost) OpenSavedInHome(ch, ts string) bool {
	if !h.Core().IsFileChannelID(ch) {
		return false
	}
	h.homes = append(h.homes, ch+"/"+ts)
	return true
}

func (h *lfHost) OpenSplit(route string) { h.splits = append(h.splits, route) }
func (h *lfHost) SplitViewOK() bool      { return true }
func (h *lfHost) InSplitView() bool      { return false }

var (
	_ SavedJumper        = (*lfHost)(nil)
	_ popups.SplitViewer = (*lfHost)(nil)
)

// lfItems serves saved.list with the canvas mention in todo state (in the
// canvas's file channel, stored as the private is_file conversation it
// is) and messages.list with its message.
func lfItems(t *testing.T, todo string) *core.App {
	t.Helper()
	now := time.Now().Unix()
	items := []map[string]any{{"item_id": fakeslack.CanvasMentionChannel, "item_type": "message", "ts": fakeslack.CanvasMentionTS,
		"todo_state": todo, "date_created": now - 100}}
	msg := map[string]any{"type": "message", "subtype": "document_mention", "ts": fakeslack.CanvasMentionTS, "user": "U0FAKE00001",
		"text": "a synthetic mention", "document_mention": map[string]any{"file_id": fakeslack.CanvasMentionFile,
			"section_id": fakeslack.CanvasMentionSection}}
	app, _ := vkFake(t, func(method string, f url.Values) (any, bool) {
		switch method {
		case "saved.list":
			return map[string]any{"ok": true, "saved_items": items}, true
		case "messages.list":
			var want []struct {
				Channel    string   `json:"channel"`
				Timestamps []string `json:"timestamps"`
			}
			_ = json.Unmarshal([]byte(f.Get("message_ids")), &want)
			out := map[string][]map[string]any{}
			for _, w := range want {
				if w.Channel == fakeslack.CanvasMentionChannel && slices.Contains(w.Timestamps, fakeslack.CanvasMentionTS) {
					out[w.Channel] = append(out[w.Channel], msg)
				}
			}
			return map[string]any{"ok": true, "messages": out}, true
		}
		return nil, false
	})
	app.S.PutConv(slack.Conversation{ID: fakeslack.CanvasMentionChannel, Name: fakeslack.CanvasMentionTitle, IsChannel: true, IsGroup: true,
		IsPrivate: true, IsFile: true, IsMember: true, NumMembers: 2})
	app.SetExperimentGroup("later_todos", "on")
	return app
}

func lfKey() string { return fakeslack.CanvasMentionChannel + "/" + fakeslack.CanvasMentionTS }

// A To-dos row, the rail peek's row and a Saved row on a saved canvas
// mention are jumpToSavedMessage: the host's file jump, beside the list
// (To-dos, Saved under hq) or in the page's place (Saved without hq),
// never the column's toMessage. The peek opens the To-dos tab first.
func TestLaterFileChannelRowsJumpLikeSlack(t *testing.T) {
	want := func(where string) []string {
		return []string{fakeslack.CanvasMentionChannel + "  " + fakeslack.CanvasMentionTS + " " + where}
	}
	t.Run("todos", func(t *testing.T) {
		h := &lfHost{w26Host: newW26Host(lfItems(t, "to_do"), false)}
		l := NewLater(h)
		e := &vaEnv{size: image.Pt(620, 900)}
		e.draw = func(gtx layout.Context) { l.Layout(gtx) }
		e.until(t, "the to-do", func() bool { return len(l.view) == 1 && l.view[0].Msg != nil })
		e.frame()
		kbClickRow(t, e, l.kb, lfKey(), e.size.Y-l.kb.viewH)
		if !slices.Equal(h.jumps, want("side")) || h.secCh != "" || len(h.navs) != 0 || len(h.threads) != 0 {
			t.Errorf("the row: jumps %q, column %q, navigated %q, threads %q; want the jump beside the list alone", h.jumps, h.secCh, h.navs, h.threads)
		}
		// the rail peek's row: maybeClosePeek, the To-dos tab, the jump
		h.jumps, h.navs = nil, nil
		closed := false
		l.peeking, l.peekClose = true, func() { closed = true }
		it := l.view[0]
		l.rowClick(&it)
		l.peeking, l.peekClose = false, nil
		if !closed || !slices.Equal(h.navs, []string{"later"}) || !slices.Equal(h.jumps, want("side")) || h.secCh != "" {
			t.Errorf("the peek's row: closed %v, navigated %q, jumps %q, column %q; want the To-dos tab and the jump beside it", closed, h.navs, h.jumps, h.secCh)
		}
	})
	t.Run("saved", func(t *testing.T) {
		app := lfItems(t, "saved")
		h := &lfHost{w26Host: newW26Host(app, false)}
		s := NewSaved(h)
		e := &vaEnv{size: image.Pt(620, 900)}
		e.draw = func(gtx layout.Context) { s.Layout(gtx) }
		e.until(t, "the saved item", func() bool { return len(s.feed.items) == 1 })
		e.frame()
		listY := e.size.Y - s.kb.viewH
		kbClickRow(t, e, s.kb, lfKey(), listY)
		if !slices.Equal(h.jumps, want("full")) || len(h.navs) != 0 {
			t.Errorf("without hq: jumps %q, navigated %q; want the jump in the page's place", h.jumps, h.navs)
		}
		h.jumps = nil
		w42SavedPage(app, true)
		e.frame()
		kbClickRow(t, e, s.kb, lfKey(), listY)
		if !slices.Equal(h.jumps, want("side")) || h.secCh != "" {
			t.Errorf("hq: jumps %q, column %q; want the jump beside the page", h.jumps, h.secCh)
		}
	})
}

// The menu's Open in Home is R9 toMessage (forceTab Home), which the host
// redirects for a file channel (OpenSavedInHome), and Open in split view
// opens the message itself in the split view (openInSplitView of R9), not
// the conversation (toChannel).
func TestLaterFileChannelMenuLikeSlack(t *testing.T) {
	h := &lfHost{w26Host: newW26Host(lfItems(t, "to_do"), false)}
	l := NewLater(h)
	e := &vaEnv{size: image.Pt(620, 900)}
	e.draw = func(gtx layout.Context) { l.Layout(gtx) }
	e.until(t, "the to-do", func() bool { return len(l.view) == 1 && l.view[0].Msg != nil })
	items := l.savedMenu(h, l.view[0])
	do := func(key string) bool {
		for _, mi := range items {
			if mi.Key == key && mi.Do != nil {
				mi.Do()
				return true
			}
		}
		return false
	}
	if !do("open_in_home") || !slices.Equal(h.homes, []string{lfKey()}) || len(h.navs) != 0 {
		t.Errorf("Open in Home: the redirect saw %q, navigated %q; want toMessage %s through the redirect", h.homes, h.navs, lfKey())
	}
	if !do(popups.SplitKey) || !slices.Equal(h.splits, []string{"c/" + fakeslack.CanvasMentionChannel + "/m/" + fakeslack.CanvasMentionTS}) {
		t.Errorf("Open in split view opened %q, want the message c/%s/m/%s", h.splits, fakeslack.CanvasMentionChannel, fakeslack.CanvasMentionTS)
	}
}
