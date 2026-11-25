package core

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math/rand/v2"
	"net/url"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// resolveCanvases fills the store's canvas slug data (richtext.CanvasRef,
// section rich text) for the lookups rendering missed, and returns the
// ids it filled. Canvases come from files.info (getFileStatus,
// richtext.FileStatusOf: a file that is gone is "deleted", one the viewer
// can't open "hidden");
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
		a.queueCanvasPreview(id)
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

// filesInfoFile is files.info's file, as a CanvasRef reads it.
type filesInfoFile struct {
	Title           string          `json:"title"`
	TitleBlocks     json.RawMessage `json:"title_blocks"`
	IsDeleted       bool            `json:"is_deleted"`
	Filetype        string          `json:"filetype"`
	Mode            string          `json:"mode"`
	PrettyType      string          `json:"pretty_type"`
	Name            string          `json:"name"`
	Subtype         string          `json:"subtype"`
	LinkedChannelID string          `json:"linked_channel_id"`
	// getFileStatus's other inputs (richtext.FileStatusOf), and a skill
	// canvas's and a folder's kinds (the inline file slug)
	FileAccess   string `json:"file_access"`
	HiddenLimit  bool   `json:"is_hidden_by_limit"`
	Tombstoned   bool   `json:"is_tombstoned"`
	IsSkill      bool   `json:"is_skill"`
	FolderType   string `json:"folder_type"`
	ListMetadata struct {
		Views []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"views"`
	} `json:"list_metadata"`
}

// canvasRefOf is the CanvasRef files.info's file f makes.
func canvasRefOf(f *filesInfoFile) richtext.CanvasRef {
	st := richtext.FileStatusOf(richtext.FileFacts{Mode: f.Mode, FileAccess: f.FileAccess, IsDeleted: f.IsDeleted,
		HiddenByLimit: f.HiddenLimit, Tombstoned: f.Tombstoned})
	ref := richtext.CanvasRef{Title: f.Title, TitleBlocks: f.TitleBlocks, Status: st, Kind: fileKindOf(f.Filetype, f.Mode, f.PrettyType),
		Name: f.Name, Filetype: f.Filetype, Mode: f.Mode, Subtype: f.Subtype, FolderType: f.FolderType, IsSkill: f.IsSkill,
		Blocked: f.Mode == "canvas_slack_connect_blocked"} // isBlockedExternalCanvas's mode
	for _, v := range f.ListMetadata.Views {
		if ref.Views == nil {
			ref.Views = map[string]string{}
		}
		ref.Views[v.ID] = v.Name
	}
	return ref
}

// fileKindOf is a file's kind: "list", "canvas" or "" (another kind).
func fileKindOf(filetype, mode, prettyType string) string {
	switch {
	case filetype == "list" || mode == "list":
		return "list"
	case IsCanvasFile(filetype, mode, prettyType):
		return "canvas"
	}
	return ""
}

// Slack Connect's refusals of a canvas or a list (fetchFileById's
// canvas_slack_connect_blocked and lists_slack_connect_blocked, IqFK).
var (
	canvasBlockedErrors = map[string]bool{"slack_connect_team_blocked": true, "slack_connect_canvas_sharing_blocked": true}
	listBlockedErrors   = map[string]bool{"slack_connect_lists_team_blocked": true, "slack_connect_lists_sharing_blocked": true}
)

func (a *App) resolveCanvasRef(ctx context.Context, id string) bool {
	var r struct {
		File filesInfoFile `json:"file"`
	}
	err := a.C.Call(ctx, "files.info", url.Values{"file": {id}, "count": {"0"}}, &r)
	if err == nil {
		a.S.PutCanvasRef(id, canvasRefOf(&r.File))
		return true
	}
	// fetchFileById keeps a file it can't read as a known object (client-
	// boot 548 20Mt j), so nothing waits on it again: the error's own
	// filetype and title (make_external_access_denied, make_not_visible)
	// say a canvas or a list the viewer can't see
	var se *slack.Error
	if !errors.As(err, &se) {
		log.Printf("canvas slug %s: files.info: %v", id, err)
		a.S.ForgetCanvas(id)
		return false
	}
	var body struct {
		Filetype string `json:"filetype"`
		Title    string `json:"title"`
	}
	_ = json.Unmarshal(se.Body, &body)
	switch code := se.Code; {
	case code == "file_not_found" || code == "file_deleted":
		a.S.PutCanvasRef(id, richtext.CanvasRef{Status: "deleted"})
	case code == "access_denied" || code == "not_allowed" || code == "file_access_denied":
		a.S.PutCanvasRef(id, richtext.CanvasRef{Status: "hidden", Kind: fileKindOf(body.Filetype, "", "")})
	case code == "not_visible":
		a.S.PutCanvasRef(id, richtext.CanvasRef{Status: "hidden", Title: body.Title, Kind: fileKindOf(body.Filetype, "", "")})
	case code == "hidden_by_limit", listBlockedErrors[code]:
		a.S.PutCanvasRef(id, richtext.CanvasRef{Status: "hidden"})
	case canvasBlockedErrors[code]:
		a.S.PutCanvasRef(id, richtext.CanvasRef{Status: "hidden", Kind: "canvas", Blocked: true})
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
// card's preview (richtext canvasfile.go) and a canvas link card's
// (canvasview linkcard_canvas.go). An answer about the file (files.info
// refusing it, no url_private, an export that isn't one or is too big, a
// 4xx other than 429) leaves no preview, as Slack's thumbnail with an
// error does. A failure to get an answer (the network, a timeout, a 5xx,
// a 429, the client's backoff) leaves the lookup asked (no per-frame
// re-ask) and asks again later (previewRetryLater). false: nothing new
// for the views.
func (a *App) resolveCanvasPreview(ctx context.Context, id string) bool {
	f, err := a.canvasFileInfo(ctx, id)
	if err != nil {
		var se *slack.Error
		if !errors.As(err, &se) || previewTransientCodes[se.Code] {
			log.Printf("canvas preview %s: files.info: %v", id, err)
			a.previewRetryLater(id)
			return false
		}
		a.previewSettled(id)
		a.S.PutCanvasPreview(id, nil) // files.info refused it: no export to show
		return true
	}
	if f.URLPrivate == "" {
		a.previewSettled(id)
		a.S.PutCanvasPreview(id, nil)
		return true
	}
	src, err := a.downloadCanvas(ctx, f.URLPrivate)
	if err != nil {
		log.Printf("canvas preview %s: %v", id, err)
		if !slack.ExportAnswered(err) {
			a.previewRetryLater(id)
			return false
		}
		a.previewSettled(id)
		a.S.PutCanvasPreview(id, nil)
		return true
	}
	a.previewSettled(id)
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

// previewTransientCodes are files.info refusals that say nothing about
// the file: asked again later.
var previewTransientCodes = map[string]bool{"ratelimited": true, "internal_error": true, "fatal_error": true,
	"request_timeout": true, "service_unavailable": true}

// Previews run on their own, four at a time, each with its own deadline
// (a page of thirty canvases would otherwise run out the resolve batch's
// 30 s while most of them waited for a slot, and be left without a
// preview), and say "names" for themselves when they land.
var (
	previewTimeout = 30 * time.Second
	previewSlots   = 4
	// previewRetryBase is the wait before a preview whose lookup failed
	// without an answer is asked for again: doubling, a minute at most, a
	// quarter either way (a var for tests).
	previewRetryBase = 2 * time.Second
)

type previewState struct {
	mu    sync.Mutex
	sem   chan struct{}
	tries map[string]int // canvases whose lookup failed without an answer
}

func (a *App) previews() *previewState {
	a.previewOnce.Do(func() { a.previewSt = &previewState{sem: make(chan struct{}, previewSlots), tries: map[string]int{}} })
	return a.previewSt
}

// queueCanvasPreview resolves canvas id's preview in the background.
func (a *App) queueCanvasPreview(id string) {
	p := a.previews()
	go func() {
		select {
		case p.sem <- struct{}{}:
		case <-a.bg.Done():
			return
		}
		ctx, cancel := context.WithTimeout(a.bg, previewTimeout)
		ok := a.resolveCanvasPreview(ctx, id)
		cancel()
		<-p.sem
		if ok {
			a.emit(Event{Type: "names", Users: []string{id}})
		}
	}()
}

// previewSettled forgets id's failed tries: it has its answer.
func (a *App) previewSettled(id string) {
	p := a.previews()
	p.mu.Lock()
	delete(p.tries, id)
	p.mu.Unlock()
}

// previewRetryLater asks for id's preview again after a backoff: the
// store's lookup is let go and the views told, so the next read asks.
func (a *App) previewRetryLater(id string) {
	p := a.previews()
	p.mu.Lock()
	p.tries[id]++
	n := p.tries[id]
	p.mu.Unlock()
	d := previewRetryBase << min(n-1, 5)
	if d > time.Minute || d <= 0 {
		d = time.Minute
	}
	d += time.Duration(rand.Int64N(int64(d)/2+1)) - d/4
	time.AfterFunc(d, func() {
		if a.bg.Err() != nil {
			return
		}
		a.retryCanvasPreview(id)
	})
}

// retryCanvasPreview lets canvas id's preview be asked for again, if its
// last lookup failed without an answer: after its backoff, on the
// canvas's own event, and when the client comes back online.
func (a *App) retryCanvasPreview(id string) {
	p := a.previews()
	p.mu.Lock()
	_, failed := p.tries[id]
	p.mu.Unlock()
	if !failed {
		return
	}
	a.S.ForgetCanvasPreview(id)
	a.emit(Event{Type: "names", Users: []string{id}})
}

// retryCanvasPreviews is retryCanvasPreview for every preview that failed
// (the client is back online).
func (a *App) retryCanvasPreviews() {
	p := a.previews()
	p.mu.Lock()
	var ids []string
	for id := range p.tries {
		ids = append(ids, id)
	}
	p.mu.Unlock()
	for _, id := range ids {
		a.retryCanvasPreview(id)
	}
}
