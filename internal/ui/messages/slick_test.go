package messages

import (
	"bytes"
	"context"
	"encoding/json"
	"image/color"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/slick"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/taut"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/richview"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Slick plugins through the native message views (the ui-slick workflow
// builds them with Slick's own build into SLICK_PLUGINS_DIR). Each test
// installs one real, unmodified plugin over the synthetic workspace of
// taut_test.go (made-up people and messages, never real Slack content),
// draws the real ChannelView / ThreadView and checks what a user would
// see: rows hidden or kept, the text drawn, the plugin's markup in the
// trees the views render, native fields the plugin's props changed.

type slickEnv struct {
	app *core.App
	api *fakeSlack
	tt  *taut.Taut
	s   *slick.Slick
	ui  *pluginUI
	h   coreHost

	fetchMu sync.Mutex
	fetches map[string]string // URL prefix → JSON body; other fetches 404
	fetched []string
}

func slickDir(t *testing.T) string {
	dir := os.Getenv("SLICK_PLUGINS_DIR")
	if dir == "" {
		t.Skip("SLICK_PLUGINS_DIR not set (the ui-slick workflow builds Slick's plugins)")
	}
	return dir
}

// newSlickEnv is pluginEnv with Slick on top and fetches answered from
// e.fetches (no network). seed adds a test's synthetic data first.
func newSlickEnv(t *testing.T, seed func(s *store.Store)) *slickEnv {
	slickDir(t)
	app, api := newTestApp(t)
	if seed != nil {
		seed(app.S)
	}
	e := &slickEnv{app: app, api: api, ui: &pluginUI{trees: map[uint64][]byte{}}, fetches: map[string]string{}}
	dir := t.TempDir()
	tt, err := taut.New(taut.Options{App: app, Dir: filepath.Join(dir, "taut"), CredsPath: filepath.Join(dir, "creds.json"), UI: e.ui.hooks(),
		NoAutoStart: true, CacheDir: filepath.Join(dir, "cache"), Fetch: e.fetch, Log: func(level, msg string) {
			if level == "error" || level == "warn" {
				t.Logf("js %s: %s", level, msg)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := slick.New(slick.Options{Taut: tt, Dir: filepath.Join(dir, "slick"), NoAutoStart: true, NoUpdates: true})
	if err != nil {
		t.Fatal(err)
	}
	e.tt, e.s = tt, s
	slackc.SetBridge(tt.Bridge())
	SetTaut(tt)
	t.Cleanup(func() {
		SetTaut(nil)
		slackc.SetBridge(nil)
		slackc.SetPatched(nil)
		_ = s.Close()
		_ = tt.Close()
	})
	e.h = coreHost{app: app, th: theme.New(false)}
	return e
}

func (e *slickEnv) fetch(ctx context.Context, req *http.Request) (*http.Response, error) {
	u := req.URL.String()
	if strings.HasPrefix(u, e.api.srv.URL) {
		return http.DefaultClient.Do(req.WithContext(ctx))
	}
	e.fetchMu.Lock()
	defer e.fetchMu.Unlock()
	e.fetched = append(e.fetched, u)
	for prefix, body := range e.fetches {
		if strings.HasPrefix(u, prefix) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}
	}
	return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
}

// install imports the built plugin (and its main half) the way a user
// does, applies settings and enables it.
func (e *slickEnv) install(t *testing.T, id string, settings map[string]any) {
	t.Helper()
	dir := slickDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, f := range []string{id + ".js", id + ".main.js"} {
		code, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			if f == id+".js" {
				t.Fatal(err)
			}
			continue
		}
		if _, err := e.s.Import(ctx, code); err != nil {
			t.Fatalf("import %s: %v", f, err)
		}
	}
	for k, v := range settings {
		if err := e.s.SetOption(ctx, id, k, v); err != nil {
			t.Fatalf("set %s.%s: %v", id, k, err)
		}
	}
	if err := e.s.SetEnabled(ctx, id, true); err != nil {
		t.Fatalf("enable %s: %v", id, err)
	}
	for _, p := range e.s.Settings().Plugins {
		if p.ID == id && (!p.Running || p.StartError != "") {
			t.Fatalf("%s didn't start: running=%v error=%q", id, p.Running, p.StartError)
		}
	}
	time.Sleep(150 * time.Millisecond)
}

// patched waits until a plugin's patches reach slackc.
func patched(t *testing.T, names ...string) {
	t.Helper()
	until(t, 5*time.Second, "patched "+strings.Join(names, ", "), func() {}, func() bool {
		for _, n := range names {
			if !slackc.Patched(n) {
				return false
			}
		}
		return true
	})
}

// dumpTrees logs the trees (on failure).
func (u *pluginUI) dump(t *testing.T) {
	u.mu.Lock()
	defer u.mu.Unlock()
	for root, tr := range u.trees {
		s := string(tr)
		if len(s) > 3000 {
			s = s[:3000] + "…"
		}
		t.Logf("tree %d: %s", root, s)
	}
}

// waitFor runs frames until cond holds, dumping the trees on timeout.
func (e *slickEnv) waitFor(t *testing.T, what string, frame func(), cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		frame()
		if cond() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	e.ui.dump(t)
	t.Fatalf("timed out waiting for %s", what)
}

// rowText is the text a row's body draws.
func rowText(r *row) string {
	if r == nil || r.rv == nil {
		return ""
	}
	return r.rv.TextRange(0, r.rv.PlainLen())
}

// itemHeight is the height an item of the list was laid out at (-1: none).
func itemHeight(s *listState, key string) int {
	for i, it := range s.items {
		if it.key == key && i < len(s.heights) {
			return s.heights[i]
		}
	}
	return -1
}

func richBlocks(elements ...map[string]any) json.RawMessage {
	els := make([]any, len(elements))
	for i, e := range elements {
		els[i] = e
	}
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "block_id": "b1", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": els},
	}}})
	return b
}

