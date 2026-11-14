package core

// A snippet's Show more (snippets.go) against a fake API: synthetic
// files only.

import (
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

func snippetMsg() slack.Message {
	return slack.Message{TS: "1700000000.000100", User: "USELF", Files: []slack.File{{ID: "F1", Name: "a.py", Title: "a.py",
		Mode: "snippet", Filetype: "python", Mimetype: "text/plain", Lines: 3, LinesMore: 2, Preview: "a = 1",
		PreviewHighlight: `<div class="CodeMirror cm-s-default CodeMirrorServer"><div class="CodeMirror-code"><div><pre><span class="cm-variable">a</span> = 1</pre></div></div></div>`,
		URLPrivate:       "https://files.slack.com/files-pri/T1-F1/a.py"}}}
}

// TestExpandSnippet is fetchAndExpand: the file's expandable goes
// expanding (an update), files.info {file, page 1, count 500, truncate
// true} brings content_highlight_html, then it is expanded (another
// update) and the message's Doc reads the whole content, cut for
// display as the server said; CollapseFile puts the preview back.
func TestExpandSnippet(t *testing.T) {
	a, f := testApp(t)
	m := snippetMsg()
	a.S.SetLatest("C1", []slack.Message{m}, false)
	full := `<div class="CodeMirror cm-s-default CodeMirrorServer"><div class="CodeMirror-code">` +
		`<div><pre><span class="cm-variable">a</span> = 1</pre></div><div><pre>b = 2</pre></div><div><pre>c = 3</pre></div></div></div>`
	f.reply["files.info"] = string(raw(map[string]any{"ok": true, "file": map[string]any{"id": "F1", "mode": "snippet"},
		"content": "a = 1\nb = 2\nc = 3", "content_highlight_html": full, "content_highlight_html_truncated": true}))
	evs, stop := a.Subscribe()
	defer stop()
	a.ExpandFile("C1", m.TS, "F1")
	ev := next(t, evs, "update")
	sn := docSnippet(t, ev.Msg)
	if !sn.Expanding || sn.Expanded {
		t.Fatalf("first update: expanding %v expanded %v", sn.Expanding, sn.Expanded)
	}
	ev = next(t, evs, "update")
	sn = docSnippet(t, ev.Msg)
	if sn.Expanding || !sn.Expanded || sn.Content == nil || len(sn.Content.Lines) != 3 || !sn.ContentTruncated {
		t.Fatalf("expanded: %+v content %+v", sn, sn.Content)
	}
	if got := sn.Content.Plain(); got != "a = 1\nb = 2\nc = 3" {
		t.Errorf("content %q", got)
	}
	p := f.last("files.info")
	if p.Get("file") != "F1" || p.Get("page") != "1" || p.Get("count") != "500" || p.Get("truncate") != "true" {
		t.Errorf("files.info params %v", p)
	}
	// expanding again does nothing; collapsing puts the preview back
	a.ExpandFile("C1", m.TS, "F1")
	a.CollapseFile("C1", m.TS, "F1")
	ev = next(t, evs, "update")
	if sn = docSnippet(t, ev.Msg); sn.Expanded || sn.Expanding {
		t.Errorf("collapsed: %+v", sn)
	}
	// a failure leaves it collapsed
	f.reply["files.info"] = `{"ok":false,"error":"file_not_found"}`
	a.ExpandFile("C1", m.TS, "F1")
	next(t, evs, "update")
	ev = next(t, evs, "update")
	if sn = docSnippet(t, ev.Msg); sn.Expanded || sn.Expanding {
		t.Errorf("after a failure: %+v", sn)
	}
}

// TestSnippetDisplayCut: content past 12,800 characters is cut for
// display (is_truncated) even when the server sent it whole.
func TestSnippetDisplayCut(t *testing.T) {
	a, f := testApp(t)
	m := snippetMsg()
	a.S.SetLatest("C1", []slack.Message{m}, false)
	line := `<div><pre>` + strings.Repeat("x", 99) + `</pre></div>`
	html := `<div class="CodeMirror"><div class="CodeMirror-code">` + strings.Repeat(line, 200) + `</div></div>`
	f.reply["files.info"] = string(raw(map[string]any{"ok": true, "file": map[string]any{"id": "F1"},
		"content_highlight_html": html, "content_highlight_html_truncated": false}))
	evs, stop := a.Subscribe()
	defer stop()
	a.ExpandFile("C1", m.TS, "F1")
	next(t, evs, "update")
	sn := docSnippet(t, next(t, evs, "update").Msg)
	if !sn.Expanded || !sn.ContentTruncated || sn.Content == nil || len(sn.Content.Lines) >= 200 || len(sn.Content.Lines) < 120 {
		n := 0
		if sn.Content != nil {
			n = len(sn.Content.Lines)
		}
		t.Errorf("cut: expanded %v truncated %v, %d lines", sn.Expanded, sn.ContentTruncated, n)
	}
}

func docSnippet(t *testing.T, m *Message) *richtext.File {
	t.Helper()
	if m == nil {
		t.Fatal("no message in the update")
	}
	for _, b := range m.Doc.Blocks {
		if b.Kind == richtext.FileBlock && b.File != nil && b.File.IsSnippet {
			return b.File
		}
	}
	t.Fatalf("no snippet in %+v", m.Doc.Blocks)
	return nil
}
