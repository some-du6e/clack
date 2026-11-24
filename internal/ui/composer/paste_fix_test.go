package composer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// jsJSON re-encodes Go's JSON the way QuickJS's JSON.stringify writes it:
// & < > as they are, where Go escapes them (& < >).
func jsJSON(t *testing.T, ops json.RawMessage) json.RawMessage {
	t.Helper()
	var v any
	if err := json.Unmarshal(ops, &v); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return bytes.TrimSpace(b.Bytes())
}

// cleaned is what a plugin like Clear URLs answers for ops, written as
// QuickJS writes them: utm_source=x gone from every text and link, and
// whether that changed anything (Taut's JS answers null for the same ops).
func cleaned(t *testing.T, ops json.RawMessage) (json.RawMessage, bool) {
	in := jsJSON(t, ops)
	out := json.RawMessage(strings.ReplaceAll(string(in), "utm_source=x&", ""))
	return out, !bytes.Equal(out, in)
}

const partyParrot = "https://emoji.slack-edge.com/T0266FRGM/partyparrot/1.gif"

// pasteDoc is a paste with what a Quill delta doesn't carry or Go escapes:
// & < > in its text, a custom emoji with its image, an Unlink run, a link
// Clear URLs cleans, a list line, and the quoteTail of Slack's copy.
func pasteDoc() *Doc {
	d := NewDoc()
	end := d.Insert(0, "R&D <b> -> ", Attr{})
	end = d.InsertAtom(end, &Atom{Kind: AtomEmoji, ID: "partyparrot", Text: ":partyparrot:", URL: partyParrot})
	end = d.Insert(end, " ", Attr{})
	end = d.Insert(end, "https://example.com/?a=1&b=2", Attr{Style: Unlink})
	end = d.Insert(end, " see ", Attr{})
	start := end
	end = d.Insert(end, "https://example.com/?utm_source=x&id=7", Attr{})
	d.SetLink(start, end, "https://example.com/?utm_source=x&id=7")
	d.Insert(end, "\nsecond line && more", Attr{})
	d.Lines[1].Kind = Bullet
	d.quoteTail = true
	return d
}

// docIndex is where s starts in d's runes, or -1.
func docIndex(d *Doc, s string) int {
	want := []rune(s)
	for i := 0; i+len(want) <= len(d.Runes); i++ {
		if slices.Equal(d.Runes[i:i+len(want)], want) {
			return i
		}
	}
	return -1
}

// atomURLs is every emoji atom's image URL in d.
func atomURLs(d *Doc) []string {
	var out []string
	for _, a := range d.Attrs {
		if a.Atom != nil && a.Atom.Kind == AtomEmoji {
			out = append(out, a.Atom.URL)
		}
	}
	return out
}

// keptAndCleaned checks that got is pasteDoc's paste with its link
// cleaned, after prefix: its emoji's image, its Unlink run and the
// cleaned link kept, quoteTail as tail, and its lines when lines is set.
func keptAndCleaned(t *testing.T, what string, got *Doc, prefix string, tail, lines bool) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: no Doc back", what)
		return
	}
	if want := prefix + strings.Replace(pasteDoc().Text(), "utm_source=x&", "", 1); got.Text() != want {
		t.Errorf("%s: text %q, want %q", what, got.Text(), want)
	}
	if u := atomURLs(got); len(u) != 1 || u[0] != partyParrot {
		t.Errorf("%s: the custom emoji's image: %q, want %s", what, u, partyParrot)
	}
	if i := docIndex(got, "https://example.com/?a=1&b=2"); i < 0 || got.Attrs[i].Style&Unlink == 0 || got.Attrs[i].Link != "" {
		t.Errorf("%s: the Unlink run lost its Unlink (at %d)", what, i)
	}
	if i := docIndex(got, "https://example.com/?id=7"); i < 0 || got.Attrs[i].Link != "https://example.com/?id=7" {
		t.Errorf("%s: the cleaned link isn't linked to itself (at %d)", what, i)
	}
	if got.quoteTail != tail {
		t.Errorf("%s: quoteTail %v, want %v", what, got.quoteTail, tail)
	}
	if lines && (len(got.Lines) != 2 || got.Lines[0].Kind != Para || got.Lines[1].Kind != Bullet) {
		t.Errorf("%s: lines %+v, want a paragraph and a bullet", what, got.Lines)
	}
}

