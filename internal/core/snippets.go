package core

import (
	"context"
	"log"
	"net/url"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// A snippet's "Show more" (CDN build 2c0d02ca130f):
//
//   - ExpandableContainer (boot-shared 9TNB) expands a message file with
//     fetchAndExpandWithIPCheck (x+ps): the file's expandable in the
//     message goes "expanding", fetchAndUpsertFileById (NQkX) calls
//     files.info {file, page 1, count 500, truncate true} and upserts the
//     file with the response's content fields merged in
//     (content_highlight_html, content_highlight_html_truncated), then the
//     expandable is expanded; "expanding" ends either way. A failure
//     leaves it collapsed, with nothing said.
//   - upsertFiles' computePreviewsForSnippet (client-boot QS2I) cuts
//     content_highlight_html to 12,800 characters with html-truncate and
//     marks the file is_truncated (richtext.TruncateHTML).
//   - Collapse (the expanded pill's "Show less", FileActions' Collapse)
//     is collapseFile, or collapseAndEnsureInView for a message; Clack's
//     list keeps the row where it is, as its other relayouts do.
//
// The expandables are keyed by file, channel and message (Slack's by
// view as well: the same message in the channel and its thread share
// one here). Each change emits the message's "update", whose Doc reads
// them (richtext.FileExpander, store/snippets.go).

// ExpandFile is fetchAndExpand for file fileID in message ch/ts.
func (a *App) ExpandFile(ch, ts, fileID string) {
	if fileID == "" {
		return
	}
	if exp, going := a.S.FileExpansion(ch, ts, fileID); exp || going {
		return
	}
	a.S.SetFileExpanding(ch, ts, fileID, true)
	a.emitStored(ch, ts)
	go func() {
		ctx, cancel := context.WithTimeout(a.bg, 30*time.Second)
		defer cancel()
		ok := a.fetchSnippet(ctx, fileID) == nil
		if ok {
			a.S.SetFileExpanded(ch, ts, fileID, true)
		}
		a.S.SetFileExpanding(ch, ts, fileID, false)
		a.emitStored(ch, ts)
	}()
}

// CollapseFile collapses file fileID's expandable in message ch/ts.
func (a *App) CollapseFile(ch, ts, fileID string) {
	if exp, _ := a.S.FileExpansion(ch, ts, fileID); !exp {
		return
	}
	a.S.SetFileExpanded(ch, ts, fileID, false)
	a.emitStored(ch, ts)
}

// fetchSnippet is fetchAndUpsertFileById for a snippet's content: the
// highlighted content goes to the store, cut for display.
func (a *App) fetchSnippet(ctx context.Context, fileID string) error {
	var r struct {
		File                          slack.File `json:"file"`
		Content                       string     `json:"content"`
		ContentHighlightHTML          string     `json:"content_highlight_html"`
		ContentHighlightHTMLTruncated bool       `json:"content_highlight_html_truncated"`
	}
	err := a.C.Call(ctx, "files.info", url.Values{"file": {fileID}, "page": {"1"}, "count": {"500"}, "truncate": {"true"}}, &r)
	if err != nil {
		log.Printf("files.info %s (fetchAndExpand): %v", fileID, err)
		return err
	}
	html, cut := richtext.TruncateHTML(r.ContentHighlightHTML, richtext.SnippetDisplayMax)
	if len(r.Content) > richtext.SnippetDisplayMax {
		if _, c := richtext.TruncateHTML(r.Content, richtext.SnippetDisplayMax); c {
			cut = true // is_truncated: the content's cut too
		}
	}
	a.S.PutSnippetContent(fileID, richtext.ParseCodeMirror(html), cut || r.ContentHighlightHTMLTruncated)
	return nil
}

// emitStored emits the "update" of stored message ch/ts.
func (a *App) emitStored(ch, ts string) {
	if m, ok := a.S.Find(ch, ts); ok {
		a.emitUpdate(ch, &m)
	}
}
