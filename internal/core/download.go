package core

// Downloads: what Slack desktop's main process does with the web app's
// startDesktopDownload (desktop main.bundle.cjs 4.53.276: the
// startFileDownloadEpic, updateDownloadEpic, cancelInProgressDownloadsEpic
// and openDownloadEpic over Electron's DownloadItem, and the "downloads"
// store keyed by file id), with the web app's activeDownloads slice
// (boot-shared yUvH: the ids the download toast shows) beside it.
//
// A download streams to disk: the body goes through one fixed buffer into
// "<save path>.crdownload" in the folder (Chromium's intermediate file),
// which is renamed to the save path once every byte has come. There is no
// size cap and nothing of the file is held in memory, unlike Fetch, which
// keeps previews (images, thumbnails, snippets) in its capped cache. An
// answer cut off before its length is a failure, never a short file.

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// DownloadState is a download's downloadState: Electron's
// DownloadItem.getState(), with "paused" for a paused item (isPaused) and
// "not_started" until the item exists (START_DOWNLOAD).
type DownloadState string

const (
	DownloadNotStarted  DownloadState = "not_started"
	DownloadProgressing DownloadState = "progressing"
	DownloadPaused      DownloadState = "paused"
	DownloadCompleted   DownloadState = "completed"
	DownloadInterrupted DownloadState = "interrupted"
	DownloadCancelled   DownloadState = "cancelled"
)

// Download is one entry of the downloads store: a file the user is
// downloading or downloaded, keyed by the file's id.
type Download struct {
	ID   string
	URL  string
	File richtext.File // the file as the message drew it (the toast's pillow)
	// Path is the save path (downloadPath): where the file is, or will be
	// once complete. "" until the folder is known.
	Path  string
	State DownloadState
	// Received and Total are the bytes so far and the file's size (-1:
	// the answer didn't say).
	Received, Total int64
	Start, End      time.Time
}

// Progress is the download's progress as Slack keeps it: received/total,
// -1 when the size isn't known.
func (d Download) Progress() float64 {
	if d.Total > 0 {
		return float64(d.Received) / float64(d.Total)
	}
	return -1
}

// DownloadRequest starts a download (Downloads.Start).
type DownloadRequest struct {
	// ID is the file's id; URL its download URL (FileDownloadURL).
	ID, URL string
	// File is the file, for its name (getFilename's fallback) and the
	// toast's pillow.
	File richtext.File
	// Dir is the folder to save in: PrefSSBFileDownloadPath, or the
	// folder chooser's answer when the preference is cleared ("Choose
	// location for each download"). An error (ErrDownloadCanceled for a
	// chooser the user closed) ends the download before it starts.
	Dir func(ctx context.Context) (string, error)
}

// ErrDownloadCanceled is a folder chooser the user closed.
var ErrDownloadCanceled = errors.New("download canceled")

// The engine's knobs (tests shorten them).
var (
	// downloadBuf is the one buffer a download reads through.
	downloadBuf = 256 << 10
	// downloadNotifyEvery spaces progress notices (Electron's "updated"
	// comes about as often).
	downloadNotifyEvery = 100 * time.Millisecond
	// downloadAutoResumes is Chromium's kMaxAutoResumeAttempts: a body cut
	// off by the network is asked for again from where it stopped this
	// many times before the download is interrupted.
	downloadAutoResumes = 5
	// downloadResumeDelay times the attempt number is the wait before an
	// auto-resume.
	downloadResumeDelay = 500 * time.Millisecond
)

// Downloads is an App's downloads (App.Downloads).
type Downloads struct {
	a *App

	mu     sync.Mutex
	items  map[string]*dlItem
	active []string // activeDownloads.downloads: the toast's ids, in order
	gen    uint64
	subs   map[int]func()
	nsub   int
	closed bool
}