// A paste a plugin hands back as it was goes in as it was, whatever JSON
// escaping its ops crossed: Go escapes & < > where QuickJS doesn't, and
// comparing bytes called it changed and rebuilt it from the ops, losing
// what they don't carry (the Unlink, the custom emoji's image, quoteTail).
// A plugin that changes one op gets only that op rebuilt; one that
// reshapes the delta gets it read back whole, still with those.
func TestPasteUnchangedOpsKeepTheirDoc(t *testing.T) {
	src := pasteDoc()
	if d := throughDelta(src.Clone(), func(ops json.RawMessage) (json.RawMessage, bool) {
		out := jsJSON(t, ops)
		return out, !bytes.Equal(out, ops) // its bytes differ: & < >
	}); d != nil {
		t.Errorf("an untouched paste with & < > came back rebuilt as %q (emoji %q): it must go in as it was", d.Text(), atomURLs(d))
	}

	got := throughDelta(src.Clone(), func(ops json.RawMessage) (json.RawMessage, bool) { return cleaned(t, ops) })
	keptAndCleaned(t, "a link cleaned", got, "", true, true)

	got = throughDelta(src.Clone(), func(ops json.RawMessage) (json.RawMessage, bool) {
		out, _ := cleaned(t, ops)
		return json.RawMessage(`[{"insert":"> "},` + string(out[1:])), true // an op more
	})
	keptAndCleaned(t, "the delta reshaped", got, "> ", true, true)
}

// A large paste a plugin changes is read back in linear time and memory,
// when it changed some of the ops and when it reshaped the delta (read
// back whole): Delta.push merging a run of plain lines one op at a time
// copied the whole run for each, GBs for a paste of 5,000 lines, all on
// the UI goroutine.
func TestPasteRebuildIsLinear(t *testing.T) {
	const lines = 5000
	var b strings.Builder
	for i := range lines {
		fmt.Fprintf(&b, "line %04d https://example.com/?utm_source=x&n=%04d tail\n", i, i)
	}
	src := NewDoc()
	src.Insert(0, b.String(), Attr{})
	runes := src.Len()
	for _, c := range []struct {
		name    string
		prepare func(ops json.RawMessage) (json.RawMessage, bool)
	}{
		{"some ops changed", func(ops json.RawMessage) (json.RawMessage, bool) { return cleaned(t, ops) }},
		{"the delta reshaped", func(ops json.RawMessage) (json.RawMessage, bool) {
			out, _ := cleaned(t, ops)
			return json.RawMessage(`[{"insert":"> "},` + string(out[1:])), true
		}},
	} {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		got := throughDelta(src, c.prepare)
		took := time.Since(start)
		runtime.ReadMemStats(&after)
		if got == nil || !strings.Contains(got.Text(), "line 4999 https://example.com/?n=4999 tail") || strings.Contains(got.Text(), "utm_") {
			t.Errorf("%s: the paste wasn't read back", c.name)
			continue
		}
		alloc := after.TotalAlloc - before.TotalAlloc
		t.Logf("%s: %d runes, %.1f MB allocated, %v", c.name, runes, float64(alloc)/(1<<20), took)
		// a few hundred bytes a rune: the ops as JSON both ways, the Docs
		if limit := uint64(runes) * 400; alloc > limit {
			t.Errorf("%s: %.1f MB allocated for a %d-rune paste, past %.1f MB: its read back isn't linear", c.name, float64(alloc)/(1<<20), runes, float64(limit)/(1<<20))
		}
	}
}

