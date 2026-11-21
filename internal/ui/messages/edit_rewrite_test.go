package messages

// An edit that brings in what a plugin rewrites. Slack runs a messages
// slice patch inside the render (its patched selector), so an edit that
// brings a word Censorship masks into a message it had left alone draws
// masked in the render after the edit: the word never shows. Clack's
// rows get JS's copy of the edit a JS round trip later, and drew core's
// copy meanwhile, the word in clear for that round trip (the reviews).
// Under plugins that rewrite text the edit's row now holds the text it
// drew until JS's copy comes, at most taut.EditHold; a patch that rewrites
// no text (ShowRealUser's sender) still draws the edit in the next frame.
// Synthetic data on internal/fakeslack.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// relayCreditJS credits the relay bot B0RELAY0001's posts in ch to
// UANA00001, as ShowRealUser's sentBy does: the sender rewritten, no text.
func relayCreditJS(ch string) []byte {
	return pluginJS(`
class RelayCredit extends TautPlugin {
  static id = 'RelayCredit'
  static pluginName = 'Relay credit (test)'
  static description = 'test: credits a relay bot post to a member, its text untouched'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const credit = (ts, m) => (m && m.bot_id === 'B0RELAY0001' ?
      { ...m, user: 'UANA00001', username: undefined, icons: undefined, bot_profile: undefined, bot_id: undefined, subtype: undefined } : m)
    const { patchSlice, mapEntries } = this.api.redux
    patchSlice('messages', (c, bucket) => (c !== '` + ch + `' || !bucket || typeof bucket !== 'object' ? bucket : mapEntries(bucket, credit)))
  }
}
return RelayCredit`)
}

// maskedChannel is ch with a message maskJS masks ("the job board is up")
// and one it leaves alone ("lunch at noon?"), both drawn, the first
// masked: the plugins rewrite text (taut TextRewrites).
func maskedChannel(t *testing.T, ch string) (app *core.App, v *ChannelView, frame func(), left string) {
	t.Helper()
	app, _, _ = shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: ch, Name: "jobs", IsChannel: true, IsMember: true, NumMembers: 12})
	base := time.Now().Add(-time.Hour).Unix()
	masked, left := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60)
	app.S.SetLatest(ch, []slack.Message{
		{Type: "message", TS: masked, User: "UANA00001", Text: "the job board is up", Blocks: shotRich(shotSection("the job board is up"))},
		{Type: "message", TS: left, User: "UBEN00001", Text: "lunch at noon?", Blocks: shotRich(shotSection("lunch at noon?"))},
	}, false)
	patchedEnv(t, app, maskJS(ch, false))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	v = NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	v.Preview = true
	frame = frameFn(v)
	until(t, 10*time.Second, "the masked message drawn masked", frame, func() bool {
		return strings.Contains(rowText(v.s.rows[masked]), "the *** board")
	})
	until(t, 10*time.Second, "the other drawn", frame, func() bool { return strings.Contains(rowText(v.s.rows[left]), "lunch at noon?") })
	for i := 0; i < 20; i++ {
		frame()
		time.Sleep(5 * time.Millisecond)
	}
	if !tautOf().TextRewrites() {
		t.Fatal("a masked message drew masked, and taut doesn't know the plugins rewrite text")
	}
	return app, v, frame, left
}

func TestEditBringingAMaskedWordNeverShowsIt(t *testing.T) {
	const ch = "C0MASKEDT1"
	app, v, frame, left := maskedChannel(t, ch)
	// the plugins' JS a moment behind the edit, as a busy runtime is
	release := tautOf().HoldMirror()
	editMsg(app, ch, left, "UBEN00001", "a job at noon?")
	check := func(when string) {
		t.Helper()
		if txt := rowText(v.s.rows[left]); strings.Contains(txt, "job") {
			t.Fatalf("%s: the edit's row drew %q: the word the plugin masks, in clear", when, txt)
		}
	}
	for dl := time.Now().Add(80 * time.Millisecond); time.Now().Before(dl); time.Sleep(4 * time.Millisecond) {
		frame()
		check("while JS answers for the edit")
	}
	release()
	for dl := time.Now().Add(5 * time.Second); ; time.Sleep(4 * time.Millisecond) {
		frame()
		check("as JS's copy of the edit came")
		if strings.Contains(rowText(v.s.rows[left]), "a *** at noon?") {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("the edit never drew masked: %q", rowText(v.s.rows[left]))
		}
	}
}

