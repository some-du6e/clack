package core

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"sync"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// resolveCanvases fills the store's canvas slug data (richtext.CanvasRef,
// section rich text) for the lookups rendering missed, and returns the
// ids it filled. Canvases come from files.info (getFileStatus: a file
// that is gone is "deleted", one the viewer can't open "hidden");
// sections from quip.getCanvasBulkSectionIdsData {file_id, section_ids,
// reason: "conditional-fetching"}, Slack's
// fetchAndUpsertCanvasSectionRichText (client-boot AYsF): its sections'
// rich_text_blocks[0] are the sections' rich text.
func (a *App) resolveCanvases(ctx context.Context, files []string, sections [][2]string, previews []string) []string {
	if len(files)+len(sections)+len(previews) == 0 {
		return nil
	}
	var mu sync.Mutex
	var done []string
	add := func(id string) { mu.Lock(); done = append(done, id); mu.Unlock() }
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, id := range files {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if a.resolveCanvasRef(ctx, id) {
				add(id)
			}
		}(id)
	}
	for _, id := range previews {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if a.resolveCanvasPreview(ctx, id) {
				add(id)
			}
		}(id)
	}
	bySection := map[string][]string{}
	for _, s := range sections {
		bySection[s[0]] = append(bySection[s[0]], s[1])
	}
	for fid, secs := range bySection {
		wg.Add(1)
		go func(fid string, secs []string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if a.resolveCanvasSections(ctx, fid, secs) {
				add(fid)
			}
		}(fid, secs)
	}
	wg.Wait()
	return done
}

func (a *App) resolveCanvasRef(ctx context.Context, id string) bool {
	var r struct {
		File struct {
			Title       string          `json:"title"`
			TitleBlocks json.RawMessage `json:"title_blocks"`
			IsDeleted   bool            `json:"is_deleted"`
		} `json:"file"`
	}
	err := a.C.Call(ctx, "files.info", url.Values{"file": {id}, "count": {"0"}}, &r)
	var se *slack.Error
	switch {
	case err == nil:
		st := "normal"
		if r.File.IsDeleted {
			st = "deleted"
		}
		a.S.PutCanvasRef(id, richtext.CanvasRef{Title: r.File.Title, TitleBlocks: r.File.TitleBlocks, Status: st})
	case errors.As(err, &se) && (se.Code == "file_not_found" || se.Code == "file_deleted"):
		a.S.PutCanvasRef(id, richtext.CanvasRef{Status: "deleted"})
	case errors.As(err, &se) && (se.Code == "access_denied" || se.Code == "not_allowed" || se.Code == "file_access_denied"):
		a.S.PutCanvasRef(id, richtext.CanvasRef{Status: "hidden"})
	default:
		log.Printf("canvas slug %s: files.info: %v", id, err)
		a.S.ForgetCanvas(id)
		return false
	}
	return true
}

func (a *App) resolveCanvasSections(ctx context.Context, fid string, secs []string) bool {
	ids, _ := json.Marshal(secs)
	var r struct {
		Sections []struct {
			ID             string            `json:"id"`
			RichTextBlocks []json.RawMessage `json:"rich_text_blocks"`
		} `json:"sections"`
	}
	if err := a.C.Call(ctx, "quip.getCanvasBulkSectionIdsData", url.Values{"file_id": {fid}, "section_ids": {string(ids)},
		"reason": {"conditional-fetching"}}, &r); err != nil {
		log.Printf("canvas slug %s: quip.getCanvasBulkSectionIdsData: %v", fid, err)
		a.S.ForgetCanvas(fid)
		return false
	}
	got := map[string]bool{}
	for _, s := range r.Sections {
		var rt json.RawMessage
		if len(s.RichTextBlocks) > 0 {
			rt = s.RichTextBlocks[0]
		}
		a.S.PutCanvasSection(fid, s.ID, rt)
		got[s.ID] = true
	}
	for _, s := range secs { // not returned: the section is gone ("{canvasName}: …")
		if !got[s] {
			a.S.PutCanvasSection(fid, s, nil)
		}
	}
	return true
}

