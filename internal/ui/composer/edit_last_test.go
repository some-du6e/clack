package composer

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// "/s/old/new" (edit_last.go). The parser and replace cases come from
// Slack's own vHCW run in node (edit_last_slack_cases_test.go); the
// composer tests below send the command through submit against the fake
// API.

// elOps reads a delta's ops as JSON the oracle printed: mentions become
// atom labels (slackmention), emoji embeds.
func elOps(t *testing.T, raw []map[string]any) []qOp {
	t.Helper()
	var out []qOp
	for i, o := range raw {
		at := qAttrs{}
		for k, v := range asMap(o["attributes"]) {
			switch x := v.(type) {
			case map[string]any:
				id, _ := x["id"].(string)
				label, _ := x["label"].(string)
				at[k] = qAtomRef{a: &Atom{Kind: AtomUser, ID: id, Text: label}, seq: i + 1}
			case float64:
				at[k] = int(x)
			default:
				at[k] = v
			}
		}
		if len(at) == 0 {
			at = nil
		}
		switch ins := o["insert"].(type) {
		case string:
			out = append(out, qOp{k: qIns, text: []rune(ins), attrs: at})
		case map[string]any:
			name, _ := asMap(ins["slackemoji"])["name"].(string)
			out = append(out, qOp{k: qEmb, emb: &qEmbed{atom: &Atom{Kind: AtomEmoji, ID: name}}, attrs: at})
		default:
			t.Fatalf("op %d: insert %T", i, o["insert"])
		}
	}
	return out
}

// elOpsJSON is elOps's inverse, for comparing with Slack's delta.
func elOpsJSON(ops []qOp) string {
	var out []map[string]any
	for _, op := range ops {
		o := map[string]any{}
		if op.k == qEmb {
			o["insert"] = map[string]any{"slackemoji": map[string]any{"name": op.emb.atom.ID}}
		} else {
			o["insert"] = string(op.text)
		}
		if len(op.attrs) > 0 {
			at := map[string]any{}
			for k, v := range op.attrs {
				if r, ok := v.(qAtomRef); ok {
					v = map[string]any{"id": r.a.ID, "label": r.a.Text}
				}
				at[k] = v
			}
			o["attributes"] = at
		}
		out = append(out, o)
	}
	b, _ := json.Marshal(out)
	return string(b)
}

func canonJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestEditLastMatchesSlack: every case gives what Slack's own code gave:
// whether s4/N calls it a command, parseEditLastMessageCommand's result,
// and editLastUserMessageText's outcome and saved delta.
func TestEditLastMatchesSlack(t *testing.T) {
	for _, tc := range editLastSlackCases {
		t.Run(tc.name, func(t *testing.T) {
			var in struct {
				ComposerOps []map[string]any `json:"composerOps"`
				MessageOps  []map[string]any `json:"messageOps"`
			}
			var want struct {
				Slash bool `json:"slash"`
				Parse *struct {
					Prev       string `json:"prev"`
					Next       string `json:"next"`
					ReplaceAll bool   `json:"replaceAll"`
					IgnoreCase bool   `json:"ignoreCase"`
				} `json:"parse"`
				Result string `json:"result"`
				Error  string `json:"error"`
				Ops    []any  `json:"ops"`
			}
			if err := json.Unmarshal([]byte(tc.in), &in); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			var q qDelta
			for _, op := range elOps(t, in.ComposerOps) {
				q.push(op)
			}
			if got := elIsCommand(q); got != want.Slash {
				t.Errorf("looksLikeSlashCommand = %v, Slack's s4/N %v", got, want.Slash)
			}
			cmd, ok := elParse(q)
			switch {
			case ok != (want.Parse != nil):
				t.Fatalf("parse ok %v (%+v), Slack %+v", ok, cmd, want.Parse)
			case ok && (cmd.Prev != want.Parse.Prev || cmd.Next != want.Parse.Next ||
				cmd.ReplaceAll != want.Parse.ReplaceAll || cmd.IgnoreCase != want.Parse.IgnoreCase):
				t.Fatalf("parse %+v, Slack %+v", cmd, *want.Parse)
			}
			if in.MessageOps == nil {
				return
			}
			result, errText, ops := elApply(elOps(t, in.MessageOps), cmd)
			got := elOpsJSON(ops)
			if result != want.Result || errText != want.Error {
				t.Fatalf("result %s %q, Slack %s %q", result, errText, want.Result, want.Error)
			}
			if result == "save" && got != canonJSON(want.Ops) {
				t.Fatalf("saved ops\n %s\nSlack\n %s", got, canonJSON(want.Ops))
			}
		})
	}
}

