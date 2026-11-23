package richtext

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// emailWork is what one EmailBlocks call allocated and took.
func emailWork(src string) (alloc uint64, took time.Duration, out []Block) {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	t0 := time.Now()
	out = EmailBlocks(src)
	took = time.Since(t0)
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc, took, out
}

// emailParts counts what blocks hold: spans (breaks included) and list
// items, and their text.
func emailParts(bs []Block) (spans, items int, text string) {
	var b strings.Builder
	for _, bl := range bs {
		spans += len(bl.Spans)
		for _, sp := range bl.Spans {
			b.WriteString(sp.Text)
		}
		items += len(bl.Items)
		for _, it := range bl.Items {
			spans += len(it)
			for _, sp := range it {
				b.WriteString(sp.Text)
			}
		}
		b.WriteByte('\n')
	}
	return spans, items, b.String()
}

// TestEmailBlocksBounded (karim-1118's review): an email's HTML costs work
// in proportion to its size whatever it repeats. 70k same-look runs in one
// paragraph merge into one span without copying the paragraph each time
// (a quadratic merge allocated about 2.5 GB for this 1 MB), and line
// breaks and list items count toward the output's caps like text does
// (250k <br> or 200k <li> built a span or an item each).
func TestEmailBlocksBounded(t *testing.T) {
	for _, c := range []struct {
		name string
		src  string
	}{
		{"same-look runs", "<p>" + strings.Repeat("<span>x</span>", 70000) + "</p>"},
		{"line breaks", strings.Repeat("x<br>", 200000)},
		{"preformatted newlines", "<pre>" + strings.Repeat("x\n", 500000) + "</pre>"},
		{"list items", "<ul>" + strings.Repeat("<li>x", 200000) + "</ul>"},
	} {
		alloc, took, out := emailWork(c.src)
		spans, items, text := emailParts(out)
		t.Logf("%s (%d bytes): %d blocks, %d spans, %d items, %d bytes of text; allocated %d MB in %s",
			c.name, len(c.src), len(out), spans, items, len(text), alloc>>20, took.Round(time.Millisecond))
		if alloc > 256<<20 {
			t.Errorf("%s: EmailBlocks allocated %d MB for %d bytes of HTML; want work in proportion to the input", c.name, alloc>>20, len(c.src))
		}
		if spans+items > 120000 {
			t.Errorf("%s: %d spans and %d list items; want them capped like the text (the parts count toward the caps)", c.name, spans, items)
		}
		if took > 3*time.Second {
			t.Errorf("%s: EmailBlocks took %s", c.name, took)
		}
	}
	_, _, out := emailWork("<p>" + strings.Repeat("<span>x</span>", 70000) + "</p>")
	if len(out) != 1 || len(out[0].Spans) != 1 || out[0].Spans[0].Text != strings.Repeat("x", 70000) {
		spans, _, _ := emailParts(out)
		t.Errorf("70k same-look runs: %d blocks, %d spans; want one paragraph of one span", len(out), spans)
	}
}

// TestEmailBlocksDeepNesting: past the open-element cap the converter
// still leaves out what an email hides (a style sheet's text never shows),
// and unclosed paragraphs and list items end where HTML ends them (a <p>
// ends the <p> before it), so they never pile up to the cap.
func TestEmailBlocksDeepNesting(t *testing.T) {
	_, _, text := emailParts(EmailBlocks(strings.Repeat("<div>", 300) + "<style>.x{color:red}</style><script>var tracking=1</script><p>visible</p>"))
	if strings.Contains(text, "color:red") || strings.Contains(text, "tracking") {
		t.Errorf("300 levels deep, a style sheet's or a script's text shows: %q", text)
	}
	if !strings.Contains(text, "visible") {
		t.Errorf("300 levels deep, the text after them is gone: %q", text)
	}
	paras := EmailBlocks(strings.Repeat("<p>a paragraph", 300))
	if len(paras) != 300 {
		t.Errorf("300 unclosed <p>: %d blocks, want 300 paragraphs", len(paras))
	}
	list := EmailBlocks("<ul>" + strings.Repeat("<li>an item", 300) + "</ul><p>after the list</p>")
	if len(list) != 2 || len(list[0].Items) != 300 {
		n := 0
		if len(list) > 0 {
			n = len(list[0].Items)
		}
		t.Errorf("300 unclosed <li>: %d blocks, %d items; want a list of 300 and the paragraph after it", len(list), n)
	}
}

// TestEmailBlocksLinksAndHead: a link's scheme is matched in any case, as
// a browser matches it (HTTPS://… stays a link), and a <head> that's never
// closed ends where the body starts.
func TestEmailBlocksLinksAndHead(t *testing.T) {
	out := EmailBlocks(`<p>see <a href="HTTPS://example.com/Upper">the upper link</a> and <a href="Mailto:someone@example.com">mail</a></p>`)
	var urls []string
	for _, b := range out {
		for _, sp := range b.Spans {
			if sp.Kind == Link {
				urls = append(urls, sp.URL)
			}
		}
	}
	if len(urls) != 2 || urls[0] != "HTTPS://example.com/Upper" || urls[1] != "Mailto:someone@example.com" {
		t.Errorf("links in upper case: %q, want both kept", urls)
	}
	_, _, text := emailParts(EmailBlocks(`<html><head><title>a title</title><body><p>the body's text</p></body></html>`))
	if !strings.Contains(text, "the body's text") || strings.Contains(text, "a title") {
		t.Errorf("an unclosed <head>: %q, want the body's text and no title", text)
	}
}
