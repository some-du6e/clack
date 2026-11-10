package composer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/store"
)

// Live markup and Slack's "Format messages with markup" pref
// (plain_text_mode). Off (Slack's default) the composer formats as you
// type; on, every marker stays as typed.

type markupCase struct {
	name, typed string
	// live result: runes, the style of rune at, and each line's kind
	runes string
	at    int
	style Style
	kinds []BlockKind
}

var markupCases = []markupCase{
	{"code", "`x` ", "x ", 0, Code, []BlockKind{Para}},
	{"bold", "*x* ", "x ", 0, Bold, []BlockKind{Para}},
	{"italic", "_x_ ", "x ", 0, Italic, []BlockKind{Para}},
	{"strike", "~x~ ", "x ", 0, Strike, []BlockKind{Para}},
	{"quote", "> x", "x", 0, 0, []BlockKind{Quote}},
	{"bullet", "- x", "x", 0, 0, []BlockKind{Bullet}},
	{"ordered", "1. x", "x", 0, 0, []BlockKind{Ordered}},
	{"fence at start", "```x", "x", 0, 0, []BlockKind{CodeBlock}},
	{"fence mid-line", "hello ```dsd", "hello\ndsd", 6, 0, []BlockKind{Para, CodeBlock}},
	{"fence mid-line, no space", "hi```x", "hi\nx", 3, 0, []BlockKind{Para, CodeBlock}},
}

func lineKinds(d *Doc) []BlockKind {
	out := make([]BlockKind, len(d.Lines))
	for i, l := range d.Lines {
		out[i] = l.Kind
	}
	return out
}

func sameKinds(a, b []BlockKind) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLiveMarkupPrefOff(t *testing.T) {
	for _, tc := range markupCases {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEditor()
			edType(e, tc.typed)
			d := e.Doc()
			wantRunes(t, d, tc.runes)
			if got := d.Attrs[tc.at].Style; got != tc.style {
				t.Errorf("style at %d = %v, want %v", tc.at, got, tc.style)
			}
			if got := lineKinds(d); !sameKinds(got, tc.kinds) {
				t.Errorf("line kinds = %v, want %v", got, tc.kinds)
			}
		})
	}
}

func TestLiveMarkupPrefOn(t *testing.T) {
	for _, tc := range markupCases {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEditor()
			e.plainText = true
			edType(e, tc.typed)
			d := e.Doc()
			wantRunes(t, d, tc.typed)
			for i, a := range d.Attrs {
				if a.Style != 0 {
					t.Fatalf("rune %d styled %v under markup mode", i, a.Style)
				}
			}
			if got := lineKinds(d); !sameKinds(got, []BlockKind{Para}) {
				t.Errorf("line kinds = %v, want one paragraph", got)
			}
		})
	}
}

// TestFenceMidLine is the reported case: "hello ```" opens a code block
// below "hello" with the caret inside; text typed next is code, undo
// brings the backticks back.
func TestFenceMidLine(t *testing.T) {
	e := NewEditor()
	edType(e, "hello ```")
	d := e.Doc()
	wantRunes(t, d, "hello\n")
	if _, car := e.Selection(); car != 6 {
		t.Fatalf("caret = %d, want 6 (inside the code block)", car)
	}
	if e.ActiveBlock() != CodeBlock {
		t.Errorf("active block = %v, want CodeBlock (toolbar's code block button)", e.ActiveBlock())
	}
	edType(e, "dsd")
	if got := lineKinds(e.Doc()); !sameKinds(got, []BlockKind{Para, CodeBlock}) {
		t.Errorf("kinds = %v", got)
	}
	e.Undo()
	e.Undo()
	if got := string(e.Doc().Runes); got != "hello ```" {
		t.Errorf("after undo: %q, want the literal fence", got)
	}

	// Typed in the middle of a line: the rest moves into the code block,
	// without the styles it had.
	e = NewEditor()
	edType(e, "ab *cd* ef")
	e.SetSelection(3, 3) // after "ab "
	edType(e, "```")
	d = e.Doc()
	wantRunes(t, d, "ab\ncd ef")
	if got := lineKinds(d); !sameKinds(got, []BlockKind{Para, CodeBlock}) {
		t.Errorf("kinds = %v", got)
	}
	for i := 3; i < d.Len(); i++ {
		if d.Attrs[i].Style != 0 {
			t.Errorf("code block rune %d keeps style %v", i, d.Attrs[i].Style)
		}
	}

	// Backticks inside inline code are text, not a fence.
	e = NewEditor()
	d = NewDoc()
	d.Insert(0, "x ``", Attr{Style: Code})
	e.SetDoc(d)
	edType(e, "`")
	if got := lineKinds(e.Doc()); !sameKinds(got, []BlockKind{Para}) {
		t.Errorf("fence inside inline code: %v", got)
	}
}

