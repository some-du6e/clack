package popups

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
)

// Synthetic fixtures only.

// ccBooted is a booted host on a fake Slack whose calls rec records.
func ccBooted(t *testing.T) (*tzHost, *fakeslack.Server, *emCalls) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	rec := &emCalls{calls: map[string][]url.Values{}}
	fs.SetHook(emHook(t, rec))
	h := hcEnvOn(t, fs)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- h.a.Boot(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("boot: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("boot timed out")
	}
	return h, fs, rec
}

func (h *tzHost) navList() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.navs...)
}

// topModal is the last overlay shown, if it's a modal.
func (h *tzHost) topModal() *Modal {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.shown) == 0 {
		return nil
	}
	m, _ := h.shown[len(h.shown)-1].(*Modal)
	return m
}

func ccCard(interact bool, cardID string) *richtext.ContactCard {
	return &richtext.ContactCard{UserID: fakeslack.ProfileID, CardID: cardID, Member: richtext.ContactMember{Name: "robin", CanInteract: interact}}
}

// Message: the DM straight away when the viewer can interact with the
// member, else contacts.add accepts the card first; a refused card is
// ContactActionErrorModal with the member's sentence.
func TestContactCardMessage(t *testing.T) {
	h, fs, rec := ccBooted(t)
	ContactCardAction(h, "message", ccCard(true, "CC0FAKEPROF"))
	maUntil(t, "the DM", func() bool { return len(h.navList()) == 1 })
	if n := h.navList()[0]; !strings.HasPrefix(n, "c/D") {
		t.Errorf("navigated to %q, want the DM", n)
	}
	rec.wait(t, "conversations.open", map[string]string{"users": fakeslack.ProfileID}, 10*time.Second)
	if adds := fs.ContactAdds(); len(adds) != 0 {
		t.Errorf("contacts.add for a member the viewer can talk to: %v", adds)
	}
	ContactCardAction(h, "message", ccCard(false, "CC0FAKEPROF"))
	rec.wait(t, "contacts.add", map[string]string{"contact_card_id": "CC0FAKEPROF", "reason": "contact_card_message"}, 10*time.Second)
	maUntil(t, "the DM after contacts.add", func() bool { return len(h.navList()) == 2 })
	ContactCardAction(h, "message", ccCard(false, fakeslack.ContactCardRefused))
	maUntil(t, "the error modal", func() bool { m := h.topModal(); return m != nil && m.o.Title == "Something’s gone wrong…" })
	if got := spansText(contactErrorText(ccCard(false, ""), "message", core.ContactCannotAddCard)); got != "You need to be in a channel with robin to message them." {
		t.Errorf("message refused: %q", got)
	}
	if got := spansText(contactErrorText(ccCard(false, ""), "save_info", core.ContactUserIsBlocked)); got != "So you can’t save robin’s contact info. But you can DM them if you are both in the same channel." {
		t.Errorf("save refused: %q", got)
	}
	if got := spansText(contactErrorText(ccCard(false, ""), "message", "fatal_error")); got != "If the problem persists, please drop us a line." {
		t.Errorf("other error: %q", got)
	}
}

func spansText(ss []richtext.Span) string {
	var b strings.Builder
	for _, s := range ss {
		b.WriteString(s.Text)
	}
	return b.String()
}

// Save contact: contacts.add, then "Saved contact" unless its dismissal
// is recorded; Got It with the box ticked records it.
func TestContactCardSave(t *testing.T) {
	h, fs, rec := ccBooted(t)
	ContactCardAction(h, "save_info", ccCard(false, "CC0FAKEPROF"))
	rec.wait(t, "contacts.add", map[string]string{"contact_card_id": "CC0FAKEPROF", "reason": "contact_card_save_info"}, 10*time.Second)
	maUntil(t, "Saved contact", func() bool { m := h.topModal(); return m != nil && m.o.Title == "Saved contact" })
	m := h.topModal()
	if len(m.o.Buttons) != 2 || !m.o.Buttons[0].Left || m.o.Buttons[1].Label != "Got It" {
		t.Fatalf("buttons %+v", m.o.Buttons)
	}
	m.o.Buttons[0].Do() // tick "Don’t show me this again"
	m.o.Buttons[1].Do()
	rec.wait(t, "users.interactions.set", map[string]string{"component": core.SaveContactModalComp, "type": core.InteractionDismiss}, 10*time.Second)
	// dismissed: no modal the next time
	shown := h.shownCount()
	fs.SetInteraction(core.SaveContactModalComp, core.InteractionDismiss, 1)
	ContactCardAction(h, "save_info", ccCard(false, "CC0FAKEPROF"))
	maUntil(t, "the second contacts.add", func() bool { return len(fs.ContactAdds()) == 2 })
	time.Sleep(200 * time.Millisecond)
	drain()
	if h.shownCount() != shown {
		t.Errorf("Saved contact again after its dismissal")
	}
}

// Share contact: "Share contact" with one destination; Share posts the
// card (an attachment with hide_color and one contact_card block) and the
// note, then opens the conversation.
func TestShareContact(t *testing.T) {
	h, _, rec := ccBooted(t)
	f := newShareContact(h, ccCard(true, "CC0FAKEPROF"))
	if got := f.title(); got != "Share contact" {
		t.Errorf("title %q", got)
	}
	if len(f.all) == 0 {
		t.Fatal("no destinations")
	}
	f.match = []int{0, 1}
	f.pick(0)
	f.match = []int{1}
	f.pick(0)
	if len(f.chips) != 1 {
		t.Fatalf("%d destinations, want one", len(f.chips))
	}
	ch := f.chips[0].t.id
	f.note.SetText("meet robin")
	f.shareContact()
	post := rec.wait(t, "chat.postMessage", map[string]string{"text": "meet robin"}, 10*time.Second)
	var att []struct {
		HideColor bool `json:"hide_color"`
		Blocks    []struct {
			Type          string `json:"type"`
			ContactUserID string `json:"contact_user_id"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal([]byte(post.Get("attachments")), &att); err != nil || len(att) != 1 || !att[0].HideColor || len(att[0].Blocks) != 1 ||
		att[0].Blocks[0].Type != "contact_card" || att[0].Blocks[0].ContactUserID != fakeslack.ProfileID {
		t.Errorf("attachments %s (%v)", post.Get("attachments"), err)
	}
	want := "c/" + ch
	if strings.HasPrefix(ch, "U") || strings.HasPrefix(ch, "W") {
		want = "c/D" // a person: their DM
	}
	maUntil(t, "the conversation", func() bool { n := h.navList(); return len(n) == 1 && strings.HasPrefix(n[0], want) })
}