// dlItem is a download and its worker.
type dlItem struct {
	d      Download
	req    DownloadRequest
	cancel context.CancelFunc // the running attempt's (nil when none)
	done   chan struct{}      // closed when the running attempt ends
	// why is what the running attempt is stopped for: "pause" or "cancel"
	why string
	// partial is the intermediate file; etag and modified the answer's
	// validators (a resume must get the same version of the file)
	partial        string
	etag, modified string
	tries          int // auto-resumes since the last start or Resume
	lastNote       time.Time
}

var downloadStates = newPerApp() // *App -> *Downloads

// Downloads is the App's downloads store.
func (a *App) Downloads() *Downloads {
	v, _ := downloadStates.LoadOrStore(a, &Downloads{a: a, items: map[string]*dlItem{}, subs: map[int]func(){}})
	return v.(*Downloads)
}

// FileDownloadURL is getFileDownloadUrl (boot-shared I83v.S) for a file
// that isn't a list: its url_private_download with origin_team set, ""
// when it has none (Slack then downloads nothing: "No download URL").
func (a *App) FileDownloadURL(urlPrivateDownload string) string {
	d := urlPrivateDownload
	if d == "" || strings.HasPrefix(d, "blob:") {
		return d
	}
	u, err := url.Parse(d)
	if err != nil {
		return d
	}
	q := u.Query()
	q.Set("origin_team", a.C.Creds.Team)
	u.RawQuery = q.Encode()
	return u.String()
}

// Subscribe calls f (on a goroutine of the engine's, never under its
// lock) whenever a download or the toast's list changes. stop ends it.
func (ds *Downloads) Subscribe(f func()) (stop func()) {
	ds.mu.Lock()
	ds.nsub++
	id := ds.nsub
	ds.subs[id] = f
	ds.mu.Unlock()
	return func() {
		ds.mu.Lock()
		delete(ds.subs, id)
		ds.mu.Unlock()
	}
}

// changed bumps the generation and tells the subscribers. Called with
// ds.mu held; the calls run after it's let go.
func (ds *Downloads) changedLocked() func() {
	ds.gen++
	fs := make([]func(), 0, len(ds.subs))
	for _, f := range ds.subs {
		fs = append(fs, f)
	}
	return func() {
		for _, f := range fs {
			f()
		}
	}
}

// Gen is a number that changes whenever anything Subscribe reports does.
func (ds *Downloads) Gen() uint64 {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.gen
}

// Get is the download of file id.
func (ds *Downloads) Get(id string) (Download, bool) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if it, ok := ds.items[id]; ok {
		return it.d, true
	}
	return Download{}, false
}

