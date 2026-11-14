package composer

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Pasted links become Slack's link slugs (TextySlugs over chat.slugifyUrl).
// Karim, 2026-11-13: "really long text becomes like the message pill
// thingy wiht like … in the end". Slack's composer turns a pasted link
// into a slug labelled with the server's short form of the URL and sends
// that as the link's text with truncated: true, and messages draw it as
// a chip; Clack sent the whole URL. The labels here are live
// chat.slugifyUrl answers (2026-11-14, synthetic example.com URLs;
// fakeslack's slug_display_test.go has the rest), and the element is the
// shape of a link Slack's client sent (keys and lengths, read 2026-11-14:
// its text was chat.slugifyUrl's display_url for its URL).

// lsPad is prefix followed by "abcdefghij" repeated, n bytes in all.
func lsPad(prefix string, n int) string {
	b := []byte(prefix)
	for i := 0; len(b) < n; i++ {
		b = append(b, "abcdefghij"[i%10])
	}
	return string(b)
}

var lsURL = lsPad("https://example.com/docs/guides/", 300) // a 300-character link

const (
	lsLabel      = "example.com/docs/…/abcdefghijab…" // chat.slugifyUrl's display_url for lsURL
	lsShort      = "https://example.com/short"
	lsShortLabel = "example.com/short"
)

// lsTruncatedEl is the element Slack's composer sends for lsURL's slug
// (createSlugChunkFromOp's "link").
func lsTruncatedEl() string {
	return `{"type":"link","url":"` + lsURL + `","text":"` + lsLabel + `","truncated":true}`
}

// lsSlugReply is chat.slugifyUrl's answer: each URL a link slug with its
// display_url.
func lsSlugReply(pairs ...[2]string) map[string]any {
	slugs := map[string]any{}
	for _, p := range pairs {
		slugs[p[0]] = map[string]any{"slug_type": "link", "display_url": p[1]}
	}
	return map[string]any{"ok": true, "slugs": slugs}
}