func textEl(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

const tsVideo = "1700000201.000100"

// DeArrow: a YouTube unfurl (MessageAttachmentYouTube) shows DeArrow's
// crowdsourced title and thumbnail, with its toggle beside the title
// (AttachmentTitle, inside the YouTube card's tree).
func TestSlickViewDeArrow(t *testing.T) {
	const ch, vid = "C0VIDEOS", "AbCdEfGhIjK"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: ch, Name: "videos", IsChannel: true, IsMember: true, NumMembers: 3})
		s.SetLatest(ch, []slack.Message{{Type: "message", TS: tsVideo, User: "UALICE", Text: "look", Attachments: []slack.Attachment{{
			ID: 1, ServiceName: "YouTube", ServiceIcon: "https://avatars.invalid/yt.png", Title: "You WON'T BELIEVE what this synthetic cat does",
			TitleLink: "https://www.youtube.com/watch?v=" + vid, FromURL: "https://www.youtube.com/watch?v=" + vid,
			ThumbURL: "https://thumbs.invalid/" + vid + ".jpg", ThumbWidth: 480, ThumbHeight: 360,
		}}}}, false)
	})
	e.fetches["https://sponsor.ajay.app/api/branding?videoID="+vid] =
		`{"titles":[{"title":"A calm synthetic cat sits still","original":false,"votes":3}],"thumbnails":[{"timestamp":42.5,"original":false,"votes":2}]}`
	e.install(t, "DeArrow", nil)
	patched(t, "MessageAttachmentYouTube", "AttachmentTitle")
	v := NewChannelView(e.h, ch)
	v.Preview = true
	e.waitFor(t, "DeArrow's title drawn in the card", frameFn(v), func() bool {
		txt := rowText(v.s.rows[tsVideo])
		return strings.Contains(txt, "A calm synthetic cat sits still") && !strings.Contains(txt, "BELIEVE")
	})
	e.waitFor(t, "DeArrow's thumbnail and its toggle beside the title", frameFn(v), func() bool {
		return e.ui.anyTree("dearrow-thumb.ajay.app/api/v1/getThumbnail?videoID="+vid) && e.ui.anyTree("slick-dearrow__toggle") &&
			e.ui.anyTree(`"n":"AttachmentTitle"`)
	})
}