// resolveCanvasPreview reads canvas id's HTML export (files.info's
// url_private, as Canvas does, without the embedded files) for a message
// card's preview (richtext canvasfile.go).
func (a *App) resolveCanvasPreview(ctx context.Context, id string) bool {
	f, err := a.canvasFileInfo(ctx, id)
	if err != nil || f.URLPrivate == "" {
		var se *slack.Error
		if err == nil || errors.As(err, &se) {
			a.S.PutCanvasPreview(id, nil) // no export to show: the card has no preview
			return true
		}
		log.Printf("canvas preview %s: %v", id, err)
		a.S.ForgetCanvas(id)
		return false
	}
	src, err := a.downloadCanvas(ctx, f.URLPrivate)
	if err != nil {
		log.Printf("canvas preview %s: %v", id, err)
		a.S.ForgetCanvas(id)
		return false
	}
	doc, err := canvas.Parse(src)
	if err != nil {
		a.S.PutCanvasPreview(id, nil)
		return true
	}
	a.resolveCanvasSpans(&doc)
	a.S.PutCanvasPreview(id, CanvasPreviewBlocks(doc))
	return true
}

// canvasPreviewMax is useQuipBlocks' slice: 11 blocks, the title's
// header among them.
const canvasPreviewMax = 11

// CanvasPreviewBlocks is a canvas's Block Kit thumbnail as Slack's
// FileQuip draws it with hideTitle: its first 11 sections, the title
// section dropped. Headings are header blocks; paragraphs, lists, quotes
// and code are rich text; dividers stay; tables, images, embeds, files
// and layouts aren't in the preview.
func CanvasPreviewBlocks(d canvas.Doc) []richtext.Block {
	bs := d.Blocks
	n := 0
	var out []richtext.Block
	title := len(bs) > 0 && bs[0].Kind == canvas.Heading1
	for i := 0; i < len(bs) && n < canvasPreviewMax; i++ {
		b := bs[i]
		n++
		if i == 0 && title {
			continue // hideTitle: the title's header
		}
		switch b.Kind {
		case canvas.Heading1, canvas.Heading2, canvas.Heading3:
			out = append(out, richtext.Block{Kind: richtext.Header, Spans: b.Spans, Level: headingLevel(b.Kind)})
		case canvas.Paragraph:
			if b.Default || len(b.Spans) == 0 {
				continue // unedited default content and empty lines draw nothing
			}
			out = append(out, richtext.Block{Kind: richtext.Paragraph, Spans: b.Spans})
		case canvas.Bullet, canvas.Numbered, canvas.Check:
			lst := richtext.Block{Kind: richtext.List, Ordered: b.Kind == canvas.Numbered, Indent: b.Level}
			for ; i < len(bs) && bs[i].Kind == b.Kind && bs[i].Level == b.Level && n <= canvasPreviewMax; i++ {
				lst.Items = append(lst.Items, bs[i].Spans)
				n++
			}
			i--
			n--
			out = append(out, lst)
		case canvas.Code:
			out = append(out, richtext.Block{Kind: richtext.Preformatted, Spans: []richtext.Span{{Kind: richtext.Text, Text: b.Code}}})
		case canvas.Quote:
			var sp []richtext.Span
			for j, k := range b.Children {
				if j > 0 {
					sp = append(sp, richtext.Span{Kind: richtext.Break})
				}
				sp = append(sp, k.Spans...)
			}
			out = append(out, richtext.Block{Kind: richtext.Quote, Spans: sp})
		case canvas.Divider:
			out = append(out, richtext.Block{Kind: richtext.Divider})
		}
	}
	return out
}

func headingLevel(k canvas.Kind) int {
	switch k {
	case canvas.Heading1:
		return 1
	case canvas.Heading2:
		return 2
	}
	return 3
}
