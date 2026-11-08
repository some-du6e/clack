package taut

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/jsrt"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

// Regression tests for bugs found running Taut's plugins live in Clack
// (docs/PLUGINS-live.md).

// Taut calls send checks as check(blocks, {channelId, threadTs}); HaikuWarning
// and OwlMode destructure the second argument, so a single context argument
// threw and the message went out unchecked.
func TestSendCheckArgs(t *testing.T) {
	h := newHarness(t)
	h.js(`
		globalThis.__seen = []
		api.composer.addSendCheck(async (blocks, { channelId, threadTs }) => {
			__seen.push({ n: blocks.length, type: blocks[0]?.type ?? '', ch: channelId, ts: threadTs ?? '' })
			return blocks.length !== 2
		})
		return 1`)
	eventually(t, 2*time.Second, "send check registered", h.tt.HasSendChecks)
	one := json.RawMessage(`[{"type":"rich_text","elements":[]}]`)
	two := json.RawMessage(`[{"type":"rich_text","elements":[]},{"type":"rich_text","elements":[]}]`)
	if !h.tt.SendCheck(h.ctx(), ComposerContext{Kind: "thread", ChannelID: chGen, ThreadTs: "1700000001.000100", Blocks: one}) {
		t.Error("a passing check held the message back")
	}
	if h.tt.SendCheck(h.ctx(), ComposerContext{Kind: "channel", ChannelID: chGen, Blocks: two}) {
		t.Error("a check returning false didn't hold the message back")
	}
	var seen []struct {
		N        int
		Type, Ch string
		Ts       string
	}
	h.jsInto(`return __seen`, &seen)
	if len(seen) != 2 || seen[0].N != 1 || seen[0].Type != "rich_text" || seen[0].Ch != chGen || seen[0].Ts != "1700000001.000100" ||
		seen[1].N != 2 || seen[1].Ts != "" {
		t.Errorf("checks saw %+v", seen)
	}
}

func TestErrWithStack(t *testing.T) {
	err := &jsrt.JSError{Name: "TypeError", Message: "not a function", Stack: "    at f (a.js:1)\n    at g (b.js:2)\n"}
	if got := errWithStack(err); got != "TypeError: not a function [at f (a.js:1) | at g (b.js:2)]" {
		t.Errorf("errWithStack = %q", got)
	}
	if got := errWithStack(errors.New("plain")); got != "plain" {
		t.Errorf("errWithStack(plain) = %q", got)
	}
	if got := logArg([]any{"MessageSender", []byte("{}")}); got != "MessageSender" {
		t.Errorf("logArg = %q", got)
	}
}

// The plugin Library compares the installed file's sha256 with its index.
func TestInstalledSHA256(t *testing.T) {
	h := newHarness(t)
	code := pluginJS(`class LibHash extends TautPlugin { static id = 'LibHash'; static pluginName = 'Lib Hash'; static description = 'd'; static authors = ['jeremy']; static defaultConfig = { enabled: false }; start() {} stop() {} }; return LibHash`)
	if h.tt.InstalledSHA256("LibHash") != "" {
		t.Fatal("hash before install")
	}
	h.install(code, false)
	sum := sha256.Sum256(code)
	if got := h.tt.InstalledSHA256("LibHash"); got != hex.EncodeToString(sum[:]) {
		t.Errorf("InstalledSHA256 = %q", got)
	}
	for _, bad := range []string{"", "../config", "a/b", ".hidden"} {
		if h.tt.InstalledSHA256(bad) != "" {
			t.Errorf("InstalledSHA256(%q) read a file", bad)
		}
	}
}