// ShowSendingBot: a message a bot posted for a person gets the bot's
// badge on the avatar (MessageAvatar → BaseMessageAvatar provide the
// message, ConnectedBaseAvatar in their tree draws the badge).
func TestSlickViewShowSendingBot(t *testing.T) {
	const ch, ts = "C0BOTPOST", "1700000301.000100"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: ch, Name: "bot-posts", IsChannel: true, IsMember: true, NumMembers: 3})
		s.PutBot("B0SENDER1", store.BotInfo{Name: "Sender Bot", Icon: "https://avatars.invalid/sender-bot-48.png"})
		s.SetLatest(ch, []slack.Message{
			{Type: "message", TS: ts, User: "UALICE", BotID: "B0SENDER1", Text: "posted for alice by a bot"},
			// a minute later, same sender: a grouped row (compact gutter, no avatar)
			{Type: "message", TS: "1700000361.000100", User: "UALICE", BotID: "B0SENDER1", Text: "and a grouped follow-up"},
		}, false)
	})
	e.install(t, "ShowSendingBot", nil)
	patched(t, "MessageKitGutter", "MessageAvatar", "BaseMessageAvatar", "ConnectedBaseAvatar", "ProgressiveDisclosureSuccessCoachmarkWrapper")
	v := NewChannelView(e.h, ch)
	v.Preview = true
	f := frameFn(v)
	e.waitFor(t, "the sending bot's badge on Alice's avatar", f, func() bool {
		return e.ui.anyTree("slick-sending-bot__badge") && e.ui.anyTree("B0SENDER1") && e.ui.anyTree(`"n":"BaseMessageAvatar"`)
	})
	// The grouped row's compact gutter gets the badge from the sender
	// context ProgressiveDisclosureSuccessCoachmarkWrapper provides above
	// it: MessageKitGutter is a nested root, a portal under that wrapper.
	e.waitFor(t, "the gutter badge on the grouped row", f, func() bool {
		return e.ui.anyTree("slick-sending-bot__badge--gutter")
	})
}

// BetterBlock: a blocked person's messages collapse (Message /
// MessageWrapper / ThreadReplyGeneric render nothing), their avatar
// leaves the reply bar (ThreadReplies' msg, ReplyBarAvatar), and mentions
// of them leave others' text (MessageText / Blocks msg overrides). Slick
// keeps only Slack-shaped ids in a names setting, hence UCAROL001.
func TestSlickViewBetterBlock(t *testing.T) {
	const (
		ch, carol                        = "C0BLOCKS", "UCAROL001"
		tsRoot, tsCarol, tsMention, tsOK = "1700000401.000100", "1700000402.000100", "1700000403.000100", "1700000404.000100"
		tsReplyAlice, tsReplyCarol       = "1700000405.000100", "1700000406.000100"
	)
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutUsers([]slack.User{{ID: carol, Name: "carol", RealName: "Carol Example", Profile: slack.Profile{DisplayName: "carol",
			Image24: "https://avatars.invalid/carol-48.png", Image48: "https://avatars.invalid/carol-48.png"}}})
		s.PutConv(slack.Conversation{ID: ch, Name: "blocks", IsChannel: true, IsMember: true, NumMembers: 4})
		s.SetLatest(ch, []slack.Message{
			{Type: "message", TS: tsRoot, User: "UALICE", Text: "the synthetic plan", ThreadTS: tsRoot, ReplyCount: 2,
				ReplyUsers: []string{"UALICE", carol}, LatestReply: tsReplyCarol},
			{Type: "message", TS: tsCarol, User: carol, Text: "carol's synthetic message"},
			{Type: "message", TS: tsMention, User: "UALICE", Text: "ask <@" + carol + "> about it",
				Blocks: richBlocks(textEl("ask "), map[string]any{"type": "user", "user_id": carol}, textEl(" about it"))},
			{Type: "message", TS: tsOK, User: tSelf, Text: "sounds good"},
		}, false)
		s.SetThread(ch, tsRoot, []slack.Message{
			{Type: "message", TS: tsRoot, User: "UALICE", Text: "the synthetic plan", ThreadTS: tsRoot, ReplyCount: 2},
			{Type: "message", TS: tsReplyAlice, User: "UALICE", Text: "first synthetic reply", ThreadTS: tsRoot},
			{Type: "message", TS: tsReplyCarol, User: carol, Text: "carol's synthetic reply", ThreadTS: tsRoot},
		})
	})
	e.install(t, "BetterBlock", map[string]any{"blockedUsers": map[string]any{carol: "carol"}})
	patched(t, "MessageWrapper", "Message", "ThreadReplyGeneric", "MessageText", "ThreadReplies", "ReplyBarAvatar")
	v := NewChannelView(e.h, ch)
	v.Preview = true
	f := frameFn(v)
	e.waitFor(t, "Carol's message collapsed, the others drawn", f, func() bool {
		return itemHeight(v.s, tsCarol) == 0 && itemHeight(v.s, tsRoot) > 0 && itemHeight(v.s, tsOK) > 0
	})
	e.waitFor(t, "the mention of Carol gone from Alice's text", f, func() bool {
		txt := rowText(v.s.rows[tsMention])
		return strings.Contains(txt, "ask") && strings.Contains(txt, "about it") && !strings.Contains(txt, "carol")
	})
	// the reply bar under Alice's thread: Alice's avatar, not Carol's
	e.waitFor(t, "Carol's avatar out of the reply bar", f, func() bool {
		r := v.s.rows[tsRoot]
		if r == nil {
			return false
		}
		alice := false
		for i := 0; i < r.replyN && i < len(r.replyAvs); i++ {
			av := &r.replyAvs[i]
			if av.id == carol && av.w > 0 {
				return false
			}
			alice = alice || av.id == "UALICE" && av.w > 0
		}
		return alice
	})
	// the thread pane: Carol's reply collapses, Alice's stay
	tv := NewThreadView(e.h, ch, tsRoot)
	ft := frameFn(tv)
	e.waitFor(t, "Carol's reply collapsed in the thread", ft, func() bool {
		return itemHeight(tv.s, tsReplyCarol) == 0 && itemHeight(tv.s, tsReplyAlice) > 0 && itemHeight(tv.s, tsRoot) > 0
	})
}

