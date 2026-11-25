package composer

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const unlinkedURL = "https://example.com/?a=1&b=2"

// unlinkedDoc is pre, a URL whose link was taken off (Remove link: Texty's
// {link: false, unlink: true}), then post.
func unlinkedDoc(pre, post string) *Doc {
	d := NewDoc()
	end := d.Insert(0, pre, Attr{})
	end = d.Insert(end, unlinkedURL, Attr{Style: Unlink})
	d.Insert(end, post, Attr{})
	return d
}

// unlinkRunAt is where s starts in d's runes, or -1.
func unlinkRunAt(d *Doc, s string) int {
	want := []rune(s)
	for i := 0; i+len(want) <= len(d.Runes); i++ {
		if slices.Equal(d.Runes[i:i+len(want)], want) {
			return i
		}
	}
	return -1
}

// checkUnlinked: every rune of the URL in d is still unlinked, no link on it.
func checkUnlinked(t *testing.T, what string, d *Doc) {
	t.Helper()
	i := unlinkRunAt(d, unlinkedURL)
	if i < 0 {
		t.Errorf("%s: no URL in %q", what, d.Text())
		return
	}
	for k := i; k < i+len([]rune(unlinkedURL)); k++ {
		if d.Attrs[k].Style&Unlink == 0 || d.Attrs[k].Link != "" {
			t.Errorf("%s: the URL lost its unlink at rune %d (%+v)", what, k-i, d.Attrs[k])
			return
		}
	}
}

// Texty's unlink format crosses Quill's view of the composer both ways
// (qFromDoc, qToDoc), so what reads a message back from its Quill ops
// keeps a URL whose link was taken off as text: Mod+Shift+F's markdown
// conversion, and the send's processor when it changes the message (the
// smart symbols here, an auto-slugged mention too). Both dropped it, and
// the send linked the URL again.
func TestQuillKeepsUnlink(t *testing.T) {
	d := unlinkedDoc("make *this* bold, see ", " ok")
	nd, _, _, ok := mdConvert(d, d.Len(), d.Len())
	if !ok || !strings.Contains(nd.Text(), "make this bold") {
		t.Fatalf("Mod+Shift+F: %q (converted %v)", nd.Text(), ok)
	}
	checkUnlinked(t, "Mod+Shift+F", nd)

	arrow, vs15 := string(rune(0x2194)), string(rune(0xfe0e))
	d = unlinkedDoc("a "+arrow+" b, see ", " ok")
	nd = processSend(d, slugStore{slugData: &slugData{}, smart: true})
	if nd == d || !strings.Contains(nd.Text(), arrow+vs15) {
		t.Fatalf("the processor changed nothing: %q", nd.Text())
	}
	checkUnlinked(t, "the send's processor", nd)
	if b := string(nd.RichTextBlocks()); strings.Contains(b, `"type":"link"`) {
		t.Errorf("the send's blocks link the URL again: %s", b)
	}
}

// qFromDocPerRune is qFromDoc as it was, a rune at a time through
// Delta.push: what qFromDoc must still give.
func qFromDocPerRune(d *Doc) qDelta {
	var q qDelta
	line, seq := 0, 0
	lineOp := func(l Line) { q = q.insert("\n", qLineAttrs(l)) }
	if len(d.Lines) > 0 && d.Lines[0].Kind == Divider {
		q.push(qOp{k: qEmb, emb: &qEmbed{hr: true}})
	}
	for i, r := range d.Runes {
		if r == '\n' {
			lineOp(d.Lines[line])
			line++
			if line < len(d.Lines) && d.Lines[line].Kind == Divider {
				q.push(qOp{k: qEmb, emb: &qEmbed{hr: true}})
			}
			continue
		}
		a := d.Attrs[i]
		if a.Atom != nil {
			if a.Atom.Kind == AtomEmoji {
				q.push(qOp{k: qEmb, emb: &qEmbed{atom: a.Atom}})
				continue
			}
			label := []rune(a.Atom.Text)
			if a.Atom.Kind == AtomBroadcast && len(label) == 0 {
				label = []rune("@" + a.Atom.ID)
			}
			if len(label) == 0 {
				label = []rune{'!'}
			}
			key := "slackmention"
			if a.Atom.Kind == AtomMessage || a.Atom.Kind == AtomLink {
				key = "slackslug"
			}
			seq++
			q = q.insertRunes(label, qAttrs{key: qAtomRef{a: a.Atom, seq: seq}})
			continue
		}
		q = q.insertRunes([]rune{r}, qStyleAttrs(a))
	}
	if line < len(d.Lines) {
		lineOp(d.Lines[line])
	} else {
		lineOp(Line{})
	}
	return q
}