// TestEditLastNeedsBlocks: editLastUserMessageText rejects "Message not
// found" for a message without blocks; one with only non-rich-text blocks
// changes nothing (convertBlocksToDelta gives no ops).
func TestEditLastNeedsBlocks(t *testing.T) {
	cmd := elCmd{Prev: "of", Next: "off"}
	if _, notFound := elDoc(core.Message{TS: "1700000000.000100", Text: "a of"}, cmd); !notFound {
		t.Fatal("a message without blocks didn't reject \"Message not found\"")
	}
	if edited, notFound := elDoc(core.Message{TS: "1700000000.000100", Text: "a of",
		Blocks: json.RawMessage(`[{"type":"section","text":{"type":"mrkdwn","text":"a of"}}]`)}, cmd); edited || notFound {
		t.Fatalf("section only: edited %v, not found %v; want no edit", edited, notFound)
	}
	if edited, _ := elDoc(core.Message{TS: "1700000000.000100", Text: "a of", Blocks: elBlocks(elText("a of"))}, cmd); !edited {
		t.Fatal("a rich_text message wasn't edited")
	}
}

// ---- through the composer ----

const (
	elOwnTS    = "1700000100.000100" // the viewer's last message in the channel
	elOlderTS  = "1700000050.000100" // an older one of theirs
	elAliceTS  = "1700000200.000100" // someone else's, newer
	elRootTS   = "1700000010.000100" // a thread's root (alice)
	elReplyTS  = "1700000020.000100" // the viewer's reply in it
	elReply2TS = "1700000030.000100" // alice's reply after it
)

func elBlocks(elems ...string) json.RawMessage {
	return json.RawMessage(`[{"type":"rich_text","block_id":"b1","elements":[{"type":"rich_text_section","elements":[` +
		strings.Join(elems, ",") + `]}]}]`)
}

func elText(s string) string {
	b, _ := json.Marshal(s)
	return `{"type":"text","text":` + string(b) + `}`
}

func elMsg(ts, user, text string, blocks json.RawMessage) slack.Message {
	return slack.Message{Type: "message", TS: ts, User: user, Text: text, Blocks: blocks}
}

