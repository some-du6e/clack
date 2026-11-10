package composer

import (
	"encoding/json"
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Unicode emoji typed (one input event, as the OS emoji picker and IMEs
// send them), committed or pasted become emoji atoms named after the
// standard emoji, the way Slack's composer swaps native emoji for its
// emoji blots (replaceNativeEmoji), and are sent as emoji elements.

// edInput is one input event of several runes at the caret.
func edInput(e *Editor, s string) { e.typeAt(e.caret, e.caret, s) }

func TestNativeEmojiTyped(t *testing.T) {
	e := emojiEditor(nil)
	edType(e, "hi ")
	edInput(e, "😀")
	edType(e, " ")
	edInput(e, "👍🏽")
	edType(e, " ")
	edInput(e, "👨‍👩‍👧‍👦")
	edType(e, " ")
	edInput(e, "❤️")
	edType(e, " ok")
	d := e.Doc()
	wantRunes(t, d, "hi ￼ ￼ ￼ ￼ ok")
	got := strings.Join(atomsOf(d), ",")
	if want := "grinning,+1::skin-tone-4,man-woman-girl-boy,heart"; got != want {
		t.Fatalf("atoms = %s, want %s", got, want)
	}
	for _, i := range []int{3, 5, 7, 9} {
		if a := d.Attrs[i].Atom; a.URL == "" || a.Uni == "" {
			t.Errorf("atom %d = %+v, want Slack's image and the unicode", i, *a)
		}
	}
	if _, c := e.Selection(); c != d.Len() {
		t.Errorf("caret %d, want the end %d", c, d.Len())
	}
	// Undo brings the typed emoji text back first (its own step).
	e2 := emojiEditor(nil)
	edInput(e2, "😀")
	e2.Undo()
	wantRunes(t, e2.Doc(), "😀")
}

// Text-default characters stay text unless VS16 asks for the emoji, and
// code, code blocks and links keep emoji as typed (they draw as Slack's
// image over a blank slot, see TestComposerEmojiNoTofu).
func TestNativeEmojiStaysText(t *testing.T) {
	for _, s := range []string{"© 2026", "a ™ b", "x ❤ y", "plain"} {
		e := emojiEditor(nil)
		edInput(e, s)
		wantRunes(t, e.Doc(), s)
	}
	e := emojiEditor(nil)
	e.ToggleStyle(Code) // the toolbar's code button: typing goes in as code
	edInput(e, "a 😀")
	if atoms := atomsOf(e.Doc()); len(atoms) != 0 {
		t.Errorf("inline code converted %v", atoms)
	}
	e = emojiEditor(nil)
	edType(e, "```")
	edInput(e, "x 😀")
	if atoms := atomsOf(e.Doc()); len(atoms) != 0 {
		t.Errorf("code block converted %v", atoms)
	}
	// no lookup (canvas and Block Kit editors): nothing converts
	e = NewEditor()
	edInput(e, "😀")
	wantRunes(t, e.Doc(), "😀")
}

func TestNativeEmojiPasted(t *testing.T) {
	e := emojiEditor(map[string]string{"sho": shoURL})
	e.paste("ship it 🚀🚀 :sho: 🇺🇸\nnext 👍🏻")
	d := e.Doc()
	wantRunes(t, d, "ship it ￼￼ ￼ ￼\nnext ￼")
	if got, want := strings.Join(atomsOf(d), ","), "rocket,rocket,sho,us,+1::skin-tone-2"; got != want {
		t.Fatalf("atoms = %s, want %s", got, want)
	}
	if _, c := e.Selection(); c != d.Len() {
		t.Errorf("caret %d, want the end %d", c, d.Len())
	}
}

// An IME composition is left alone until it is committed, then its emoji
// convert (Slack's emoji module checks isInComposition).
func TestNativeEmojiAfterComposition(t *testing.T) {
	e := emojiEditor(nil)
	e.ime.composition = key.Range{Start: 0, End: 1}
	e.typeAt(0, 0, "😀")
	wantRunes(t, e.Doc(), "😀")
	// the window ends the composition
	was := e.ime.composition
	e.ime.composition = key.Range{Start: -1, End: -1}
	if was.End > was.Start && !e.composing() {
		e.convertEmoji(was.Start, was.End)
	}
	wantRunes(t, e.Doc(), "￼")
}

// The caret steps over an emoji atom in one press, Shift+arrow selects
// it whole, and Backspace/Delete remove it whole.
func TestNativeEmojiCaretSelectDelete(t *testing.T) {
	e := emojiEditor(nil)
	edType(e, "a")
	edInput(e, "👨‍👩‍👧‍👦")
	edType(e, "b")
	wantRunes(t, e.Doc(), "a￼b")
	e.SetSelection(1, 1)
	edPress(e, key.NameRightArrow, 0)
	if _, c := e.Selection(); c != 2 {
		t.Fatalf("→ from before the emoji: caret %d, want 2", c)
	}
	edPress(e, key.NameLeftArrow, key.ModShift)
	if a, c := e.Selection(); a != 2 || c != 1 {
		t.Fatalf("⇧← selection %d..%d, want 2..1", a, c)
	}
	if got := e.SelectedText(); got != "👨‍👩‍👧‍👦" {
		t.Errorf("selected text %q, want the family", got)
	}
	edPress(e, key.NameDeleteBackward, 0)
	wantRunes(t, e.Doc(), "ab")
	e.Undo()
	wantRunes(t, e.Doc(), "a￼b")
	e.SetSelection(1, 1)
	edPress(e, key.NameDeleteForward, 0)
	wantRunes(t, e.Doc(), "ab")
}

// What is sent: Slack's emoji elements (createEmojiChunkFromOp): a
// standard emoji is its base name plus its unicode (the tone lives in the
// unicode), the mrkdwn fallback its shortcode.
func TestNativeEmojiBlocks(t *testing.T) {
	e := emojiEditor(nil)
	edType(e, "hi ")
	edInput(e, "😀 👍🏽 ❤️")
	var got []map[string]any
	if err := json.Unmarshal(e.Doc().RichTextBlocks(), &got); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got[0]["elements"])
	want := `[{"elements":[{"text":"hi ","type":"text"},` +
		`{"name":"grinning","type":"emoji","unicode":"1f600"},{"text":" ","type":"text"},` +
		`{"name":"+1","type":"emoji","unicode":"1f44d-1f3fd"},{"text":" ","type":"text"},` +
		`{"name":"heart","type":"emoji","unicode":"2764-fe0f"}],"type":"rich_text_section"}]`
	if string(b) != want {
		t.Errorf("blocks\n%s\nwant\n%s", b, want)
	}
	if m := e.Doc().Mrkdwn(); m != "hi :grinning: :+1::skin-tone-4: :heart:" {
		t.Errorf("mrkdwn %q", m)
	}
	// prepareSend converts emoji that arrived without a lookup pass
	d := NewDoc()
	d.Insert(0, "go 🚀", Attr{})
	look, _ := testEmoji(nil)
	convertNativeEmojiRange(d, 0, d.Len(), look)
	if got := strings.Join(atomsOf(d), ","); got != "rocket" {
		t.Errorf("send-time conversion: %s", got)
	}
}

// recImgs serves a 1x1 image for every URL and records the URLs.
type recImgs struct {
	mu   sync.Mutex
	urls []string
}

func (r *recImgs) get(url string, _ int) (paint.ImageOp, bool) {
	r.mu.Lock()
	r.urls = append(r.urls, url)
	r.mu.Unlock()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	return paint.NewImageOp(img), true
}

func (r *recImgs) asked(stem string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.urls {
		if strings.HasSuffix(u, "/"+stem+".png") {
			return true
		}
	}
	return false
}

// The composer draws no tofu: emoji atoms are images, and unicode emoji
// left as text (inline code, editors without a lookup) shape as blank
// slots with Slack's image drawn over them.
func TestComposerEmojiNoTofu(t *testing.T) {
	sh := theme.NewShaper()
	st := DefaultEditorStyle(sh)
	imgs := &recImgs{}
	st.Images = imgs.get
	layoutEd := func(e *Editor) {
		var ops op.Ops
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 2, PxPerSp: 2}, Constraints: layout.Exact(image.Pt(1200, 200))}
		e.Layout(gtx, st)
	}

	// message composer: typed emoji are atoms
	e := emojiEditor(nil)
	edType(e, "hi ")
	edInput(e, "😱 🧑🏽‍💻 ok")
	stop := theme.StartTofuScan()
	layoutEd(e)
	s := stop()
	if s.Glyphs == 0 || s.Notdef > 0 || s.Fallback > 0 {
		t.Fatalf("composer tofu scan: %+v", s)
	}
	for _, stem := range []string{"1f631", "1f9d1-1f3fd-200d-1f4bb"} {
		if !imgs.asked(stem) {
			t.Errorf("image %s never drawn (%v)", stem, imgs.urls)
		}
	}

	// inline code keeps the emoji as text: a blank slot under the image.
	// (Code text itself is the system monospace off macOS, which the scan
	// counts as Fallback: only the emoji is checked here.)
	e = emojiEditor(nil)
	e.ToggleStyle(Code)
	edInput(e, "code 🚀")
	stop = theme.StartTofuScan()
	layoutEd(e)
	if s := stop(); s.Notdef > 0 || s.Slots != 1 {
		t.Fatalf("inline code scan: %+v, want the 🚀 as one blank slot", s)
	}
	if !imgs.asked("1f680") {
		t.Errorf("inline code 🚀 image never drawn (%v)", imgs.urls)
	}

	// an editor without a lookup (canvas, Block Kit rich_text_input)
	e = NewEditor()
	edInput(e, "plan 🇺🇸 ❤️ 👍🏿 done")
	imgs.urls = nil
	stop = theme.StartTofuScan()
	layoutEd(e)
	s = stop()
	if s.Notdef > 0 || s.Fallback > 0 || s.Slots != 3 {
		t.Fatalf("raw emoji scan: %+v, want 3 blank slots and no tofu", s)
	}
	for _, stem := range []string{"1f1fa-1f1f8", "2764-fe0f", "1f44d-1f3ff"} {
		if !imgs.asked(stem) {
			t.Errorf("raw emoji image %s never drawn (%v)", stem, imgs.urls)
		}
	}
	// one caret stop per emoji: → crosses the whole flag
	e.SetSelection(5, 5)
	edPress(e, key.NameRightArrow, 0)
	if _, c := e.Selection(); c != 7 {
		t.Errorf("→ over a raw flag: caret %d, want 7", c)
	}
}

