package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func clientMsgID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func isCode(err error, codes ...string) bool {
	var se *slack.Error
	if !errors.As(err, &se) {
		return false
	}
	for _, c := range codes {
		if se.Code == c {
			return true
		}
	}
	return false
}

// Edit replaces a message's text (and optional blocks).
func (a *App) Edit(ctx context.Context, ch, ts, text string, blocks json.RawMessage) error {
	return a.C.Update(ctx, ch, ts, text, blocks)
}

// Delete deletes a message.
func (a *App) Delete(ctx context.Context, ch, ts string) error { return a.C.Delete(ctx, ch, ts) }

// DeleteAttachment removes a link preview from a message, as Slack's
// removeAttachmentsIncludingChildren: the attachment (and the others
// unfurled from the same from_url, its children) leave the stored
// message at once, then chat.deleteAttachment goes out for each.
func (a *App) DeleteAttachment(ctx context.Context, ch, ts, id string) error {
	ids := []string{id}
	if m, ok := a.S.Find(ch, ts); ok {
		from := ""
		for _, at := range m.Attachments {
			if strconv.Itoa(at.ID) == id {
				from = at.FromURL
			}
		}
		if from != "" {
			for _, at := range m.Attachments {
				if s := strconv.Itoa(at.ID); at.FromURL == from && s != id {
					ids = append(ids, s)
				}
			}
		}
	}
	drop := map[string]bool{}
	for _, s := range ids {
		drop[s] = true
	}
	if upd, ok := a.S.Mutate(ch, ts, func(x *slack.Message) {
		kept := x.Attachments[:0:0]
		for _, at := range x.Attachments {
			if !drop[strconv.Itoa(at.ID)] {
				kept = append(kept, at)
			}
		}
		x.Attachments = kept
	}); ok {
		a.emitUpdate(ch, &upd)
	}
	for _, s := range ids {
		if err := a.C.DeleteAttachment(ctx, ch, ts, s); err != nil {
			return err
		}
	}
	return nil
}

// Mark moves the read cursor of ch to ts and returns the new unread state.
// Marking an earlier ts keeps the conversation unread (see MarkUnread).
func (a *App) Mark(ctx context.Context, ch, ts string) (Count, error) {
	if a.isLocalTS(ts) { // a pending local send: mark the newest confirmed message instead
		if tl := a.S.Timeline(ch); tl != nil {
			for i := len(tl.Msgs) - 1; i >= 0; i-- {
				if !a.isLocalTS(tl.Msgs[i].TS) {
					ts = tl.Msgs[i].TS
					break
				}
			}
		}
		if a.isLocalTS(ts) {
			return a.Count(ch), nil
		}
	}
	if err := a.C.Mark(ctx, ch, ts); err != nil {
		return a.Count(ch), err
	}
	e := a.S.MarkRead(ch, ts)
	cv := a.Count(ch)
	a.emit(Event{Type: "marked", Channel: ch, TS: ts, Count: &cv})
	if e.HasUnreads {
		a.refreshCountsSoon() // the server knows the mention count below the new cursor
	}
	return cv, nil
}

// MarkUnread marks ts and everything after it unread ("Mark unread" in the
// message menu): the read cursor moves to just before ts.
func (a *App) MarkUnread(ctx context.Context, ch, ts string) (Count, error) {
	return a.Mark(ctx, ch, tsBefore(a.S.Timeline(ch), ts))
}

// tsBefore returns the ts of the message preceding ts in t, or ts minus one
// microsecond when it is unknown.
func tsBefore(t *store.Timeline, ts string) string {
	if t != nil {
		prev := ""
		for _, m := range t.Msgs {
			if !store.TSLess(m.TS, ts) {
				break
			}
			prev = m.TS
		}
		if prev != "" {
			return prev
		}
	}
	sec, frac, ok := strings.Cut(ts, ".")
	if !ok || len(frac) != 6 {
		return ts
	}
	s, err1 := strconv.ParseInt(sec, 10, 64)
	f, err2 := strconv.ParseInt(frac, 10, 64)
	if err1 != nil || err2 != nil {
		return ts
	}
	if f == 0 {
		s, f = s-1, 999999
	} else {
		f--
	}
	return fmt.Sprintf("%d.%06d", s, f)
}

// Typing sends a typing indicator (optionally in a thread). Call it at most
// every few seconds while the user types.
func (a *App) Typing(ch, threadTS string) error { return a.RTM.Typing(ch, threadTS) }

