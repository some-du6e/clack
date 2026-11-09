package ui

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/blockkit"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// Block Kit surfaces on the fake Slack's blockkit-gallery (synthetic data
// only): legacy attachments, message permalink pills and unfurl cards,
// the modal stack driven by the fake app (shortcut → errors → push →
// update → back → clear, ×), App Home tabs, an ephemeral message, and a
// pasted permalink turning into the composer's pill. BK_SURFACES_SHOTS_DIR
// saves light and dark screenshots (the bk-surfaces workflow).
func TestBlockKitSurfaces(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Gallery)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	dir := os.Getenv("BK_SURFACES_SHOTS_DIR")
	e := startEnv(t, fs, t.TempDir())
	if dir != "" && e.gpu == nil {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	// the fake app's realtime events (view_opened…) reach core as the
	// socket would deliver them
	fs.Emit = func(ev map[string]any) {
		b, _ := json.Marshal(ev)
		typ, _ := ev["type"].(string)
		e.a.data.core.Inject(slack.Event{Type: typ, Raw: b})
	}
	e.waitReady()
	a := e.a
	ch := fakeslack.BlockKitGalleryID
	shots := func(name string) {
		if dir == "" {
			return
		}
		for _, dark := range []bool{false, true} {
			a.th.SetDark(dark)
			e.settle(900 * time.Millisecond)
			e.shotTo(t, dir, name+map[bool]string{false: "-light", true: "-dark"}[dark])
		}
		a.th.SetDark(false)
		e.settle(300 * time.Millisecond)
	}

	// the gallery
	e.openChannel(ch)
	e.until("the gallery's messages", 20*time.Second, func() bool { return e.loadedMessages(ch) >= 7 })
	e.settle(2 * time.Second)
	shots("bk-surfaces-gallery-bottom")
	e.scroll(e.listPoint(), -4000)
	e.settle(1500 * time.Millisecond)
	shots("bk-surfaces-gallery-top")

	// global shortcut → the fake app's modal
	ticket := core.AppShortcut{ActionID: "SURFA1", AppID: fakeslack.AppAppID, AppName: "Deploy Bot", Name: "File a ticket", Type: "global_action"}
	blockkit.RunShortcut(a, ticket, ch, "")
	modal := func(what string, cond func(m blockkit.ModalInfo) bool) blockkit.ModalInfo {
		t.Helper()
		var m blockkit.ModalInfo
		e.until(what, 15*time.Second, func() bool {
			var ok bool
			m, ok = blockkit.OpenModal(a)
			return ok && cond(m)
		})
		return m
	}
	m := modal("the ticket modal", func(m blockkit.ModalInfo) bool { return m.ViewID == fakeslack.SurfTicketView })
	if m.Title != "New ticket" || m.Submit != "Next" || m.Close != "Cancel" || m.BackShown {
		t.Errorf("modal chrome: %+v", m)
	}
	shots("bk-surfaces-modal")

	// response_action errors: the message under the input
	blockkit.SubmitModal(a)
	m = modal("validation errors", func(m blockkit.ModalInfo) bool { return m.Errors["title"] != "" && !m.Busy })
	if m.Depth != 1 || m.Banner != "" {
		t.Errorf("errors: %+v", m)
	}
	shots("bk-surfaces-modal-errors")

	// response_action push: a second view with the back arrow
	blockkit.SubmitModal(a)
	m = modal("the pushed view", func(m blockkit.ModalInfo) bool { return m.Depth == 2 && m.Title == "Confirm" })
	if !m.BackShown || m.Close != "Back" || m.Submit != "Create" {
		t.Errorf("pushed: %+v", m)
	}
	shots("bk-surfaces-modal-pushed")

	// response_action update: the view changes in place
	blockkit.SubmitModal(a)
	m = modal("the updated view", func(m blockkit.ModalInfo) bool { return m.Title == "Ticket created" && !m.Busy })
	if m.Depth != 2 || m.Submit != "Finish" {
		t.Errorf("updated: %+v", m)
	}
	shots("bk-surfaces-modal-updated")

	// back: pops to the first view and views.close's the pushed one
	blockkit.BackModal(a)
	modal("back on the first view", func(m blockkit.ModalInfo) bool { return m.Depth == 1 && m.ViewID == fakeslack.SurfTicketView })
	e.until("views.close of the pushed view", 10*time.Second, func() bool {
		closes, _, _, _ := fs.SurfaceApp().Snapshot()
		return len(closes) > 0 && closes[len(closes)-1].Get("view_id") == "VSURF00002" && closes[len(closes)-1].Get("root_view_id") == ""
	})

	// push again, then response_action clear closes the whole stack
	blockkit.SubmitModal(a)
	modal("pushed again", func(m blockkit.ModalInfo) bool { return m.Depth == 2 && !m.Busy })
	blockkit.SubmitModal(a)
	e.until("clear closes the stack", 15*time.Second, func() bool { _, open := blockkit.OpenModal(a); return !open })

	// × on a fresh modal: views.close with the root (notify_on_close)
	blockkit.RunShortcut(a, ticket, ch, "")
	modal("the modal again", func(m blockkit.ModalInfo) bool { return m.ViewID == fakeslack.SurfTicketView })
	blockkit.CloseModal(a)
	e.until("views.close of the stack", 10*time.Second, func() bool {
		closes, _, _, _ := fs.SurfaceApp().Snapshot()
		n := len(closes)
		return n > 0 && closes[n-1].Get("root_view_id") == fakeslack.SurfTicketView
	})
	if _, open := blockkit.OpenModal(a); open {
		t.Error("× left the modal open")
	}

	// message shortcut: the message's channel and ts go along
	msg := core.AppShortcut{ActionID: "SURFA2", AppID: fakeslack.AppAppID, AppName: "Deploy Bot", Name: "Ticket from message", Type: "message_action"}
	blockkit.RunShortcut(a, msg, ch, fakeslack.SurfLegacyTS)
	modal("the message shortcut's modal", func(m blockkit.ModalInfo) bool { return true })
	_, execs, _, _ := fs.SurfaceApp().Snapshot()
	if last := execs[len(execs)-1].Get("context"); !strings.Contains(last, fakeslack.SurfLegacyTS) || !strings.Contains(last, ch) {
		t.Errorf("message shortcut context %s", last)
	}
	if items := blockkit.MessageShortcutItems(a, core.Message{Channel: ch, TS: fakeslack.SurfLegacyTS}); len(items) < 2 || items[0].Label != "Ticket from message" {
		t.Errorf("message menu shortcuts: %+v", items)
	}
	blockkit.CloseModal(a)

	// an ephemeral message: "Only visible to you"
	eph := map[string]any{"type": "message", "channel": ch, "ts": "1762515500.000100", "user": fakeslack.SlackbotID, "is_ephemeral": true,
		"text": "Only you can see this synthetic note.", "blocks": []any{}}
	b, _ := json.Marshal(eph)
	a.data.core.Inject(slack.Event{Type: "message", Channel: json.RawMessage(`"` + ch + `"`), Raw: b})
	e.until("the ephemeral flag", 10*time.Second, func() bool { return blockkit.Ephemeral(a, ch, "1762515500.000100") })
	e.scroll(e.listPoint(), 6000)
	e.settle(1500 * time.Millisecond)
	shots("bk-surfaces-ephemeral")

	// a pasted message permalink becomes the composer's pill
	if cb, ok := a.convoFor(ch, "").comp.(composerBox); ok {
		cb.PasteText("look https://fakeclub.slack.com/archives/" + ch + "/p" + strings.ReplaceAll(fakeslack.SurfLegacyTS, ".", ""))
		e.until("the pasted link's slug", 15*time.Second, cb.HasMessageSlug)
		shots("bk-surfaces-composer-slug")
	} else {
		t.Errorf("composer: %T", a.convoFor(ch, "").comp)
	}

	// App Home: the app DM opens on Messages (sidebar), Home publishes
	a.Navigate("c/" + fakeslack.AppDMID)
	var dm *blockkit.AppDM
	e.until("the app DM's tabs", 20*time.Second, func() bool { dm = blockkit.AppDMFor(a, fakeslack.AppDMID); return dm != nil })
	if dm.Tab() != "messages" || strings.Join(dm.Tabs(), ",") != "home,messages,about" {
		t.Errorf("tabs: %s of %v", dm.Tab(), dm.Tabs())
	}
	e.until("app_home_opened for Messages", 10*time.Second, func() bool {
		_, _, _, opens := fs.SurfaceApp().Snapshot()
		return len(opens) > 0 && opens[0] == "messages"
	})
	dm.SetTab("home")
	e.until("the published home view", 10*time.Second, func() bool { return dm.HomeHash() == "home1" })
	shots("bk-surfaces-app-home")
	dm.SetTab("about")
	e.settle(1500 * time.Millisecond)
	shots("bk-surfaces-app-about")
}
