package ui

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// A custom section's Share ▸ Share directly is Slack's
// ShareChannelSectionMultiPaneDialog: the section's channels, all
// ticked; a person becomes the destination (conversations.open), which
// users.channelSections.validateShare checks (refused channels drop out);
// Share (after the private-channel confirmation) posts one message with
// the note and a channel_section attachment, closes, and jumps to it.
func TestShareSectionDirectly(t *testing.T) {
	e, fs := previewEnv(t)
	var sec *sbSection
	e.until("a custom section with channels", 20*time.Second, func() bool {
		sec = e.a.sectionByID("L0FAKE01")
		return sec != nil && len(sec.All) > 1
	})
	share := e.a.shareItem(sec)
	if share == nil {
		t.Fatal("no Share item on a custom section")
	}
	var direct MenuItem
	for _, it := range share.Sub {
		if it.Label == "Share directly" {
			direct = it
		}
	}
	if direct.Do == nil {
		t.Fatal("no Share directly row")
	}
	direct.Do()
	m, ok := topOverlay[*popups.ShareSectionModal](e.a)
	if !ok {
		t.Fatal("Share directly opened no share dialog")
	}
	e.settle(100 * time.Millisecond)
	shareable := m.Shareable()
	if len(shareable) < 2 {
		t.Fatalf("fewer than two shareable channels in %v: %v", sec.All, shareable)
	}
	if !slices.Equal(m.Selected(), shareable) {
		t.Errorf("selected %v, want every shareable channel %v", m.Selected(), shareable)
	}
	if m.CanShare() {
		t.Error("Share is enabled with no one to share with")
	}

	refused := shareable[0]
	var mu sync.Mutex
	var validated, posted url.Values
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		mu.Lock()
		defer mu.Unlock()
		switch method {
		case "users.channelSections.validateShare":
			validated = form
			return map[string]any{"ok": true, "validation_errors": []any{
				map[string]any{"channel_id": refused, "error": "actor_cannot_invite_member"}}}, true
		case "chat.postMessage":
			posted = form
		}
		return nil, false
	})
	who := ""
	for id, c := range e.a.model().Convs {
		if c.Kind == "im" && c.User != "" && c.User != e.a.model().SelfID && strings.HasPrefix(id, "D") {
			who = c.User
			break
		}
	}
	if who == "" {
		t.Fatal("fakeslack has no DM partner")
	}
	m.AddPerson(who)
	e.until("the destination validated", 10*time.Second, func() bool { return m.Destination() != "" && !m.Busy() })
	mu.Lock()
	if validated == nil || validated.Get("target_channel") != m.Destination() || validated.Get("channels") != strings.Join(shareable, ",") {
		t.Errorf("validateShare form %v, want target %s and channels %v", validated, m.Destination(), shareable)
	}
	mu.Unlock()
	if errs := m.ChannelErrors()[refused]; len(errs) != 1 || errs[0] != "actor_cannot_invite_member" {
		t.Errorf("refusals %v", m.ChannelErrors())
	}
	want := shareable[1:]
	if !slices.Equal(m.Selected(), want) {
		t.Errorf("selected %v after the refusal, want %v", m.Selected(), want)
	}
	m.Toggle(refused)
	if slices.Contains(m.Selected(), refused) {
		t.Error("a refused channel could be ticked")
	}
	if !m.CanShare() {
		t.Fatal("Share is disabled with a destination and channels")
	}
	e.shot("share-section-dialog")

	m.SetNote("Here are our channels")
	m.Share()
	if m.Confirming() {
		m.Confirm()
	}
	e.until("the share was posted", 10*time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return posted != nil })
	mu.Lock()
	form := posted
	mu.Unlock()
	if form.Get("channel") != m.Destination() {
		t.Errorf("posted to %q, want %q", form.Get("channel"), m.Destination())
	}
	var att []struct {
		Blocks []struct {
			Type     string   `json:"type"`
			Name     string   `json:"channel_set_name"`
			Channels []string `json:"channel_ids"`
			Emoji    string   `json:"emoji"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(form.Get("attachments")), &att); err != nil || len(att) != 1 || len(att[0].Blocks) != 1 {
		t.Fatalf("attachments %q (%v)", form.Get("attachments"), err)
	}
	b := att[0].Blocks[0]
	if b.Type != "channel_section" || b.Name != sec.Name || b.Emoji != sec.Emoji || !slices.Equal(b.Channels, want) {
		t.Errorf("channel_section block %+v, want %q %q %v", b, sec.Name, sec.Emoji, want)
	}
	if !strings.Contains(form.Get("blocks"), "Here are our channels") || form.Get("text") != "Here are our channels" {
		t.Errorf("note: text %q blocks %s", form.Get("text"), form.Get("blocks"))
	}
	e.until("the dialog closed and Clack jumped to the message", 10*time.Second, func() bool {
		return m.Dismissed() && strings.HasPrefix(string(e.a.router.Current()), "c/"+form.Get("channel"))
	})
}

// The People field turns the picks into one conversation; more than
// eight others is Slack's "Only 9 people can be in a direct message."
// A section with nothing shareable says so.
func TestShareSectionLimits(t *testing.T) {
	e, _ := previewEnv(t)
	var sec *sbSection
	e.until("a custom section with channels", 20*time.Second, func() bool {
		sec = e.a.sectionByID("L0FAKE01")
		return sec != nil && len(sec.All) > 0
	})
	m := popups.NewShareSectionModal(e.a, popups.ShareSectionOpts{Name: sec.Name, Channels: sec.All})
	e.a.ShowOverlay(m)
	for i := 1; i <= 9; i++ {
		m.AddPerson("U0LIMIT0" + string(rune('0'+i)))
	}
	if got := m.DestinationError(); got != "Only 9 people can be in a direct message." {
		t.Errorf("nine people: %q", got)
	}
	if m.CanShare() {
		t.Error("Share is enabled past the group DM limit")
	}

	var dms []string
	for id, c := range e.a.model().Convs {
		if c.Kind == "im" {
			dms = append(dms, id)
		}
	}
	none := popups.NewShareSectionModal(e.a, popups.ShareSectionOpts{Name: "DMs", Channels: dms})
	if len(none.Shareable()) != 0 {
		t.Errorf("DMs are shareable: %v", none.Shareable())
	}
	e.a.ShowOverlay(none)
	e.settle(100 * time.Millisecond)
	e.shot("share-section-empty")
}
