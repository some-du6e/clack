package core

// Canvases (Slack's quip-backed documents), as the 2026 web client loads
// them (recorded in the real client, shapes only):
//
//   - files.info gives the metadata: title, permalink, access ("read" |
//     "write" for the viewer), quip_thread_id, comments_count, updated.
//   - url_private (files.slack.com/files-pri/<team>-<file>/canvas) is the
//     rendered document as HTML ("quip-canvas-content"). Every block carries
//     its quip section id, so it is also the map for edits. We parse it into
//     CanvasBlocks (canvas_html.go).
//   - The editor itself speaks Quip's protobuf syncer: POST
//     /canvas/-/load-data/editor/1 (sections with their RTML markup and
//     versions) and /canvas/-/edit-document (section updates). See
//     canvas_quip.go.
//   - Other clients' edits arrive as RTM document_update / file_change
//     events; core turns them into Event{Type: "canvas"}.
//
// Scraping policy: canvas contents are message-like. They live only in
// memory (the returned values); nothing here is written to disk or logged.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
)

// CanvasBlockKind is the kind of one canvas block.
type CanvasBlockKind uint8

const (
	CanvasParagraph CanvasBlockKind = iota
	CanvasH1
	CanvasH2
	CanvasH3
	CanvasBullet
	CanvasNumbered
	CanvasCheck
	CanvasQuote
	CanvasCode
	CanvasDivider
	CanvasTable
	CanvasImage
	CanvasFile
	CanvasEmbed // a link shown as an unfurl card
	CanvasCallout
)

// CanvasBlock is one rendered block of a canvas, in document order.
type CanvasBlock struct {
	Kind CanvasBlockKind
	// ID is the quip section id ("temp:C:…"): the handle for edits. Empty
	// for blocks that aren't a single section (tables, dividers without id).
	ID      string
	Indent  int  // list nesting level (0 = top)
	Number  int  // CanvasNumbered: the ordinal shown
	Checked bool // CanvasCheck
	// Spans is the inline content (text, styles, links, mentions, emoji).
	// Code blocks use Break spans between lines.
	Spans []richtext.Span
	Rows  [][]CanvasCell // CanvasTable
	Image *richtext.Image
	File  *richtext.File
	URL   string // CanvasEmbed target; CanvasImage/CanvasFile link
	Emoji string // CanvasCallout: leading emoji name
}

// CanvasCell is one table cell.
type CanvasCell struct {
	ID     string
	Spans  []richtext.Span
	Header bool
}

// CanvasMeta is a canvas or list file's metadata (files.info).
type CanvasMeta struct {
	ID, Title, Permalink string
	ThreadID             string // quip thread id (canvases)
	User                 string // creator
	Created, Updated     int64
	// Access is the viewer's access: "write", "read" or "" (unknown). Slack
	// shows "View only" for read.
	Access        string
	Editable      bool
	CommentsCount int
	IsList        bool
	Channels      []string // conversations it is shared in
	Deleted       bool
	URL           string // url_private (HTML export / CSV)
}

// CanWrite reports whether the viewer may edit it.
func (m CanvasMeta) CanWrite() bool { return m.Access == "write" }

// Canvas is a loaded canvas document.
type Canvas struct {
	Meta CanvasMeta
	// Title is the document title (the first heading section).
	Title   string
	TitleID string
	Blocks  []CanvasBlock
	// Controls maps quip control ids (emoji, mentions, links inside text)
	// to the text they display, for editing (canvas_quip.go).
	Controls map[string]string
}

// canvasFileRaw is the files.info subset canvases and lists need.
type canvasFileRaw struct {
	ID            string          `json:"id"`
	Created       int64           `json:"created"`
	Updated       int64           `json:"updated"`
	Name          string          `json:"name"`
	Title         string          `json:"title"`
	Filetype      string          `json:"filetype"`
	Mode          string          `json:"mode"`
	User          string          `json:"user"`
	URLPrivate    string          `json:"url_private"`
	Permalink     string          `json:"permalink"`
	QuipThreadID  string          `json:"quip_thread_id"`
	Access        string          `json:"access"`
	Editable      bool            `json:"editable"`
	CommentsCount int             `json:"comments_count"`
	IsDeleted     bool            `json:"is_deleted"`
	Channels      []string        `json:"channels"`
	Groups        []string        `json:"groups"`
	IMs           []string        `json:"ims"`
	ListMetadata  json.RawMessage `json:"list_metadata"`
	ListLimits    json.RawMessage `json:"list_limits"`
	Shares        map[string]map[string][]struct {
		Access string `json:"access"`
	} `json:"shares"`
	DMUsers []struct {
		UserID string `json:"user_id"`
		Access string `json:"access"`
	} `json:"dm_mpdm_users_with_file_access"`
}

func (r *canvasFileRaw) meta(self string) CanvasMeta {
	m := CanvasMeta{ID: r.ID, Title: firstNonEmpty(r.Title, r.Name), Permalink: r.Permalink, ThreadID: r.QuipThreadID,
		User: r.User, Created: r.Created, Updated: r.Updated, Access: r.Access, Editable: r.Editable,
		CommentsCount: r.CommentsCount, IsList: r.Filetype == "list" || r.Mode == "list", URL: r.URLPrivate,
		Deleted: r.IsDeleted || r.Mode == "tombstone"}
	m.Channels = append(append(append(m.Channels, r.Channels...), r.Groups...), r.IMs...)
	if m.Access == "" {
		// older payloads: the best access any share or the DM grant gives
		for _, byCh := range r.Shares {
			for _, ss := range byCh {
				for _, s := range ss {
					if s.Access == "write" || m.Access == "" {
						m.Access = s.Access
					}
				}
			}
		}
		for _, u := range r.DMUsers {
			if u.UserID == self && u.Access != "" {
				m.Access = u.Access
			}
		}
	}
	return m
}

