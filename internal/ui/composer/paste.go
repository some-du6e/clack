package composer

import (
	"context"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/ui/richclip"
)

// Paste keeps formatting, like Slack's composer: Ctrl/Cmd+V reads the
// clipboard's HTML (where the platform offers it) next to the plain text
// Gio delivers, and converts it with docFromHTML; plain text that looks
// like a list ("- a", "• b", "1. c") becomes a list. Ctrl/Cmd+Shift+V
// (and Cmd+Opt+Shift+V, macOS' Paste and Match Style) pastes plain text.

// clipHTMLWait is how long a paste waits for the HTML read (started with
// the key press) after the plain text arrived; then it pastes plain text.
// The editor keeps drawing frames meanwhile, so the window still answers
// clipboard requests (on X11 Clack may own the selection itself).
const clipHTMLWait = 300 * time.Millisecond

// clipRead is one in-flight read of the clipboard's HTML.
type clipRead struct {
	done chan struct{}
	html string
}

// startClipHTML starts reading the clipboard's HTML, or returns nil where
// the platform can't.
func startClipHTML() *clipRead {
	if !clipHTMLSupported() {
		return nil
	}
	r := &clipRead{done: make(chan struct{})}
	go func() {
		defer close(r.done)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Slack's own copy first (slack/texty, then slack/html, in
		// Chromium's web custom data: paste_texty.go), then text/html
		if m, _ := richclip.ReadCustom(ctx); m != nil {
			if h := textyHTML(m["slack/texty"]); h != "" {
				r.html = h
				return
			}
			if h := m["slack/html"]; h != "" && len(h) <= phMaxHTML {
				r.html = h
				return
			}
		}
		b, err := clipReadHTML(ctx)
		if err == nil && len(b) > 0 && len(b) <= phMaxHTML {
			r.html = clipHTMLText(b)
		}
	}()
	return r
}

// result returns the HTML once the read finished.
func (r *clipRead) result() (string, bool) {
	select {
	case <-r.done:
		return r.html, true
	default:
		return "", false
	}
}

// pendingPaste is clipboard text waiting for the HTML read.
type pendingPaste struct {
	text  string
	until time.Time
}

// receivePaste handles the plain text of a paste.
func (e *Editor) receivePaste(gtx layout.Context, s string) {
	if e.richRead == nil {
		e.paste(s)
		return
	}
	e.pendPaste = &pendingPaste{text: s, until: gtx.Now.Add(clipHTMLWait)}
	e.flushPaste(gtx)
}

// flushPaste pastes the pending text once the HTML read is done (or took
// too long), and otherwise asks for another frame.
func (e *Editor) flushPaste(gtx layout.Context) {
	p := e.pendPaste
	if p == nil {
		return
	}
	html, done := "", true
	if e.richRead != nil {
		html, done = e.richRead.result()
	}
	if !done && gtx.Now.Before(p.until) {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(8 * time.Millisecond)})
		return
	}
	e.pendPaste, e.richRead = nil, nil
	e.scrollCaret = true
	e.pasteHTML(p.text, html)
}

// pasteRich inserts src at lo (after deleting the selection) and returns
// the end of the pasted text. A first line with a block format (a list,
// a quote) starts its own line: at the start of a paragraph that line
// takes the format, mid-line the line is split first. Lists pasted into
// a list nest under the current item.
func (e *Editor) pasteRich(lo int, src *Doc) int {
	d := e.doc
	src = src.Clone()
	if LinkTransforms != nil {
		// Pasted links go through the plugins' link transforms too
		// (ClearURLs), like a URL pasted over a selection.
		seen := map[string]string{}
		for i := range src.Attrs {
			if u := src.Attrs[i].Link; u != "" {
				v, ok := seen[u]
				if !ok {
					v = transformLink(u)
					seen[u] = v
				}
				src.Attrs[i].Link = v
			}
		}
	}
	line := d.LineOf(lo)
	cur := d.Lines[line]
	first := src.Lines[0]
	switch {
	case rtIsList(cur.Kind) && rtIsList(first.Kind):
		base := cur.Indent - first.Indent
		for i := range src.Lines {
			if rtIsList(src.Lines[i].Kind) {
				src.Lines[i].Indent = min(max(src.Lines[i].Indent+base, 0), edMaxIndent)
			}
		}
	case cur.Kind == Para && first.Kind != Para:
		if lo > d.LineStart(line) {
			lo = d.Insert(lo, "\n", Attr{})
			line++
		}
		d.Lines[line] = first
	}
	return d.InsertDoc(lo, src)
}