// lsElements is the first section's inline elements of blocks.
func lsElements(t *testing.T, blocks json.RawMessage) []json.RawMessage {
	t.Helper()
	var bs []struct {
		Elements []struct {
			Elements []json.RawMessage `json:"elements"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(blocks, &bs); err != nil || len(bs) == 0 || len(bs[0].Elements) == 0 {
		t.Fatalf("blocks %s (%v)", blocks, err)
	}
	return bs[0].Elements[0].Elements
}

func TestLinkSlugSerialisation(t *testing.T) {
	at := linkAtom(lsURL, lsLabel)
	if at.Kind != AtomLink || at.ID != lsURL || at.URL != lsURL || at.Text != lsLabel {
		t.Fatalf("atom %+v", *at)
	}
	d := NewDoc()
	end := d.Insert(0, "see ", Attr{})
	d.InsertAtom(end, at)
	els := lsElements(t, d.RichTextBlocks())
	// createSlugChunkFromOp's "link": type, url, the label as text, truncated
	if got := string(els[len(els)-1]); got != lsTruncatedEl() {
		t.Errorf("element:\n got %s\nwant %s", got, lsTruncatedEl())
	}
	if m := d.Mrkdwn(); m != "see <"+lsURL+"|"+lsLabel+">" {
		t.Errorf("text %q", m)
	}
	// the Taut delta: the label with slackslug (formatLinkSlugAttributesForDelta)
	ops := d.Delta()["ops"].([]any)
	op := ops[1].(map[string]any)
	sl, _ := op["attributes"].(map[string]any)["slackslug"].(map[string]any)
	if op["insert"] != lsLabel || sl["type"] != "link" || sl["id"] != lsURL || sl["url"] != lsURL || sl["label"] != lsLabel {
		t.Errorf("delta op %v", op)
	}
	// getMessageLengthForDelta counts the label, as for a message slug
	if n, want := slackLength(d), jsLen("see ")+jsLen(lsLabel); n != want {
		t.Errorf("length %d, want %d", n, want)
	}
	// copying it out of the composer gives the URL (treatSlugsAsUrls)
	if got := d.copyText(); got != "see "+lsURL {
		t.Errorf("copy %q", got)
	}
	// no display_url: the URL is the label, and it goes out a plain link
	bare := linkAtom(lsShort, " ")
	if bare.Text != lsShort {
		t.Errorf("blank display_url: label %q", bare.Text)
	}
	d2 := NewDoc()
	d2.InsertAtom(0, bare)
	if got := string(lsElements(t, d2.RichTextBlocks())[0]); got != `{"type":"link","url":"`+lsShort+`"}` {
		t.Errorf("unlabelled slug %s", got)
	}
	if m := d2.Mrkdwn(); m != "<"+lsShort+">" {
		t.Errorf("unlabelled slug's text %q", m)
	}
	// labels are NFC, like every resolved slug's (canonicalizeUnicode)
	if a := linkAtom("https://example.com/cafe", "example.com/café"); a.Text != "example.com/café" {
		t.Errorf("label %q not NFC", a.Text)
	}
	// slugAtom: a link answer is a link slug; without its URL nothing
	if a := slugAtom(btResolver{}, core.Slug{Type: "link", DisplayURL: lsLabel}, lsURL); a == nil || a.Kind != AtomLink || a.Text != lsLabel || a.URL != lsURL {
		t.Errorf("slugAtom: %+v", a)
	}
	if a := slugAtom(btResolver{}, core.Slug{Type: "link", DisplayURL: lsLabel}, ""); a != nil {
		t.Errorf("slugAtom without a URL: %+v", a)
	}
}

// Every pasted link whose text is the URL is offered to chat.slugifyUrl,
// Slack's or not, once per paste (convertLinksToUnprocessedSlugs); the
// answers swap in for the links. Typed links, links with their own
// label, www. links (the text isn't the URL), code and the markup
// composer are left alone.
func TestPastedLinksBecomeLinkSlugs(t *testing.T) {
	e := NewEditor()
	var asked [][]string
	e.slugs.onPaste = func(urls []string) { asked = append(asked, urls) }
	e.paste("read " + lsURL + " and " + lsShort + " (or " + lsURL + ")")
	if len(asked) != 1 || strings.Join(asked[0], " ") != lsURL+" "+lsShort {
		t.Fatalf("asked %v, want each pasted link once", asked)
	}
	e.applySlugs(map[string]*Atom{lsURL: linkAtom(lsURL, lsLabel), lsShort: linkAtom(lsShort, lsShortLabel)}, asked[0])
	d := e.Doc()
	var labels []string
	for _, a := range d.Attrs {
		if a.Atom != nil && a.Atom.Kind == AtomLink {
			labels = append(labels, a.Atom.Text)
		}
	}
	if got := strings.Join(labels, "|"); got != lsLabel+"|"+lsShortLabel+"|"+lsLabel {
		t.Fatalf("slugs %q", got)
	}
	if got, want := d.Text(), "read "+lsLabel+" and "+lsShortLabel+" (or "+lsLabel+")"; got != want {
		t.Errorf("text %q, want %q", got, want)
	}
	if e.caret != d.Len() {
		t.Errorf("caret %d, want the end (%d)", e.caret, d.Len())
	}
	if b := string(d.RichTextBlocks()); strings.Count(b, `"truncated":true`) != 3 || strings.Contains(b, `"text":"`+lsURL) {
		t.Errorf("blocks %s", b)
	}
	e.Undo()
	if got := e.Doc().Text(); strings.Count(got, lsURL) != 2 || !strings.Contains(got, lsShort) {
		t.Errorf("undo: %q", got)
	}

	n := 0
	fresh := func() *Editor {
		e := NewEditor()
		e.slugs.onPaste = func([]string) { n++ }
		return e
	}
	edType(fresh(), lsURL+" ")
	fresh().pasteHTML("the guide", `<a href="`+lsURL+`">the guide</a>`)
	fresh().paste("www.example.com/" + strings.Repeat("w", 200))
	code := fresh()
	cd := NewDoc()
	cd.Insert(0, "x", Attr{})
	cd.Lines[0].Kind = CodeBlock
	code.SetDoc(cd)
	code.SetSelection(1, 1)
	code.paste(lsURL)
	markup := fresh()
	markup.plainText = true // "Format messages with markup": pastes go in as text
	markup.paste(lsURL)
	if n != 0 {
		t.Errorf("%d of the typed, labelled, www., code and markup links were offered to chat.slugifyUrl", n)
	}
}

// The whole composer: the pasted link goes to chat.slugifyUrl, the answer
// makes it a link slug, and the message carries Slack's truncated link.
func TestLinkSlugPasteSendsTruncatedLink(t *testing.T) {
	app, fs := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	fs.mu.Lock()
	fs.reply = map[string]any{"chat.slugifyUrl": lsSlugReply([2]string{lsURL, lsLabel})}
	fs.mu.Unlock()
	h := &ovHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	c := freshComposer(t, h)
	c.PasteText(lsURL)
	waitUntil(t, "the pasted link's slug", func() bool {
		runRestores()
		d := c.ed.Doc()
		return d.Len() == 1 && d.Attrs[0].Atom != nil && d.Attrs[0].Atom.Kind == AtomLink
	})
	calls := fs.called("chat.slugifyUrl")
	if len(calls) != 1 || calls[0].Get("urls") != `["`+lsURL+`"]` || calls[0].Get("reason") != "link-paste-slugify" || calls[0].Get("channel") != tChGen {
		t.Errorf("chat.slugifyUrl calls %v", calls)
	}
	if a := c.ed.Doc().Attrs[0].Atom; a.Text != lsLabel || a.URL != lsURL {
		t.Errorf("slug %+v", *a)
	}
	c.submit()
	waitUntil(t, "chat.postMessage", func() bool { return len(fs.called("chat.postMessage")) == 1 })
	post := fs.called("chat.postMessage")[0]
	if b := post.Get("blocks"); !strings.Contains(b, lsTruncatedEl()) {
		t.Errorf("blocks %s, want %s", b, lsTruncatedEl())
	}
	if text := post.Get("text"); text != "<"+lsURL+"|"+lsLabel+">" {
		t.Errorf("text %q", text)
	}
}

// Editing a message with a truncated link shows the link slug with its
// label (convertBlocksToDelta, V/2c), not the URL, and an unchanged Save
// sends the element back as it was.
func TestEditComposerLinkSlug(t *testing.T) {
	app, _ := syntheticApp(t, erSeed)
	stubTyping(t)
	h := &keysHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	m := erMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"text","text":"read "},` + lsTruncatedEl() + `]}]}]`)
	m.Doc.Blocks = []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{
		{Kind: richtext.Text, Text: "read "}, {Kind: richtext.Link, Text: lsLabel, URL: lsURL, Truncated: true}}}}
	saved := make(chan [2]string, 1)
	c := NewEdit(h, m, func() {})
	t.Cleanup(c.Close)
	c.SaveEdit = func(_ context.Context, _, _, text string, blocks json.RawMessage, _ []string) error {
		saved <- [2]string{text, string(blocks)}
		return nil
	}
	d := c.ed.Doc()
	a := d.Attrs[d.Len()-1].Atom
	if a == nil || a.Kind != AtomLink || a.Text != lsLabel || a.URL != lsURL || a.ID != lsURL {
		t.Fatalf("the edit ends with %+v, not the link slug (text %q)", d.Attrs[d.Len()-1], d.Text())
	}
	if got := d.rtPlain(0, d.Len()); got != "read "+lsLabel {
		t.Errorf("visible text %q", got)
	}
	c.submit()
	select {
	case s := <-saved:
		if want := erCanon(t, m.Blocks); !reflect.DeepEqual(erCanon(t, json.RawMessage(s[1])), want) {
			t.Errorf("saved blocks %s, want %s", s[1], m.Blocks)
		}
		if s[0] != "read <"+lsURL+"|"+lsLabel+">" {
			t.Errorf("saved text %q", s[0])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Save didn't save")
	}
}

// The other ways a truncated link comes back: a rendered span (drafts, a
// message without blocks) is the slug too; in a code block, where atoms
// can't go, the URL stands in (expandTruncatedLinks' text); a link with a
// text but no truncated stays a labelled link, and one with truncated
// but no text its bare URL.
func TestDocFromTruncatedLink(t *testing.T) {
	d := DocFromRichtext([]richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{
		{Kind: richtext.Link, Text: lsLabel, URL: lsURL, Truncated: true}}}})
	if a := d.Attrs[0].Atom; d.Len() != 1 || a == nil || a.Kind != AtomLink || a.Text != lsLabel || a.URL != lsURL {
		t.Errorf("rendered span: %q %+v", d.Text(), d.Attrs)
	}
	load := func(section, el string) *Doc {
		t.Helper()
		d, err := DocFromBlocksJSON(json.RawMessage(`[{"type":"rich_text","elements":[{"type":"`+section+`","elements":[`+el+`]}]}]`), btResolver{})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	if d := load("rich_text_preformatted", lsTruncatedEl()); d.Text() != lsURL || d.Attrs[0].Atom != nil {
		t.Errorf("code block: %q", d.Text())
	}
	if d := load("rich_text_section", `{"type":"link","url":"`+lsURL+`","text":"the guide"}`); d.Text() != "the guide" || d.Attrs[0].Link != lsURL || d.Attrs[0].Atom != nil {
		t.Errorf("labelled link: %q %+v", d.Text(), d.Attrs[0])
	}
	if d := load("rich_text_section", `{"type":"link","url":"`+lsURL+`","truncated":true}`); d.Text() != lsURL || d.Attrs[0].Atom != nil {
		t.Errorf("truncated without a text: %q", d.Text())
	}
}

// TestLinkSlugShots draws the composer with pasted links turned into
// link slugs (the 300-character one and a short one), light and dark at
// 2x, into COMPOSER_AUDIT_SHOTS_DIR (the composer-audit-shots workflow).
// Synthetic data only.
func TestLinkSlugShots(t *testing.T) {
	dir := os.Getenv("COMPOSER_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPOSER_AUDIT_SHOTS_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	app, fs := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	fs.mu.Lock()
	fs.reply = map[string]any{"chat.slugifyUrl": lsSlugReply([2]string{lsURL, lsLabel}, [2]string{lsShort, lsShortLabel})}
	fs.mu.Unlock()
	const scale, w, hgt = 2, 760, 160
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			th := theme.New(dark)
			h := &ovHost{slickHost: &slickHost{app: app, th: th}}
			c := freshComposer(t, h)
			c.PasteText("The guide: " + lsURL + " (and " + lsShort + ")")
			waitUntil(t, "the pasted links' slugs", func() bool {
				runRestores()
				n := 0
				for _, a := range c.ed.Doc().Attrs {
					if a.Atom != nil && a.Atom.Kind == AtomLink {
						n++
					}
				}
				return n == 2
			})
			var ops op.Ops
			var r input.Router
			for i := 0; i < 5; i++ {
				time.Sleep(40 * time.Millisecond)
				ops.Reset()
				paint.FillShape(&ops, th.C.Bg, clip.Rect{Max: image.Pt(w*scale, hgt*scale)}.Op())
				gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
					Constraints: layout.Constraints{Max: image.Pt(w*scale, hgt*scale)}, Now: time.Now(), Source: r.Source()}
				slackc.Frame(gtx)
				off := op.Offset(image.Pt(0, 16*scale)).Push(gtx.Ops)
				c.Layout(gtx)
				off.Pop()
				r.Frame(&ops)
			}
			img := pixeltest.Render(t, w*scale, hgt*scale, &ops)
			f, err := os.Create(filepath.Join(dir, "composer-link-slug-"+mode+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
		})
	}
}
