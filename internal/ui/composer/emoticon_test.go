package composer

import (
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
)

func TestLastEmoticon(t *testing.T) {
	for _, c := range []struct {
		in   string
		at   int
		want string
	}{
		{"hi :)", 3, ":)"}, {":D", 0, ":D"}, {"a:)", -1, ""}, {"so :/ ok :(", 9, ":("}, {"x :-) y", 2, ":-)"},
		{"8)", 0, "8)"}, {"<3", 0, "<3"}, {"see :)!", 4, ":)"}, {"url:/x", -1, ""},
	} {
		at, s := lastEmoticon([]rune(c.in))
		if at != c.at || s != c.want {
			t.Errorf("%q: %d %q, want %d %q", c.in, at, s, c.at, c.want)
		}
	}
}

// With convert_emoticons on, Space after ":D" turns it into :smile:; off,
// or with emoji shown as text, it stays.
func TestEmoticonOnSpace(t *testing.T) {
	t.Cleanup(uipref.Reset)
	mk := func() *Editor {
		e := &Editor{}
		e.init()
		e.emoji = func(name string) (*Atom, bool) { return &Atom{Kind: AtomEmoji, ID: name}, true }
		return e
	}
	run := func(p store.Prefs) *Editor {
		uipref.Set(p)
		e := mk()
		for _, r := range "ok :D " {
			e.typeAt(e.caret, e.caret, string(r))
		}
		return e
	}
	e := run(store.Prefs{ConvertEmoticons: true})
	if a := e.doc.Attrs[3].Atom; a == nil || a.ID != "smile" || e.doc.Len() != 5 {
		t.Fatalf("not converted: %q %+v", string(e.doc.Runes), e.doc.Attrs[3].Atom)
	}
	if e.caret != 5 {
		t.Fatalf("caret %d", e.caret)
	}
	for _, p := range []store.Prefs{{}, {ConvertEmoticons: true, MorePrefs: store.MorePrefs{EmojiAsText: true}}} {
		if e := run(p); e.doc.Attrs[3].Atom != nil || string(e.doc.Runes) != "ok :D " {
			t.Fatalf("%+v converted: %q", p, string(e.doc.Runes))
		}
	}
}