// List is every download, newest first (the downloads selector DXtE:
// startTime descending).
func (ds *Downloads) List() []Download {
	ds.mu.Lock()
	out := make([]Download, 0, len(ds.items))
	for _, it := range ds.items {
		out = append(out, it.d)
	}
	ds.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Start.Equal(out[j].Start) {
			return out[i].Start.After(out[j].Start)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Active is the toast's ids (activeDownloads.downloads).
func (ds *Downloads) Active() []string {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return append([]string(nil), ds.active...)
}

// AddActive is addActiveDownload: id joins the toast's list (once).
func (ds *Downloads) AddActive(id string) {
	ds.mu.Lock()
	for _, a := range ds.active {
		if a == id {
			ds.mu.Unlock()
			return
		}
	}
	ds.active = append(ds.active, id)
	notify := ds.changedLocked()
	ds.mu.Unlock()
	notify()
}

// RemoveActive is removeActiveDownload.
func (ds *Downloads) RemoveActive(id string) {
	ds.mu.Lock()
	n := ds.active[:0]
	gone := false
	for _, a := range ds.active {
		if a == id {
			gone = true
			continue
		}
		n = append(n, a)
	}
	ds.active = n
	var notify func()
	if gone {
		notify = ds.changedLocked()
	}
	ds.mu.Unlock()
	if notify != nil {
		notify()
	}
}

// Start is startDesktopDownload → START_DOWNLOAD: the file's entry starts
// over as not_started (a download of a file already in the store is
// downloaded again), then the folder, the request and the stream, off
// the caller's goroutine. A failure before the body (a refused URL, an
// HTTP error, a page instead of a file, the chooser closed) removes the
// entry, as REMOVE_DOWNLOADS does.
func (ds *Downloads) Start(req DownloadRequest) {
	ds.mu.Lock()
	if ds.closed || req.ID == "" {
		ds.mu.Unlock()
		return
	}
	old := ds.items[req.ID]
	it := &dlItem{req: req, d: Download{ID: req.ID, URL: req.URL, File: req.File, State: DownloadNotStarted, Total: -1, Start: time.Now()}}
	ds.items[req.ID] = it
	notify := ds.changedLocked()
	ds.mu.Unlock()
	notify()
	go func() {
		if old != nil {
			ds.stopItem(old, "cancel")
			ds.mu.Lock()
			partial, keep := old.partial, old.d.State == DownloadCompleted
			ds.mu.Unlock()
			if partial != "" && !keep {
				os.Remove(partial)
			}
		}
		ds.run(it, 0)
	}()
}

// Pause is the item's pause(): the stream stops (the request ends), the
// bytes so far stay in the partial file.
func (ds *Downloads) Pause(id string) {
	ds.mu.Lock()
	it := ds.items[id]
	if it == nil {
		ds.mu.Unlock()
		return
	}
	switch it.d.State {
	case DownloadProgressing:
		ds.mu.Unlock()
		ds.stopItem(it, "pause")
		return
	case DownloadInterrupted:
		if it.cancel != nil {
			// waiting to auto-resume: stop that, as a Pause stops the stream
			ds.mu.Unlock()
			ds.stopItem(it, "pause")
			return
		}
		// nothing is running: the item is paused as it stands
		it.d.State = DownloadPaused
		notify := ds.changedLocked()
		ds.mu.Unlock()
		notify()
		return
	}
	ds.mu.Unlock()
}

// Resume is the item's resume() (when canResume): the rest of the file
// asked for from where the partial file ends.
func (ds *Downloads) Resume(id string) {
	ds.mu.Lock()
	it := ds.items[id]
	if it == nil || it.cancel != nil || (it.d.State != DownloadPaused && it.d.State != DownloadInterrupted) {
		ds.mu.Unlock()
		return
	}
	it.d.State = DownloadProgressing
	it.tries = 0
	notify := ds.changedLocked()
	ds.mu.Unlock()
	notify()
	go ds.run(it, it.d.Received)
}

// Cancel is the item's cancel(): the stream ends, the partial file is
// deleted and the entry is removed (the "done" event's "cancelled" →
// REMOVE_DOWNLOADS). It waits for the stream to stop, which may wait on
// a folder chooser still open: call it (and Pause, Remove) off the UI
// goroutine.
func (ds *Downloads) Cancel(id string) {
	ds.mu.Lock()
	it := ds.items[id]
	ds.mu.Unlock()
	if it == nil {
		return
	}
	ds.stopItem(it, "cancel")
	ds.mu.Lock()
	done := it.d.State == DownloadCompleted
	partial := it.partial
	ds.mu.Unlock()
	if partial != "" && !done {
		os.Remove(partial) // first: once the entry is gone, nothing of it is left
	}
	ds.mu.Lock()
	if ds.items[id] == it && !done {
		delete(ds.items, id)
	}
	notify := ds.changedLocked()
	ds.mu.Unlock()
	notify()
}

// Remove is REMOVE_DOWNLOADS for ids: their entries go (a running one is
// cancelled first).
func (ds *Downloads) Remove(ids ...string) {
	for _, id := range ids {
		ds.mu.Lock()
		it := ds.items[id]
		ds.mu.Unlock()
		if it == nil {
			continue
		}
		if it.d.State != DownloadCompleted {
			ds.Cancel(id)
			continue
		}
		ds.mu.Lock()
		if ds.items[id] == it {
			delete(ds.items, id)
		}
		notify := ds.changedLocked()
		ds.mu.Unlock()
		notify()
	}
}

// Clear is clearTeamDesktopDownloads: every download that isn't running
// leaves the list ("Clears all stopped downloads").
func (ds *Downloads) Clear() {
	ds.mu.Lock()
	for id, it := range ds.items {
		if it.d.State != DownloadProgressing && it.d.State != DownloadNotStarted {
			delete(ds.items, id)
			if it.d.State != DownloadCompleted && it.partial != "" {
				os.Remove(it.partial)
			}
		}
	}
	notify := ds.changedLocked()
	ds.mu.Unlock()
	notify()
}

// CancelAll is cancelInProgressDownloadsEpic (on quit): every running
// download is cancelled and the call waits for them to stop.
func (ds *Downloads) CancelAll() {
	ds.mu.Lock()
	ds.closed = true
	var ids []string
	for id, it := range ds.items {
		if it.d.State == DownloadProgressing || it.d.State == DownloadNotStarted || it.d.State == DownloadPaused || it.d.State == DownloadInterrupted {
			ids = append(ids, id)
		}
	}
	ds.mu.Unlock()
	for _, id := range ids {
		ds.Cancel(id)
	}
}

// stopItem ends it's running attempt (why: "pause" or "cancel") and waits
// for it.
func (ds *Downloads) stopItem(it *dlItem, why string) {
	ds.mu.Lock()
	c, done := it.cancel, it.done
	if c != nil {
		it.why = why
	}
	ds.mu.Unlock()
	if c == nil {
		return
	}
	c()
	<-done
}

// set updates it's download under the lock and tells the subscribers;
// progress notices are spaced downloadNotifyEvery apart unless force.
func (ds *Downloads) set(it *dlItem, force bool, f func(d *Download)) {
	ds.mu.Lock()
	f(&it.d)
	if !force && time.Since(it.lastNote) < downloadNotifyEvery {
		ds.mu.Unlock()
		return
	}
	it.lastNote = time.Now()
	notify := ds.changedLocked()
	ds.mu.Unlock()
	notify()
}

// remove drops it's entry after a failure before the body
// (REMOVE_DOWNLOADS from observeWillDownloadEvent's d()).
func (ds *Downloads) remove(it *dlItem) {
	ds.mu.Lock()
	if ds.items[it.d.ID] == it {
		delete(ds.items, it.d.ID)
	}
	notify := ds.changedLocked()
	ds.mu.Unlock()
	notify()
}

// run is one attempt: from off (a resume) or the start.
func (ds *Downloads) run(it *dlItem, off int64) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ds.mu.Lock()
	if ds.items[it.d.ID] != it || it.cancel != nil {
		ds.mu.Unlock()
		cancel()
		return
	}
	it.cancel, it.done, it.why = cancel, done, ""
	ds.mu.Unlock()
	defer func() {
		cancel()
		ds.mu.Lock()
		it.cancel = nil
		ds.mu.Unlock()
		close(done)
	}()
	for {
		again, err := ds.attempt(ctx, it, off)
		ds.mu.Lock()
		why := it.why
		ds.mu.Unlock()
		switch {
		case err == nil:
			return // complete: a Pause or Cancel that came as it ended is too late
		case why == "cancel":
			return // Cancel removes the entry and the partial file
		case why == "pause":
			ds.set(it, true, func(d *Download) { d.State = DownloadPaused })
			return
		case again && it.tries < downloadAutoResumes && ctx.Err() == nil:
			it.tries++
			select {
			case <-ctx.Done():
				continue // why says what stopped it
			case <-time.After(time.Duration(it.tries) * downloadResumeDelay):
			}
			ds.mu.Lock()
			off = it.d.Received
			ds.mu.Unlock()
			continue
		}
		return
	}
}

// attempt runs one request. again: the body was cut off after the save
// path was known, and the server can resume it (a range answer with a
// validator), so run may ask for the rest.
func (ds *Downloads) attempt(ctx context.Context, it *dlItem, off int64) (again bool, err error) {
	a := ds.a
	req := it.req
	ds.mu.Lock()
	started := it.partial != ""
	ds.mu.Unlock()
	dir := ""
	if !started {
		if !a.C.CredentialsOK(req.URL) {
			ds.remove(it) // "Blocked download URL"
			return false, errors.New("core: download URL refused")
		}
		if req.Dir != nil {
			if dir, err = req.Dir(ctx); err != nil {
				ds.remove(it) // the chooser closed: "Download canceled"
				return false, err
			}
		}
		if dir == "" {
			if dir, err = DefaultDownloadDir(); err != nil {
				ds.remove(it)
				return false, err
			}
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			ds.remove(it)
			return false, err
		}
	}
	resp, info, err := a.C.DownloadRange(ctx, req.URL, off, -1)
	if err != nil {
		if !started {
			ds.remove(it) // "Loaded a page instead of a file", "loadURL failed"
			return false, err
		}
		if ctx.Err() == nil {
			ds.set(it, true, func(d *Download) { d.State = DownloadInterrupted })
		}
		return isTransient(err), err
	}
	defer resp.Body.Close()
	var f *os.File
	switch {
	case !started:
		name := responseFileName(resp, req)
		p, partial, err := ds.reserve(dir, name)
		if err == nil {
			f, err = os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		}
		if err != nil {
			ds.remove(it)
			return false, err
		}
		ds.mu.Lock()
		it.partial = partial
		ds.mu.Unlock()
		ds.set(it, true, func(d *Download) { d.Path, d.State = p, DownloadProgressing })
	case off > 0 && info.Status == http.StatusPartialContent && sameVersion(it, info):
		f, err = os.OpenFile(it.partial, os.O_WRONLY|os.O_APPEND, 0)
		if err == nil {
			if st, serr := f.Stat(); serr != nil || st.Size() != off {
				f.Close()
				f, err = nil, errors.New("core: download: the partial file changed")
			}
		}
	case info.Status == http.StatusOK || info.Start == 0:
		// the whole file (a server that answers no ranges): over again
		f, err = os.OpenFile(it.partial, os.O_WRONLY|os.O_TRUNC, 0)
		off = 0
	default:
		// a range of another version of the file: all of the new one
		resp.Body.Close()
		return ds.attempt(ctx, it, 0)
	}
	if err != nil {
		ds.set(it, true, func(d *Download) { d.State = DownloadInterrupted })
		return false, err
	}
	it.etag, it.modified = info.ETag, info.LastModified
	total := info.Total
	o := off
	ds.set(it, true, func(d *Download) { d.State, d.Received, d.Total = DownloadProgressing, o, total })
	got := off
	buf := make([]byte, downloadBuf)
	var rerr error
	for {
		n, e := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				rerr = werr
				break
			}
			got += int64(n)
			g := got
			ds.set(it, false, func(d *Download) { d.Received = g })
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			rerr = e
			break
		}
	}
	cerr := f.Close()
	if rerr == nil && cerr != nil {
		rerr = cerr
	}
	if rerr == nil && total >= 0 && got != total {
		rerr = io.ErrUnexpectedEOF // fewer bytes than the file has: cut off
	}
	g := got
	ds.set(it, true, func(d *Download) { d.Received = g })
	if rerr != nil {
		if ctx.Err() != nil {
			return false, ctx.Err() // paused or cancelled: run says which
		}
		ds.set(it, true, func(d *Download) { d.State = DownloadInterrupted })
		return info.Seekable && (it.etag != "" || it.modified != ""), rerr
	}
	final, err := finishDownload(it.partial, it.d.Path)
	if err != nil {
		ds.set(it, true, func(d *Download) { d.State = DownloadInterrupted })
		return false, err
	}
	MarkDownloaded(final, req.URL)
	ds.set(it, true, func(d *Download) {
		d.Path, d.State, d.End = final, DownloadCompleted, time.Now()
	})
	return false, nil
}

