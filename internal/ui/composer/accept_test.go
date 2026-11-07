package composer

import (
	"testing"

	"gioui.org/io/key"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
)

// These tests cover "accept a suggestion, then keep typing" (the round-4
// reorder bug: "@karim : I'll type… now"). In the app, the autocomplete
// state is refreshed once per frame, after the editor has drained the
// frame's input. A fast typist (or a slow software-GL frame) can deliver
// the last query letter, Tab/Enter and the next words in one batch. The
// fix applies the pick inside the Tab/Enter event against the live
// document, so the following keys land after the pill.

// acBound returns an editor and popup wired like the composer, with
// synthetic sources (no core).
func acBound(t *testing.T) (*Editor, *Autocomplete) {
	t.Helper()
	e := NewEditor()
	ac := newAutocomplete(nil, "C1")
	ac.known = map[string]core.User{"U1": {ID: "U1", Name: "karim", RealName: "Karim Hacker"}}
	ac.convs = []core.Conv{{ID: "C9", Name: "general", Kind: "channel"}}
	ac.commands = []core.Command{{Name: "/remind"}}
	ac.emojiIdx = buildEmojiIndex(nil, map[string]string{"smile": "https://emoji.example/smile.png"})
	ac.gen++
	bindAutocomplete(e, ac, nil)
	return e, ac
}

// acFrame is the end-of-frame refresh the composer runs after the editor.
func acFrame(e *Editor, ac *Autocomplete) { ac.Update(e.Doc(), caretOf(e)) }

// acCheck asserts the document text, the atom at pos and a caret at the end.
func acCheck(t *testing.T, e *Editor, want string, pos int, kind AtomKind, id string) {
	t.Helper()
	d := e.Doc()
	if got := d.Text(); got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if pos >= 0 {
		if pos >= d.Len() || d.Attrs[pos].Atom == nil {
			t.Fatalf("no atom at %d (runes %q)", pos, string(d.Runes))
		}
		if a := d.Attrs[pos].Atom; a.Kind != kind || a.ID != id {
			t.Fatalf("atom at %d = %+v, want kind %d id %q", pos, *a, kind, id)
		}
	}
	if _, car := e.Selection(); car != d.Len() {
		t.Fatalf("caret = %d, want end %d (text %q)", car, d.Len(), d.Text())
	}
}

func TestAcceptMentionThenTypeSameBatch(t *testing.T) {
	e, ac := acBound(t)
	edType(e, "hi @kari")
	acFrame(e, ac) // popup open for "kari"
	if !ac.Open() {
		t.Fatal("popup should be open")
	}
	// One input batch: the last query letter, Tab, then more words.
	edType(e, "m")
	edPress(e, key.NameTab, 0)
	edType(e, "now")
	acFrame(e, ac)
	acCheck(t, e, "hi @karim now", 3, AtomUser, "U1")
	// Later typing keeps going at the end (the reported symptom put it
	// before the earlier words).
	edType(e, ": rest")
	acFrame(e, ac)
	acCheck(t, e, "hi @karim now: rest", 3, AtomUser, "U1")
	if ac.Open() {
		t.Error("popup reopened after the pick")
	}
}

func TestAcceptMentionWithEnterThenType(t *testing.T) {
	e, ac := acBound(t)
	edType(e, "@karim")
	acFrame(e, ac)
	edPress(e, key.NameReturn, 0) // accepts; must not submit
	if edHas(e, EvSubmit) {
		t.Fatal("Enter on an open popup must accept, not send")
	}
	edType(e, "hello")
	acCheck(t, e, "@karim hello", 0, AtomUser, "U1")
}

// TestAcceptBeforeFirstFrame: the popup never rendered (Tab arrived in
// the same batch as the trigger); Tab still accepts the top match like
// Slack, whose popup updates synchronously as you type.
func TestAcceptBeforeFirstFrame(t *testing.T) {
	e, _ := acBound(t)
	edType(e, "@kari")
	edPress(e, key.NameTab, 0)
	edType(e, "x")
	acCheck(t, e, "@karim x", 0, AtomUser, "U1")
}

// TestAcceptMentionIMEEdits drives the editor with key.EditEvents whose
// ranges come from the window's own (stale) model of the text, as the X11
// and Wayland input methods send them.
func TestAcceptMentionIMEEdits(t *testing.T) {
	e, ac := acBound(t)
	win := 0 // the window's caret model; it only learns ours via SelectionCmd
	imeType := func(s string) {
		for _, r := range s {
			e.onEdit(key.EditEvent{Range: key.Range{Start: win, End: win}, Text: string(r)})
			win++
		}
	}
	imeType("ok @kar")
	acFrame(e, ac)
	// Batch: "im", Tab, " now" with no SelectionCmd in between.
	imeType("im")
	edPress(e, key.NameTab, 0)
	imeType("now")
	acCheck(t, e, "ok @karim now", 3, AtomUser, "U1")
}

func TestAcceptEmojiThenTypeSameBatch(t *testing.T) {
	e, ac := acBound(t)
	edType(e, "ok :sm")
	acFrame(e, ac)
	if !ac.Open() {
		t.Fatal("emoji popup should be open")
	}
	edType(e, "i")
	edPress(e, key.NameTab, 0)
	edType(e, "great")
	acCheck(t, e, "ok :smile: great", 3, AtomEmoji, "smile")
}

func TestAcceptChannelThenTypeSameBatch(t *testing.T) {
	e, ac := acBound(t)
	edType(e, "see #ge")
	acFrame(e, ac)
	if !ac.Open() {
		t.Fatal("channel popup should be open")
	}
	edType(e, "n")
	edPress(e, key.NameTab, 0)
	edType(e, "now")
	acCheck(t, e, "see #general now", 4, AtomChannel, "C9")
}

func TestAcceptCommandThenTypeSameBatch(t *testing.T) {
	e, ac := acBound(t)
	edType(e, "/re")
	acFrame(e, ac)
	if !ac.Open() {
		t.Fatal("command popup should be open")
	}
	edType(e, "m")
	edPress(e, key.NameTab, 0)
	edType(e, "me")
	acCheck(t, e, "/remind me", -1, 0, "")
}

// TestAcceptMentionTwiceInOneMessage: two picks in one message, the second
// before its popup ever rendered.
func TestAcceptMentionTwiceInOneMessage(t *testing.T) {
	e, ac := acBound(t)
	edType(e, "@ka")
	acFrame(e, ac)
	edType(e, "r")
	edPress(e, key.NameTab, 0)
	edType(e, "and #ge")
	edPress(e, key.NameTab, 0) // popup never framed for "#ge"
	edType(e, "ok")
	acFrame(e, ac)
	acCheck(t, e, "@karim and #general ok", 0, AtomUser, "U1")
	if a := e.Doc().Attrs[6].Atom; a == nil || a.ID != "C9" {
		t.Fatalf("channel pill missing at 6: runes %q", string(e.Doc().Runes))
	}
}
