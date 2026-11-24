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

// FileDetails' data (boot-shared eg5B, FileDetailsWithSubscription): jruc
// withFileSubscription with {alwaysFetchOnMount, skipShares} fetches the
// file on mount and whenever its id changes, through 20Mt
// fetchAndUpsertFileById's fetchFileById:
//
//	files.info {file, page: 1, count: 500, truncate: true, skip_shares: true}
//
// (truncate is `undefined || null || true`, so true; public_shared is
// undefined there and isn't sent). The call's reason,
// "file-subscription.fetchFileInfo", is request metadata (_x_reason),
// which Clack sends for no call. Concurrent fetches of one file share
// the one request, as Slack's per-team, per-file promise cache does.
//
// A refusal becomes the file stub Slack upserts in the file's place:
// file_deleted a tombstone, file_not_found {mode: not_found},
// access_denied and not_visible their modes (with the answer's filetype
// and title), hidden_by_limit, and the canvas and list Slack Connect
// blocks; any other error stays an error (FileDetails keeps what the
// store had, or draws FileDetailsError for a file it never had).

// FileDetailsInfo is files.info's answer for FileDetails.
type FileDetailsInfo struct {
	// File is the file object (nil for a stub: Mode says which).
	File *slack.File
	// Mode is a stub's mode ("tombstone", "not_found", "access_denied",
	// "not_visible", "hidden_by_limit", "canvas_slack_connect_blocked",
	// "lists_slack_connect_blocked"), "" for a file.
	Mode string
	// AltText and AltByAI are the file's alt_txt and is_modified_by_ai
	// (R3SR getFileAltText: FileDetailsFigure's caption).
	AltText string
	AltByAI bool
	// Username is file.username (getFileOwner's last fallback), and
	// CanvasCreator canvas_creator_id (the owner before file.user).
	Username, CanvasCreator string
}

// fileDetailsArgs is fetchFileById's args for FileDetails.
func fileDetailsArgs(id string) url.Values {
	return url.Values{"file": {id}, "page": {"1"}, "count": {"500"}, "truncate": {"true"}, "skip_shares": {"true"}}
}

// fileStubMode maps a files.info refusal to the stub's mode
// (fetchFileById's catch).
func fileStubMode(code string) string {
	switch {
	case code == "file_deleted":
		return "tombstone"
	case code == "file_not_found":
		return "not_found"
	case canvasBlockedErrors[code]:
		return "canvas_slack_connect_blocked"
	case listBlockedErrors[code]:
		return "lists_slack_connect_blocked"
	case code == "access_denied", code == "not_visible", code == "hidden_by_limit":
		return code
	}
	return ""
}

type fileDetailsKey struct {
	a  *App
	id string
}

// fileDetailsFlight is one files.info under way, which every caller asking
// for the same file meanwhile waits on (a.once returns only an error).
type fileDetailsFlight struct {
	done chan struct{}
	info FileDetailsInfo
	err  error
}

var fileDetailsFlights = struct {
	mu sync.Mutex
	m  map[fileDetailsKey]*fileDetailsFlight
}{m: map[fileDetailsKey]*fileDetailsFlight{}}

// FileDetails reads file id for its FileDetails view.
func (a *App) FileDetails(ctx context.Context, id string) (FileDetailsInfo, error) {
	key := fileDetailsKey{a, id}
	fl := &fileDetailsFlights
	fl.mu.Lock()
	if c, ok := fl.m[key]; ok {
		fl.mu.Unlock()
		select {
		case <-c.done:
			return c.info, c.err
		case <-ctx.Done():
			return FileDetailsInfo{}, ctx.Err()
		}
	}
	c := &fileDetailsFlight{done: make(chan struct{})}
	fl.m[key] = c
	fl.mu.Unlock()
	c.info, c.err = a.fileDetails(ctx, id)
	fl.mu.Lock()
	delete(fl.m, key)
	fl.mu.Unlock()
	close(c.done)
	return c.info, c.err
}

func (a *App) fileDetails(ctx context.Context, id string) (FileDetailsInfo, error) {
	var r struct {
		File json.RawMessage `json:"file"`
	}
	err := a.C.Call(ctx, "files.info", fileDetailsArgs(id), &r)
	if err != nil {
		var se *slack.Error
		if errors.As(err, &se) {
			if mode := fileStubMode(se.Code); mode != "" {
				return FileDetailsInfo{Mode: mode}, nil
			}
		}
		log.Printf("files.info %s (file-subscription.fetchFileInfo): %v", id, err)
		return FileDetailsInfo{}, err
	}
	var f slack.File
	var extra struct {
		AltTxt          string `json:"alt_txt"`
		IsModifiedByAI  bool   `json:"is_modified_by_ai"`
		Username        string `json:"username"`
		CanvasCreatorID string `json:"canvas_creator_id"`
	}
	if len(r.File) == 0 || json.Unmarshal(r.File, &f) != nil || f.ID == "" {
		return FileDetailsInfo{}, errors.New("files.info: no file")
	}
	_ = json.Unmarshal(r.File, &extra)
	if f.Mode == "tombstone" {
		return FileDetailsInfo{Mode: "tombstone"}, nil
	}
	return FileDetailsInfo{File: &f, AltText: extra.AltTxt, AltByAI: extra.IsModifiedByAI, Username: extra.Username, CanvasCreator: extra.CanvasCreatorID}, nil
}

// RichFile is the answer's file as a message's file block has it
// (richtext.FileOf), its id kept; a stub is a file with only the id and
// the stub's mode.
func (i FileDetailsInfo) RichFile(id string) *richtext.File {
	if i.File == nil {
		return &richtext.File{ID: id, Mode: i.Mode}
	}
	return richtext.FileOf(i.File)
}