// Only the editors that are Texty's Quill in Slack paste through a
// clipboard plugin, each with its kind: one that isn't (a canvas's or a
// list cell's, Quip's editor in Slack) pastes as it is, never as a
// channel composer's paste; the topic input's paste is "other".
func TestPasteHookOnlyInTextyInputs(t *testing.T) {
	var kinds []string
	t.Cleanup(setPreparePaste(func(kind string, src *Doc, formats map[string]any) *Doc {
		kinds = append(kinds, kind)
		return nil
	}))
	ed := NewEditor() // no TautKind: a canvas's or a list cell's
	ed.paste("https://example.com/?utm_source=x")
	if len(kinds) != 0 {
		t.Errorf("an editor that isn't a Texty input pasted through the clipboard as %q", kinds)
	}
	ed = NewEditor()
	setTautKind(ed, "thread")
	ed.paste("https://example.com/?utm_source=x")
	in := newTestTexty("")
	in.ed.paste("https://example.com/?utm_source=x")
	if strings.Join(kinds, ",") != "thread,other" {
		t.Errorf("a thread composer's paste and the topic input's reached the clipboard as %q, want thread, other", kinds)
	}
}

// The composer's own copy (Texty's slack/texty clipboard) and a rich HTML
// paste go through a clipboard plugin as Slack's pastes do, and keep what
// it left: the own copy its emoji's image and its Unlink, the HTML its
// formats, both the link it cleaned. An HTML paste it leaves goes in as
// it does without a plugin.
func TestPasteOwnCopyAndRichThroughClipboard(t *testing.T) {
	var asked, answer *Doc
	t.Cleanup(setPreparePaste(func(kind string, src *Doc, formats map[string]any) *Doc {
		asked = src
		answer = throughDelta(src, func(ops json.RawMessage) (json.RawMessage, bool) { return cleaned(t, ops) })
		return answer
	}))
	ed := NewEditor()
	setTautKind(ed, "channel")
	src := pasteDoc()
	sl := src.Slice(0, src.Len()) // what copySel keeps for the editor's own paste
	ed.clipText, ed.clipDoc = sl.copyText(), sl
	ed.paste(ed.clipText)
	if asked == nil || len(atomURLs(asked)) != 1 {
		t.Fatalf("the hook wasn't asked with the own copy (%v)", asked)
	}
	keptAndCleaned(t, "the own copy's answer", answer, "", false, false)
	keptAndCleaned(t, "the own copy pasted", ed.Doc(), "", false, false)

	const html = `<b>R&amp;D &lt;b&gt;</b> see <a href="https://example.com/?utm_source=x&amp;id=7">https://example.com/?utm_source=x&amp;id=7</a>`
	ed = NewEditor()
	setTautKind(ed, "channel")
	ed.pasteHTML("R&D <b> see https://example.com/?utm_source=x&id=7", html)
	rich := ed.Doc()
	if want := "R&D <b> see https://example.com/?id=7"; rich.Text() != want {
		t.Errorf("rich paste: %q, want %q", rich.Text(), want)
	}
	if i := docIndex(rich, "R&D <b>"); i < 0 || rich.Attrs[i].Style&Bold == 0 || rich.Attrs[i+6].Style&Bold == 0 {
		t.Errorf("rich paste: the bold run lost its bold (at %d)", i)
	}
	if i := docIndex(rich, "https://example.com/?id=7"); i < 0 || rich.Attrs[i].Link != "https://example.com/?id=7" {
		t.Errorf("rich paste: the cleaned link (at %d)", i)
	}

	const left = `<b>a &amp; b</b> -&gt; <i>c &lt; d</i> <a href="https://example.com/?a=1&amp;b=2">x</a>`
	want := NewEditor() // not a composer's: no hook
	want.pasteHTML("a & b -> c < d x", left)
	ed = NewEditor()
	setTautKind(ed, "channel")
	ed.pasteHTML("a & b -> c < d x", left)
	if !docsEqual(ed.Doc(), want.Doc()) {
		t.Errorf("an HTML paste the plugin left went in as %q %+v, want %q %+v", ed.Doc().Text(), ed.Doc().Attrs, want.Doc().Text(), want.Doc().Attrs)
	}
}
