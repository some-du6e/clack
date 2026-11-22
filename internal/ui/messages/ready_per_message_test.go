package messages

// A row's view of its message is ready (taut's MessageViewState) once JS
// answered after the change to that message. Round 5 judged it by the
// channel: any change in the channel within a JS round trip made every
// message's view not ready. In a busy channel (a bot posting, reactions
// coming in) an edit that brings in a word Censorship masks drew it
// unmasked until the channel had a quiet round trip, and a viewed row
// whose plugin stopped changing it never went back to its source, so its
// next update waited for JS too. Synthetic data on internal/fakeslack.

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/jsrt"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// maskJS masks "job" in the text and rich text of every message of ch
// (the messages slice, as Censorship does). slow: each answer takes JS
// 15 ms (a busy runtime under Karim's plugins), so in a busy channel
// every answer lands after another change to the channel.
func maskJS(ch string, slow bool) []byte {
	wait := ""
	if slow {
		wait = "const t0 = Date.now(); while (Date.now() - t0 < 15) {}"
	}
	return pluginJS(`
class MaskJob extends TautPlugin {
  static id = 'MaskJob'
  static pluginName = 'Mask a word (test)'
  static description = 'test: masks a word in the messages slice'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const fix = (t) => (typeof t === 'string' ? t.replace(/job/g, '***') : t)
    const els = (list) => (Array.isArray(list) ? list.map((e) => (e && e.type === 'text' ? { ...e, text: fix(e.text) } : e && e.elements ? { ...e, elements: els(e.elements) } : e)) : list)
    const mask = (ts, m) => (m && typeof m.text === 'string' && m.text.includes('job') ? { ...m, text: fix(m.text), blocks: els(m.blocks) } : m)
    const { patchSlice, mapEntries } = this.api.redux
    patchSlice('messages', (ch, bucket) => {
      if (ch !== '` + ch + `' || !bucket || typeof bucket !== 'object') return bucket
      ` + wait + `
      return mapEntries(bucket, mask)
    })
  }
}
return MaskJob`)
}

// busy keeps ch busy until stop is called: a reaction on message ts
// added and taken away every few ms, as a busy channel's events come.
// do runs f between them, on the same goroutine (core takes its events
// from one, the socket's).
func busy(app *core.App, ch, ts string) (stop func(), do func(f func())) {
	quit := make(chan struct{})
	fns := make(chan func())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-quit:
				return
			case f := <-fns:
				f()
				continue
			case <-time.After(4 * time.Millisecond):
			}
			typ := "reaction_added"
			if i%2 == 1 {
				typ = "reaction_removed"
			}
			injectEvent(app, ch, typ, "", map[string]any{"user": "UBEN00001", "reaction": "eyes",
				"item": map[string]any{"type": "message", "channel": ch, "ts": ts}})
		}
	}()
	return func() {
			close(quit)
			wg.Wait()
		}, func(f func()) {
			done := make(chan struct{})
			fns <- func() { f(); close(done) }
			<-done
		}
}

// editMsg is message ts's edit to text, as the socket brings it.
func editMsg(app *core.App, ch, ts, user, text string) {
	evTS := fmt.Sprintf("%d.%06d", time.Now().Unix(), time.Now().Nanosecond()/1000)
	injectEvent(app, ch, "message", "message_changed", map[string]any{"hidden": true, "ts": evTS, "message": map[string]any{
		"type": "message", "user": user, "ts": ts, "text": text, "edited": map[string]any{"user": user, "ts": evTS},
		"blocks": []any{map[string]any{"type": "rich_text", "block_id": "e", "elements": []any{map[string]any{"type": "rich_text_section",
			"elements": []any{map[string]any{"type": "text", "text": text}}}}}}}})
}

