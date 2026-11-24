package canvasview

import (
	"fmt"
	"image"
	"net/http"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// A canvas link card in Quip's real export shape: no id, no anchor, only
// the placeholder Quip's server writes ahead of the URL.
const (
	cardCanvasID = "F0CANVCARD1"
	cardURL      = "https://example.com/repo"
	cardHTML     = `<div class="quip-canvas-content"><p id="temp:C:LC1p1" class="line">Before the card</p>` +
		`<p class='embedded-link'>Link URL: ` + cardURL + `</p></div>`
	cardTS = "1762500000.000100"
)

// cardEnv serves the card canvas, its container ch/ts answered with m (nil:
// messages.list answers without it), and opens it in mode at sz.
func cardEnv(t *testing.T, dark bool, m *slack.Message, ts string, prep func(fs *fakeslack.Server)) (*testHost, *fakeslack.Server, func(Mode, image.Point) (*View, *shooter)) {
	t.Helper()
	h, fs := newEnv(t, dark)
	d, err := canvas.Parse(cardHTML)
	if err != nil {
		t.Fatal(err)
	}
	fs.AddCanvas(fakeslack.FakeCanvas{ID: cardCanvasID, Title: "Cards", HTML: cardHTML, Doc: d})
	setUnfurl(fs, cardURL, ts, m)
	if prep != nil {
		prep(fs)
	}
	open := func(mode Mode, sz image.Point) (*View, *shooter) {
		v := New(h, cardCanvasID, mode, Actions{FullView: func() {}, SideView: func() {}, Close: func() {}})
		s := newShooter(t, sz)
		settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil && embedBlk(v) != nil })
		return v, s
	}
	return h, fs, open
}

// appUnfurl is a container holding a synthetic GitHub-like app unfurl, as
// Slack's GitHub app posts them (title, two short fields, a colour,
// bot_id and is_app_unfurl).
func appUnfurl(ts string, unfurled bool) *slack.Message {
	return unfurledAs(&slack.Message{Type: "message", TS: ts, BotID: "B0FAKEGH001",
		Attachments: []slack.Attachment{{ID: 1, Color: "24292f", Title: "octo/repo", TitleLink: cardURL, FromURL: cardURL, OriginalURL: cardURL,
			Text: "A synthetic repository", Fallback: "octo/repo", BotID: "B0FAKEGH001", IsAppUnfurl: true,
			Fields: []slack.AttachmentField{{Title: "Language", Value: "Go", Short: true}, {Title: "Stars", Value: "7", Short: true}}}}}, unfurled)
}

// waitCard frames until the card is kind (and its text has want, when
// set), or draws Quip's placeholder (the bug: the checks after it fail).
func waitCard(t *testing.T, s *shooter, v *View, kind, want string) {
	t.Helper()
	settle(t, s, v, "a "+kind+" card", func() bool {
		k, _, _ := cardState(v)
		return k == kind && (want == "" || strings.Contains(embedText(v), want)) || labelled(v)
	})
}

// labelled: the card draws the export's placeholder.
func labelled(v *View) bool { return strings.Contains(embedText(v), "Link URL") }

func noLabel(t *testing.T, v *View) {
	t.Helper()
	if txt := embedText(v); strings.Contains(txt, "Link URL") {
		t.Errorf("the card draws Quip's placeholder: %q", txt)
	}
}

