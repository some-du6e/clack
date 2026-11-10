package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif" // DecodeConfig of pasted images
	_ "image/jpeg"
	_ "image/png"
	"log"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/some-du6e/clack/internal/localimg"
	"github.com/some-du6e/clack/internal/slack"
)

// Sending files, the way Slack's web client does (gantry-v2 build
// a84662d2d135, public CDN):
//
// Attach (UploadQueue, eager upload): files.getUploadURL, the multipart
// POST, files.completeUpload; the pending file then sits in PROCESSING
// until the file_created realtime event (filesCreated →
// maybeResolveUploadInQueueWithFileId → files.info), or, after 60 s
// (_setProcessingTimeout) or without a realtime connection, until
// pollForFileProcessing finds it (first check after 7.5 s, doubling to
// 30 s, for up to 10 min). Only then is the upload done: the composer
// tile's spinner stops and the file joins the draft.
//
// Send (prepareAndSendMessage → sendDraftFiles): appendPlaceholderMessage
// puts {…msgParams, client_msg_id, user, is_pending: true, files,
// pendingFiles} in the channel at once: files when every upload is done,
// else pendingFiles (the uploads in flight, drawn from the File itself at
// .5 under "Waiting to upload…" / "Uploading…", then "Sending…" once
// syncPlaceholderWithUploads has swapped in the real ids). files.share
// goes out when every upload has resolved, so the message Slack echoes
// carries processed files. The echo (same client_msg_id) replaces the
// placeholder; a failed share removes it (cleanUpPlaceholder) and gives
// the files back to the draft.

// Processing waits (variables so tests can shorten them).
var (
	UploadEventWait = 60 * time.Second        // _setProcessingTimeout
	UploadPollFirst = 7500 * time.Millisecond // checkWithBackoff(7500)
	UploadPollMax   = 30 * time.Second        // Math.min(2*c, 3e4)
	UploadPollFor   = 10 * time.Minute        // pollForFileProcessing timeout 6e5
	// UploadDegradedRefresh is dispatchDegradedModeEventsForFileUpload's
	// history refresh after a share without a realtime connection.
	UploadDegradedRefresh = 5 * time.Second
)

// ErrProcessingTimeout is Slack's ProcessingTimeout: the file never
// finished processing.
var ErrProcessingTimeout = errors.New("core: polling for processing timed out")

// PendingFile is an attached file whose eager upload may still be running
// (Slack's pendingFile with its uploadPromise). The composer makes one per
// attachment, reports the upload's progress and its end, and hands it to
// Upload, which draws the placeholder from it and waits for it.
type PendingFile struct {
	Name, Title string
	Data        []byte // in memory only

	mu       sync.Mutex
	sent     int64
	total    int64
	finished bool
	res      PreparedUpload
	err      error
	done     chan struct{}
	watchers map[int]func()
	nextW    int
}

// NewPendingFile starts tracking an attached file.
func NewPendingFile(name, title string, data []byte) *PendingFile {
	return &PendingFile{Name: name, Title: title, Data: data, done: make(chan struct{})}
}

// Progress records the upload's bytes so far.
func (p *PendingFile) Progress(sent, total int64) {
	p.mu.Lock()
	p.sent, p.total = sent, total
	ws := p.watchersLocked()
	p.mu.Unlock()
	for _, w := range ws {
		w()
	}
}

// Finish ends the upload (once): its prepared file, or the error.
func (p *PendingFile) Finish(res PreparedUpload, err error) {
	p.mu.Lock()
	if p.finished {
		p.mu.Unlock()
		return
	}
	p.finished, p.res, p.err = true, res, err
	close(p.done)
	ws := p.watchersLocked()
	p.mu.Unlock()
	for _, w := range ws {
		w()
	}
}

// Wait blocks until the upload is done.
func (p *PendingFile) Wait(ctx context.Context) (PreparedUpload, error) {
	select {
	case <-p.done:
	case <-ctx.Done():
		return PreparedUpload{}, ctx.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.res, p.err
}

// Result is the finished upload (ok=false while it runs).
func (p *PendingFile) Result() (res PreparedUpload, err error, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.res, p.err, p.finished
}

// percent is Slack's percentComplete: 0 until bytes move, 100 once they
// are all sent (processing).
func (p *PendingFile) percent() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finished {
		return 100
	}
	if p.total <= 0 {
		return 0
	}
	return int(min(100, p.sent*100/p.total))
}

