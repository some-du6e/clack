package composer

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"

	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// Typed ":name:" shortcodes become emoji atoms on the closing colon, with
// no autocomplete involved (the reported bug: ":sho:" typed quickly stayed
// text and was sent as text).

const shoURL = "https://emoji.example/sho.png"

// testEmoji is the composer's lookup over a synthetic custom set, resolved
// the way the store does (emoji.Custom: custom map, aliases, standard).
func testEmoji(custom map[string]string) (emojiLookup, *emoji.Custom) {
	c := new(emoji.Custom)
	c.Set(custom)
	return func(name string) (*Atom, bool) {
		url, txt, ok := c.Resolve(name, "")
		if !ok {
			return nil, false
		}
		return emojiAtomFrom(name, url, txt), true
	}, c
}

func emojiEditor(custom map[string]string) *Editor {
	e := NewEditor()
	e.emoji, _ = testEmoji(custom)
	return e
}

// atomsOf lists the emoji atom ids in d, in order.
func atomsOf(d *Doc) []string {
	var out []string
	for _, a := range d.Attrs {
		if a.Atom != nil && a.Atom.Kind == AtomEmoji {
			out = append(out, a.Atom.ID)
		}
	}
	return out
}

func wantRunes(t *testing.T, d *Doc, want string) {
	t.Helper()
	if got := string(d.Runes); got != want {
		t.Fatalf("runes = %q, want %q", got, want)
	}
}

func TestTypedCustomEmoji(t *testing.T) {
	e := emojiEditor(map[string]string{"sho": shoURL})
	edType(e, "hi :sho: there")
	d := e.Doc()
	wantRunes(t, d, "hi ￼ there")
	a := d.Attrs[3].Atom
	if a == nil || a.ID != "sho" || a.URL != shoURL || a.Text != ":sho:" || a.Uni != "" {
		t.Fatalf("atom = %+v", a)
	}
	if _, car := e.Selection(); car != d.Len() {
		t.Errorf("caret = %d, want the end %d", car, d.Len())
	}
	// Undo brings the literal shortcode back, like the other live formats.
	e2 := emojiEditor(map[string]string{"sho": shoURL})
	edType(e2, ":sho:")
	e2.Undo()
	wantRunes(t, e2.Doc(), ":sho:")
}

func TestTypedStandardEmojiAndAliases(t *testing.T) {
	e := emojiEditor(map[string]string{
		"sho":  shoURL,
		"shoo": "alias:sho", // custom alias of a custom emoji
		"yes":  "alias:+1",  // custom alias of a standard emoji
	})
	edType(e, ":smile: :thumbsup: :+1: :shoo: :yes:")
	d := e.Doc()
	wantRunes(t, d, "￼ ￼ ￼ ￼ ￼")
	got := atomsOf(d)
	want := []string{"smile", "thumbsup", "+1", "shoo", "yes"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("atoms = %v, want %v", got, want)
	}
	if u := d.Attrs[0].Atom.Uni; u != "😄" {
		t.Errorf(":smile: unicode = %q", u)
	}
	if u := d.Attrs[2].Atom.Uni; u != "👍" {
		t.Errorf(":thumbsup: unicode = %q", u)
	}
	if a := d.Attrs[6].Atom; a.URL != shoURL {
		t.Errorf(":shoo: url = %q, want sho's", a.URL)
	}
	if a := d.Attrs[8].Atom; a.Uni != "👍" {
		t.Errorf(":yes: (alias:+1) = %+v", *a)
	}
}

func TestTypedSkinTones(t *testing.T) {
	e := emojiEditor(nil)
	edType(e, ":thumbsup::skin-tone-3: :+1::skin-tone-2:")
	d := e.Doc()
	wantRunes(t, d, "￼ ￼")
	got := atomsOf(d)
	if strings.Join(got, ",") != "thumbsup::skin-tone-3,+1::skin-tone-2" {
		t.Fatalf("atoms = %v", got)
	}
	if u := d.Attrs[0].Atom.Uni; u != "👍🏼" {
		t.Errorf("skin-tone-3 unicode = %q", u)
	}
	if u := d.Attrs[2].Atom.Uni; u != "👍🏻" {
		t.Errorf("skin-tone-2 unicode = %q", u)
	}
	// A custom emoji has no tones: the tone stays its own emoji.
	e = emojiEditor(map[string]string{"sho": shoURL})
	edType(e, ":sho::skin-tone-4:")
	if got := atomsOf(e.Doc()); strings.Join(got, ",") != "sho,skin-tone-4" {
		t.Fatalf("custom + tone atoms = %v", got)
	}
}