func TestEditMaskedWhileTheChannelIsBusy(t *testing.T) {
	app, _, _ := shotsApp(t)
	const ch = "C0BUSYMSK1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "lunch", IsChannel: true, IsMember: true, NumMembers: 12})
	base := time.Now().Add(-time.Hour).Unix()
	a, b := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60)
	app.S.SetLatest(ch, []slack.Message{
		{Type: "message", TS: a, User: "UANA00001", Text: "lunch at noon?", Blocks: shotRich(shotSection("lunch at noon?"))},
		{Type: "message", TS: b, User: "UBEN00001", Text: "ok", Blocks: shotRich(shotSection("ok"))},
	}, false)
	patchedEnv(t, app, maskJS(ch, true))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	v.Preview = true
	frame := frameFn(v)
	row := func() *row { return v.s.rows[a] }
	until(t, 10*time.Second, "the rows drawn", frame, func() bool { return row() != nil && itemHeight(v.s, b) > 0 })
	until(t, 10*time.Second, "the message's view read", frame, func() bool {
		_, ok := tautOf().MessageView(ch, a)
		return ok
	})
	for i := 0; i < 20; i++ {
		frame()
		time.Sleep(5 * time.Millisecond)
	}
	stop, do := busy(app, ch, b)
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	time.Sleep(50 * time.Millisecond)
	do(func() { editMsg(app, ch, a, "UANA00001", "job fair at noon?") })
	// the edit's word is masked within a JS round trip or a few, however
	// busy the rest of the channel, and once masked stays masked
	masked, back := false, ""
	for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl); {
		frame()
		txt := rowText(row())
		switch {
		case strings.Contains(txt, "*** fair at noon?"):
			masked = true
		case masked && strings.Contains(txt, "job") && back == "":
			back = txt
		}
		time.Sleep(4 * time.Millisecond)
	}
	stop()
	stop = nil
	if !masked {
		t.Errorf("the edit's masked word never drew masked in 3 s while another message of the channel changed every few ms: %q", rowText(row()))
	}
	if back != "" {
		t.Errorf("the edit drew unmasked again after its mask: %q", back)
	}
}

func TestViewedRowGoesBackToItsSource(t *testing.T) {
	app, _, _ := shotsApp(t)
	const ch = "C0BACKSRC1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "fair", IsChannel: true, IsMember: true, NumMembers: 12})
	base := time.Now().Add(-time.Hour).Unix()
	a, b := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60)
	app.S.SetLatest(ch, []slack.Message{
		{Type: "message", TS: a, User: "UANA00001", Text: "the job fair is today", Blocks: shotRich(shotSection("the job fair is today"))},
		{Type: "message", TS: b, User: "UBEN00001", Text: "ok", Blocks: shotRich(shotSection("ok"))},
	}, false)
	patchedEnv(t, app, maskJS(ch, true))
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	v.Preview = true
	frame := frameFn(v)
	row := func() *row { return v.s.rows[a] }
	until(t, 10*time.Second, "the masked view drawn", frame, func() bool {
		r := row()
		return r != nil && r.viewed && strings.Contains(rowText(r), "the *** fair is today")
	})
	// the edit takes the word out: the plugin leaves the message as it is
	// now, and JS's answer comes while the channel is busy
	stop, do := busy(app, ch, b)
	do(func() { editMsg(app, ch, a, "UANA00001", "the fair is today") })
	for dl := time.Now().Add(700 * time.Millisecond); time.Now().Before(dl); {
		frame()
		time.Sleep(4 * time.Millisecond)
	}
	stop()
	// quiet again: the row draws its source
	back := false
	for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl) && !back; {
		frame()
		r := row()
		back = r != nil && !r.viewed && strings.Contains(rowText(r), "the fair is today")
		time.Sleep(10 * time.Millisecond)
	}
	if !back {
		r := row()
		t.Fatalf("the row whose plugin no longer changes it still draws the plugins' view 3 s on (viewed %v, %q)", r != nil && r.viewed, rowText(r))
	}
	// so its next update draws at once, with JS busy
	h0 := itemHeight(v.s, a)
	rt, err := tautOf().JS()
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	rt.Post(func(*jsrt.Ctx) { <-release })
	defer close(release)
	injectEvent(app, ch, "reaction_added", "", map[string]any{"user": "UBEN00001", "reaction": "tada",
		"item": map[string]any{"type": "message", "channel": ch, "ts": a}})
	drawn := false
	for i := 0; i < 30 && !drawn; i++ {
		frame()
		drawn = itemHeight(v.s, a) > h0
		time.Sleep(5 * time.Millisecond)
	}
	if !drawn {
		t.Errorf("a reaction to the row, back on its source, isn't drawn while JS is busy (%dpx, %dpx before)", itemHeight(v.s, a), h0)
	}
}