// MessageLogger: a deleted message stays in the channel, drawn under the
// plugin's .slick-ml-deleted row (MessageWrapper), and an edited one is
// marked .slick-ml-edited.
func TestSlickViewMessageLogger(t *testing.T) {
	const ch, tsGone, tsEdit, tsRich = "C0LOGGED", "1700000501.000100", "1700000502.000200", "1700009503.000300"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: ch, Name: "logged", IsChannel: true, IsMember: true, NumMembers: 3})
		s.SetLatest(ch, []slack.Message{
			{Type: "message", TS: tsGone, User: "UALICE", Text: "this synthetic note will vanish"},
			{Type: "message", TS: tsEdit, User: "UBOB", Text: "first synthetic draft"},
			{Type: "message", TS: tsRich, User: "UALICE", Text: "plainword boldword …", Blocks: cssRowBlocks(),
				Reactions: []slack.Reaction{{Name: "tada", Count: 3, Users: []string{"UBOB", tSelf, "UALICE"}}}},
		}, false)
	})
	e.install(t, "MessageLogger", nil)
	patched(t, "MessageWrapper", "MessageText", "Blocks")
	v := NewChannelView(e.h, ch)
	v.Preview = true
	f := frameFn(v)
	e.waitFor(t, "both rows drawn through the plugin", f, func() bool {
		return v.s.rows[tsGone] != nil && v.s.rows[tsEdit] != nil && e.ui.anyTree(tsGone) && e.ui.anyTree(tsEdit)
	})
	inject := func(ev map[string]any) {
		raw, _ := json.Marshal(ev)
		e.app.Inject(slack.Event{Type: "message", Subtype: ev["subtype"].(string), Channel: json.RawMessage(`"` + ch + `"`), Raw: raw})
	}
	inject(map[string]any{"type": "message", "subtype": "message_deleted", "channel": ch, "hidden": true, "ts": "1700000510.000100", "deleted_ts": tsGone,
		"previous_message": map[string]any{"type": "message", "user": "UALICE", "text": "this synthetic note will vanish", "ts": tsGone}})
	e.waitFor(t, "the deleted message kept and marked deleted", f, func() bool {
		r := v.s.rows[tsGone]
		return r != nil && itemHeight(v.s, tsGone) > 0 && strings.Contains(rowText(r), "will vanish") && e.ui.anyTree("slick-ml-deleted")
	})
	inject(map[string]any{"type": "message", "subtype": "message_changed", "channel": ch, "hidden": true, "ts": "1700000511.000100",
		"message":          map[string]any{"type": "message", "user": "UBOB", "text": "second synthetic draft", "ts": tsEdit, "edited": map[string]any{"user": "UBOB", "ts": "1700000511.000000"}},
		"previous_message": map[string]any{"type": "message", "user": "UBOB", "text": "first synthetic draft", "ts": tsEdit}})
	// MessageLogger's red deleted style reaches every native part of the
	// row, as ".slick-ml-deleted *" does in Slack's DOM.
	inject(map[string]any{"type": "message", "subtype": "message_deleted", "channel": ch, "hidden": true, "ts": "1700000512.000100", "deleted_ts": tsRich,
		"previous_message": map[string]any{"type": "message", "user": "UALICE", "text": "plainword boldword …", "ts": tsRich, "blocks": json.RawMessage(cssRowBlocks())}})
	e.waitFor(t, "the rich deleted message kept", f, func() bool {
		r := v.s.rows[tsRich]
		return r != nil && itemHeight(v.s, tsRich) > 0 && strings.Contains(rowText(r), "codeword") && e.ui.anyTree(tsRich)
	})
	red := color.NRGBA{R: 0xe0, G: 0x1e, B: 0x5a, A: 0xff}
	var labels map[string]color.NRGBA
	e.waitFor(t, "every part of the deleted row red", func() {
		labels = map[string]color.NRGBA{}
		labelMu.Lock()
		labelHook = func(txt string, s textStyle) { labels[txt] = s.col }
		f()
		labelHook = nil
		labelMu.Unlock()
	}, func() bool {
		r := v.s.rows[tsRich]
		if r == nil || r.rv == nil {
			return false
		}
		ok := labels[clock(tsTime(tsRich))] == red && labels["alice"] == red
		n := 0
		r.rv.Runs(func(r richview.Run) {
			n++
			if r.Color != red {
				ok = false
			}
		})
		return ok && n > 0
	})
	if r := v.s.rows[tsRich]; r != nil && r.rv != nil {
		r.rv.Runs(func(r richview.Run) {
			if r.Color != red {
				t.Errorf("deleted row: %q painted %v", r.Text, r.Color)
			}
		})
	}
	if c := labels["bob"]; c == red {
		t.Error("the sender of a row that isn't deleted is red")
	}
	// the row that wasn't deleted keeps its colors
	if r := v.s.rows[tsEdit]; r != nil && r.rv != nil {
		r.rv.Runs(func(r richview.Run) {
			if r.Color == red {
				t.Errorf("a row that isn't deleted: %q red", r.Text)
			}
		})
	}

	e.waitFor(t, "the edited message drawn with its new text, marked edited", f, func() bool {
		return strings.Contains(rowText(v.s.rows[tsEdit]), "second synthetic draft") && e.ui.anyTree("slick-ml-edited")
	})
	// The edit history (old text, struck through) is drawn by the plugin's
	// Blocks patch from a context its row patch provides: Blocks is its own
	// slackc root, mounted as a portal under the row (NestedBridge), so the
	// context reaches it as in Slack's single tree.
	e.waitFor(t, "the edit history (original text) under the edited message", f, func() bool {
		return e.ui.anyTree("slick-ml-edited-original") && e.ui.anyTree("first synthetic draft")
	})
}

