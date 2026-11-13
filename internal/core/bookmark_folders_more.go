package core

// Bookmark folders' writes past pins-folders, as Slack's web client wires
// them (build 859cadc0e0ab, read from the public CDN without a cookie;
// docs/CHANNELS-AUDIT.md "Pins tab and bookmark folders"):
//
//   - addBookmarkForChannel (Tyab eV) with a file
//     (callAddBookmarksForChannelId, YQEj Kt): bookmarks.add {channel,
//     link: the file's permalink, title, type: file, parent_id, entity_id,
//     access_level}, reason "bookmarks-add-api"; the answer's bookmark
//     goes into the store (KI).
//   - addBookmarkFolderForChannel (qSP5 ev → YQEj MW): bookmarks.add
//     {channel, title, type: folder}, reason "bookmarks-add-folder-api";
//     Slack's server adds the folder's tab.
//   - onMoveBookmark (mF/7): the bookmark takes its new parent at once
//     (mK), then bookmarks.reorder {channel, parent_id, center: its id,
//     bookmark_type, entity_id} (YQEj No, no left or right); the answer's
//     bookmark replaces it, a failure puts the old parent back.
//   - uploadFilesToChannelFolder (WftO KQ): with the channel's bookmarks
//     and the new files 100 or more, nothing uploads ("too_many_bookmarks");
//     a Slack Connect channel whose can_upload_files is false refuses
//     ("cannot_share_file_uploads_disabled"). Each file is an eager upload
//     (uKY8 addAndUploadPendingFile: files.getUploadURL, the bytes,
//     files.completeUpload, not shared) whose pending id sits in the
//     folder's upload store (zrPf: tempFileIds, a FILES_UPLOADING row in
//     the folder) until it is done; then files.info (reason
//     "share-multiple-files-as-folder-items") and the file bookmark. The
//     files run side by side (Promise.allSettled); the names of those that
//     failed come back for "We couldn’t add … to the folder".

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
)

// Folder write errors (ak4d's error names).
var (
	ErrTooManyBookmarks     = errors.New("too_many_bookmarks")
	ErrFolderUploadDisabled = errors.New("cannot_share_file_uploads_disabled")
)

// MaxTabItems is the cap on a conversation's folders, folder items, pins
// and workflows (WftO: 100).
const MaxTabItems = 100

// AddFolderFile is addBookmarkForChannel for a file in folder parentID
// ("" the channel's loose bookmarks). accessLevel is the share modal's
// pick for a canvas or list ("" none).
func (a *App) AddFolderFile(ctx context.Context, ch, parentID string, f File, accessLevel string) (Bookmark, error) {
	p := url.Values{"channel": {ch}, "link": {f.Permalink}, "title": {firstNonEmpty(f.Title, f.Name)}, "type": {"file"},
		"reason": {"bookmarks-add-api"}}
	if parentID != "" {
		p.Set("parent_id", parentID)
	}
	if f.ID != "" {
		p.Set("entity_id", f.ID)
	}
	if accessLevel != "" {
		p.Set("access_level", accessLevel)
	}
	var res struct {
		Bookmark bookmarkRaw `json:"bookmark"`
	}
	if err := a.C.Call(ctx, "bookmarks.add", p, &res); err != nil {
		return Bookmark{}, err
	}
	b := a.bookmarkOf(res.Bookmark)
	if b.ID != "" {
		a.putBookmark(ch, b)
	}
	return b, nil
}

// AddBookmarkFolder is addBookmarkFolderForChannel: a new folder named
// title (Create a folder's Create).
func (a *App) AddBookmarkFolder(ctx context.Context, ch, title string) (Bookmark, error) {
	var res struct {
		Bookmark bookmarkRaw `json:"bookmark"`
	}
	if err := a.C.Call(ctx, "bookmarks.add", url.Values{"channel": {ch}, "title": {title}, "type": {"folder"},
		"reason": {"bookmarks-add-folder-api"}}, &res); err != nil {
		return Bookmark{}, err
	}
	b := a.bookmarkOf(res.Bookmark)
	if b.ID == "" {
		return Bookmark{}, errors.New("Cannot add bookmark")
	}
	a.putBookmark(ch, b)
	return b, nil
}

