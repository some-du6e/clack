package core

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"sync"

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
func (a *App) resolveCanvases(ctx context.Context, files []string, sections [][2]string) []string {
	if len(files)+len(sections) == 0 {
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
