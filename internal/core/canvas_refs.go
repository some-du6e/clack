package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/quip"
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

// resolveCanvasPreview reads a canvas card's preview, as Slack's FileQuip
// does (useQuipBlocks: the Quip thread's getBlockKitThumbnailValue): the
// canvas's Block Kit thumbnail through Quip (canvasThumbnail), recorded
// as of the file's updated time. A canvas with no Quip thread, gone or
// not the viewer's, or whose thumbnail Quip won't load has no preview
// (useQuipBlocks draws none for an errored thumbnail); one the network
// fails is asked for again after a backoff.
func (a *App) resolveCanvasPreview(ctx context.Context, id string) bool {
	f, err := a.canvasFileInfo(ctx, id)
	if err != nil {
		var se *slack.Error
		if errors.As(err, &se) {
			a.S.PutCanvasPreview(id, nil, 0)
			return true
		}
		a.canvasPreviewBackoff(id, err)
		return false
	}
	if f.QuipThreadID == "" {
		a.S.PutCanvasPreview(id, nil, f.Updated)
		return true
	}
	thumb, err := a.canvasThumbnail(ctx, f.QuipThreadID)
	var le *quip.LoadError
	var qe *slack.Error
	switch {
	case err == nil:
	case errors.Is(err, quip.ErrThumbnail), errors.Is(err, quip.ErrNotFound), errors.As(err, &le), errors.As(err, &qe):
		thumb = nil // Quip or Slack answered: the canvas has no thumbnail to show
	default:
		a.canvasPreviewBackoff(id, err)
		return false
	}
	a.canvasPreviewOK(id)
	a.S.PutCanvasPreview(id, thumb, f.Updated)
	return true
}

// canvasThumbnail is threadBlockKitThumbnailImpl (the collab webapp's
// WpDR): load-data for the thread object, then for its BLOCK_KIT_THUMBNAIL
// parcel keyed by the thread's document id at its updated_usec. A thread
// without a document has the empty thumbnail.
func (a *App) canvasThumbnail(ctx context.Context, thread string) (json.RawMessage, error) {
	var w quip.LoadWrapper
	if err := a.quipCall(ctx, quip.NewThreadRequest(thread), &w); err != nil {
		return nil, err
	}
	t, err := quip.ReadThread(thread, &w)
	if err != nil {
		return nil, err
	}
	doc := ""
	if t.DocumentID != nil {
		doc = *t.DocumentID
	}
	if doc == "" {
		return json.RawMessage(`{"blocks": []}`), nil
	}
	var usec int64
	if t.UpdatedUsec != nil {
		usec = *t.UpdatedUsec
	}
	w = quip.LoadWrapper{}
	if err := a.quipCall(ctx, quip.NewThumbnailRequest(thread, doc, usec), &w); err != nil {
		return nil, err
	}
	b, err := quip.ReadThumbnail(doc, &w)
	if err != nil {
		return nil, err
	}
	if !json.Valid(b) {
		return nil, quip.ErrThumbnail
	}
	return json.RawMessage(b), nil
}

// quipCall sends one load-data request and decodes its answer into w.
func (a *App) quipCall(ctx context.Context, req *quip.LoadRequest, w *quip.LoadWrapper) error {
	b, err := a.C.CanvasCall(ctx, "load-data", []slack.CanvasField{{Name: "request_binary", Value: quip.MarshalBase64(req)}})
	if err != nil {
		return err
	}
	if err := quip.Unmarshal(b, w); err != nil {
		return fmt.Errorf("core: load-data: %w", err)
	}
	return nil
}

// Previews' backoff: a canvas whose thumbnail the network failed is
// asked for again (once its card is still on screen) after 30 s, then
// twice as long each time, at most 10 min.
var (
	CanvasPreviewRetryFirst = 30 * time.Second
	CanvasPreviewRetryMax   = 10 * time.Minute
)

type previewRetries struct {
	mu    sync.Mutex
	delay map[string]time.Duration
}

var previewRetriesOf = newPerApp() // *App -> *previewRetries; Release drops an App's

func (a *App) previewRetries() *previewRetries {
	v, _ := previewRetriesOf.LoadOrStore(a, &previewRetries{delay: map[string]time.Duration{}})
	return v.(*previewRetries)
}

// canvasPreviewBackoff keeps canvas id's card from asking again until
// its delay passes.
func (a *App) canvasPreviewBackoff(id string, err error) {
	r := a.previewRetries()
	r.mu.Lock()
	d := r.delay[id]
	if d == 0 {
		d = CanvasPreviewRetryFirst
	} else {
		d = min(2*d, CanvasPreviewRetryMax)
	}
	r.delay[id] = d
	r.mu.Unlock()
	log.Printf("canvas preview %s: %v (again after %s)", id, err, d)
	time.AfterFunc(d, func() { a.S.ForgetCanvasPreview(id) })
}

// canvasPreviewOK resets canvas id's backoff.
func (a *App) canvasPreviewOK(id string) {
	r := a.previewRetries()
	r.mu.Lock()
	delete(r.delay, id)
	r.mu.Unlock()
}