// MoveFolderBookmark is onMoveBookmark: b into folder parent ("" the
// channel's loose Bookmarks folder).
func (a *App) MoveFolderBookmark(ctx context.Context, ch string, b Bookmark, parent string) error {
	old := b.ParentID
	moved := b
	moved.ParentID = parent
	a.putBookmark(ch, moved)
	typ := b.Type
	if typ == "" {
		typ = "link"
	}
	p := url.Values{"channel": {ch}, "center": {b.ID}, "bookmark_type": {typ}, "reason": {"bookmarks-reorder-api"}}
	if parent != "" {
		p.Set("parent_id", parent)
	}
	if b.EntityID != "" {
		p.Set("entity_id", b.EntityID)
	}
	var res struct {
		OK       bool        `json:"ok"`
		Bookmark bookmarkRaw `json:"bookmark"`
	}
	if err := a.C.Call(ctx, "bookmarks.reorder", p, &res); err != nil {
		back := b
		back.ParentID = old
		a.putBookmark(ch, back)
		return err
	}
	if res.Bookmark.ID != "" {
		a.putBookmark(ch, a.bookmarkOf(res.Bookmark))
	}
	return nil
}

// ---- folder uploads ----

// PendingFolderFile is a FILES_UPLOADING row: a file on its way into a
// folder.
type PendingFolderFile struct {
	ID, Name string
}

type folderUploads struct {
	mu  sync.Mutex
	m   map[string][]PendingFolderFile // by channel + "/" + folder id
	seq atomic.Int64
}

var folderUploadIndex sync.Map // *App → *folderUploads

func (a *App) folderUploads() *folderUploads {
	v, _ := folderUploadIndex.LoadOrStore(a, &folderUploads{m: map[string][]PendingFolderFile{}})
	return v.(*folderUploads)
}

// FolderUploads are the folder's files still uploading (tempFileIds), in
// the order they started.
func (a *App) FolderUploads(ch, folder string) []PendingFolderFile {
	u := a.folderUploads()
	u.mu.Lock()
	defer u.mu.Unlock()
	return slices.Clone(u.m[ch+"/"+folder])
}

// updateFolderUpload is updateFolderUpload: add or drop a pending file.
func (a *App) updateFolderUpload(ch, folder string, f PendingFolderFile, add bool) {
	u := a.folderUploads()
	k := ch + "/" + folder
	u.mu.Lock()
	if add {
		u.m[k] = append(slices.Clone(u.m[k]), f)
	} else {
		u.m[k] = slices.DeleteFunc(slices.Clone(u.m[k]), func(p PendingFolderFile) bool { return p.ID == f.ID })
		if len(u.m[k]) == 0 {
			delete(u.m, k) // maybeRemoveFolderUpload
		}
	}
	u.mu.Unlock()
	a.emit(Event{Type: "bookmarks", Channel: ch})
}

// CheckFolderUpload is uploadFilesToChannelFolder's checks before its
// share prompt: the item cap, and for a Slack Connect channel its
// can_upload_files pref (Slack reads its conversationsListPrefs store;
// Clack asks channels.prefs.get for it).
func (a *App) CheckFolderUpload(ctx context.Context, ch string, n int) error {
	all, _ := a.ChannelBookmarks(ch)
	if len(all)+n >= MaxTabItems {
		return ErrTooManyBookmarks
	}
	if c, ok := a.S.Conv(ch); ok && c.IsExtShared {
		if v := a.ChannelPref(ctx, ch, "can_upload_files"); v.OK && string(v.Raw) == "false" {
			return ErrFolderUploadDisabled
		}
	}
	return nil
}

// FolderUploadSkipsPrompt is N9WL V: the viewer's own DM shares without
// asking.
func (a *App) FolderUploadSkipsPrompt(ch string) bool {
	c, ok := a.S.Conv(ch)
	return ok && c.IsIM && c.User == a.S.SelfID()
}

