package core

// The Files sidebar's file collections (custom_file_sections) and its
// Recently viewed list, ported from Slack's web client (CDN build
// 4d0c78085232, fetched without a cookie):
//
//   - the store is unifiedFilesBrowser's (client-boot mSgJ, wbGe):
//     setCollections sorts by position, highest first, and keeps the
//     collapsed state the client already had (custom_file_sections_
//     collapsed_persist is off, so collapsing never leaves the client);
//   - maybeFetchCollections (boot-shared GIC/): files.collections.list
//     once custom_file_sections is on, again after a day, with the names
//     unescaped (jHyp processCollections, lodash unescape);
//   - createFileCollection / renameFileCollection (vBP0):
//     files.collections.create {name, emoji} and files.collections.update
//     {collection, name, emoji}; changeCollectionSort (boot-render Q1Ql):
//     files.collections.update {collection, sort}; deleteFileCollection:
//     files.collections.delete {collection};
//   - moveFileToCollection (ICbw): the file leaves its old collection and
//     joins the new one at the top (position generateKeyBetween(max,
//     null), DdYY) before files.favorites.add {file_id, collection_id};
//     removeFileFromCollectionThunk (MgtY): files.favorites.remove
//     {file_id, collection_id}. A failed call puts the store back;
//   - filesOpenInSessionStore (boot-shared KUDA, u7AE, d9Vx): the files
//     opened in canvas and list views, in display and recency order,
//     twenty at most; the sidebar shows the display order of the ten most
//     recently opened (getRecentlyViewedFileIds). Slack keeps it in local
//     storage per team; Clack hands it to SetOpenFilesSaver's saver.
//
// createApiMethodFetcher snake-cases the arguments and drops reason, so
// those are the calls' only fields.

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RecentlyViewedCollection is the Recently viewed header's collection id
// (Pr+I pl, "recently-viewed-canvas-header"): only its collapsed state is
// kept (isRecentlyViewedCollapsed).
const RecentlyViewedCollection = "recently-viewed-canvas-header"

// File collection sorts (Pr+I): files.collections.update's sort values.
const (
	CollectionSortDateAdded      = "date_added" // the default (J7)
	CollectionSortRecentlyViewed = "recently_viewed"
	CollectionSortLastUpdated    = "last_updated"
	CollectionSortAlpha          = "alpha"
)

// FileCollection is one of the Files sidebar's sections
// (files.collections.list): the starred collection (Type "starred") or a
// custom one.
type FileCollection struct {
	ID, Name, Emoji string
	Type            string // "starred" | "custom"
	Position        string
	Sort            string // "" is date_added
	Collapsed       bool   // is_collapsed: the server's at first, then the client's
	Files           []CollectionFile
}

// CollectionFile is a file in a collection with its position.
type CollectionFile struct{ ID, Position string }

// Starred reports whether c is the starred collection.
func (c FileCollection) Starred() bool { return c.Type == "starred" }

// SortOrDefault is the collection's sort (getCollectionSort, Xg).
func (c FileCollection) SortOrDefault() string {
	if c.Sort == "" {
		return CollectionSortDateAdded
	}
	return c.Sort
}

type collectionRaw struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Emoji       string          `json:"emoji"`
	Type        string          `json:"type"`
	Position    json.RawMessage `json:"position"`
	Sort        string          `json:"sort"`
	IsCollapsed bool            `json:"is_collapsed"`
	Files       []struct {
		ID       string          `json:"id"`
		Position json.RawMessage `json:"position"`
	} `json:"files"`
}

// rawPosition reads a position sent as a string or a number.
func rawPosition(r json.RawMessage) string {
	if len(r) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(r, &n) == nil {
		return n.String()
	}
	return ""
}

// lodashUnescape is lodash's unescape (L7LR), processCollection's.
var lodashUnescape = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")

func (r *collectionRaw) collection() FileCollection {
	c := FileCollection{ID: r.ID, Name: lodashUnescape.Replace(r.Name), Emoji: r.Emoji, Type: r.Type,
		Position: rawPosition(r.Position), Sort: r.Sort, Collapsed: r.IsCollapsed}
	for _, f := range r.Files {
		if f.ID != "" {
			c.Files = append(c.Files, CollectionFile{ID: f.ID, Position: rawPosition(f.Position)})
		}
	}
	return c
}