// Upload shares files (held in memory, never written to disk) the way
// Slack's composer sends attached files (upload_send.go): the placeholder
// goes into the conversation at once; then the share waits for every
// upload: Pending ones (the composer's eager uploads), and new Files
// uploaded now (PrepareUpload; o.Progress reports bytes across them);
// renamed Prepared ones get files.edit; finally one files.share with the
// message's blocks (or its plain comment) and the placeholder's
// client_msg_id. The realtime echo replaces the placeholder; a failure
// removes it.
func (a *App) Upload(ctx context.Context, o UploadOpts) ([]UploadedFile, error) {
	if len(o.Files) == 0 && len(o.Prepared) == 0 && len(o.Pending) == 0 {
		return nil, errors.New("core: Upload needs a file")
	}
	t0 := time.Now()
	cid := clientMsgID()
	raws := make([]*PendingFile, len(o.Files))
	for i, f := range o.Files {
		raws[i] = NewPendingFile(f.Name, firstNonEmpty(f.Title, f.Name), f.Data)
	}
	var ph *filePlaceholder
	if o.Channel != "" {
		ph = a.startPlaceholder(ctx, o, raws, cid)
	}
	fail := func(err error) ([]UploadedFile, error) {
		if ph != nil {
			a.DiscardPending(o.Channel, cid) // cleanUpPlaceholder
		}
		return nil, err
	}
	slot := func(i int) *phSlot {
		if ph == nil || i >= len(ph.slots) {
			return nil
		}
		return ph.slots[i]
	}
	n := 0
	out := make([]UploadedFile, 0, len(o.Prepared)+len(o.Pending)+len(o.Files))
	ids := make([]string, 0, cap(out))
	add := func(p PreparedUpload) error {
		if p.Title != "" && p.Name != "" && p.Title != p.Name {
			// renamed after the upload (completeUpload titled it by name)
			if err := a.C.EditFile(ctx, p.ID, p.Title); err != nil {
				return err
			}
		}
		ids = append(ids, p.ID)
		out = append(out, UploadedFile{ID: p.ID, Name: p.Name, Title: firstNonEmpty(p.Title, p.Name)})
		return nil
	}
	for _, p := range o.Prepared {
		if err := add(p); err != nil {
			return fail(err)
		}
		n++
	}
	for _, pf := range o.Pending {
		p, err := pf.Wait(ctx)
		if err != nil {
			return fail(err)
		}
		if s := slot(n); s != nil && !s.done {
			ph.resolve(s, p)
		}
		if err := add(p); err != nil {
			return fail(err)
		}
		n++
	}
	var all, done int64
	for _, f := range o.Files {
		all += int64(len(f.Data))
	}
	for i, f := range o.Files {
		base, size := done, int64(len(f.Data))
		pf := raws[i]
		prog := func(sent, total int64) {
			pf.Progress(sent, total)
			if o.Progress != nil {
				if total > 0 {
					sent = sent * size / total // body bytes -> file bytes
				}
				o.Progress(base+sent, all)
			}
		}
		p, err := a.PrepareUpload(ctx, f, prog)
		pf.Finish(p, err)
		if err != nil {
			return fail(err)
		}
		p.Title = firstNonEmpty(f.Title, f.Name)
		done += size
		if s := slot(n); s != nil && !s.done {
			ph.resolve(s, p)
		}
		if err := add(p); err != nil {
			return fail(err)
		}
		n++
	}
	log.Printf("upload: sharing %d file(s) as %s", len(ids), cid[:8])
	err := a.C.ShareFiles(ctx, slack.ShareOpts{Files: ids, Channel: o.Channel, ThreadTS: o.ThreadTS, Broadcast: o.Broadcast,
		Blocks: o.Blocks, Comment: o.Comment, ClientMsgID: cid})
	if err != nil {
		var se *slack.Error
		if ph != nil && !errors.As(err, &se) && a.sharedAnyway(o.Channel, o.ThreadTS, cid) {
			// the reply was lost but Slack posted it: confirmed, so a
			// retry from the composer can't post it twice
			log.Printf("upload: share %s reply lost (%s); found in history", cid[:8], uploadErrClass(err))
			return out, nil
		}
		log.Printf("upload: share %s failed (%s); placeholder removed", cid[:8], uploadErrClass(err))
		return fail(err)
	}
	if ph != nil {
		a.degradedRefresh(ph)
	}
	log.Printf("upload: %d file(s), %d new bytes in %s", len(ids), all, time.Since(t0).Round(time.Millisecond))
	return out, nil
}