// A ":" pick from the picker's search carries the skin tone in use: the
// atom is the toned emoji and is sent as Slack sends it.
func TestEmojiMatchItemsTone(t *testing.T) {
	its := emojiMatchItems([]popups.EmojiMatch{
		{Name: "wave", Toned: "wave::skin-tone-3", URL: "https://x/1f44b-1f3fc.png"},
		{Name: "sho", Toned: "sho", URL: shoURL, Custom: true},
	})
	if len(its) != 2 {
		t.Fatalf("items %+v", its)
	}
	w := its[0].Atom
	if w.ID != "wave::skin-tone-3" || w.Text != ":wave::skin-tone-3:" || w.Uni != "👋🏼" || its[0].Name != "wave" {
		t.Errorf("toned atom %+v (item name %q)", *w, its[0].Name)
	}
	if s := its[1].Atom; s.ID != "sho" || s.Uni != "" || s.URL != shoURL {
		t.Errorf("custom atom %+v", *s)
	}
	e := NewEditor()
	edType(e, ":wav")
	applyPick(e, its[0], 0, 4)
	b, _ := json.Marshal(rtAtomOut(e.Doc().Attrs[0].Atom))
	if string(b) != `{"type":"emoji","name":"wave","unicode":"1f44b-1f3fc"}` {
		t.Errorf("sent as %s", b)
	}
}