// sameVersion: a resumed answer is the file the partial one began (the
// same strong validator).
func sameVersion(it *dlItem, info slack.RangeInfo) bool {
	if it.etag != "" || info.ETag != "" {
		return it.etag == info.ETag && !strings.HasPrefix(info.ETag, "W/")
	}
	return it.modified != "" && it.modified == info.LastModified
}

// isTransient: an error a resume might get past (the network), not an
// answer that refuses the file.
func isTransient(err error) bool {
	var he *slack.HTTPError
	if errors.As(err, &he) {
		return he.Status >= 500
	}
	return !errors.Is(err, slack.ErrNotAFile) && !errors.Is(err, slack.ErrUntrustedHost) && !errors.Is(err, slack.ErrSignedOut)
}

// reserve is setUniqueSavePath: the save path in dir for name, as Slack
// numbers it ("name (1).ext", or a "(n)" the name ends with counted on),
// skipping names a file, another download's partial file or save path
// has; and creates nothing (the partial file comes with the body).
func (ds *Downloads) reserve(dir, name string) (savePath, partial string, err error) {
	taken := func(p string) bool {
		if _, err := os.Lstat(p); err == nil || !errors.Is(err, os.ErrNotExist) {
			return true
		}
		if _, err := os.Lstat(p + partialSuffix); err == nil || !errors.Is(err, os.ErrNotExist) {
			return true
		}
		ds.mu.Lock()
		defer ds.mu.Unlock()
		for _, it := range ds.items {
			if it.d.Path == p && it.d.State != DownloadCompleted {
				return true
			}
		}
		return false
	}
	p := uniqueSavePath(dir, name, taken)
	if p == "" {
		return "", "", errors.New("core: download: no free file name")
	}
	return p, p + partialSuffix, nil
}

