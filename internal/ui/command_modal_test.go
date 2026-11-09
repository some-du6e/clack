package ui

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/blockkit"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// A slash command typed in the composer opens the app's modal, as in
// Slack: chat.command carries a client_token, the fake app's views.open
// comes back as view_opened for it, then its views.update; a failing
// command gets Slackbot's "*/ticket* failed …" ephemeral; and the typing
// indicator under the composer for one, two and several people.
// Synthetic data only (fakeslack's blockkit-gallery). COMMAND_SHOTS_DIR
// saves light and dark screenshots (the command-modals workflow).
func TestCommandModalAndTyping(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Gallery)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	dir := os.Getenv("COMMAND_SHOTS_DIR")
	e := startEnv(t, fs, t.TempDir())
	if dir != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	fs.Emit = func(ev map[string]any) {
		b, _ := json.Marshal(ev)
		typ, _ := ev["type"].(string)
		var chRaw json.RawMessage
		if c, ok := ev["channel"].(string); ok {
			chRaw = json.RawMessage(`"` + c + `"`)
		}
		e.a.data.core.Inject(slack.Event{Type: typ, Channel: chRaw, Raw: b})
	}
	e.waitReady()
	a := e.a
	ch := fakeslack.BlockKitGalleryID
	e.openChannel(ch)
	e.until("the gallery's messages", 20*time.Second, func() bool { return e.loadedMessages(ch) >= 7 })
	e.settle(1500 * time.Millisecond)

	save := func(name string, img image.Image) {
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
	// shots saves the window and, with crop set, the composer and the bar
	// under it, in light and dark.
	shots := func(name string, crop bool) {
		if dir == "" {
			return
		}
		for _, dark := range []bool{false, true} {
			a.th.SetDark(dark)
			e.settle(900 * time.Millisecond)
			mode := map[bool]string{false: "-light", true: "-dark"}[dark]
			img := image.NewRGBA(image.Rectangle{Max: e.size})
			if err := e.gpu.Screenshot(img); err != nil {
				t.Fatalf("screenshot: %v", err)
			}
			save(name+mode, img)
			if cv := a.convoFor(ch, ""); crop && cv != nil && !cv.compRect.Empty() {
				r := cv.compRect
				r.Min.X, r.Min.Y = max(r.Min.X, 0), max(r.Min.Y-24, 0)
				r.Max.X, r.Max.Y = min(r.Max.X, e.size.X), min(r.Max.Y+8, e.size.Y)
				save(name+mode+"-composer", img.SubImage(r))
			}
		}
		a.th.SetDark(false)
		e.settle(300 * time.Millisecond)
	}

	cv := a.convoFor(ch, "")
	if cv == nil || cv.compRect.Empty() {
		t.Fatal("no composer laid out")
	}
	cb, ok := cv.comp.(composerBox)
	if !ok {
		t.Fatalf("composer: %T", cv.comp)
	}
	e.click(cv.compRect.Min.Add(image.Pt(60, cv.compRect.Dy()/2)))
	e.settle(200 * time.Millisecond)
	// run types a command and sends it with Return (a second Return when
	// the first one picked the autocomplete's row).
	run := func(cmd string) {
		t.Helper()
		before := len(fs.CommandApp().Snapshot())
		// drafts live per process: an earlier test's draft in this
		// channel (TestBlockKitSurfaces pastes a link) would make the
		// command a message
		e.key("A", key.ModShortcut)
		e.key(key.NameDeleteBackward, 0)
		e.settle(200 * time.Millisecond)
		if got := strings.TrimSpace(cb.DocText()); got != "" {
			t.Fatalf("couldn't clear the composer: %q", got)
		}
		e.typeText(cmd)
		e.settle(200 * time.Millisecond)
		if got := cb.DocText(); !strings.Contains(got, cmd) {
			t.Fatalf("typed %q, the composer holds %q", cmd, got)
		}
		e.settle(400 * time.Millisecond)
		for i := 0; i < 3 && len(fs.CommandApp().Snapshot()) == before; i++ {
			if i > 0 && cb.DocText() == "" {
				break
			}
			e.key(key.NameReturn, 0)
			e.settle(500 * time.Millisecond)
		}
		e.until("chat.command for "+cmd, 15*time.Second, func() bool { return len(fs.CommandApp().Snapshot()) > before })
	}

	// /ticket anchor → the app's modal, then its views.update
	run("/ticket anchor")
	calls := fs.CommandApp().Snapshot()
	last := calls[len(calls)-1]
	if last.Get("command") != fakeslack.TicketCommand || last.Get("text") != "anchor" || !strings.HasPrefix(last.Get("client_token"), "web-") {
		t.Errorf("chat.command: %v", last)
	}
	var m blockkit.ModalInfo
	e.until("the command's modal", 15*time.Second, func() bool {
		var open bool
		m, open = blockkit.OpenModal(a)
		return open && m.ViewID == fakeslack.SurfTicketView
	})
	if m.Title != "New ticket" || m.Submit != "Next" {
		t.Errorf("modal chrome: %+v", m)
	}
	e.settle(1500 * time.Millisecond) // the views.update
	shots("command-modal", false)

	// its submits run the app's flow: response_action errors first
	blockkit.SubmitModal(a)
	e.until("validation errors", 15*time.Second, func() bool {
		m, _ = blockkit.OpenModal(a)
		return m.Errors["title"] != "" && !m.Busy
	})
	shots("command-modal-errors", false)
	blockkit.SubmitModal(a)
	e.until("the pushed view", 15*time.Second, func() bool { m, _ = blockkit.OpenModal(a); return m.Depth == 2 && !m.Busy })
	blockkit.CloseModal(a)
	e.until("the modal closed", 10*time.Second, func() bool { _, open := blockkit.OpenModal(a); return !open })
	e.settle(500 * time.Millisecond)

	// a failure: Slackbot's ephemeral with Slack's text
	e.click(cv.compRect.Min.Add(image.Pt(60, cv.compRect.Dy()/2)))
	e.settle(200 * time.Millisecond)
	run("/ticket timeout")
	want := "*/ticket* failed because the app did not respond. Please try again or contact the app developer."
	e.until("Slackbot's failure note", 15*time.Second, func() bool { return hasEphemeral(a, ch, want) })
	e.scroll(e.listPoint(), 6000)
	e.settle(1500 * time.Millisecond)
	shots("command-failed", false)

	// typing: one, two, several (user_typing on the socket)
	typing := func(users ...string) {
		for _, u := range users {
			b, _ := json.Marshal(map[string]any{"type": "user_typing", "channel": ch, "user": u})
			a.data.core.Inject(slack.Event{Type: "user_typing", Channel: json.RawMessage(`"` + ch + `"`), Raw: b})
		}
	}
	for _, c := range []struct {
		name  string
		users []string
		want  string
	}{
		{"typing-one", []string{fakeslack.ProfileID}, " is typing"},
		{"typing-two", []string{fakeslack.ProfileID, fakeslack.AdminID}, " are typing"},
		{"typing-several", []string{fakeslack.ProfileID, fakeslack.AdminID, fakeslack.ExternalID}, "Several people are typing"},
	} {
		typing(c.users...)
		e.until(c.name, 10*time.Second, func() bool { return strings.Contains(cb.TypingLine(), c.want) })
		if strings.Contains(cb.TypingLine(), "…") {
			t.Errorf("%s: %q has an ellipsis Slack doesn't", c.name, cb.TypingLine())
		}
		shots(c.name, true)
		typing(c.users...) // keep them typing past the shots
	}
	// nobody types for 6 s: the line goes (checkTypingMap)
	e.until("the typing line expiring", 15*time.Second, func() bool { return cb.TypingLine() == "" })
}

// hasEphemeral reports an ephemeral message in ch with exactly text.
func hasEphemeral(a *App, ch, text string) bool {
	tl := a.data.core.S.Timeline(ch)
	if tl == nil {
		return false
	}
	for _, m := range tl.Msgs {
		if m.Text == text && blockkit.Ephemeral(a, ch, m.TS) {
			return true
		}
	}
	return false
}