type fileCollectionStore struct {
	mu      sync.Mutex
	list    []FileCollection
	status  string // "" (unfetched) | "loading" | "loaded"
	fetched time.Time
	// recentsCollapsed is isRecentlyViewedCollapsed.
	recentsCollapsed bool
}

var fileCollectionStores sync.Map // *App -> *fileCollectionStore

func (a *App) fileCollections() *fileCollectionStore {
	if v, ok := fileCollectionStores.Load(a); ok {
		return v.(*fileCollectionStore)
	}
	v, _ := fileCollectionStores.LoadOrStore(a, &fileCollectionStore{})
	return v.(*fileCollectionStore)
}

func copyCollections(in []FileCollection) []FileCollection {
	out := make([]FileCollection, len(in))
	for i, c := range in {
		c.Files = append([]CollectionFile(nil), c.Files...)
		out[i] = c
	}
	return out
}

// sortCollections is sortCollectionsByPosition: the highest position first.
func sortCollections(cs []FileCollection) {
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].Position > cs[j].Position })
}

// FileSectionsOn reports custom_file_sections (the sidebar's collections
// instead of the Starred sub-header).
func (a *App) FileSectionsOn() bool { return a.Experiment("custom_file_sections") == "on" }

// FileCollections returns the collections (getFileCollections: none
// without custom_file_sections) and whether files.collections.list has
// answered.
func (a *App) FileCollections() ([]FileCollection, bool) {
	if !a.FileSectionsOn() {
		return nil, false
	}
	s := a.fileCollections()
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyCollections(s.list), s.status == "loaded"
}

// FileCollectionsLoading reports a first files.collections.list in flight
// (getCollectionsFetchStatus "loading": the sidebar's shimmer rows).
func (a *App) FileCollectionsLoading() bool {
	s := a.fileCollections()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status == "loading"
}

// FileCollection returns collection id (getCollectionById).
func (a *App) FileCollection(id string) (FileCollection, bool) {
	cs, _ := a.FileCollections()
	for _, c := range cs {
		if c.ID == id {
			return c, true
		}
	}
	return FileCollection{}, false
}

// FileCollectionOf returns the collection file id is in
// (getCollectionForFileId).
func (a *App) FileCollectionOf(fileID string) (FileCollection, bool) {
	cs, _ := a.FileCollections()
	for _, c := range cs {
		for _, f := range c.Files {
			if f.ID == fileID {
				return c, true
			}
		}
	}
	return FileCollection{}, false
}

// FetchFileCollections is maybeFetchCollections: files.collections.list
// when custom_file_sections is on and the last answer is missing or more
// than a day old. Only the first load shows as loading.
func (a *App) FetchFileCollections(ctx context.Context) error {
	if !a.FileSectionsOn() {
		return nil
	}
	s := a.fileCollections()
	s.mu.Lock()
	if s.status == "loading" || !s.fetched.IsZero() && time.Since(s.fetched) <= 24*time.Hour {
		s.mu.Unlock()
		return nil
	}
	first := s.status == ""
	if first {
		s.status = "loading"
	}
	s.mu.Unlock()
	var r struct {
		Collections []collectionRaw `json:"collections"`
	}
	err := a.C.Call(ctx, "files.collections.list", url.Values{}, &r)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if first {
			s.status = ""
		}
		return err
	}
	a.setCollectionsLocked(s, r.Collections)
	s.fetched, s.status = time.Now(), "loaded"
	return nil
}

// setCollectionsLocked is setCollections: the new list, each known
// collection keeping the client's collapsed state.
func (a *App) setCollectionsLocked(s *fileCollectionStore, raws []collectionRaw) {
	old := map[string]bool{}
	for _, c := range s.list {
		old[c.ID] = c.Collapsed
	}
	out := make([]FileCollection, 0, len(raws))
	for i := range raws {
		c := raws[i].collection()
		if c.ID == "" {
			continue
		}
		if was, ok := old[c.ID]; ok {
			c.Collapsed = was
		}
		out = append(out, c)
	}
	sortCollections(out)
	s.list = out
}