func TestTypedUnknownStaysText(t *testing.T) {
	e := emojiEditor(map[string]string{"sho": shoURL})
	for _, s := range []string{":nope:", "at 12:30: ok", "a:sho: b", ":sh o:", "::", ":sho"} {
		e.SetDoc(nil)
		edType(e, s)
		wantRunes(t, e.Doc(), s)
	}
}

func TestTypedEmojiNotInCode(t *testing.T) {
	e := emojiEditor(nil)
	// inline code: the shortcode is typed inside a code span
	d := NewDoc()
	d.Insert(0, "x :smile", Attr{Style: Code})
	e.SetDoc(d)
	edType(e, ":")
	wantRunes(t, e.Doc(), "x :smile:")
	// a code block
	e.SetDoc(nil)
	e.Doc().Lines[0] = Line{Kind: CodeBlock}
	edType(e, ":smile:")
	wantRunes(t, e.Doc(), ":smile:")
	// pasted into a code block
	e.paste(" :+1:")
	wantRunes(t, e.Doc(), ":smile: :+1:")
}

// TestTypedFastBatch: the IME delivers ":sho: ok" as edits whose ranges
// are the window's model, which runs ahead of ours after a conversion; and
// as one multi-rune edit. Neither loses the emoji or reorders text.
func TestTypedFastBatch(t *testing.T) {
	e := emojiEditor(map[string]string{"sho": shoURL})
	for i, r := range []rune(":sho: ok") {
		e.onEdit(key.EditEvent{Range: key.Range{Start: i, End: i}, Text: string(r)})
	}
	wantRunes(t, e.Doc(), "￼ ok")

	e = emojiEditor(map[string]string{"sho": shoURL})
	e.onEdit(key.EditEvent{Text: "hey :sho: and :smile:"})
	wantRunes(t, e.Doc(), "hey ￼ and ￼")
	if _, car := e.Selection(); car != e.Doc().Len() {
		t.Errorf("caret = %d, want the end", car)
	}
}

// TestTypedWithAutocompleteOpen: the colon converts while the popup shows
// the query, and the popup closes.
func TestTypedWithAutocompleteOpen(t *testing.T) {
	e, ac := acBound(t)
	e.emoji, _ = testEmoji(map[string]string{"smile": "https://emoji.example/smile.png"})
	edType(e, "ok :smi")
	acFrame(e, ac)
	if !ac.Open() {
		t.Fatal("popup should be open")
	}
	edType(e, "le:")
	edType(e, " x")
	acFrame(e, ac)
	wantRunes(t, e.Doc(), "ok ￼ x")
	if ac.Open() {
		t.Error("popup still open")
	}
}

func TestPastedEmoji(t *testing.T) {
	e := emojiEditor(map[string]string{"sho": shoURL})
	e.paste("see :sho: and :nope: :+1::skin-tone-5:")
	wantRunes(t, e.Doc(), "see ￼ and :nope: ￼")
	if got := atomsOf(e.Doc()); strings.Join(got, ",") != "sho,+1::skin-tone-5" {
		t.Fatalf("atoms = %v", got)
	}
}

// TestLateCustomSet: ":sho:" typed before the custom set loaded stays text,
// then converts once the set has it, with the caret kept in place.
func TestLateCustomSet(t *testing.T) {
	e := NewEditor()
	look, set := testEmoji(nil)
	e.emoji = look
	edType(e, "a :sho: b :nope:")
	wantRunes(t, e.Doc(), "a :sho: b :nope:")
	e.SetSelection(9, 9) // after "b"
	set.Set(map[string]string{"sho": shoURL})
	if !e.ConvertEmoji() {
		t.Fatal("nothing converted")
	}
	wantRunes(t, e.Doc(), "a ￼ b :nope:")
	if _, car := e.Selection(); car != 5 {
		t.Errorf("caret = %d, want 5 (still after b)", car)
	}
	if e.ConvertEmoji() {
		t.Error("second scan changed something")
	}
}