// MessageLogger's opacity deleted style: the deleted row's natives are
// drawn under its wrapper's opacity 0.5, the others at 1.
func TestSlickViewMessageLoggerOpacity(t *testing.T) {
	const ch, tsGone, tsKept = "C0LOGGED2", "1700000521.000100", "1700000522.000200"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: ch, Name: "logged-two", IsChannel: true, IsMember: true, NumMembers: 3})
		s.SetLatest(ch, []slack.Message{
			{Type: "message", TS: tsGone, User: "UALICE", Text: "a synthetic note that fades"},
			{Type: "message", TS: tsKept, User: "UBOB", Text: "a synthetic note that stays"},
		}, false)
	})
	e.install(t, "MessageLogger", map[string]any{"deletedStyle": "opacity"})
	patched(t, "MessageWrapper")
	v := NewChannelView(e.h, ch)
	v.Preview = true
	f := frameFn(v)
	e.waitFor(t, "rows drawn through the plugin", f, func() bool { return v.s.rows[tsGone] != nil && e.ui.anyTree(tsGone) })
	raw, _ := json.Marshal(map[string]any{"type": "message", "subtype": "message_deleted", "channel": ch, "hidden": true, "ts": "1700000530.000100", "deleted_ts": tsGone,
		"previous_message": map[string]any{"type": "message", "user": "UALICE", "text": "a synthetic note that fades", "ts": tsGone}})
	e.app.Inject(slack.Event{Type: "message", Subtype: "message_deleted", Channel: json.RawMessage(`"` + ch + `"`), Raw: raw})
	var op map[string]float32
	e.waitFor(t, "the deleted row drawn at opacity 0.5", func() {
		op = map[string]float32{}
		labelMu.Lock()
		labelHook = func(txt string, _ textStyle) { op[txt] = slackc.EffectiveOpacity() }
		f()
		labelHook = nil
		labelMu.Unlock()
	}, func() bool {
		return e.ui.anyTree("slick-ml-deleted") && op["alice"] == 0.5
	})
	if o, ok := op["bob"]; !ok || o != 1 {
		t.Errorf("the row that wasn't deleted is drawn at opacity %v (drawn %v)", o, ok)
	}
}