func (a *App) canvasInfo(ctx context.Context, fileID string) (*canvasFileRaw, error) {
	var r struct {
		File canvasFileRaw `json:"file"`
	}
	if err := a.C.Call(ctx, "files.info", url.Values{"file": {fileID}}, &r); err != nil {
		if isCode(err, "file_not_found", "file_deleted") {
			return nil, ErrCanvasDeleted
		}
		return nil, err
	}
	return &r.File, nil
}

// ErrCanvasDeleted is returned for a canvas or list that no longer exists.
var ErrCanvasDeleted = errors.New("core: this file was deleted")

// CanvasMeta reads a canvas or list file's metadata (files.info).
func (a *App) CanvasMeta(ctx context.Context, fileID string) (CanvasMeta, error) {
	r, err := a.canvasInfo(ctx, fileID)
	if err != nil {
		return CanvasMeta{}, err
	}
	return r.meta(a.S.SelfID()), nil
}

// Canvas loads a canvas: metadata from files.info and the document from
// its HTML rendering (memory only).
func (a *App) Canvas(ctx context.Context, fileID string) (Canvas, error) {
	r, err := a.canvasInfo(ctx, fileID)
	if err != nil {
		return Canvas{}, err
	}
	meta := r.meta(a.S.SelfID())
	if meta.Deleted {
		return Canvas{Meta: meta}, ErrCanvasDeleted
	}
	if meta.URL == "" {
		return Canvas{Meta: meta}, errors.New("core: canvas has no document URL")
	}
	body, err := a.fetchFresh(ctx, meta.URL)
	if err != nil {
		return Canvas{Meta: meta}, err
	}
	c := parseCanvasHTML(a.S, body)
	c.Meta = meta
	if c.Title == "" {
		c.Title = meta.Title
	}
	return c, nil
}

// fetchFresh downloads an authenticated files.slack.com document without
// the Fetch LRU (documents change under us). Memory only.
func (a *App) fetchFresh(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && !testAllowHTTP) || !fetchHostAllowed(u.Hostname()) {
		return nil, fmt.Errorf("core: refusing to fetch %q", rawURL)
	}
	resp, err := a.C.Download(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("core: canvas: http %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxFetch+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFetch {
		return nil, errors.New("core: canvas too large")
	}
	return b, nil
}

// CanvasLink is the link Slack's "Copy link" puts on the clipboard.
func (m CanvasMeta) CanvasLink() string { return m.Permalink }

// ShareCanvas posts the canvas link into a conversation (Slack's Share
// dialog posts the link with an optional note; the link unfurls as the
// canvas card).
func (a *App) ShareCanvas(ctx context.Context, m CanvasMeta, channel, note string) error {
	if m.Permalink == "" {
		return errors.New("core: nothing to share")
	}
	text := m.Permalink
	if strings.TrimSpace(note) != "" {
		text = note + "\n" + m.Permalink
	}
	_, err := a.Send(ctx, SendOpts{Channel: channel, Text: text})
	return err
}

// ---- realtime ----

// canvasEvent turns canvas and list RTM events into Event{Type: "canvas"}
// with ID set to the file id (or the quip thread / list id the event
// names). It reports whether it handled the event. Called by handle().
func (a *App) canvasEvent(typ string, raw json.RawMessage) bool {
	switch typ {
	case "document_update", "document_change", "file_change", "file_channel_canvas_reset",
		"list_record_created", "list_record_updated", "list_record_removed", "list_records_archived",
		"list_cells_updated", "list_contents_merged", "list_metadata_updated", "list_updated":
	default:
		return false
	}
	if typ == "document_change" {
		return true // transient presence/cursor frames: nothing to reload
	}
	id := canvasEventID(raw)
	a.emit(Event{Type: "canvas", ID: id, Name: typ})
	return true
}

// canvasEventID finds the file (or list / quip thread) id an event is about.
func canvasEventID(raw json.RawMessage) string {
	var e struct {
		FileID   string `json:"file_id"`
		ListID   string `json:"list_id"`
		ThreadID string `json:"thread_id"`
		File     struct {
			ID string `json:"id"`
		} `json:"file"`
		Payload json.RawMessage `json:"payload"`
	}
	_ = json.Unmarshal(raw, &e)
	if id := firstNonEmpty(e.FileID, e.File.ID, e.ListID, e.ThreadID); id != "" {
		return id
	}
	if len(e.Payload) > 0 && e.Payload[0] == '{' {
		return canvasEventID(e.Payload)
	}
	return ""
}

// CanvasChanged reports whether a "canvas" event may concern the file
// (meta.ID) or its quip thread. Events without an id match everything.
func CanvasChanged(ev Event, m CanvasMeta) bool {
	if ev.Type != "canvas" {
		return false
	}
	return ev.ID == "" || ev.ID == m.ID || (m.ThreadID != "" && ev.ID == m.ThreadID)
}

// CanvasUpdated returns the canvas's last-updated time (files.info
// "updated"), for cheap change polling while a canvas is open.
func (a *App) CanvasUpdated(ctx context.Context, fileID string) (int64, error) {
	r, err := a.canvasInfo(ctx, fileID)
	if err != nil {
		return 0, err
	}
	return r.Updated, nil
}

// canvasPollEvery is how often an open canvas or list re-checks
// files.info "updated" when no realtime event arrived.
const canvasPollEvery = 6 * time.Second

// CanvasPollInterval is canvasPollEvery for the UI.
func CanvasPollInterval() time.Duration { return canvasPollEvery }