// PrepareUpload uploads a file's bytes as soon as it is attached
// (files.getUploadURL, the bytes, files.completeUpload) without sharing
// it, then waits until Slack has processed it (awaitProcessed): the
// result carries files.info's object. Pass it in UploadOpts.Prepared (or
// through a PendingFile) when sending. progress (may be nil) runs on the
// calling goroutine. Unshared prepared files expire on Slack's side.
//
// Like Slack's UploadQueue, a first failure retries the whole upload once
// (_retryUpload: a new upload slot); Slack API errors (ok:false) don't.
// Every step is logged with the file id, sizes and timings only.
func (a *App) PrepareUpload(ctx context.Context, f UploadFile, progress func(sent, total int64)) (PreparedUpload, error) {
	id, err := a.uploadOnce(ctx, f, progress)
	if err != nil && retryableUpload(ctx, err) {
		log.Printf("upload: first attempt failed (%s); retrying once", uploadErrClass(err))
		id, err = a.uploadOnce(slack.NoBackoff(ctx), f, progress) // a deliberate retry: past the failure backoff
	}
	if err != nil {
		log.Printf("upload: failed (%s)", uploadErrClass(err))
		return PreparedUpload{}, err
	}
	pu := PreparedUpload{ID: id, Name: f.Name, Title: f.Title, Size: int64(len(f.Data))}
	file, err := a.awaitProcessed(ctx, id)
	if err != nil {
		log.Printf("upload: %s processing failed (%s)", id, uploadErrClass(err))
		return PreparedUpload{}, err
	}
	pu.File = file
	return pu, nil
}

// uploadOnce is one eager upload attempt: files.getUploadURL {filename,
// length, snippet_type, alt_txt} (Slack's getUploadURL fetcher sends the
// last two for snippets and for images with alt text), the multipart POST
// to upload_url, files.completeUpload.
func (a *App) uploadOnce(ctx context.Context, f UploadFile, progress func(sent, total int64)) (string, error) {
	t0 := time.Now()
	var r struct {
		File      string `json:"file"`
		FileID    string `json:"file_id"`
		UploadURL string `json:"upload_url"`
	}
	n := len(f.Data)
	args := url.Values{"filename": {f.Name}, "length": {strconv.Itoa(n)}}
	if f.SnippetType != "" {
		args.Set("snippet_type", f.SnippetType)
	}
	if f.AltText != "" {
		args.Set("alt_txt", f.AltText)
	}
	if err := a.C.Call(ctx, "files.getUploadURL", args, &r); err != nil {
		return "", err
	}
	id := firstNonEmpty(r.File, r.FileID)
	if id == "" || r.UploadURL == "" {
		return "", errors.New("core: files.getUploadURL returned no upload slot")
	}
	log.Printf("upload: %s slot for %d bytes", id, n)
	if err := a.C.UploadBytes(ctx, r.UploadURL, f.Name, f.Data, progress); err != nil {
		log.Printf("upload: %s POST failed after %s (%s)", id, time.Since(t0).Round(time.Millisecond), uploadErrClass(err))
		return "", err
	}
	log.Printf("upload: %s POST %d bytes in %s", id, n, time.Since(t0).Round(time.Millisecond))
	if err := a.C.CompleteUpload(ctx, []slack.UploadedRef{{ID: id, Title: firstNonEmpty(f.Title, f.Name)}}); err != nil {
		log.Printf("upload: %s completeUpload failed (%s)", id, uploadErrClass(err))
		return "", err
	}
	log.Printf("upload: %s complete; processing", id)
	return id, nil
}

// retryableUpload: anything but a Slack API error, a sign-out or the
// caller giving up gets UploadQueue's one automatic retry.
func retryableUpload(ctx context.Context, err error) bool {
	var se *slack.Error
	return ctx.Err() == nil && !errors.As(err, &se) && !errors.Is(err, slack.ErrSignedOut) &&
		!errors.Is(err, slack.ErrUntrustedHost) && !errors.Is(err, context.Canceled)
}

// uploadErrClass is a log-safe description of an upload error: the API
// method and code, the HTTP status, or "network error".
func uploadErrClass(err error) string {
	var se *slack.Error
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &se):
		return se.Method + " " + se.Code
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, ErrProcessingTimeout):
		return "processing timed out"
	}
	if _, rest, ok := strings.Cut(err.Error(), ": http "); ok && len(rest) >= 3 {
		return "http " + rest[:3]
	}
	return "network error"
}

// Pin / Unpin a message.
func (a *App) Pin(ctx context.Context, ch, ts string) error {
	err := a.C.Call(ctx, "pins.add", url.Values{"channel": {ch}, "timestamp": {ts}}, nil)
	if isCode(err, "already_pinned") {
		return nil
	}
	return err
}

func (a *App) Unpin(ctx context.Context, ch, ts string) error {
	err := a.C.Call(ctx, "pins.remove", url.Values{"channel": {ch}, "timestamp": {ts}}, nil)
	if isCode(err, "no_pin") {
		return nil
	}
	return err
}