// elSend types s in a composer for ch (a thread's when root is set),
// sends it and waits for the fake API to see chat.update, or wait to pass.
func elSend(t *testing.T, seed func(*store.Store), root, s string, wait time.Duration) (*Composer, *fakeSlack, []url.Values) {
	t.Helper()
	app, fs := syntheticApp(t, seed)
	stubTyping(t)
	h := &slickHost{app: app, th: theme.New(false)}
	c := New(h, tChGen, root)
	t.Cleanup(c.Close)
	d := NewDoc()
	d.Insert(0, s, Attr{})
	c.ed.SetDoc(d)
	c.submit()
	if !c.ed.Doc().Empty() {
		t.Fatalf("the composer kept %q after Enter", s)
	}
	deadline := time.Now().Add(wait)
	var ups []url.Values
	for time.Now().Before(deadline) {
		runRestores()
		if ups = fs.called("chat.update"); len(ups) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	runRestores()
	for _, m := range []string{"chat.command", "chat.postMessage"} {
		if n := len(fs.called(m)); n != 0 {
			t.Errorf("%d %s calls for %q", n, m, s)
		}
	}
	return c, fs, ups
}

// elUpdateText is the text of chat.update's rich_text section elements.
func elUpdateText(t *testing.T, u url.Values) []map[string]any {
	t.Helper()
	var bs []struct {
		Type     string `json:"type"`
		Elements []struct {
			Elements []map[string]any `json:"elements"`
		} `json:"elements"`
	}
	if err := json.Unmarshal([]byte(u.Get("blocks")), &bs); err != nil || len(bs) != 1 || bs[0].Type != "rich_text" || len(bs[0].Elements) != 1 {
		t.Fatalf("chat.update blocks %s (%v)", u.Get("blocks"), err)
	}
	return bs[0].Elements[0].Elements
}

// TestEditLastInChannel: Karim's /s/of/off in a channel edits the
// viewer's newest message there (not someone else's newer one, not an
// older one of theirs) with chat.update, and is neither run as a command
// nor sent.
func TestEditLastInChannel(t *testing.T) {
	_, _, ups := elSend(t, func(s *store.Store) {
		s.SetLatest(tChGen, []slack.Message{
			elMsg(elOlderTS, tSelf, "an older of", elBlocks(elText("an older of"))),
			elMsg(elOwnTS, tSelf, "turn it of please", elBlocks(elText("turn it of please"))),
			elMsg(elAliceTS, "UALICE", "of course", elBlocks(elText("of course"))),
		}, false)
	}, "", "/s/of/off", 5*time.Second)
	if len(ups) != 1 {
		t.Fatalf("%d chat.update calls, want 1", len(ups))
	}
	u := ups[0]
	if u.Get("channel") != tChGen || u.Get("ts") != elOwnTS || u.Get("text") != "turn it off please" ||
		u.Get("skip_dlp_user_warning") != "false" {
		t.Fatalf("chat.update channel %q ts %q text %q skip_dlp_user_warning %q", u.Get("channel"), u.Get("ts"), u.Get("text"),
			u.Get("skip_dlp_user_warning"))
	}
	if els := elUpdateText(t, u); len(els) != 1 || els[0]["text"] != "turn it off please" {
		t.Fatalf("chat.update elements %v", els)
	}
}

// TestEditLastInThread: in a thread's reply box /s edits the viewer's
// newest reply there, though their newest channel message is newer.
func TestEditLastInThread(t *testing.T) {
	_, _, ups := elSend(t, func(s *store.Store) {
		root := elMsg(elRootTS, "UALICE", "a question", elBlocks(elText("a question")))
		root.ThreadTS, root.ReplyCount = elRootTS, 2
		reply := elMsg(elReplyTS, tSelf, "is it of", elBlocks(elText("is it of")))
		reply.ThreadTS = elRootTS
		later := elMsg(elReply2TS, "UALICE", "it is of", elBlocks(elText("it is of")))
		later.ThreadTS = elRootTS
		s.SetLatest(tChGen, []slack.Message{root, elMsg(elOwnTS, tSelf, "channel of", elBlocks(elText("channel of")))}, false)
		s.SetThread(tChGen, elRootTS, []slack.Message{root, reply, later})
	}, elRootTS, "/s/of/off", 5*time.Second)
	if len(ups) != 1 {
		t.Fatalf("%d chat.update calls, want 1", len(ups))
	}
	if u := ups[0]; u.Get("ts") != elReplyTS || u.Get("text") != "is it off" {
		t.Fatalf("chat.update ts %q text %q, want the reply %s \"is it off\"", u.Get("ts"), u.Get("text"), elReplyTS)
	}
}

// TestEditLastKeepsFormatting: the replace runs inside each text op, so
// bold, links and mentions stay as they were; /g reaches every op.
func TestEditLastKeepsFormatting(t *testing.T) {
	blocks := elBlocks(`{"type":"text","text":"of","style":{"bold":true}}`, elText(" see "),
		`{"type":"link","url":"https://example.com/of","text":"of"}`, elText(" "), `{"type":"user","user_id":"UALICE"}`, elText(" of"))
	_, _, ups := elSend(t, func(s *store.Store) {
		s.SetLatest(tChGen, []slack.Message{elMsg(elOwnTS, tSelf, "*of* see <https://example.com/of|of> <@UALICE> of", blocks)}, false)
	}, "", "/s/of/off/g", 5*time.Second)
	if len(ups) != 1 {
		t.Fatalf("%d chat.update calls, want 1", len(ups))
	}
	els := elUpdateText(t, ups[0])
	var bold, link, user bool
	for _, e := range els {
		st := asMap(e["style"])
		switch e["type"] {
		case "text":
			bold = bold || e["text"] == "off" && st["bold"] == true
		case "link":
			link = link || e["text"] == "off" && e["url"] == "https://example.com/of"
		case "user":
			user = user || e["user_id"] == "UALICE"
		}
	}
	if !bold || !link || !user {
		t.Errorf("bold off %v, link off %v, mention %v:\n%s", bold, link, user, canonJSON(els))
	}
	if last := els[len(els)-1]; last["type"] != "text" || last["text"] != " off" {
		t.Errorf("the last element is %v, want the text \" off\"", last)
	}
	if got, want := ups[0].Get("text"), "*off* see <https://example.com/of|off> <@UALICE> off"; got != want {
		t.Errorf("chat.update text %q, want %q", got, want)
	}
}

// TestEditLastNoMatch: a command whose old text isn't in the message
// clears the composer and does nothing else: no chat.update, no command,
// no message.
func TestEditLastNoMatch(t *testing.T) {
	c, _, ups := elSend(t, func(s *store.Store) {
		s.SetLatest(tChGen, []slack.Message{elMsg(elOwnTS, tSelf, "turn it of please", elBlocks(elText("turn it of please")))}, false)
	}, "", "/s/zzz/off", 500*time.Millisecond)
	if len(ups) != 0 {
		t.Fatalf("%d chat.update calls for a command that matches nothing", len(ups))
	}
	if !c.ed.Doc().Empty() {
		t.Fatalf("composer holds %q", c.ed.Doc().Text())
	}
}

// TestEditLastNothingToEdit: with no message of the viewer's here, the
// edit rejects "Message not found" and the command goes back in the
// composer (restoreDraftOnFailure), unsent.
func TestEditLastNothingToEdit(t *testing.T) {
	c, _, ups := elSend(t, func(s *store.Store) {
		s.SetLatest(tChGen, []slack.Message{elMsg(elAliceTS, "UALICE", "of course", elBlocks(elText("of course")))}, false)
	}, "", "/s/of/off", 500*time.Millisecond)
	if len(ups) != 0 {
		t.Fatalf("%d chat.update calls with no own message", len(ups))
	}
	if got := c.ed.Doc().Text(); got != "/s/of/off" {
		t.Fatalf("composer holds %q, want the command back", got)
	}
}

// TestEditLastNotACommandInCode: inline code isn't a command (s4/N's
// isOpCode), so it goes out as a message like any text.
func TestEditLastNotACommandInCode(t *testing.T) {
	var q qDelta
	q.push(qOp{k: qIns, text: []rune("/s/of/off"), attrs: qAttrs{"code": true}})
	q.push(qOp{k: qIns, text: []rune("\n")})
	if elIsCommand(q) {
		t.Fatal("inline code /s/of/off reads as a command")
	}
}