// partialSuffix is Chromium's intermediate file's (.crdownload).
const partialSuffix = ".crdownload"

var trailingCount = regexp.MustCompile(`\((\d*)\)$`)

// uniqueSavePath is setUniqueSavePath's loop: join(dir, name), then while
// that's taken, the stem with " (n)" added, or its own trailing "(n)"
// counted up from n+1 (parseInt of an empty "()" is NaN in Slack's code,
// and so its names read "(NaN)").
func uniqueSavePath(dir, name string, taken func(string) bool) string {
	p := filepath.Join(dir, name)
	ext := nodeExtname(name)
	stem := strings.TrimSuffix(name, ext)
	m := trailingCount.FindStringSubmatch(stem)
	l := 1
	nan := false
	if m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			l = n + 1
		} else {
			nan = true
		}
	}
	for i := 0; taken(p); i++ {
		if i >= 10000 {
			return ""
		}
		num := strconv.Itoa(l)
		if nan {
			num = "NaN"
		}
		l++
		var e string
		if m != nil {
			e = trailingCount.ReplaceAllLiteralString(stem, "("+num+")") + ext
		} else {
			e = stem + " (" + num + ")" + ext
		}
		p = filepath.Join(dir, e)
	}
	return p
}

// nodeExtname is Node's path.extname: from the last "." of the base name
// that isn't its first character ("" for ".bashrc" and "name").
func nodeExtname(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 || strings.Trim(name[:i], ".") == "" {
		return ""
	}
	return name[i:]
}