// TestRenderLinkCard: Slack draws a canvas link card from Quip's model (a
// type-56 section naming its unfurl container), never the export's "Link
// URL: <url>". Each of Card's kinds: the fallback around the container's
// app unfurl, EmptyUnfurl without one, the skeleton while it unfurls,
// EmptyUnfurl for an old container that never did, EmptyUnfurl while
// messages.list is out, and EmptyUnfurl when the model doesn't load.
func TestRenderLinkCard(t *testing.T) {
	empty := cardURL + "\nUnable to create preview."
	t.Run("fallback", func(t *testing.T) {
		for _, dark := range []bool{false, true} {
			for _, m := range []struct {
				mode Mode
				name string
				sz   image.Point
			}{{Full, "full", image.Pt(1100, 900)}, {Tab, "tab", image.Pt(900, 700)}} {
				_, _, open := cardEnv(t, dark, appUnfurl(cardTS, true), cardTS, nil)
				v, s := open(m.mode, m.sz)
				waitCard(t, s, v, "fallback", "octo/repo")
				txt := embedText(v)
				if !strings.Contains(txt, "Language") {
					t.Errorf("%s: the fallback card %q has no fields", m.name, txt)
				}
				noLabel(t, v)
				_, size, borderless := cardState(v)
				if !borderless {
					t.Errorf("%s: the attachment keeps its colour bar or its width cap", m.name)
				}
				if m.mode == Full && size.X != 800 { // the column, not 716
					t.Errorf("full: the card is %d wide, want the column's 800", size.X)
				}
				s.shot(fmt.Sprintf("canvas-link-card-%s-%v", m.name, dark), s.frame(func(gtx layout.Context) { v.Layout(gtx) }))
			}
		}
	})
	t.Run("empty", func(t *testing.T) {
		_, _, open := cardEnv(t, false, nil, cardTS, nil)
		v, s := open(Full, image.Pt(1100, 900))
		waitCard(t, s, v, "empty", "Unable")
		// after messages.list answered without the container
		for i := 0; i < 30; i++ {
			s.frame(func(gtx layout.Context) { v.Layout(gtx) })
			time.Sleep(10 * time.Millisecond)
		}
		if txt := embedText(v); txt != empty {
			t.Errorf("text %q, want %q", txt, empty)
		}
		s.shot("canvas-link-card-empty", s.frame(func(gtx layout.Context) { v.Layout(gtx) }))
	})
	t.Run("unfurling", func(t *testing.T) {
		ts := fmt.Sprintf("%d.000100", time.Now().Add(-5*time.Second).Unix())
		m := unfurledAs(&slack.Message{Type: "message", TS: ts}, false)
		_, _, open := cardEnv(t, false, m, ts, nil)
		v, s := open(Full, image.Pt(1100, 900))
		waitCard(t, s, v, "loading", "")
		if txt := embedText(v); txt != "" {
			t.Errorf("the skeleton draws text %q", txt)
		}
		s.shot("canvas-link-card-unfurling", s.frame(func(gtx layout.Context) { v.Layout(gtx) }))
	})
	t.Run("unfurling-old", func(t *testing.T) {
		ts := fmt.Sprintf("%d.000100", time.Now().Add(-20*time.Minute).Unix())
		m := unfurledAs(&slack.Message{Type: "message", TS: ts}, false)
		_, fs, open := cardEnv(t, false, m, ts, nil)
		n := fs.CallCount("messages.list")
		v, s := open(Full, image.Pt(1100, 900))
		settle(t, s, v, "the container's answer", func() bool {
			return fs.CallCount("messages.list") > n || embedText(v) == empty || labelled(v)
		})
		for i := 0; i < 30; i++ {
			s.frame(func(gtx layout.Context) { v.Layout(gtx) })
			time.Sleep(10 * time.Millisecond)
			if k, _, _ := cardState(v); k == "loading" {
				t.Fatal("an old container that never unfurled draws the skeleton")
			}
		}
		if txt := embedText(v); txt != empty {
			t.Errorf("text %q, want %q", txt, empty)
		}
	})
	t.Run("in-flight", func(t *testing.T) {
		hold := 3 * time.Second
		_, fs, open := cardEnv(t, false, appUnfurl(cardTS, true), cardTS, func(fs *fakeslack.Server) {
			fs.SetFault("messages.list", fakeslack.Fault{Delay: hold}) // every call: whichever round takes the container
		})
		n := fs.CallCount("messages.list")
		v, s := open(Full, image.Pt(1100, 900))
		settle(t, s, v, "messages.list going out", func() bool { return fs.CallCount("messages.list") > n || labelled(v) })
		sent := time.Now()
		for time.Since(sent) < hold/2 {
			s.frame(func(gtx layout.Context) { v.Layout(gtx) })
			time.Sleep(20 * time.Millisecond)
		}
		if k, _, _ := cardState(v); k != "empty" {
			t.Errorf("while messages.list is out the card is %q, want empty (no container in the store)", k)
		}
		noLabel(t, v)
		fs.SetFault("messages.list", fakeslack.Fault{})
		waitCard(t, s, v, "fallback", "octo/repo")
		noLabel(t, v)
	})
	t.Run("model-refused", func(t *testing.T) {
		_, _, open := cardEnv(t, false, appUnfurl(cardTS, true), cardTS, func(fs *fakeslack.Server) {
			fs.SetFault("canvas/load-data", fakeslack.Fault{Status: http.StatusBadGateway})
		})
		v, s := open(Full, image.Pt(1100, 900))
		settle(t, s, v, "the model's failure", func() bool {
			return v.h.Core().CanvasModelError(cardCanvasID) != nil || labelled(v)
		})
		for i := 0; i < 20; i++ {
			s.frame(func(gtx layout.Context) { v.Layout(gtx) })
			time.Sleep(10 * time.Millisecond)
		}
		if txt := embedText(v); txt != empty {
			t.Errorf("text %q, want %q", txt, empty)
		}
	})
}