func savedParams(ch, ts string) url.Values {
	return url.Values{"item_type": {"message"}, "item_id": {ch}, "ts": {ts}}
}

// Save / Unsave a message ("Save for later"). dateDue 0 means no reminder.
func (a *App) Save(ctx context.Context, ch, ts string, dateDue int64) error {
	p := savedParams(ch, ts)
	if dateDue > 0 {
		p.Set("date_due", strconv.FormatInt(dateDue, 10))
	}
	return a.C.Call(ctx, "saved.add", p, nil)
}

func (a *App) Unsave(ctx context.Context, ch, ts string) error {
	return a.C.Call(ctx, "saved.delete", savedParams(ch, ts), nil)
}

// UpdateSaved marks a saved item completed/archived or changes its due date.
func (a *App) UpdateSaved(ctx context.Context, ch, ts string, u SavedUpdate) error {
	p := savedParams(ch, ts)
	if u.Completed != nil {
		p.Set("mark", map[bool]string{true: "completed", false: "uncompleted"}[*u.Completed])
	}
	if u.Archived != nil {
		p.Set("mark", map[bool]string{true: "archived", false: "unarchived"}[*u.Archived])
	}
	if u.DateDue != nil {
		p.Set("date_due", strconv.FormatInt(*u.DateDue, 10))
	}
	return a.C.Call(ctx, "saved.update", p, nil)
}

// SubscribeThread follows or unfollows a thread.
func (a *App) SubscribeThread(ctx context.Context, ch, threadTS string, follow bool) error {
	m := "subscriptions.thread.remove"
	if follow {
		m = "subscriptions.thread.add"
	}
	err := a.C.Call(ctx, m, url.Values{"channel": {ch}, "thread_ts": {threadTS}}, nil)
	if err == nil {
		// The root's subscribed flag now, as the thread_subscribed event
		// would set it (it may come late or, on some workspaces, not at all).
		if msg, ok := a.S.Mutate(ch, threadTS, func(m *slack.Message) { m.Subscribed = follow }); ok {
			a.emitUpdate(ch, &msg)
		}
		a.refreshCountsSoon()
	}
	return err
}

// MarkThread marks a thread read up to ts.
func (a *App) MarkThread(ctx context.Context, ch, threadTS, ts string) error {
	err := a.C.Call(ctx, "subscriptions.thread.mark", url.Values{"channel": {ch}, "thread_ts": {threadTS}, "ts": {ts}, "read": {"1"}}, nil)
	if err == nil {
		a.refreshCountsSoon()
	}
	return err
}

// Join joins a channel and adds it to the sidebar.
func (a *App) Join(ctx context.Context, ch string) (Conv, error) { return a.join(ctx, ch, 0) }

// join is conversations.join, with expiration_ts for a temporary
// membership (JoinFor).
func (a *App) join(ctx context.Context, ch string, exp int64) (Conv, error) {
	var res struct {
		Channel slack.Conversation `json:"channel"`
	}
	if err := a.C.Call(ctx, "conversations.join", joinParams(ch, exp), &res); err != nil {
		return Conv{}, err
	}
	a.setTemp(ch, exp)
	c := res.Channel
	if full, err := a.C.ConversationInfo(ctx, ch); err == nil {
		c = *full
	}
	a.S.PutConv(c)
	v := a.conv(&c)
	a.emit(Event{Type: "conv", Channel: ch, Conv: &v})
	return v, nil
}

// Leave leaves a channel (or closes a DM) and removes it from the sidebar.
func (a *App) Leave(ctx context.Context, ch string) error {
	m := "conversations.leave"
	if strings.HasPrefix(ch, "D") {
		m = "conversations.close"
	}
	if err := a.C.Call(ctx, m, url.Values{"channel": {ch}}, nil); err != nil {
		return err
	}
	if m == "conversations.close" {
		a.markDMClosed(ch) // still known (search, switcher); back on a new message
	} else {
		a.dropMembership(ch, false)
	}
	a.emit(Event{Type: "conv_removed", Channel: ch})
	return nil
}