// finishDownload renames the complete partial file to savePath, or the
// next free name if a file took savePath meanwhile: never over a file.
func finishDownload(partial, savePath string) (string, error) {
	dir, name := filepath.Split(savePath)
	p := savePath
	for i := 0; i < 1000; i++ {
		if i > 0 {
			p = uniqueSavePath(dir, name, func(q string) bool {
				_, err := os.Lstat(q)
				return err == nil || !errors.Is(err, os.ErrNotExist)
			})
		}
		// a hard link fails if p exists; where links don't work (FAT,
		// some network shares) a checked rename stands in
		if err := os.Link(partial, p); err == nil {
			os.Remove(partial)
			return p, nil
		} else if errors.Is(err, os.ErrExist) {
			continue
		}
		if _, err := os.Lstat(p); err == nil {
			continue
		}
		if err := os.Rename(partial, p); err != nil {
			return "", err
		}
		return p, nil
	}
	return "", errors.New("core: download: no free file name")
}

// responseFileName is DownloadItem.getFilename(): the answer's
// Content-Disposition filename, else the file's name, else the URL's
// last segment, made one plain file name (CleanFileName).
func responseFileName(resp *http.Response, req DownloadRequest) string {
	name := ""
	if _, ps, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		name = ps["filename"]
	}
	if name == "" {
		name = req.File.Name
	}
	if name == "" {
		name = req.File.Title
	}
	if name == "" {
		if u, err := url.Parse(req.URL); err == nil {
			name = path.Base(u.Path)
		}
	}
	name = CleanFileName(name)
	if name == "" {
		name = "download"
	}
	return name
}