// TestMarkupModeSend: under "Format messages with markup" the typed markup
// goes out formatted (Slack's send parses it), pills and emoji intact.
// With the pref off, literal markers that reached the doc stay literal.
func TestMarkupModeSend(t *testing.T) {
	t.Cleanup(func() { plainTextLocal = nil })
	s := store.New("")
	d := NewDoc()
	pos := d.Insert(0, "*bold* _it_ ~st~ `c` hi ", Attr{})
	pos = d.InsertAtom(pos, &Atom{Kind: AtomUser, ID: "UALICE", Text: "@alice"})
	pos = d.Insert(pos, " ", Attr{})
	pos = d.InsertAtom(pos, emojiAtomFrom("smile", "https://e.example/s.png", "😄"))
	d.Insert(pos, "\n> quoted\n```\ncode *x*\n```", Attr{})
	out := parseMarkup(d, s)
	blocks := string(out.RichTextBlocks())
	for _, want := range []string{
		`{"type":"text","text":"bold","style":{"bold":true}}`,
		`{"type":"text","text":"it","style":{"italic":true}}`,
		`{"type":"text","text":"st","style":{"strike":true}}`,
		`{"type":"text","text":"c","style":{"code":true}}`,
		`{"type":"user","user_id":"UALICE"}`,
		`{"type":"emoji","name":"smile","unicode":"1f604"}`,
		`"type":"rich_text_quote"`,
		`"type":"rich_text_preformatted"`,
		`{"type":"text","text":"code *x*"}`,
	} {
		if !strings.Contains(blocks, want) {
			t.Errorf("blocks %s\nmissing %s", blocks, want)
		}
	}

	// end to end through the composer and the fake chat.postMessage
	for _, on := range []bool{true, false} {
		c, fs, _ := composerEnv(t, nil)
		SetPlainTextMode(on)
		c.ed.plainText = on
		edType(c.ed, "*hey* there")
		c.submit()
		_, blocks := lastPost(t, fs)
		bold := strings.Contains(blocks, `{"type":"text","text":"hey","style":{"bold":true}}`)
		literal := strings.Contains(blocks, `*hey* there`)
		if on && (!bold || literal) {
			t.Errorf("markup mode sent %s, want bold \"hey\"", blocks)
		}
		if !on && !bold {
			t.Errorf("live mode sent %s, want bold (formatted while typing)", blocks)
		}
	}
	plainTextLocal = nil

	// Pref off: markers that stayed literal (typed with a space inside)
	// are sent as typed.
	c, fs, _ := composerEnv(t, nil)
	edType(c.ed, "2 * 3 * 4")
	c.submit()
	if _, blocks := lastPost(t, fs); !strings.Contains(blocks, `"2 * 3 * 4"`) {
		t.Errorf("literal stars changed: %s", blocks)
	}
}

// TestMarkupModeFollowsSlackPref: with no local choice, Slack's
// plain_text_mode decides; the local choice wins once made.
func TestMarkupModeFollowsSlackPref(t *testing.T) {
	plainTextLocal = nil
	t.Cleanup(func() { plainTextLocal = nil })
	c, _, s := composerEnv(t, nil)
	if PlainTextMode(c.api.a) {
		t.Fatal("markup mode on by default")
	}
	s.SetPrefs(store.Prefs{PlainTextMode: true}, nil)
	if !PlainTextMode(c.api.a) {
		t.Error("Slack's plain_text_mode ignored")
	}
	SetPlainTextMode(false)
	if PlainTextMode(c.api.a) {
		t.Error("the local choice should win")
	}
	if p, _ := store.ParsePrefs(map[string]json.RawMessage{}); p.PlainTextMode {
		t.Error("plain_text_mode defaults on")
	}
}

// TestMarkupModePaste: under markup mode a paste is text (no HTML
// formatting, no list detection); live mode keeps paste-rich's formatting.
func TestMarkupModePaste(t *testing.T) {
	for _, on := range []bool{false, true} {
		e := NewEditor()
		e.plainText = on
		e.pasteHTML("- one\n- two", "<ul><li>one</li><li>two</li></ul>")
		d := e.Doc()
		list := d.Lines[0].Kind == Bullet
		if on && (list || string(d.Runes) != "- one\n- two") {
			t.Errorf("markup mode paste = %q kinds %v, want the text as is", string(d.Runes), lineKinds(d))
		}
		if !on && !list {
			t.Errorf("live paste = %q kinds %v, want a list", string(d.Runes), lineKinds(d))
		}
	}
}