// upsertCollection is updateCollection: the fields of c over the stored
// one (its collapsed state kept), or c added.
func (s *fileCollectionStore) upsert(c FileCollection, keepFiles bool) {
	for i := range s.list {
		if s.list[i].ID == c.ID {
			if keepFiles {
				c.Files = s.list[i].Files
			}
			c.Collapsed = s.list[i].Collapsed
			s.list[i] = c
			sortCollections(s.list)
			return
		}
	}
	s.list = append(s.list, c)
	sortCollections(s.list)
}

func (s *fileCollectionStore) get(id string) (FileCollection, bool) {
	for _, c := range s.list {
		if c.ID == id {
			c.Files = append([]CollectionFile(nil), c.Files...)
			return c, true
		}
	}
	return FileCollection{}, false
}

// CreateFileCollection is createFileCollection: files.collections.create
// {name, emoji}, then the new collection (the reply's first) joins the
// store.
func (a *App) CreateFileCollection(ctx context.Context, name, emoji string) (FileCollection, error) {
	v := url.Values{"name": {name}}
	if emoji != "" {
		v.Set("emoji", emoji)
	}
	var r struct {
		Collections []collectionRaw `json:"collections"`
	}
	if err := a.C.Call(ctx, "files.collections.create", v, &r); err != nil {
		return FileCollection{}, err
	}
	if len(r.Collections) == 0 || r.Collections[0].ID == "" {
		return FileCollection{}, errors.New("files.collections.create: no collection")
	}
	c := r.Collections[0].collection()
	s := a.fileCollections()
	s.mu.Lock()
	s.upsert(c, false)
	s.mu.Unlock()
	return c, nil
}

// RenameFileCollection is renameFileCollection: the new name and emoji at
// once, files.collections.update {collection, name, emoji}, the reply's
// collection over the store, and the old ones back on failure.
func (a *App) RenameFileCollection(ctx context.Context, id, name, emoji string) error {
	s := a.fileCollections()
	s.mu.Lock()
	old, ok := s.get(id)
	if !ok {
		s.mu.Unlock()
		return errors.New("Collection not found")
	}
	nc := old
	nc.Name, nc.Emoji = name, emoji
	s.upsert(nc, true)
	s.mu.Unlock()
	var r struct {
		Collection *collectionRaw `json:"collection"`
	}
	err := a.C.Call(ctx, "files.collections.update", url.Values{"collection": {id}, "name": {name}, "emoji": {emoji}}, &r)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if cur, ok := s.get(id); ok {
			cur.Name, cur.Emoji = old.Name, old.Emoji
			s.upsert(cur, true)
		}
		return err
	}
	if r.Collection != nil && r.Collection.ID != "" {
		s.upsert(r.Collection.collection(), true)
	}
	return nil
}

// SetFileCollectionSort is changeCollectionSort: the sort at once,
// files.collections.update {collection, sort}, the old sort back if it
// fails.
func (a *App) SetFileCollectionSort(ctx context.Context, id, sortBy string) error {
	s := a.fileCollections()
	s.mu.Lock()
	c, ok := s.get(id)
	if !ok {
		s.mu.Unlock()
		return nil
	}
	was := c.Sort
	c.Sort = sortBy
	s.upsert(c, true)
	s.mu.Unlock()
	var r struct {
		Collection *collectionRaw `json:"collection"`
	}
	err := a.C.Call(ctx, "files.collections.update", url.Values{"collection": {id}, "sort": {sortBy}}, &r)
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.get(id)
	if !ok {
		return err
	}
	if err != nil {
		cur.Sort = was
		s.upsert(cur, true)
		return err
	}
	if r.Collection != nil && r.Collection.ID != "" && cur.Sort == sortBy {
		s.upsert(r.Collection.collection(), true)
	}
	return nil
}

// DeleteFileCollection is deleteFileCollection's onConfirm:
// files.collections.delete {collection}, then the collection leaves the
// store.
func (a *App) DeleteFileCollection(ctx context.Context, id string) error {
	if err := a.C.Call(ctx, "files.collections.delete", url.Values{"collection": {id}}, nil); err != nil {
		return err
	}
	s := a.fileCollections()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.list[:0:0]
	for _, c := range s.list {
		if c.ID != id {
			out = append(out, c)
		}
	}
	s.list = out
	return nil
}

