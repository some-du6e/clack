package popups

import (
	"context"
	"image"
	"net/url"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

func ccEnv(t *testing.T) (*tzHost, *fakeslack.Server) {
	t.Helper()
	oldD, oldR := ccDebounce, ccRevealErr
	ccDebounce, ccRevealErr = 5*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { ccDebounce, ccRevealErr = oldD, oldR })
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &tzHost{a: a, th: theme.New(false), trees: map[uint64][]byte{}}, fs
}

// ccFrame lays o out once (and runs posted results).
func ccFrame(o Overlay) {
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1440, 900)), Now: time.Now()}
	o.Layout(gtx)
}

func ccUntil(t *testing.T, o Overlay, what string, cond func() bool) {
	t.Helper()
	dl := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(dl) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
		Pump()
		ccFrame(o)
	}
}

func TestCreateChannelModalSteps(t *testing.T) {
	h, fs := ccEnv(t)
	m := NewCreateChannelModal(h, CreateChannelOpts{})
	ccFrame(m)
	if m.Step() != "name" || m.canNext() {
		t.Fatalf("starts on %s, next enabled %v", m.Step(), m.canNext())
	}
	if f := m.modal.o.FooterNote; f != "Step 1 of 2" {
		t.Errorf("footer %q", f)
	}

	// Slack's input rewrites each edit
	m.SetName("#Hello World")
	if m.Name() != "hello-world" || m.ed.Text() != "hello-world" {
		t.Fatalf("normalized to %q (editor %q)", m.Name(), m.ed.Text())
	}
	if m.canNext() {
		t.Error("Next enabled before the name was checked")
	}
	ccUntil(t, m, "check", func() bool { return !m.checking })
	if m.NameError() != "" || !m.canNext() {
		t.Fatalf("free name: error %q next %v", m.NameError(), m.canNext())
	}

	// server errors, in Slack's words
	for name, want := range map[string]string{
		"a.b":                   "Channel names can’t contain spaces or punctuation. Use dashes to separate words.",
		"__":                    "Channel names can’t contain spaces or punctuation. Use dashes to separate words.",
		strings.Repeat("z", 81): "Channel names can’t be longer than 80 characters.",
		ccTaken(t, h, fs):       "That name is already taken by a channel, username, or user group in Fake Club.",
	} {
		m.SetName(name)
		ccUntil(t, m, "check "+name, func() bool { return !m.checking })
		if got := m.NameError(); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
		if m.canNext() {
			t.Errorf("%q: Next enabled", name)
		}
		m.Next()
		if m.Step() != "name" {
			t.Fatalf("%q: Next moved on", name)
		}
	}
	m.SetName("")
	ccUntil(t, m, "check empty", func() bool { return !m.checking })
	if got := m.NameError(); !strings.HasPrefix(got, "Give your channel a name") {
		t.Errorf("empty: %q", got)
	}

	// a stale answer never wins: only the last edit's check counts
	n := fs.CallCount("conversations.validateName")
	m.SetName("first-try")
	m.SetName("ship-it")
	ccUntil(t, m, "check ship-it", func() bool { return !m.checking })
	if m.NameError() != "" || m.Name() != "ship-it" {
		t.Fatalf("after two edits: %q %q", m.Name(), m.NameError())
	}
	if d := fs.CallCount("conversations.validateName") - n; d != 1 {
		t.Errorf("%d checks for two quick edits, want 1 (debounced)", d)
	}

	m.Next()
	ccFrame(m)
	if m.Step() != "visibility" || m.modal.o.FooterNote != "Step 2 of 2" || m.modal.o.Subtitle != "ship-it" || m.modal.o.SubtitleIcon != "hash" {
		t.Fatalf("visibility step: %s %+v", m.Step(), m.modal.o)
	}
	if labels := ccLabels(m.modal.o.Buttons); labels != "Back,Create" {
		t.Errorf("buttons %s", labels)
	}
	m.Back()
	if m.Step() != "name" || m.Name() != "ship-it" {
		t.Errorf("Back: %s %q", m.Step(), m.Name())
	}
	m.Next()
	m.SetPrivate(true)
	if m.modal.o.SubtitleIcon != "lock" {
		t.Error("private: no lock in the header")
	}
	var created core.Conv
	m.OnCreated = func(c core.Conv) { created = c }
	m.Create()
	ccUntil(t, m, "create", func() bool { return created.ID != "" })
	if created.Kind != "private" || created.Name != "ship-it" || !m.Dismissed() {
		t.Errorf("created %+v, modal closed %v", created, m.Dismissed())
	}
	if len(h.navs) == 0 || h.navs[len(h.navs)-1] != "c/"+created.ID {
		t.Errorf("navigated %v", h.navs)
	}
	var add *AddPeopleModal
	for _, o := range h.shown {
		if p, ok := o.(*AddPeopleModal); ok {
			add = p
		}
	}
	if add == nil {
		t.Fatal("no Add people step")
	}
	if add.modal.o.Title != "Add people to " || add.modal.o.TitleIcon != "lock" || add.modal.o.TitleAfter != "ship-it" {
		t.Errorf("add people title %+v", add.modal.o)
	}
	ccFrame(add)
	if b := strings.Join(add.Buttons(), ","); b != "Skip for now" {
		t.Errorf("buttons %s", b)
	}
	add.search("u")
	ccUntil(t, add, "results", func() bool { return len(add.results) > 0 })
	for _, u := range add.results {
		if u.ID == fakeslack.SelfID {
			t.Error("self offered")
		}
	}
	first := add.results[0]
	add.Pick(first)
	add.Pick(first) // twice: once
	if len(add.Picked()) != 1 || strings.Join(add.Buttons(), ",") != "Add" {
		t.Fatalf("picked %d, buttons %v", len(add.Picked()), add.Buttons())
	}
	ccFrame(add)
	add.Add()
	ccUntil(t, add, "invite", func() bool { return add.Dismissed() })
	if got := fs.Invited(created.ID); len(got) != 1 {
		t.Errorf("invited %v", got)
	}
}

// Create failures replace the content with Slack's message and leave only
// "Okay", which closes.
func TestCreateChannelModalCreateError(t *testing.T) {
	h, fs := ccEnv(t)
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method == "conversations.create" {
			return map[string]any{"ok": false, "error": "restricted_action"}, true
		}
		return nil, false
	})
	m := NewCreateChannelModal(h, CreateChannelOpts{Name: "launch"})
	ccUntil(t, m, "check", func() bool { return !m.checking })
	m.Next()
	m.Create()
	ccUntil(t, m, "error", func() bool { return m.CreateError() != "" })
	if !strings.HasPrefix(m.CreateError(), "Your organization’s settings have changed") {
		t.Errorf("error %q", m.CreateError())
	}
	if l := ccLabels(m.modal.o.Buttons); l != "Okay" || m.modal.o.FooterNote != "" {
		t.Errorf("footer %s %q", l, m.modal.o.FooterNote)
	}
	ccFrame(m)
	m.modal.press(m.okay)
	if !m.Dismissed() {
		t.Error("Okay didn't close")
	}
}

func ccTaken(t *testing.T, h *tzHost, fs *fakeslack.Server) string {
	c, ok := h.a.Conv(fs.ChannelIDs()[0])
	if !ok || c.Name == "" {
		t.Fatal("no channel to collide with")
	}
	return c.Name
}

func ccLabels(bs []*ModalButton) string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Label)
	}
	return strings.Join(out, ",")
}