func TestEmojiBlocks(t *testing.T) {
	e := emojiEditor(map[string]string{"sho": shoURL})
	edType(e, "hi :sho: :smile: :thumbsup::skin-tone-3:")
	var got []map[string]any
	if err := json.Unmarshal(e.Doc().RichTextBlocks(), &got); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got[0]["elements"])
	s := string(b)
	for _, want := range []string{
		`{"name":"sho","type":"emoji"}`,
		`{"name":"smile","type":"emoji","unicode":"1f604"}`,
		`{"name":"thumbsup","skin_tone":3,"type":"emoji","unicode":"1f44d-1f3fc"}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("blocks %s\nmissing %s", s, want)
		}
	}
	if m := e.Doc().Mrkdwn(); m != "hi :sho: :smile: :thumbsup::skin-tone-3:" {
		t.Errorf("text = %q", m)
	}
}

// composerEnv is a channel composer over the synthetic core.
func composerEnv(t *testing.T, custom map[string]string) (*Composer, *fakeSlack, *store.Store) {
	t.Helper()
	app, fs := syntheticApp(t, func(s *store.Store) { s.CustomEmoji.Set(custom) })
	stubTyping(t)
	c := New(&slickHost{app: app, th: theme.New(false)}, tChGen, "")
	t.Cleanup(c.Close)
	return c, fs, app.S
}

// lastPost waits for a chat.postMessage and returns its blocks' elements.
func lastPost(t *testing.T, fs *fakeSlack) (text, blocks string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(fs.called("chat.postMessage")) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no chat.postMessage")
		}
		time.Sleep(10 * time.Millisecond)
	}
	p := fs.called("chat.postMessage")
	f := p[len(p)-1]
	return f.Get("text"), f.Get("blocks")
}

// TestSendConvertsShortcodes: text that reached the composer without
// conversion (inserted, or undone) is converted on send, and the sent
// rich_text carries Slack's emoji elements.
func TestSendConvertsShortcodes(t *testing.T) {
	c, fs, _ := composerEnv(t, map[string]string{"sho": shoURL})
	d := NewDoc()
	end := d.Insert(0, "ship it :sho: :nope: ", Attr{})
	d.Insert(end, ":smile:", Attr{Style: Code})
	c.ed.SetDoc(d)
	c.submit()
	text, blocks := lastPost(t, fs)
	if !strings.Contains(blocks, `{"type":"emoji","name":"sho"}`) {
		t.Errorf("blocks %s: no sho emoji element", blocks)
	}
	if !strings.Contains(blocks, ` :nope: `) {
		t.Errorf("blocks %s: unknown name should stay text", blocks)
	}
	if !strings.Contains(blocks, `"text":":smile:","style":{"code":true}`) {
		t.Errorf("blocks %s: inline code should stay text", blocks)
	}
	if !strings.Contains(text, ":sho:") {
		t.Errorf("text = %q", text)
	}
}

// TestComposerLateEmojiSet: the composer rescans when the store's custom
// set changes (emoji.list landed after the user typed).
func TestComposerLateEmojiSet(t *testing.T) {
	c, _, s := composerEnv(t, nil)
	edType(c.ed, "late :sho:")
	wantRunes(t, c.ed.Doc(), "late :sho:")
	c.syncEmojiSet()
	wantRunes(t, c.ed.Doc(), "late :sho:")
	s.CustomEmoji.Set(map[string]string{"sho": shoURL})
	c.syncEmojiSet()
	wantRunes(t, c.ed.Doc(), "late ￼")
	// typing now converts at once
	edType(c.ed, " :sho:")
	if got := atomsOf(c.ed.Doc()); len(got) != 2 {
		t.Fatalf("atoms = %v", got)
	}
}