// sameQ reports where a and b differ ("" when they don't).
func sameQ(a, b qDelta) string {
	if len(a.ops) != len(b.ops) {
		return fmt.Sprintf("%d ops, want %d", len(a.ops), len(b.ops))
	}
	for i := range a.ops {
		x, y := a.ops[i], b.ops[i]
		switch {
		case x.k != y.k || string(x.text) != string(y.text) || x.n != y.n || !attrsEqual(x.attrs, y.attrs):
			return fmt.Sprintf("op %d: %d %q %v, want %d %q %v", i, x.k, string(x.text), x.attrs, y.k, string(y.text), y.attrs)
		case (x.emb == nil) != (y.emb == nil) || x.emb != nil && *x.emb != *y.emb:
			return fmt.Sprintf("op %d: embed %+v, want %+v", i, x.emb, y.emb)
		}
	}
	return ""
}

// qFromDoc gathers each run of same-format text into one op, which is
// what pushing it a rune at a time made of it: the same ops for formats,
// links, the unlink, mentions, slugs, emoji, dividers and line formats.
func TestQFromDocRunsAsPushed(t *testing.T) {
	user := &Atom{Kind: AtomUser, ID: "U0FAKE00001", Text: "@ana"}
	here := &Atom{Kind: AtomBroadcast, ID: "here"}
	slug := &Atom{Kind: AtomMessage, ID: "1700000000.000100", Text: "ana in #general", URL: "https://x.slack.com/archives/C1/p1700000000000100"}
	smile := &Atom{Kind: AtomEmoji, ID: "smile", Text: ":smile:", Uni: string(rune(0x1f604))}
	d := NewDoc()
	end := d.Insert(0, "plain ", Attr{})
	end = d.Insert(end, "bold", Attr{Style: Bold})
	end = d.Insert(end, "both", Attr{Style: Bold | Italic})
	end = d.Insert(end, " link", Attr{Link: "https://l.example/"})
	end = d.Insert(end, " "+unlinkedURL, Attr{Style: Unlink})
	end = d.InsertAtom(end, user)
	end = d.InsertAtom(end, user)
	end = d.InsertAtom(end, here)
	end = d.Insert(end, " ", Attr{})
	end = d.InsertAtom(end, slug)
	end = d.InsertAtom(end, smile)
	d.Insert(end, "\nitem one\nitem two\n\ncode `x`\nlast", Attr{})
	d.Lines[1] = Line{Kind: Bullet}
	d.Lines[2] = Line{Kind: Bullet, Indent: 1, Quoted: true}
	d.Lines[3] = Line{Kind: Divider}
	d.Lines[4] = Line{Kind: CodeBlock}
	d.Lines[5] = Line{Kind: Header2}
	for _, c := range []struct {
		name string
		d    *Doc
	}{{"every kind", d}, {"empty", NewDoc()}, {"a divider first", &Doc{Runes: []rune("\nx"), Attrs: make([]Attr, 2), Lines: []Line{{Kind: Divider}, {}}}}} {
		got, _ := qFromDoc(c.d)
		if diff := sameQ(got, qFromDocPerRune(c.d)); diff != "" {
			t.Errorf("%s: %s", c.name, diff)
		}
	}
}

// Quill's view of a long message is built in linear time and memory:
// qFromDoc pushed it a rune at a time, and Delta.push copied the run so
// far for each, 3 GB for a 39,000-character message (Slack takes 40,000),
// three times at each send (the processor, the slash command test, /s) on
// the UI goroutine.
func TestQFromDocIsLinear(t *testing.T) {
	var b strings.Builder
	for i := range 1700 {
		fmt.Fprintf(&b, "line %04d of a message\n", i)
	}
	d := NewDoc()
	d.Insert(0, b.String(), Attr{})
	runes := d.Len()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	q, pos := qFromDoc(d)
	runtime.ReadMemStats(&after)
	if q.length() != runes+1 || len(pos) != runes+1 {
		t.Fatalf("Quill's view: %d long, %d positions, want %d", q.length(), len(pos), runes+1)
	}
	alloc := after.TotalAlloc - before.TotalAlloc
	t.Logf("%d runes, %.1f MB allocated", runes, float64(alloc)/(1<<20))
	// tens of bytes a rune: the positions, the run and its op
	if limit := uint64(runes) * 200; alloc > limit {
		t.Errorf("%.1f MB allocated for a %d-rune message, past %.1f MB: qFromDoc isn't linear", float64(alloc)/(1<<20), runes, float64(limit)/(1<<20))
	}
}