// QuickJoin: a channel you can join, mentioned in a message, gets a Join
// button under it (CodeChannelEntityCard, empty natively).
func TestSlickViewQuickJoin(t *testing.T) {
	const ch, open, ts = "C0LOBBY", "C0JOINME", "1700000601.000100"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: ch, Name: "lobby", IsChannel: true, IsMember: true, NumMembers: 3})
		s.PutConv(slack.Conversation{ID: open, Name: "join-me", IsChannel: true, IsMember: false, NumMembers: 12})
		s.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: "UALICE", Text: "come to <#" + open + ">",
			Blocks: richBlocks(textEl("come to "), map[string]any{"type": "channel", "channel_id": open})}}, false)
	})
	e.install(t, "QuickJoin", nil)
	patched(t, "CodeChannelEntityCard")
	v := NewChannelView(e.h, ch)
	v.Preview = true
	e.waitFor(t, "a Join Channel button under the message", frameFn(v), func() bool {
		return e.ui.anyTree("Join Channel") && e.ui.anyTree("slick-qj-card") && e.ui.anyTree(open)
	})
}

// MentionProfiles: @mention pills (MemberSlug, inside MemberLink) get the
// person's picture, and with showAtSymbol off the "@" goes
// (supressMentionAtSymbol).
func TestSlickViewMentionProfiles(t *testing.T) {
	const ch, ts = "C0MENTION", "1700000701.000100"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: ch, Name: "mentions", IsChannel: true, IsMember: true, NumMembers: 3})
		s.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: "UBOB", Text: "thanks <@UALICE>",
			Blocks: richBlocks(textEl("thanks "), map[string]any{"type": "user", "user_id": "UALICE"})}}, false)
	})
	e.install(t, "MentionProfiles", map[string]any{"showAtSymbol": false})
	patched(t, "MemberSlug")
	v := NewChannelView(e.h, ch)
	v.Preview = true
	e.waitFor(t, "Alice's picture in her mention pill, without the @", frameFn(v), func() bool {
		txt := rowText(v.s.rows[ts])
		return e.ui.anyTree("slick-mention-profile") && e.ui.anyTree("alice-48.png") &&
			strings.Contains(txt, "alice") && !strings.Contains(txt, "@alice")
	})
}