// OpenDM opens (or resumes) a DM with one user or a group DM with several,
// and adds it to the sidebar.
func (a *App) OpenDM(ctx context.Context, users ...string) (Conv, error) {
	if len(users) == 0 {
		return Conv{}, errors.New("core: OpenDM needs users")
	}
	var res struct {
		Channel slack.Conversation `json:"channel"`
	}
	if err := a.C.Call(ctx, "conversations.open", url.Values{"users": {strings.Join(users, ",")}, "return_im": {"true"}}, &res); err != nil {
		return Conv{}, err
	}
	c := res.Channel
	if c.ID != "" && c.User == "" && !c.IsMPIM {
		if full, err := a.C.ConversationInfo(ctx, c.ID); err == nil {
			c = *full
		}
	}
	if len(users) == 1 && c.User == "" {
		c.IsIM, c.User = true, users[0]
	}
	a.EnsureUsers(ctx, append(users, c.User))
	a.S.PutConv(c)
	a.openDM(c.ID)
	v := a.conv(&c)
	a.emit(Event{Type: "conv", Channel: c.ID, Conv: &v})
	return v, nil
}

// AddToDM adds people to a DM or group DM. Like Slack, this opens the
// group DM of everyone involved (a new conversation; history is not
// shared) and adds it to the sidebar.
func (a *App) AddToDM(ctx context.Context, ch string, users ...string) (Conv, error) {
	c, ok := a.S.Conv(ch)
	if !ok || !(c.IsIM || c.IsMPIM) {
		return Conv{}, errors.New("core: AddToDM needs a DM or group DM")
	}
	members := append([]string(nil), c.Members...)
	if c.IsIM {
		members = []string{c.User}
	} else if len(members) == 0 {
		us, _, err := a.C.ChannelMembers(ctx, ch, "", 100, false)
		if err != nil {
			return Conv{}, err
		}
		for _, u := range us {
			members = append(members, u.ID)
		}
	}
	self := a.S.SelfID()
	seen := map[string]bool{self: true}
	var all []string
	for _, u := range append(members, users...) {
		if u != "" && !seen[u] {
			seen[u] = true
			all = append(all, u)
		}
	}
	if len(all) > 8 {
		return Conv{}, errors.New("core: a group DM holds at most 9 people")
	}
	return a.OpenDM(ctx, all...)
}

// SetStatus sets the viewer's custom status (empty text and emoji clear it).
func (a *App) SetStatus(ctx context.Context, text, emoji string, expiration int64) error {
	if emoji != "" && !strings.HasPrefix(emoji, ":") {
		emoji = ":" + emoji + ":"
	}
	prof, _ := json.Marshal(map[string]any{"status_text": text, "status_emoji": emoji, "status_expiration": expiration})
	return a.C.Call(ctx, "users.profile.set", url.Values{"profile": {string(prof)}}, nil)
}

// ManuallyAway reports the viewer's manual_presence "away" (users.getPresence
// for the caller: manual_away), what Slack's presence prompt checks.
func (a *App) ManuallyAway(ctx context.Context) (bool, error) {
	var r struct {
		ManualAway bool `json:"manual_away"`
	}
	if err := a.C.Call(ctx, "users.getPresence", url.Values{}, &r); err != nil {
		return false, err
	}
	return r.ManualAway, nil
}

// SetPresence sets "auto" or "away".
func (a *App) SetPresence(ctx context.Context, presence string) error {
	if presence != "auto" && presence != "away" {
		return errors.New("core: presence must be auto or away")
	}
	return a.C.Call(ctx, "users.setPresence", url.Values{"presence": {presence}}, nil)
}

// SetDND snoozes notifications for minutes (0 ends the snooze).
func (a *App) SetDND(ctx context.Context, minutes int) error {
	if minutes > 0 {
		return a.C.Call(ctx, "dnd.setSnooze", url.Values{"num_minutes": {strconv.Itoa(minutes)}}, nil)
	}
	return a.C.Call(ctx, "dnd.endSnooze", nil, nil)
}

// Command runs a slash command. /shrug, /me and /topic are handled
// locally like the official client; others go through chat.command
// (command.go: client_token, so a modal the app opens shows here).
func (a *App) Command(ctx context.Context, o CommandOpts) (CommandResult, error) {
	cmd := o.Command
	if !strings.HasPrefix(cmd, "/") {
		cmd = "/" + cmd
	}
	switch cmd {
	case "/shrug":
		m, err := a.Send(ctx, SendOpts{Channel: o.Channel, Text: strings.TrimSpace(o.Text + ` ¯\_(ツ)_/¯`), ThreadTS: o.ThreadTS})
		if err != nil {
			return CommandResult{}, err
		}
		return CommandResult{Msg: &m}, nil
	case "/me":
		return CommandResult{}, a.C.Call(ctx, "chat.meMessage", url.Values{"channel": {o.Channel}, "text": {o.Text}}, nil)
	case "/topic":
		return CommandResult{}, a.SetTopic(ctx, o.Channel, o.Text)
	}
	return a.serverCommand(ctx, cmd, o) // command.go
}