// The hold lasts taut.EditHold at most: a JS that doesn't answer for the
// edit (stalled) leaves the row drawing core's copy of it after that, not
// the revision before for as long as JS is busy.
func TestEditHoldEndsAtEditHold(t *testing.T) {
	const ch = "C0MASKCAP1"
	app, v, frame, left := maskedChannel(t, ch)
	release := tautOf().HoldMirror()
	defer release()
	editMsg(app, ch, left, "UBEN00001", "a job at noon?")
	until(t, 5*time.Second, "the edit in the list's model", frame, func() bool {
		return modelHas(v, left, func(m *core.Message) bool { return m.Text == "a job at noon?" })
	})
	start := time.Now()
	for {
		frame()
		txt := rowText(v.s.rows[left])
		if strings.Contains(txt, "a job at noon?") {
			if el := time.Since(start); el < taut.EditHold/2 {
				t.Fatalf("core's copy of the edit drew %v after it, inside the hold (%v)", el.Round(time.Millisecond), taut.EditHold)
			}
			break
		}
		if time.Since(start) > taut.EditHold+time.Second {
			t.Fatalf("%v after the edit with JS stalled, the row still drew %q: the hold outlasted %v", time.Since(start).Round(time.Millisecond), txt, taut.EditHold)
		}
		time.Sleep(4 * time.Millisecond)
	}
}

// A patch that rewrites no text, ShowRealUser's (a relay bot's post
// credited to the member who sent it), holds nothing: an edit of another
// message draws in the frame after the list has it, with JS behind it.
func TestEditUnderASenderPatchDrawsNextFrame(t *testing.T) {
	app, _, _ := shotsApp(t)
	const ch = "C0RELAYED1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "relays", IsChannel: true, IsMember: true, NumMembers: 12})
	base := time.Now().Add(-time.Hour).Unix()
	relay, plain := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60)
	app.S.SetLatest(ch, []slack.Message{
		{Type: "message", Subtype: "bot_message", TS: relay, BotID: "B0RELAY0001", Username: "relay", Text: "a relayed post",
			Blocks: shotRich(shotSection("a relayed post"))},
		{Type: "message", TS: plain, User: "UBEN00001", Text: "lunch at noon?", Blocks: shotRich(shotSection("lunch at noon?"))},
	}, false)
	patchedEnv(t, app, relayCreditJS(ch))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	v.Preview = true
	frame := frameFn(v)
	until(t, 10*time.Second, "the relayed post credited to the member", frame, func() bool {
		r := v.s.rows[relay]
		return r != nil && r.viewed && r.vmsg != nil && r.vmsg.User == "UANA00001"
	})
	until(t, 10*time.Second, "the other drawn", frame, func() bool { return strings.Contains(rowText(v.s.rows[plain]), "lunch at noon?") })
	for i := 0; i < 20; i++ {
		frame()
		time.Sleep(5 * time.Millisecond)
	}
	if tautOf().TextRewrites() {
		t.Fatal("a patch that rewrote only a sender counts as rewriting text")
	}
	release := tautOf().HoldMirror()
	defer release()
	editMsg(app, ch, plain, "UBEN00001", "lunch at one?")
	until(t, 5*time.Second, "the edit in the list's model", func() {}, func() bool {
		return modelHas(v, plain, func(m *core.Message) bool { return m.Text == "lunch at one?" })
	})
	// the frame that takes the model's new copy draws it, or the one after
	// (latStats' two frames); a hold would keep the old text for
	// taut.EditHold, JS being behind
	for i := 0; i < 2; i++ {
		frame()
		if strings.Contains(rowText(v.s.rows[plain]), "lunch at one?") {
			return
		}
	}
	t.Errorf("two frames after the list took the edit the row drew %q, not the edit: a pass-through patch held it", rowText(v.s.rows[plain]))
}