// CustomSlackbot: Slackbot's custom responses (subtype slackbot_response)
// get the plugin's avatar and name. On the message row its CSS hits
// .c-message_kit__background--labels--custom_response: the avatar is the
// rule's background-image, and the sender name is font-size 0 with the
// new name in ::after. In the reply bar, ReplyBarAvatar asks Slack's
// hasSlackbotCustomResponseInThread(state, channelId, threadTs, userId) and
// draws its own avatar. Plain Slackbot messages and threads keep Slack's.
func TestSlickViewCustomSlackbot(t *testing.T) {
	const ch, helper = "C0SBTHRD", "https://avatars.invalid/helper.png"
	const tsPlain, tsA, replyA, tsB, replyB, tsCustom = "1700000800.000100", "1700000801.000100", "1700000801.000200",
		"1700000802.000100", "1700000802.000200", "1700000803.000100"
	e := newSlickEnv(t, func(s *store.Store) {
		s.PutConv(slack.Conversation{ID: ch, Name: "faq", IsChannel: true, IsMember: true, NumMembers: 3})
		s.SetLatest(ch, []slack.Message{
			{Type: "message", TS: tsPlain, User: tBot, Text: "a plain synthetic Slackbot note"},
			{Type: "message", TS: tsA, User: "UALICE", Text: "where is the synthetic wiki?", ThreadTS: tsA,
				ReplyCount: 1, ReplyUsers: []string{tBot}, LatestReply: replyA},
			{Type: "message", TS: tsB, User: "UALICE", Text: "and the synthetic lunch menu?", ThreadTS: tsB,
				ReplyCount: 1, ReplyUsers: []string{tBot}, LatestReply: replyB},
			{Type: "message", TS: tsCustom, User: tBot, Subtype: core.SubtypeSlackbotResponse, Text: "a synthetic custom response in the channel"},
		}, false)
		s.SetThread(ch, tsA, []slack.Message{
			{Type: "message", TS: tsA, User: "UALICE", Text: "where is the synthetic wiki?", ThreadTS: tsA, ReplyCount: 1},
			{Type: "message", TS: replyA, User: tBot, Subtype: core.SubtypeSlackbotResponse, Text: "a synthetic custom response", ThreadTS: tsA},
		})
		s.SetThread(ch, tsB, []slack.Message{
			{Type: "message", TS: tsB, User: "UALICE", Text: "and the synthetic lunch menu?", ThreadTS: tsB, ReplyCount: 1},
			{Type: "message", TS: replyB, User: tBot, Text: "a plain synthetic Slackbot reply", ThreadTS: tsB},
		})
	})
	e.install(t, "CustomSlackbot", map[string]any{"name": "Helper", "url": helper})
	patched(t, "ReplyBarAvatar")
	v := NewChannelView(e.h, ch)
	v.Preview = true
	slackbotAv := func(ts string) bool {
		r := v.s.rows[ts]
		drawn := false
		for i := 0; r != nil && i < r.replyN && i < len(r.replyAvs); i++ {
			drawn = drawn || r.replyAvs[i].id == tBot && r.replyAvs[i].w > 0
		}
		return drawn
	}
	e.waitFor(t, "the custom response's avatar and name, and the plugin's reply-bar avatar", frameFn(v), func() bool {
		c := v.s.rows[tsCustom]
		return c != nil && c.avDrawn == helper && c.nameShown == "Helper" &&
			slackbotAv(tsA) && e.ui.anyTree("slick-custom-slackbot-reply-avatar") &&
			// the plain thread's reply bar renders Slack's ReplyBarAvatar with Slack's props
			slackbotAv(tsB) && e.ui.anyTree(`"n":"ReplyBarAvatar"`) && e.ui.anyTree(`"threadTs":"`+tsB+`"`)
	})
	if !v.s.rows[tsCustom].m.SlackbotCustomResponse() {
		t.Fatal("the slackbot_response message isn't a custom response in core")
	}
	// the plain Slackbot message keeps Slack's avatar and name
	p := v.s.rows[tsPlain]
	if p == nil || p.avDrawn == helper || p.nameShown == "" || strings.Contains(p.nameShown, "Helper") {
		var av, name string
		if p != nil {
			av, name = p.avDrawn, p.nameShown
		}
		t.Fatalf("plain Slackbot message: avatar %q name %q, want Slack's own", av, name)
	}
	// only the custom-response thread's ReplyBarAvatar was replaced
	if e.ui.anyTree(`"threadTs":"` + tsA + `"`) {
		e.ui.dump(t)
		t.Fatal("the custom-response thread still rendered Slack's ReplyBarAvatar")
	}
}

// WhoReacted: each reaction pill lists its reactors' avatars (Reaction
// provides them, UnstyledButton data-qa=reactji in its tree draws them).
func TestSlickViewWhoReacted(t *testing.T) {
	e := newSlickEnv(t, nil)
	e.install(t, "WhoReacted", nil)
	patched(t, "Reaction", "UnstyledButton")
	v := NewChannelView(e.h, tGen)
	v.Preview = true
	e.waitFor(t, "the reactors' avatars in the tada pill", frameFn(v), func() bool {
		return e.ui.anyTree("slick-wr__av") && e.ui.anyTree("alice-48.png")
	})
}
