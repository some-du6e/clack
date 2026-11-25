package popups

import (
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// raEnv is a fake Slack whose answers to conversations.rename,
// conversations.archive and conversations.validateName the test sets
// (answer), a core over it with #plans (C0PLANS001) and the private
// #secret (G0SECRET01) in the store, and the calls made.
type raEnv struct {
	h   *tzHost
	rec *emCalls
	mu  sync.Mutex
	ans map[string]map[string]any
}

func newRAEnv(t *testing.T) *raEnv {
	t.Helper()
	old := rnDebounce
	rnDebounce = 5 * time.Millisecond
	t.Cleanup(func() { rnDebounce = old })
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := &raEnv{rec: &emCalls{calls: map[string][]url.Values{}}, ans: map[string]map[string]any{}}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		e.rec.mu.Lock()
		e.rec.calls[method] = append(e.rec.calls[method], form)
		e.rec.mu.Unlock()
		e.mu.Lock()
		defer e.mu.Unlock()
		if a, ok := e.ans[method]; ok {
			return a, true
		}
		return nil, false
	})
	e.h = hcEnvOn(t, fs)
	e.h.a.S.PutConv(slack.Conversation{ID: "C0PLANS001", Name: "plans", IsChannel: true, IsMember: true})
	e.h.a.S.PutConv(slack.Conversation{ID: "G0SECRET01", Name: "secret", IsGroup: true, IsPrivate: true, IsMember: true})
	return e
}

func (e *raEnv) answer(method string, a map[string]any) {
	e.mu.Lock()
	e.ans[method] = a
	e.mu.Unlock()
}

// RenameChannelModal: the name given (or the channel's) in the input,
// Save Changes only once it differs and passes the check, the rename
// with validate, and Slack's failure texts at the top.
func TestRenameChannelModal(t *testing.T) {
	e := newRAEnv(t)
	m := NewRenameChannelModal(e.h, "C0PLANS001", "")
	ccFrame(m)
	if m.Title() != "Rename this channel" || m.Name() != "plans" || m.CanSave() {
		t.Fatalf("own name: title %q name %q save %v", m.Title(), m.Name(), m.CanSave())
	}
	if !m.Save() {
		t.Error("Save with the channel's own name doesn't just close")
	}

	m = NewRenameChannelModal(e.h, "C0PLANS001", "Q3 Plans")
	ccFrame(m)
	if m.Name() != "Q3 Plans" || !m.CanSave() {
		t.Fatalf("given name: %q, save %v (the name given stays as given until an edit)", m.Name(), m.CanSave())
	}
	// an edit is normalized and checked
	e.answer("conversations.validateName", map[string]any{"ok": false, "error": "name_taken"})
	m.SetName("#Big Plans")
	if m.Name() != "big-plans" {
		t.Fatalf("normalized to %q", m.Name())
	}
	ccUntil(t, m, "the check", func() bool { return m.NameError() != "" })
	if got := m.NameError(); got != "That name is already taken by a channel, username, or user group in this workspace." || m.CanSave() {
		t.Errorf("taken: %q, save %v", got, m.CanSave())
	}
	e.answer("conversations.validateName", map[string]any{"ok": true, "channel_name": "big-plans-2"})
	m.SetName("big-plans-2")
	ccUntil(t, m, "the second check", func() bool { return m.NameError() == "" && m.CanSave() })

	for code, want := range map[string]string{
		"name_taken":            "big-plans-2 is already taken by a channel, username, or user group.",
		"invalid_name_specials": "Channel names can’t support special characters (i.e. !#$%&).",
		"not_authorized":        "Oops, something went wrong with renaming the channel. Please try again.",
	} {
		e.answer("conversations.rename", map[string]any{"ok": false, "error": code})
		m.Save()
		ccUntil(t, m, code, func() bool { return m.Error() != "" && !m.saving })
		if got := m.Error(); got != want || m.Dismissed() {
			t.Errorf("%s: %q (dismissed %v)", code, got, m.Dismissed())
		}
	}
	e.answer("conversations.rename", map[string]any{"ok": true, "channel": map[string]any{"id": "C0PLANS001", "name": "big-plans-2", "is_channel": true}})
	m.Save()
	ccUntil(t, m, "the rename", m.Dismissed)
	p := e.rec.wait(t, "conversations.rename", map[string]string{"channel": "C0PLANS001", "name": "big-plans-2"}, time.Second)
	if p.Get("validate") != "true" {
		t.Errorf("conversations.rename %v", p)
	}
}

// ArchiveChannelModal: Archive Channel archives and closes; a failure
// stays open with ArchiveChannelModalContents' text for its code.
func TestArchiveChannelModal(t *testing.T) {
	e := newRAEnv(t)
	m := NewArchiveChannelModal(e.h, "G0SECRET01")
	ccFrame(m)
	if m.Title() != "Archive this channel?" {
		t.Fatalf("title %q", m.Title())
	}
	e.answer("conversations.archive", map[string]any{"ok": false, "error": "restricted_action"})
	m.Archive()
	ccUntil(t, m, "the refusal", func() bool { return m.Error() != "" })
	if got := m.Error(); got != "You don’t have permission to archive private channels. Talk to your Workspace Owner." || m.Dismissed() {
		t.Errorf("private, restricted: %q", got)
	}
	e.answer("conversations.archive", map[string]any{"ok": true})
	m.Archive()
	ccUntil(t, m, "the archive", m.Dismissed)
	if p := e.rec.wait(t, "conversations.archive", map[string]string{"channel": "G0SECRET01"}, time.Second); p == nil {
		t.Error("no conversations.archive")
	}
	for code, want := range map[string]string{
		"restricted_action":    "You don’t have permission to archive channels. Talk to your Workspace Owner.",
		"already_archived":     "This channel was already archived.",
		"cant_archive_general": "This channel cannot be archived.",
		"last_ra_channel":      "Sorry, you can’t archive this channel because it is the only channel one of the guest account members belongs to.",
		"weird":                "Archiving failed with error: weird",
	} {
		if got := ArchiveErrorText(code, false, false); got != want {
			t.Errorf("%s: %q", code, got)
		}
	}
}
