package composer

import (
	"context"
	"time"

	"gioui.org/io/clipboard"
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

// clipRead is one in-flight read of the clipboard's rich text: Slack's
// own copy (texty), else the HTML.
type clipRead struct {
	done  chan struct{}
	html  string
	texty *Doc
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
			if d := textyDoc(m["slack/texty"]); d != nil {
				r.texty = d
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

// result returns what the read found once it finished.
func (r *clipRead) result() (html string, texty *Doc, done bool) {
	select {
	case <-r.done:
		return r.html, r.texty, true
	default:
		return "", nil, false
	}
}

// filePaste is a paste's probe for files (attachments.pasteImage): whether
// the clipboard's files, or its image, go to the composer. Then the
// paste's text isn't inserted, as in Slack, whose useUploadPastedFiles
// takes getPastedFiles(clipboardData) (the files, minus an office copy's
// rendered picture) to the draft and preventDefaults the paste, and
// Texty's shouldSkipOnPaste skips a prevented paste. Finder's ⌘C puts the
// files' names on the pasteboard as text next to their URLs.
type filePaste struct {
	done chan struct{}
	took bool
}

func newFilePaste() *filePaste { return &filePaste{done: make(chan struct{})} }

// decide records the probe's answer; the first call wins.
func (f *filePaste) decide(took bool) {
	select {
	case <-f.done:
	default:
		f.took = took
		close(f.done)
	}
}

// result returns whether the paste took files, once the probe knows.
func (f *filePaste) result() (took, done bool) {
	select {
	case <-f.done:
		return f.took, true
	default:
		return false, false
	}
}

// clipFilesWait is how long a paste's text waits for the file probe
// (which reads the clipboard's file list, then its image) after the text
// arrived; then the text goes in.
const clipFilesWait = time.Second

// pendingPaste is clipboard text waiting for the HTML read and the file
// probe.
type pendingPaste struct {
	text       string
	until      time.Time // for the HTML
	filesUntil time.Time // for the file probe
}

// startPaste starts reading the clipboard for a paste (plain: Ctrl/Cmd+
// Shift+V, no rich text). Where the platform reads it at once (Windows:
// pasteSnap) the text, the rich text and the composer's file probe all
// come from that one read; elsewhere Gio delivers the text
// (clipboard.ReadCmd) and the HTML is read beside it (startClipHTML).
// It reports whether it started a read (the composer's file probe goes
// with it: EvPasted).
//
// A paste in the frame this editor copied or cut in pastes that copy: Gio
// writes the clipboard only after the frame, so a read started now would
// take what the clipboard held before (a cut then a paste sent at once:
// SendInput, AutoHotkey's Send ^x^v, remote control). Gio's own
// ReadCmd ran after the write, so before the one-session read this held
// by itself.
func (e *Editor) startPaste(gtx layout.Context, plain bool) bool {
	e.richRead, e.filesRead, e.clipSnap, e.clipSnapText = nil, nil, nil, false
	if s := startPasteSnap(plain); s != nil {
		e.clipSnap, e.clipSnapText = s, true
		if s.rich != nil {
			e.richRead = s.rich
		}
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(8 * time.Millisecond)})
		return true
	}
	if !plain {
		e.richRead = startClipHTML()
	}
	gtx.Execute(clipboard.ReadCmd{Tag: e})
	return true
}

// flushSnap hands the snapshot's text to the paste once it was read (as
// Gio's DataEvent would), and otherwise asks for another frame.
func (e *Editor) flushSnap(gtx layout.Context) {
	s := e.clipSnap
	if s == nil || !e.clipSnapText {
		return
	}
	if !s.ready() {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(8 * time.Millisecond)})
		return
	}
	e.clipSnapText = false
	e.scrollCaret = true
	e.scroller.Stop()
	e.receivePaste(gtx, s.text)
}

// receivePaste handles the plain text of a paste.
func (e *Editor) receivePaste(gtx layout.Context, s string) {
	if e.richRead == nil && e.filesRead == nil {
		e.paste(s)
		return
	}
	e.pendPaste = &pendingPaste{text: s, until: gtx.Now.Add(clipHTMLWait), filesUntil: gtx.Now.Add(clipFilesWait)}
	e.flushPaste(gtx)
}

