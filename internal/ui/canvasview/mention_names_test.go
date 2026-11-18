package canvasview

// Karim (α.106, 2026-11-18): #ship's canvas heading read "#C0M8PUPU6
// Guidelines" for a channel he is in. Quip's export writes a mention as
// its id only (<control><a>#C…</a></control>, no name, no link), and
// Clack took that for a name. Slack's canvas names every mention from the
// channel's model, whether or not the viewer is in it. Synthetic
// fakeslack canvases in the export's markup.

import (
	"encoding/json"
	"image"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

const (
	mentionCanvas  = "F0CANVMENT1"
	mentionOther   = "C0CMNONMEM1" // a public channel the viewer isn't in
	mentionOtherNm = "ship-showcase"
)

// mentionExport is a canvas in Quip's export markup: a title heading that
// mentions member, a heading that mentions other, a paragraph.
func mentionExport(member, other string) string {
	ctl := func(n int, text string) string {
		return `<control id="temp:C:MNTc000` + string(rune('0'+n)) + `" data-remapped="true"><a>` + text + `</a></control>`
	}
	return `<div class="quip-canvas-content">` +
		`<h1 id="temp:C:MNT00001">` + ctl(1, "#"+member) + ` Guidelines</h1>` +
		`<h2 id="temp:C:MNT00002">` + ctl(2, "#"+other) + ` rules</h2>` +
		`<p id="temp:C:MNT00003" class="line">Post what you made.</p>` +
		`</div>`
}

// blockText is what block i of the shown canvas drew (its plain text).
func blockText(v *View, i int) string {
	if i >= len(v.blocks) || v.blocks[i].rv == nil {
		return "(no block)"
	}
	rv := v.blocks[i].rv
	return rv.TextRange(0, rv.PlainLen())
}

// A heading's channel mention reads #name for a channel the viewer is in
// (on the canvas's first frame) and for a public one they aren't in. The
// latter never shows its id while its lookup is out, and reads #name in
// the first frame drawn after the lookup answered.
func TestRenderMentionNames(t *testing.T) {
	h, fs := newEnv(t, false)
	var member string
	for _, c := range fs.ChannelLinkCases() {
		if c.Name == "member" {
			member = c.ID
		}
	}
	cv, ok := h.a.Conv(member)
	if member == "" || !ok || cv.Name == "" {
		t.Fatalf("no member channel (%q)", member)
	}
	fs.AddOtherChannel(slack.Conversation{ID: mentionOther, Name: mentionOtherNm, NumMembers: 900})
	// hold the non-member channel's lookup until released
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var asked atomic.Bool
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if method == "edge/channels/info" && strings.Contains(string(body["updated_ids"]), mentionOther) {
			asked.Store(true)
			<-release
		}
		return nil, false
	})
	src := mentionExport(member, mentionOther)
	doc, err := canvas.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	fs.AddCanvas(fakeslack.FakeCanvas{ID: mentionCanvas, Title: "<#" + member + "> Guidelines", HTML: src, Doc: doc,
		Created: time.Now().Unix() - 86400, Updated: time.Now().Unix() - 600})
	v := New(h, mentionCanvas, Tab, Actions{})
	s := newShooter(t, image.Pt(900, 500))
	var ops *op.Ops
	frame := func() { ops = s.frame(func(gtx layout.Context) { v.Layout(gtx) }) }
	settle(t, s, v, "the canvas", func() bool { return v.shown != nil && len(v.blocks) == 3 })
	frame()
	if got, want := blockText(v, 0), "#"+cv.Name+" Guidelines"; got != want {
		t.Errorf("title heading (a member channel): %q, want %q", got, want)
	}
	deadline := time.Now().Add(15 * time.Second)
	for !asked.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the non-member channel was never looked up")
		}
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	frame()
	if got := blockText(v, 1); strings.Contains(got, mentionOther) {
		t.Errorf("heading drew the channel's id while its lookup was out: %q", got)
	}
	s.shot("mention-names-loading", ops)
	once.Do(func() { close(release) })
	// the answer lands with no frame drawn; the next one draws the name
	deadline = time.Now().Add(15 * time.Second)
	for h.a.S.ChannelName(mentionOther) != mentionOtherNm {
		if time.Now().After(deadline) {
			t.Fatal("the lookup never named the channel")
		}
		time.Sleep(10 * time.Millisecond)
	}
	frame()
	if got, want := blockText(v, 1), "#"+mentionOtherNm+" rules"; got != want {
		t.Errorf("heading (a public channel the viewer isn't in), first frame after its name arrived: %q, want %q", got, want)
	}
	if got, want := blockText(v, 0), "#"+cv.Name+" Guidelines"; got != want {
		t.Errorf("title heading after the lookup: %q, want %q", got, want)
	}
	s.shot("mention-names", ops)
}
