package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/hovercard"
)

// Karim (alpha.15, Chat Tabs and Message Logger on): "I have to click like
// 5 times", "I couldn't type cuz I was getting deselected until I went
// back and forth on the tabs". One click on a sidebar row navigates; one
// click on the composer focuses it, and it keeps focus while realtime
// messages, typing and reactions arrive and a hover card opens and closes,
// so what's typed afterwards lands in it. Alone and with the plugins.
func TestSlickViewComposerKeepsFocus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		plugins []string
	}{
		{"alone", nil},
		{"ChatTabs", []string{"ChatTabs"}},
		{"ChatTabs+MessageLogger", []string{"ChatTabs", "MessageLogger"}},
	} {
		t.Run(tc.name, func(t *testing.T) { composerKeepsFocus(t, tc.plugins) })
	}
}

func composerKeepsFocus(t *testing.T, plugins []string) {
	e := newSV(t)
	tabs := false
	for _, id := range plugins {
		e.install(id, nil)
		tabs = tabs || id == "ChatTabs"
	}
	if tabs {
		e.until(8*time.Second, "the tab bar", func() bool { return e.tree(`slick-chat-tabs`, `data-tab-count`) != "" })
	}
	e.wait(400*time.Millisecond, func() bool { return false })

	// sidebar rows: one click each
	for _, ch := range []string{svRandom, ttGen} {
		e.click(ch)
		if got := string(e.a.router.Current()); got != "c/"+ch {
			t.Fatalf("one click on %s's row: route %s", ch, got)
		}
	}
	e.wait(300*time.Millisecond, func() bool { return false })

	cv := e.a.convos[ttGen+"/"]
	if cv == nil || cv.comp == nil || cv.compRect.Empty() {
		t.Fatal("no #general composer laid out")
	}
	cb, ok := cv.comp.(composerBox)
	if !ok {
		t.Fatalf("the composer is a %T", cv.comp)
	}
	focused := func() bool { return e.rt.Source().Focused(cb.FocusTag()) }

	// one click on the composer focuses it
	p := cv.compRect.Min.Add(image.Pt(60, cv.compRect.Dy()/2))
	pt := f32.Pt(float32(p.X), float32(p.Y))
	e.clock += time.Second
	e.rt.Queue(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: pt, Time: e.clock},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: pt, Time: e.clock + 10*time.Millisecond},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: pt, Time: e.clock + 60*time.Millisecond},
	)
	for i := 0; i < 3; i++ {
		e.frame()
	}
	if !focused() {
		t.Fatal("one click on the composer didn't focus it")
	}

	// idle with realtime traffic and a hover card
	ctl := &e.a.hover.ctl
	lost := -1
	base := time.Now().Unix()
	for i := 0; i < 80; i++ {
		switch i % 20 {
		case 0: // someone posts
			ts := fmt.Sprintf("%d.%06d", base, 100+i)
			raw, _ := json.Marshal(map[string]any{"type": "message", "channel": ttGen, "user": svAlice, "text": "hi " + ts, "ts": ts})
			e.ca.Inject(slack.Event{Type: "message", Channel: json.RawMessage(`"` + ttGen + `"`), Raw: raw})
		case 5: // typing
			raw, _ := json.Marshal(map[string]any{"type": "user_typing", "channel": ttGen, "user": svBob})
			e.ca.Inject(slack.Event{Type: "user_typing", Channel: json.RawMessage(`"` + ttGen + `"`), Raw: raw})
		case 8: // a reaction on the newest message
			ts := fmt.Sprintf("%d.%06d", base, 100+i-8)
			raw, _ := json.Marshal(map[string]any{"type": "reaction_added", "user": svBob, "reaction": "tada",
				"item": map[string]any{"type": "message", "channel": ttGen, "ts": ts}})
			e.ca.Inject(slack.Event{Type: "reaction_added", Raw: raw})
		case 10: // a hover card opens over a name, then closes
			ctl.Show(hovercard.Request{Key: svAlice, UserID: svAlice, Anchor: image.Rect(300, 300, 360, 318)}, time.Now())
		case 15:
			ctl.Close()
		}
		e.frame()
		if lost < 0 && !focused() {
			lost = i
		}
		time.Sleep(15 * time.Millisecond)
	}
	if lost >= 0 {
		t.Errorf("the composer lost key focus at idle frame %d (realtime traffic and hover cards only)", lost)
	}

	// typing lands in it
	st := e.rt.EditorState()
	e.rt.Queue(key.EditEvent{Range: st.Selection.Range, Text: "hello"})
	for i := 0; i < 3; i++ {
		e.frame()
	}
	if got := cb.DocText(); !strings.Contains(got, "hello") {
		t.Errorf("typed after idling, the composer holds %q", got)
	}
}