// UploadFilesToFolder is uploadFilesToChannelFolder's onSubmit: every
// file uploads and becomes a bookmark in folder; the names of the files
// that failed come back.
func (a *App) UploadFilesToFolder(ctx context.Context, ch, folder string, files []UploadFile) (failed []string) {
	u := a.folderUploads()
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, f := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pf := PendingFolderFile{ID: "pending-" + strconv.FormatInt(u.seq.Add(1), 10), Name: firstNonEmpty(f.Title, f.Name)}
			a.updateFolderUpload(ch, folder, pf, true)
			defer a.updateFolderUpload(ch, folder, pf, false)
			if err := a.uploadFolderFile(ctx, ch, folder, f); err != nil {
				log.Printf("folder upload: %s", uploadErrClass(err))
				mu.Lock()
				failed = append(failed, f.Name)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return failed
}

func (a *App) uploadFolderFile(ctx context.Context, ch, folder string, f UploadFile) error {
	pu, err := a.PrepareUpload(ctx, f, nil)
	if err != nil {
		return err
	}
	var r struct {
		File browseFileRaw `json:"file"`
	}
	if err := a.C.Call(ctx, "files.info", url.Values{"file": {pu.ID}, "reason": {"share-multiple-files-as-folder-items"}}, &r); err != nil {
		return err
	}
	_, err = a.AddFolderFile(ctx, ch, folder, r.File.file(), "")
	return err
}

// FolderFileInfo is ien3's files.info for a file picked into a folder
// (reason "open-add-file-in-folder-share-dialog"), when the store's copy
// has no share locations.
func (a *App) FolderFileInfo(ctx context.Context, f File) (File, error) {
	if len(f.Channels) > 0 && f.Permalink != "" {
		return f, nil
	}
	var r struct {
		File browseFileRaw `json:"file"`
	}
	if err := a.C.Call(ctx, "files.info", url.Values{"file": {f.ID}, "reason": {"open-add-file-in-folder-share-dialog"}}, &r); err != nil {
		return File{}, err
	}
	return r.File.file(), nil
}

// FolderShareDirect is maybeOpenShareFileAsFolderItemConfirmationModal's
// test for adding without the share prompt: the viewer's own DM (N9WL),
// a file already shared in the conversation (its public or private share
// locations), or an external file (not fileIsHosted).
func (a *App) FolderShareDirect(ch string, f File) bool {
	return a.FolderUploadSkipsPrompt(ch) || slices.Contains(f.Channels, ch) || f.Mode == "external"
}

// ---- the Channel Expert's knowledge folder ----

// KnowledgeAgentAppID is the workspace's Channel Expert app
// (WFcP wL: the team pref default_channel_knowledge_agent's app_id; "" for
// none).
func (a *App) KnowledgeAgentAppID() string {
	raw, ok := a.TeamPref("default_channel_knowledge_agent")
	if !ok {
		return ""
	}
	var v struct {
		AppID string `json:"app_id"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.AppID
}

// IsKnowledgeFolder is FolderTabView's y: the folder tab is the channel's
// Channel Expert folder (j9If nj getAgentforceChannelExpertFolderTab: the
// tab's data.app_id, or its folder bookmark's app_id, is the agent's).
func (a *App) IsKnowledgeFolder(ch, tabAppID, folderID string) bool {
	agent := a.KnowledgeAgentAppID()
	if agent == "" || folderID == "" {
		return false
	}
	if tabAppID == agent {
		return true
	}
	all, _ := a.ChannelBookmarks(ch)
	for _, b := range all {
		if b.ID == folderID {
			return b.AppID == agent
		}
	}
	return false
}

// CanEditAgentSources is hWVz C: on Grid, an org-shared channel needs an
// org admin, any other a workspace admin acting in it; a channel manager
// may anyway.
func (a *App) CanEditAgentSources(ch string) bool {
	c, ok := a.S.Conv(ch)
	if !ok {
		if c, ok = a.S.ExtConv(ch); !ok {
			return false
		}
	}
	admin := a.selfIsWorkspaceAdmin()
	if a.IsEnterprise() && c.IsOrgShared && !c.IsIM && !c.IsMPIM {
		u, _ := a.S.User(a.S.SelfID())
		admin = u != nil && u.EnterpriseUser != nil && (u.EnterpriseUser.IsAdmin || u.EnterpriseUser.IsOwner)
	}
	return admin || a.isChannelManager(ch)
}