func (p *PendingFile) watch(f func()) (stop func()) {
	p.mu.Lock()
	if p.watchers == nil {
		p.watchers = map[int]func(){}
	}
	p.nextW++
	id := p.nextW
	p.watchers[id] = f
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		delete(p.watchers, id)
		p.mu.Unlock()
	}
}

func (p *PendingFile) watchersLocked() []func() {
	out := make([]func(), 0, len(p.watchers))
	for _, w := range p.watchers {
		out = append(out, w)
	}
	return out
}

// ---- processing (file_created / polling) ----

// fileEvents tracks file_created events: Slack's upload resolves when one
// names its file.
type fileEvents struct {
	mu   sync.Mutex
	seen map[string]time.Time
	wait map[string][]chan struct{}
}

func (a *App) fileEv() *fileEvents {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fileEvs == nil {
		a.fileEvs = &fileEvents{seen: map[string]time.Time{}, wait: map[string][]chan struct{}{}}
	}
	return a.fileEvs
}

// onFileCreated is Slack's filesCreated: it resolves an upload waiting
// for the file.
func (a *App) onFileCreated(ev slack.Event) {
	var e struct {
		FileID string `json:"file_id"`
		File   struct {
			ID string `json:"id"`
		} `json:"file"`
	}
	_ = json.Unmarshal(ev.Raw, &e)
	id := firstNonEmpty(e.FileID, e.File.ID)
	if id == "" {
		return
	}
	fe := a.fileEv()
	fe.mu.Lock()
	now := time.Now()
	for k, t := range fe.seen {
		if now.Sub(t) > UploadPollFor {
			delete(fe.seen, k)
		}
	}
	fe.seen[id] = now
	ws := fe.wait[id]
	delete(fe.wait, id)
	fe.mu.Unlock()
	for _, w := range ws {
		close(w)
	}
}

// fileCreated returns a channel closed once file_created names id.
func (a *App) fileCreated(id string) <-chan struct{} {
	fe := a.fileEv()
	fe.mu.Lock()
	defer fe.mu.Unlock()
	c := make(chan struct{})
	if _, ok := fe.seen[id]; ok {
		close(c)
		return c
	}
	fe.wait[id] = append(fe.wait[id], c)
	return c
}

// awaitProcessed waits for Slack to finish processing an uploaded file
// (UploadQueue's PROCESSING state) and returns files.info's object.
func (a *App) awaitProcessed(ctx context.Context, id string) (*slack.File, error) {
	created := a.fileCreated(id)
	a.mu.Lock()
	online := a.connected
	a.mu.Unlock()
	t0 := time.Now()
	if online {
		t := time.NewTimer(UploadEventWait)
		select {
		case <-created:
			t.Stop()
			f, err := a.fileInfo(ctx, id) // resolveCurrentUpload refetches the file
			if err == nil {
				log.Printf("upload: %s processed (file_created) in %s", id, time.Since(t0).Round(time.Millisecond))
				return &f, nil
			}
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		}
	}
	// pollForFileProcessing
	deadline := time.Now().Add(UploadPollFor)
	delay := UploadPollFirst
	for {
		t := time.NewTimer(delay)
		select {
		case <-t.C:
		case <-created:
			t.Stop()
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		}
		if f, err := a.fileInfo(ctx, id); err == nil && !fileStub(&f) {
			log.Printf("upload: %s processed (polled) in %s", id, time.Since(t0).Round(time.Millisecond))
			return &f, nil
		}
		if time.Now().After(deadline) {
			return nil, ErrProcessingTimeout
		}
		delay = min(2*delay, UploadPollMax)
	}
}

// ---- the placeholder ----

// filePlaceholder is one optimistic file message.
type filePlaceholder struct {
	a        *App
	ch, ts   string
	threadTS string
	cid      string
	slots    []*phSlot
	started  bool // some upload was in flight at send: Slack's pendingFiles, with the meta line
	mu       sync.Mutex
	stops    []func()
}

type phSlot struct {
	file    slack.File // what the placeholder shows
	pf      *PendingFile
	preview string // localimg URL of an image's bytes
	done    bool
}

var pendingSeq atomic.Int64

// localFile is the placeholder's object for a file still uploading.
func localFile(self, name, title string, data []byte) (slack.File, string) {
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	f := slack.File{ID: "pending:" + strconv.FormatInt(pendingSeq.Add(1), 10), Name: name, Title: firstNonEmpty(title, name),
		Mimetype: mimeByExt(ext), Filetype: ext, Size: int64(len(data)), Mode: "hosted", User: self, Pending: true}
	var preview string
	if strings.HasPrefix(f.Mimetype, "image/") && len(data) > 0 {
		if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil && cfg.Width > 0 && cfg.Height > 0 {
			preview = localimg.Put(data)
			f.Thumb360, f.OriginalW, f.OriginalH = preview, cfg.Width, cfg.Height
		}
	}
	return f, preview
}