// flushPaste pastes the pending text once the HTML read and the file
// probe are done (or took too long), and otherwise asks for another
// frame. A paste whose files went to the composer inserts no text.
func (e *Editor) flushPaste(gtx layout.Context) {
	p := e.pendPaste
	if p == nil {
		return
	}
	html, done := "", true
	var texty *Doc
	if e.richRead != nil {
		html, texty, done = e.richRead.result()
	}
	took, fdone := false, true
	if e.filesRead != nil {
		took, fdone = e.filesRead.result()
	}
	if !done && gtx.Now.Before(p.until) || !fdone && gtx.Now.Before(p.filesUntil) {
		gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(8 * time.Millisecond)})
		return
	}
	e.pendPaste, e.richRead, e.filesRead = nil, nil, nil
	if took {
		e.plainPaste = false
		return
	}
	e.scrollCaret = true
	e.pasteClip(p.text, html, texty)
}

// pasteRich inserts src at lo (after deleting the selection) and returns
// the end of the pasted text. A first line with a block format (a list,
// a quote) starts its own line: at the start of a paragraph that line
// takes the format, mid-line the line is split first. Lists pasted into
// a list nest under the current item, and a paste into a quote goes in
// as Slack's does (pasteQuoted). lineHasText is whether the caret's line
// had anything on it before the selection was deleted.
func (e *Editor) pasteRich(lo int, src *Doc, lineHasText bool) int {
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
	if cur.Kind == Quote {
		return d.pasteQuoted(lo, src, lineHasText)
	}
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

// pasteQuoted pastes src at lo in a quote line the way Slack's composer
// does, and returns the end of the paste (where the caret goes): Texty's
// handleOnPaste runs prepareDeltaForPaste (+hBz) with the caret's formats,
// then insertContentsAtCursor's matchBlockFormat (LPC/
// convertDeltaToBlockFormat) with its block format, {blockquote: true}.
//
//   - With anything on the caret's line, a paste holding one list item
//     loses its last line's format (prepare's w.slice(0, length - 1): the
//     delta's last newline), and a paste starting with a code line starts
//     on a line of its own ("\n" before it), or, a lone code line of text,
//     goes in as text.
//   - Each op then takes blockquote, or border 1 where it was a quote, a
//     list or code, and the block format makes every line a quote line,
//     but a list item one inside the quote ({blockquote: false, border: 1,
//     list, indent}) and a code line one too ({blockquote: false, border:
//     1, code-block}). A pasted quote, heading or paragraph is a quote
//     line. A list item keeps its indent up to 4, except when the paste
//     holds exactly two items (getRelativeIndent: both at 0).
//   - Quill's newline carries a line's format, so the first pasted line's
//     format is the caret line's from the caret back, and what was after
//     the caret ends the last pasted line. When that line is a list item
//     or a code line, convertDeltaToBlockFormat keeps the paste's final
//     newline: what was after the caret stays a quote line of its own.
func (d *Doc) pasteQuoted(lo int, src *Doc, lineHasText bool) int {
	if len(src.Runes) == 0 && len(src.Lines) <= 1 {
		return lo
	}
	items := func() int {
		n := 0
		for _, l := range src.Lines {
			if rtIsList(l.Kind) {
				n++
			}
		}
		return n
	}
	last := len(src.Lines) - 1
	if lineHasText {
		if items() == 1 {
			src.Lines[last] = Line{}
		}
		if src.Lines[0].Kind == CodeBlock {
			atoms := false
			for _, a := range src.Attrs {
				atoms = atoms || a.Atom != nil
			}
			if last > 0 || atoms {
				src.Runes = append([]rune{'\n'}, src.Runes...)
				src.Attrs = append([]Attr{{}}, src.Attrs...)
				src.Lines = append([]Line{{}}, src.Lines...)
				last++
			} else {
				src.Lines[0] = Line{}
			}
		}
	}
	n := items()
	for i, l := range src.Lines {
		switch {
		case rtIsList(l.Kind):
			indent := min(l.Indent, 4)
			if n == 2 {
				indent = 0
			}
			src.Lines[i] = Line{Kind: l.Kind, Indent: min(max(indent, 0), edMaxIndent), Quoted: true}
		case l.Kind == CodeBlock:
			src.Lines[i] = Line{Kind: CodeBlock, Quoted: true}
		default:
			src.Lines[i] = Line{Kind: Quote}
		}
	}
	keep := rtIsList(src.Lines[last].Kind) || src.Lines[last].Kind == CodeBlock
	line := d.LineOf(d.clamp(lo))
	quote := d.Lines[line]
	end := d.InsertDoc(lo, src)
	copy(d.Lines[line:], src.Lines)
	if keep {
		end = d.Insert(end, "\n", Attr{})
		d.Lines[line+last+1] = quote
	} else {
		d.Lines[line+last] = quote
	}
	return end
}