// addFileLocked is addFileToCollection: the file at the top of
// collection id (position generateKeyBetween(max, null)).
func (s *fileCollectionStore) addFile(id, fileID string) {
	for i := range s.list {
		c := &s.list[i]
		if c.ID != id {
			continue
		}
		var top string
		for _, f := range c.Files {
			if f.ID == fileID {
				return
			}
			if top == "" || f.Position > top {
				top = f.Position
			}
		}
		c.Files = append([]CollectionFile{{ID: fileID, Position: OrderKeyAfter(top)}}, c.Files...)
		return
	}
}

// removeFiles is removeFilesFromCollection (every collection when id is
// "").
func (s *fileCollectionStore) removeFiles(id string, fileIDs ...string) {
	drop := map[string]bool{}
	for _, f := range fileIDs {
		drop[f] = true
	}
	for i := range s.list {
		c := &s.list[i]
		if id != "" && c.ID != id {
			continue
		}
		out := c.Files[:0:0]
		for _, f := range c.Files {
			if !drop[f.ID] {
				out = append(out, f)
			}
		}
		c.Files = out
	}
}

// MoveFileToCollection is moveFileToCollection: the file leaves the
// collection it was in and joins collection id, it leaves Recently viewed
// (removeOpenFileAndPersist), and files.favorites.add {file_id,
// collection_id} goes out; a failure puts it back. It returns the
// collection the file was in ("" for none), for the toast's Undo.
func (a *App) MoveFileToCollection(ctx context.Context, fileID, id string) (from string, err error) {
	s := a.fileCollections()
	s.mu.Lock()
	if prev, ok := a.collectionOfLocked(s, fileID); ok {
		from = prev
	}
	if from != "" && from != id {
		s.removeFiles(from, fileID)
	}
	s.addFile(id, fileID)
	s.mu.Unlock()
	a.RemoveOpenFile(fileID)
	err = a.C.Call(ctx, "files.favorites.add", url.Values{"file_id": {fileID}, "collection_id": {id}}, nil)
	if err != nil {
		s.mu.Lock()
		s.removeFiles(id, fileID)
		if from != "" {
			s.addFile(from, fileID)
		}
		s.mu.Unlock()
	}
	return from, err
}

func (a *App) collectionOfLocked(s *fileCollectionStore, fileID string) (string, bool) {
	for _, c := range s.list {
		for _, f := range c.Files {
			if f.ID == fileID {
				return c.ID, true
			}
		}
	}
	return "", false
}

// RemoveFileFromCollection is removeFileFromCollectionThunk: the file
// leaves collection id at once and files.favorites.remove {file_id,
// collection_id} goes out; a failure puts it back.
func (a *App) RemoveFileFromCollection(ctx context.Context, fileID, id string) error {
	s := a.fileCollections()
	s.mu.Lock()
	s.removeFiles(id, fileID)
	s.mu.Unlock()
	if err := a.C.Call(ctx, "files.favorites.remove", url.Values{"file_id": {fileID}, "collection_id": {id}}, nil); err != nil {
		s.mu.Lock()
		s.addFile(id, fileID)
		s.mu.Unlock()
		return err
	}
	return nil
}

// ToggleFileCollectionCollapsed is toggleCollectionCollapse: the client
// store only (custom_file_sections_collapsed_persist is off). Recently
// viewed's id toggles isRecentlyViewedCollapsed.
func (a *App) ToggleFileCollectionCollapsed(id string) {
	s := a.fileCollections()
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == RecentlyViewedCollection {
		s.recentsCollapsed = !s.recentsCollapsed
		return
	}
	for i := range s.list {
		if s.list[i].ID == id {
			s.list[i].Collapsed = !s.list[i].Collapsed
		}
	}
}

// SetAllFileCollectionsCollapsed is expandAllCollections /
// collapseAllCollections (an Option-click on a header): both leave
// Recently viewed open.
func (a *App) SetAllFileCollectionsCollapsed(on bool) {
	s := a.fileCollections()
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.list {
		s.list[i].Collapsed = on
	}
	s.recentsCollapsed = false
}

// FileCollectionCollapsed reports whether collection id is collapsed
// (getCollapsedCollectionIds).
func (a *App) FileCollectionCollapsed(id string) bool {
	s := a.fileCollections()
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == RecentlyViewedCollection {
		return s.recentsCollapsed
	}
	for _, c := range s.list {
		if c.ID == id {
			return c.Collapsed
		}
	}
	return false
}