func mimeByExt(ext string) string {
	switch ext {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "bmp":
		return "image/bmp"
	case "heic":
		return "image/heic"
	case "mp4":
		return "video/mp4"
	case "mov":
		return "video/quicktime"
	case "pdf":
		return "application/pdf"
	case "txt":
		return "text/plain"
	}
	return "application/octet-stream"
}

// status is PendingFilesMetaTextWrapper's text (caller holds ph.mu).
func (ph *filePlaceholder) statusLocked() string {
	if !ph.started {
		return ""
	}
	var r *phSlot
	pending := 0
	for _, s := range ph.slots {
		if s.done {
			continue
		}
		pending++
		if r == nil || (s.file.UploadPercent > 0 && r.file.UploadPercent == 0) {
			r = s
		}
	}
	if r == nil {
		return "Sending…"
	}
	if n := 1 + len(ph.slots) - pending; r.file.UploadPercent == 0 && n <= 1 {
		return "Waiting to upload…"
	}
	return "Uploading…"
}

// messageLocked is the placeholder's current message (caller holds ph.mu).
func (ph *filePlaceholder) messageLocked(base slack.Message) slack.Message {
	m := base
	m.Files = make([]slack.File, len(ph.slots))
	for i, s := range ph.slots {
		m.Files[i] = s.file
	}
	m.UploadStatus = ph.statusLocked()
	return m
}

// refresh rewrites the stored placeholder and emits an update.
func (ph *filePlaceholder) refresh() {
	a := ph.a
	ph.mu.Lock()
	for _, s := range ph.slots {
		if s.pf != nil && !s.done {
			s.file.UploadPercent = s.pf.percent()
		}
	}
	files := make([]slack.File, len(ph.slots))
	for i, s := range ph.slots {
		files[i] = s.file
	}
	status := ph.statusLocked()
	ph.mu.Unlock()
	if !a.isPlaceholder(ph.cid, ph.ts) {
		return
	}
	upd, ok := a.S.Mutate(ph.ch, ph.ts, func(m *slack.Message) {
		if m.ClientMsgID == ph.cid {
			m.Files, m.UploadStatus = files, status
		}
	})
	if !ok {
		return
	}
	if ph.threadTS != "" {
		a.S.AddReply(ph.ch, upd)
	}
	v := a.message(ph.ch, &upd)
	a.emit(Event{Type: "update", Channel: ph.ch, TS: ph.ts, ThreadTS: ph.threadTS, Msg: &v, ClientMsgID: ph.cid})
}

// isPlaceholder reports whether cid's local copy (at ts) still stands.
func (a *App) isPlaceholder(cid, ts string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[cid]
	return ok && !p.confirmed && p.ts == ts
}

// resolve swaps a slot's pending file for the uploaded one
// (syncPlaceholderWithUploads).
func (ph *filePlaceholder) resolve(s *phSlot, p PreparedUpload) {
	ph.mu.Lock()
	s.done = true
	if p.File != nil {
		s.file = *p.File
	} else {
		s.file = slack.File{ID: p.ID, Name: p.Name, Title: firstNonEmpty(p.Title, p.Name), Size: p.Size}
	}
	ph.mu.Unlock()
	ph.refresh()
}

// release drops the placeholder's previews and progress watchers.
func (ph *filePlaceholder) release() {
	ph.mu.Lock()
	stops := ph.stops
	ph.stops = nil
	for _, s := range ph.slots {
		localimg.Drop(s.preview)
	}
	ph.mu.Unlock()
	for _, f := range stops {
		f()
	}
}