// A patch that renders <Original> with new props (BetterTypingIndicator's
// <Original firstTyper={…}/>) keeps the Go slot it was mounted for, so Clack
// still draws the native ("no native component for TypingNames" live). A
// fresh instance of the same component inside it doesn't inherit the slot.
func TestOriginalKeepsSlot(t *testing.T) {
	h := newHarness(t)
	h.js(`
		const R = globalThis.React
		api.patchComponent('TypingNames', (Original) => () => R.createElement('span', null,
			R.createElement(Original, { firstTyper: 'Ann', severalPeopleAreTyping: false }),
			R.createElement(T.slackComponent('TypingNames'), { __original: true, firstTyper: 'Bob' })))
		return 1`)
	nodes := h.render(4242, "TypingNames", map[string]any{"$slot": 7, "firstTyper": "x"})
	var found []map[string]any
	var walk func([]any)
	walk = func(ns []any) {
		for _, x := range ns {
			m, ok := x.(map[string]any)
			if !ok {
				continue
			}
			if m["t"] == "@" && m["n"] == "TypingNames" {
				found = append(found, m)
			}
			if kids, ok := m["c"].([]any); ok {
				walk(kids)
			}
		}
	}
	walk(nodes)
	if len(found) != 2 {
		t.Fatalf("TypingNames nodes: %v", nodes)
	}
	p0, _ := found[0]["p"].(map[string]any)
	p1, _ := found[1]["p"].(map[string]any)
	if p0["$slot"] != float64(7) || p0["firstTyper"] != "Ann" {
		t.Errorf("Original with new props: %v", p0)
	}
	if _, ok := p1["$slot"]; ok || p1["firstTyper"] != "Bob" {
		t.Errorf("a fresh instance inherited the slot: %v", p1)
	}
}

// Slack's store keeps a slice's keys on its prototype, at both levels of
// messages, and LastSeen scans them there; Clack's proxy reported
// Object.prototype, so LastSeen never saw a cached message.
func TestSliceKeysOnPrototype(t *testing.T) {
	h := newHarness(t)
	var got struct {
		Channels []string
		TS       []string
		User     string
		Members  bool
	}
	h.jsInto(`
		const st = T.redux.getStore().getState()
		const keys = (v) => Object.keys(Object.getPrototypeOf(v))
		const chs = keys(st.messages)
		const b = st.messages['`+chGen+`']
		return { Channels: chs, TS: keys(b), User: b['1700000001.000100']?.user ?? '', Members: keys(st.members).includes('UALICE') }`, &got)
	found := false
	for _, c := range got.Channels {
		found = found || c == chGen
	}
	tsFound := false
	for _, ts := range got.TS {
		tsFound = tsFound || ts == "1700000001.000100"
	}
	if !found || !tsFound || got.User == "" || !got.Members {
		t.Errorf("keys on the prototype: %+v", got)
	}
}

// showNotification's message is the stored message in Slack's shape.
func TestSlackMessage(t *testing.T) {
	h := newHarness(t)
	m := h.tt.SlackMessage(chGen, "1700000001.000100")
	if m == nil || m["user"] != "UALICE" || m["channel"] != chGen || m["text"] != "the job fair is today" {
		t.Errorf("SlackMessage = %v", m)
	}
	if h.tt.SlackMessage(chGen, "1.2") != nil {
		t.Error("a message that isn't cached")
	}
	var nilT *Taut
	if nilT.SlackMessage(chGen, "1700000001.000100") != nil {
		t.Error("nil Taut")
	}
}

// Edge profiles carry avatar_hash and no image_* URLs; the members mirror
// fills them in like Slack's client (WhoReacted drew no reactor avatars).
func TestMemberAvatarURLs(t *testing.T) {
	h := newHarness(t)
	h.app.S.PutUsers([]slack.User{{ID: "UAVATAR1", Name: "ava", TeamID: teamID, Profile: slack.Profile{AvatarHash: "g5d230ad5be8"}},
		{ID: "UAVATAR2", Name: "own", Profile: slack.Profile{AvatarHash: "abc", Image48: "https://example.com/own-48.png"}}})
	var got struct{ A24, A48, B48, B24 string }
	h.jsInto(`
		const m = T.redux.getStore().getState().members
		return { A24: m.UAVATAR1?.profile?.image_24 ?? '', A48: m.UAVATAR1?.profile?.image_48 ?? '',
			B48: m.UAVATAR2?.profile?.image_48 ?? '', B24: m.UAVATAR2?.profile?.image_24 ?? '' }`, &got)
	if got.A24 != "https://ca.slack-edge.com/"+teamID+"-UAVATAR1-g5d230ad5be8-24" || got.A48 == "" {
		t.Errorf("filled URLs: %+v", got)
	}
	if got.B48 != "https://example.com/own-48.png" || got.B24 == "" {
		t.Errorf("Slack's own URLs are kept, missing ones filled: %+v", got)
	}
}