// SortCollectionFiles is sortCollectionFiles (boot-render R3yk): by
// last_read, by updated (else timestamp), or by title A-Z for the files
// the client has loaded (the others after them, by position), else by
// position, highest first. file looks a file up (false: not loaded).
func SortCollectionFiles(files []CollectionFile, sortBy string, file func(id string) (File, bool)) []CollectionFile {
	byPos := func(fs []CollectionFile) []CollectionFile {
		out := append([]CollectionFile(nil), fs...)
		sort.SliceStable(out, func(i, j int) bool { return out[i].Position > out[j].Position })
		return out
	}
	var less func(a, b File) bool
	switch sortBy {
	case CollectionSortRecentlyViewed:
		less = func(a, b File) bool { return lastReadSeconds(a.LastRead) > lastReadSeconds(b.LastRead) }
	case CollectionSortLastUpdated:
		less = func(a, b File) bool { return a.Modified > b.Modified }
	case CollectionSortAlpha:
		less = func(a, b File) bool {
			return strings.ToLower(stripEmoji(firstNonEmpty(a.Title, a.Name))) < strings.ToLower(stripEmoji(firstNonEmpty(b.Title, b.Name)))
		}
	default:
		return byPos(files)
	}
	type loaded struct {
		cf CollectionFile
		f  File
	}
	var have []loaded
	var rest []CollectionFile
	for _, cf := range files {
		if f, ok := file(cf.ID); ok {
			have = append(have, loaded{cf, f})
		} else {
			rest = append(rest, cf)
		}
	}
	sort.SliceStable(have, func(i, j int) bool { return less(have[i].f, have[j].f) })
	out := make([]CollectionFile, 0, len(files))
	for _, h := range have {
		out = append(out, h.cf)
	}
	return append(out, byPos(rest)...)
}

// lastReadSeconds is R3yk's lastReadInSeconds: milliseconds become
// seconds.
func lastReadSeconds(v int64) int64 {
	if v >= 1e10 {
		return v / 1000
	}
	return v
}

// stripEmoji is the A-Z sort's title cleanup (boot-shared 7Db2 h): the
// emoji characters (p7O4's emoji regex) go, the first double space
// becomes one, and the ends are trimmed.
func stripEmoji(s string) string {
	var b strings.Builder
	for _, r := range s {
		if isEmojiRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(strings.Replace(b.String(), "  ", " ", 1))
}

func isEmojiRune(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF, r >= 0x2600 && r <= 0x27BF, r >= 0x2B00 && r <= 0x2BFF,
		r >= 0x2300 && r <= 0x23FF, r >= 0xE0020 && r <= 0xE007F, r == 0xFE0F, r == 0x200D, r == 0x20E3:
		return true
	}
	return false
}