// startPlaceholder appends the placeholder and announces it.
func (a *App) startPlaceholder(ctx context.Context, o UploadOpts, raws []*PendingFile, cid string) *filePlaceholder {
	self := a.S.SelfID()
	ph := &filePlaceholder{a: a, ch: o.Channel, threadTS: o.ThreadTS, cid: cid, ts: a.pendingTS(o.Channel, o.ThreadTS)}
	for _, p := range o.Prepared {
		f := p.File
		if f == nil {
			if got, err := a.fileInfo(ctx, p.ID); err == nil { // Slack has it in its file store
				f = &got
			}
		}
		s := &phSlot{done: true}
		if f != nil {
			s.file = *f
		} else {
			s.file = slack.File{ID: p.ID, Name: p.Name, Title: firstNonEmpty(p.Title, p.Name), Size: p.Size}
		}
		ph.slots = append(ph.slots, s)
	}
	addPending := func(pf *PendingFile) {
		if res, err, ok := pf.Result(); ok && err == nil {
			ph.slots = append(ph.slots, &phSlot{done: true})
			ph.resolveNow(ph.slots[len(ph.slots)-1], res)
			return
		}
		f, preview := localFile(self, pf.Name, pf.Title, pf.Data)
		f.UploadPercent = pf.percent()
		ph.slots = append(ph.slots, &phSlot{file: f, pf: pf, preview: preview})
		ph.started = true
	}
	for _, pf := range o.Pending {
		addPending(pf)
	}
	for _, rf := range raws {
		addPending(rf)
	}
	for _, s := range ph.slots {
		if s.pf != nil && !s.done {
			ph.stops = append(ph.stops, s.pf.watch(ph.onProgress))
		}
	}
	local := slack.Message{Type: "message", TS: ph.ts, User: self, Text: "", ThreadTS: o.ThreadTS, ClientMsgID: cid,
		Blocks: o.Blocks, ReplyBroadcast: o.Broadcast && o.ThreadTS != ""}
	if len(o.Blocks) == 0 {
		local.Text = o.Comment
	}
	ph.mu.Lock()
	local = ph.messageLocked(local)
	ph.mu.Unlock()
	a.mu.Lock()
	a.pending[cid] = &pendingSend{opts: SendOpts{Channel: o.Channel, ThreadTS: o.ThreadTS}, ts: ph.ts, files: true, release: ph.release}
	a.mu.Unlock()
	a.putLocal(o.Channel, local)
	v := a.message(o.Channel, &local)
	a.emit(Event{Type: "message", Channel: o.Channel, TS: ph.ts, ThreadTS: o.ThreadTS, UserID: self, Msg: &v,
		ClientMsgID: cid, Reply: !isTopLevel(&local), ConvKind: a.kindOf(o.Channel)})
	return ph
}

// resolveNow sets a slot from a finished upload before the placeholder
// is announced.
func (ph *filePlaceholder) resolveNow(s *phSlot, p PreparedUpload) {
	s.done = true
	if p.File != nil {
		s.file = *p.File
	} else {
		s.file = slack.File{ID: p.ID, Name: p.Name, Title: firstNonEmpty(p.Title, p.Name), Size: p.Size}
	}
}

// onProgress re-renders when an upload's status line would change.
func (ph *filePlaceholder) onProgress() {
	ph.mu.Lock()
	before := ph.statusLocked()
	for _, s := range ph.slots {
		if s.pf != nil && !s.done {
			s.file.UploadPercent = s.pf.percent()
		}
	}
	after := ph.statusLocked()
	ph.mu.Unlock()
	if before != after {
		ph.refresh()
	}
}

// sharedAnyway looks for the message with client_msg_id cid in the
// conversation (a share whose reply never came) and confirms the
// placeholder with it.
func (a *App) sharedAnyway(ch, threadTS, cid string) bool {
	ctx, cancel := context.WithTimeout(a.bg, 15*time.Second)
	defer cancel()
	var r struct {
		Messages []slack.Message `json:"messages"`
	}
	method, p := "conversations.history", url.Values{"channel": {ch}, "limit": {"20"}}
	if threadTS != "" {
		method, p = "conversations.replies", url.Values{"channel": {ch}, "ts": {threadTS}, "limit": {"50"}}
	}
	if a.C.Call(ctx, method, p, &r) != nil {
		return false
	}
	for i := range r.Messages {
		if m := r.Messages[i]; m.ClientMsgID == cid {
			a.confirmPending(ch, &m)
			return true
		}
	}
	return false
}

// degradedRefresh is dispatchDegradedModeEventsForFileUpload: without a
// realtime connection no echo comes, so the channel's history (or the
// thread) is refetched after 5 s and the shared message confirms the
// placeholder.
func (a *App) degradedRefresh(ph *filePlaceholder) {
	a.mu.Lock()
	online := a.connected
	a.mu.Unlock()
	if online {
		return
	}
	time.AfterFunc(UploadDegradedRefresh, func() {
		if a.isPlaceholder(ph.cid, ph.ts) && a.sharedAnyway(ph.ch, ph.threadTS, ph.cid) {
			log.Printf("upload: share %s confirmed from history (no realtime)", ph.cid[:8])
		}
	})
}