// zoneIdentifier is the Zone.Identifier stream Chromium writes on a
// download (quarantine_win.cc SetInternetZoneIdentifierDirectly):
// [ZoneTransfer], ZoneId=3 (the internet), ReferrerUrl when there is one
// and HostUrl (about:internet when the source is unknown), every line
// ending CRLF. Slack's downloads have no referrer (a hidden window loads
// the URL itself).
func zoneIdentifier(source, referrer string) string {
	if source == "" {
		source = "about:internet"
	}
	s := "[ZoneTransfer]\r\nZoneId=3\r\n"
	if referrer != "" {
		s += "ReferrerUrl=" + referrer + "\r\n"
	}
	return s + "HostUrl=" + source + "\r\n"
}

// DefaultDownloadDir is ~/Downloads (Slack's default
// PrefSSBFileDownloadPath, app.getPath("downloads")).
func DefaultDownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}

// CleanFileName makes a Slack-supplied name safe as one plain file name:
// no separators, control characters, bidi controls, "..", trailing dots
// or spaces, drive letters or Windows device names, at most 200 bytes.
func CleanFileName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == 0x7f, strings.ContainsRune(`/\:*?"<>|`, r):
			return '_'
		case r == 0x200e, r == 0x200f, r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
			return '_' // bidi controls: "‮gnp.exe" would display as "exe.png"
		}
		return r
	}, strings.TrimSpace(name))
	name = strings.Trim(name, ". ")
	if len(name) > maxFileName {
		// TruncateFilename: the stem gives way, the extension stays (room
		// is left for " (n)" and ".crdownload" under the 255 a path part
		// may have)
		ext := nodeExtname(name)
		if len(ext) > 32 {
			ext = ""
		}
		stem := strings.ToValidUTF8(name[:maxFileName-len(ext)], "")
		name = strings.TrimRight(stem, ". ") + ext
	}
	// Windows device names (CON, NUL, COM1, CONIN$, …) open the device, not
	// a file, whatever the extension.
	stem := strings.ToUpper(name)
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	stem = strings.TrimRight(stem, " ")
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		name = "_" + name
	default:
		if p, d, ok := strings.Cut(stem, "COM"); ok && p == "" && isDeviceDigit(d) {
			name = "_" + name
		} else if p, d, ok := strings.Cut(stem, "LPT"); ok && p == "" && isDeviceDigit(d) {
			name = "_" + name
		}
	}
	return name
}

// maxFileName bounds a cleaned name (bytes).
const maxFileName = 200

// isDeviceDigit is COMn's and LPTn's n as Windows reads it: 0-9, or the
// superscript ¹ ² ³ it treats as those devices too.
func isDeviceDigit(d string) bool {
	switch d {
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "\u00b9", "\u00b2", "\u00b3":
		return true
	}
	return false
}
