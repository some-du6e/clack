package canvasview

import (
	"image"
	"strings"
	"testing"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/composer"
)

// emojiHost is the test host with the shell's ":" typeahead (prefix
// matches over the standard set).
type emojiHost struct{ *testHost }

func (h emojiHost) CanvasEmoji(q string, limit int) []EmojiHit {
	var out []EmojiHit
	for _, e := range emoji.StandardList() {
		for _, n := range e.Names {
			if strings.HasPrefix(n, q) {
				u, _, _ := h.a.Emoji(n)
				out = append(out, EmojiHit{Name: n, URL: u})
				break
			}
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

// Quip's emoji rules in the canvas editor.
func TestEmojiTriggerRules(t *testing.T) {
	for _, c := range []struct {
		text string
		q    string
		ok   bool
	}{
		{"hi :ta", "ta", true},
		{":ta", "ta", true},
		{"hi :t", "", false}, // one character: no typeahead
		{"a:ta", "", false},  // a word before the colon
		{"(:ta", "ta", true}, // allowed punctuation before it
		{"hi :t.a", "", false},
		{"hi ：ta", "ta", true}, // the full-width colon
	} {
		rs := []rune(c.text)
		_, q, ok := emojiTrigger(rs, len(rs))
		if ok != c.ok || q != c.q {
			t.Errorf("%q: %q %v, want %q %v", c.text, q, ok, c.q, c.ok)
		}
	}
	for _, c := range []struct {
		text, name string
		ok         bool
	}{
		{"go :tada:", "tada", true},
		{":tada:", "tada", true},
		{"x:tada:", "", false},
		{"12:30:", "", false},
		{"see https://e.example/a:b:", "", false},
		{"(:+1::skin-tone-3:", "+1::skin-tone-3", true},
	} {
		rs := []rune(c.text)
		_, name, ok := typedEmoji(rs, len(rs))
		if ok != c.ok || name != c.name {
			t.Errorf("typed %q: %q %v, want %q %v", c.text, name, ok, c.name, c.ok)
		}
	}
}

// The canvas title takes emoji the way Slack's title section does: ":ta"
// opens the emoji typeahead, picking one inserts the emoji; typing a
// whole ":tada:" turns it into the emoji on the closing colon; the title
// is saved with the emoji (canvases.edit markdown ":tada:", which the
// canvas stores as an emoji control); title emoji draw at the title's
// 34px line (control.c54 img), body emoji at 22px. Light and dark shots.
func TestTitleEmoji(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		th, fs := newEnv(t, dark)
		h := emojiHost{th}
		v := New(h, fakeslack.CanvasChannel, Full, Actions{})
		s := newShooter(t, image.Pt(1100, 700))
		frame := func() { s.frame(func(gtx layout.Context) { v.Layout(gtx) }) }
		settle(t, s, v, "the canvas", func() bool { return v.Doc() != nil })
		frame()
		if v.styles.title.EmojiSize != titleLine || v.styles.body.EmojiSize != bodyLine {
			t.Fatalf("emoji sizes title %v body %v, want the lines' %d and %d", v.styles.title.EmojiSize, v.styles.body.EmojiSize, titleLine, bodyLine)
		}
		if !titleSection(v.blocks) {
			t.Fatal("the fixture canvas has no title section")
		}
		tb := v.blocks[0]
		id := tb.b.ID
		v.edit.start(v, tb)
		frame()
		ed := v.edit.cur.ed
		if st := editorStyle(h.th, canvas.Heading1, h); st.EmojiSize != h1Line {
			t.Errorf("an H1's editor emoji %v, want %d", st.EmojiSize, h1Line)
		}
		typ := func(s string) {
			for _, r := range s {
				ed.InsertText(string(r))
				frame()
			}
		}
		end := ed.Doc().Len()
		ed.SetSelection(end, end)
		frame()
		// the typeahead
		typ(" :ta")
		if !v.edit.pick.active() || v.edit.pick.trig != ':' || v.edit.pick.items[0].kind != composer.AtomEmoji {
			t.Fatalf("no emoji typeahead after \":ta\": %+v", v.edit.pick)
		}
		s.shot("canvas-title-emoji-typeahead-"+mode, s.frame(func(gtx layout.Context) { v.Layout(gtx) }))
		pick := v.edit.pick.items[0].id
		v.edit.pick.accept, v.edit.pick.acceptSet = 0, true
		frame()
		// a typed shortcode
		typ(" :tada:")
		var atoms []string
		d := ed.Doc()
		for i := range d.Runes {
			if a := d.Attrs[i].Atom; a != nil && a.Kind == composer.AtomEmoji {
				atoms = append(atoms, a.ID)
			}
		}
		if len(atoms) < 3 || atoms[len(atoms)-2] != pick || atoms[len(atoms)-1] != "tada" {
			t.Fatalf("emoji atoms %q, want the title's own, %q, then tada", atoms, pick)
		}
		if strings.Contains(string(d.Runes), ":tada:") {
			t.Error(":tada: stayed text")
		}
		s.shot("canvas-title-emoji-editing-"+mode, s.frame(func(gtx layout.Context) { v.Layout(gtx) }))
		v.edit.finish(v)
		settle(t, s, v, "the saved title", func() bool {
			b := fakeBlock(t, fs, id)
			if b == nil {
				return false
			}
			n := 0
			for _, sp := range b.Spans {
				if sp.Kind == richtext.Emoji && (sp.ID == "tada" || sp.ID == pick) {
					n++
				}
			}
			return n >= 2
		})
		md := lastChange(t, fs)["document_content"].(map[string]any)["markdown"].(string)
		if !strings.HasPrefix(md, "# ") || !strings.HasSuffix(md, ":"+pick+": :tada:") {
			t.Errorf("saved title %q", md)
		}
		frame()
		s.shot("canvas-title-emoji-"+mode, s.frame(func(gtx layout.Context) { v.Layout(gtx) }))
	}
}