// OrderKeyAfter is generateKeyBetween(a, null) (client-boot DdYY) for
// the base-62 order keys Slack keeps file positions in: the integer part
// (ten digits) plus one, or "5000000000" with nothing before it.
func OrderKeyAfter(a string) string {
	const width = 10
	if a == "" {
		return "5000000000"
	}
	n, err := strconv.ParseInt(strings.SplitN(a, ".", 2)[0], 10, 64)
	if err != nil {
		return "5000000000"
	}
	s := strconv.FormatInt(n+1, 10)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// ---- Recently viewed ----

// openFileStore is filesOpenInSessionStore.
type openFileStore struct {
	mu      sync.Mutex
	display []string // displayOrderedFileIds: new files at the top, reopened ones stay put
	cache   []string // cacheOrderedFileIds: the most recently opened first
	save    func(display, cache []string)
}

var openFileStores sync.Map // *App -> *openFileStore

func (a *App) openFiles() *openFileStore {
	if v, ok := openFileStores.Load(a); ok {
		return v.(*openFileStore)
	}
	v, _ := openFileStores.LoadOrStore(a, &openFileStore{})
	return v.(*openFileStore)
}

var fileIDPattern = regexp.MustCompile(`^F[A-Z0-9]{8,14}$`)

func dropString(s []string, v string) []string {
	out := s[:0:0]
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// SetOpenFilesSaver sets where the list goes after each change
// (maybeSaveOpenFilesToLocalStorage; Clack's shell keeps it per team in
// ui.json).
func (a *App) SetOpenFilesSaver(fn func(display, cache []string)) {
	s := a.openFiles()
	s.mu.Lock()
	s.save = fn
	s.mu.Unlock()
}

// RehydrateOpenFiles is rehydrateOpenFilesList: the saved orders, any id
// only in the display order appended to the cache order.
func (a *App) RehydrateOpenFiles(display, cache []string) {
	if len(display) == 0 || len(cache) == 0 {
		return
	}
	s := a.openFiles()
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]bool{}
	c := append([]string(nil), cache...)
	for _, id := range c {
		seen[id] = true
	}
	for _, id := range display {
		if !seen[id] {
			c = append(c, id)
			seen[id] = true
		}
	}
	s.display, s.cache = append([]string(nil), display...), c
}

func (s *openFileStore) persist() {
	if s.save != nil {
		s.save(append([]string(nil), s.display...), append([]string(nil), s.cache...))
	}
}

// AddOpenFile is addOpenFileAndPersist's addToOpenFiles: id moves to the
// front of the recency order; a file not in the list yet also goes to the
// top of the display order, and past twenty the least recently opened one
// leaves both.
func (a *App) AddOpenFile(id string) {
	s := a.openFiles()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) > 0 && s.cache[0] == id || !fileIDPattern.MatchString(id) {
		return
	}
	known := false
	for _, x := range s.display {
		if x == id {
			known = true
		}
	}
	s.cache = append([]string{id}, dropString(s.cache, id)...)
	if !known {
		s.display = append([]string{id}, s.display...)
		if len(s.display) > 20 {
			last := s.cache[len(s.cache)-1]
			s.cache = s.cache[:len(s.cache)-1]
			s.display = dropString(s.display, last)
		}
	}
	s.persist()
}

// RemoveOpenFile is removeOpenFileAndPersist: the file leaves Recently
// viewed.
func (a *App) RemoveOpenFile(id string) {
	s := a.openFiles()
	s.mu.Lock()
	defer s.mu.Unlock()
	known := false
	for _, x := range s.display {
		if x == id {
			known = true
		}
	}
	if !known {
		return
	}
	s.display, s.cache = dropString(s.display, id), dropString(s.cache, id)
	s.persist()
}

// RecentlyViewedFileIDs is getRecentlyViewedFileIds: the display order of
// the ten most recently opened files.
func (a *App) RecentlyViewedFileIDs() []string {
	s := a.openFiles()
	s.mu.Lock()
	defer s.mu.Unlock()
	top := map[string]bool{}
	for i, id := range s.cache {
		if i == 10 {
			break
		}
		top[id] = true
	}
	var out []string
	for _, id := range s.display {
		if top[id] {
			out = append(out, id)
		}
	}
	return out
}

// FileOpened is FileOpenSubscriber's maybeAddToRecentIfNotStarred for a
// canvas or list a view opened (not in a channel tab: getCurrentChannelTab
// keeps those out): the file joins Recently viewed unless it is starred,
// or with custom_file_sections in any collection.
func (a *App) FileOpened(id string) {
	if a.FileSectionsOn() {
		if _, in := a.FileCollectionOf(id); in {
			return
		}
	} else if a.FileStarred(id) {
		return
	}
	a.AddOpenFile(id)
}

// ---- the team's plan ----

type teamPlan struct {
	mu                 sync.Mutex
	known              bool
	plan, enterpriseID string
}

var teamPlans sync.Map // *App -> *teamPlan

func (a *App) teamPlan() *teamPlan {
	if v, ok := teamPlans.Load(a); ok {
		return v.(*teamPlan)
	}
	v, _ := teamPlans.LoadOrStore(a, &teamPlan{})
	return v.(*teamPlan)
}

func (a *App) setTeamPlan(plan, enterpriseID string) {
	t := a.teamPlan()
	t.mu.Lock()
	t.known, t.plan, t.enterpriseID = true, plan, enterpriseID
	t.mu.Unlock()
}

// OnFreePlan is client-boot mAu1's isOnFreePlan for the current team
// (team.info): no plan and not an enterprise team. false until team.info
// answers. Free teams get no file sections to create and no Bulk manage
// files.
func (a *App) OnFreePlan() bool {
	t := a.teamPlan()
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.known && t.plan == "" && t.enterpriseID == "" && a.C.Creds.EnterpriseID == ""
}
