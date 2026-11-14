package core

// Canvas comments (docs/CANVASES.md "Comments"), from Slack's web client
// (modern.vendor build 93ac48dd3c752f09; the comment stream's lazy chunk
// quip-popout_quip-comment-stream-view 0893e737; boot-shared f9df0f12),
// read from the public CDN without a cookie.
//
// A canvas's comments are Slack messages in the canvas's file channel, the
// file's id with F → C (7/Dp l/p). Each comment thread is a thread there:
// its root has subtype document_comment_root and a document_comment
// {thread_id, authors, is_archived, is_visible, last_seen,
// most_recent_editor}; the root's text is the highlighted part of the
// canvas, the comments are its replies. Quip makes the root when someone
// highlights text and starts a thread (a CONTROL_HIGHLIGHT_TYPE control
// saved through Quip's binary edit-document, then
// comments_data_provider startCommentsThread); everything after that is
// the Web API:
//
//   - the comment stream (documentHistoryFetcher): conversations.history
//     {channel, limit 20, include_pin_count, inclusive, no_user_profile,
//     include_free_team_extra_messages, include_date_joined false,
//     document_stream: latest_reply | unread | archived, cursor}; the
//     answer's document_comment_root messages are the roots and the other
//     replies (thread_ts ≠ ts) their latest replies (pkMj W$).
//   - a thread (requestQuipComments → fetchReplies): conversations.replies
//     {channel, ts, inclusive, limit}; Slack may pass the "Qpc:" form of
//     the Quip id as ts before it knows the root.
//   - replies, edits, deletes and reactions: chat.postMessage (thread_ts =
//     the root), chat.update, chat.delete, reactions.add/remove, as for any
//     thread.
//   - Resolve thread / Re-open thread (updateArchiveState, XuFq):
//     quip.thread.archive / quip.thread.unarchive {channel_id, thread_ts}
//     (createApiMethodFetcher camel-cases its input to snake case); the
//     answer's message replaces the root.
//   - Resolve all threads (archiveAllThreads): quip.thread.archiveAll
//     {channel_id}.

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/slack"
)

// Comment stream filters (uh5G: the document_stream values).
const (
	CommentsLatest   = "latest_reply" // "Most recent"
	CommentsUnread   = "unread"       // "Unread"
	CommentsResolved = "archived"     // "Resolved"
)

// SubtypeCommentRoot is a canvas comment thread's root message.
const SubtypeCommentRoot = "document_comment_root"

// CanvasFileChannel is the file channel of canvas fileID (7/Dp p: F → C;
// FileOfChannel, sidebar_docs.go, goes back).
func CanvasFileChannel(fileID string) string {
	if strings.HasPrefix(fileID, "F") {
		return "C" + fileID[1:]
	}
	return fileID
}

// IsCommentRoot reports whether m is a canvas comment thread's root
// (KN0O isDocumentCommentRootMessage).
func (m *Message) IsCommentRoot() bool { return m != nil && m.Subtype == SubtypeCommentRoot }

// CommentThreadID is the root's Quip comment thread (T6ns S:
// document_comment.thread_id), "" for other messages.
func (m *Message) CommentThreadID() string {
	if m == nil || m.DocumentComment == nil {
		return ""
	}
	return m.DocumentComment.ThreadID
}

// CommentResolved is REqS isArchivedDocumentComment.
func (m *Message) CommentResolved() bool {
	return m != nil && m.DocumentComment != nil && m.DocumentComment.IsArchived
}

// CommentRead is p6HR isDocumentCommentRead: whether the viewer has seen
// the root's latest reply (last_seen, µs, equals latest_reply). ok is
// false when Slack can't tell (no latest reply or no last_seen), and the
// stream then draws no unread separator.
func (m *Message) CommentRead() (read, ok bool) {
	if m == nil || m.DocumentComment == nil || m.LatestReply == "" || m.DocumentComment.LastSeen == "" {
		return false, false
	}
	seen, err := m.DocumentComment.LastSeen.Int64()
	if err != nil {
		return false, false
	}
	return tsMicros(m.LatestReply) == seen, true
}

// tsMicros is a message ts in microseconds (eQ kZ).
func tsMicros(ts string) int64 {
	sec, frac, _ := strings.Cut(ts, ".")
	s, err := strconv.ParseInt(sec, 10, 64)
	if err != nil {
		return 0
	}
	frac = (frac + "000000")[:6]
	f, _ := strconv.ParseInt(frac, 10, 64)
	return s*1e6 + f
}

// EncodeCommentThreadID is neAw encodeQuipCommentThreadId: "Qpc:" and the
// id with "temp:" condensed to "t:" (the ts Slack can pass before the
// root is known).
func EncodeCommentThreadID(id string) string { return "Qpc:" + strings.Replace(id, "temp:", "t:", 1) }

// CanvasCommentsPage is one page of a canvas's comment stream.
type CanvasCommentsPage struct {
	// Roots are the thread roots in the server's order, each once
	// (getRows' uniqBy ts).
	Roots []Message
	// Replies are the latest replies that came with them, by root ts
	// (the reducer's replies map).
	Replies map[string][]Message
	Cursor  string
	HasMore bool
}

// CanvasComments is documentHistoryFetcher: one page (20 roots) of
// canvas fileID's comment threads under filter (CommentsLatest,
// CommentsUnread, CommentsResolved), from cursor ("" = the first page).
// A canvas whose file channel doesn't exist yet has no comments.
func (a *App) CanvasComments(ctx context.Context, fileID, filter, cursor string) (CanvasCommentsPage, error) {
	ch := CanvasFileChannel(fileID)
	p := url.Values{
		"channel": {ch}, "limit": {"20"}, "include_pin_count": {"true"}, "inclusive": {"true"},
		"no_user_profile": {"true"}, "include_free_team_extra_messages": {"true"}, "include_date_joined": {"false"},
	}
	if filter != "" {
		p.Set("document_stream", filter)
	}
	if cursor != "" {
		p.Set("cursor", cursor)
	}
	var r struct {
		Messages []slack.Message `json:"messages"`
		HasMore  bool            `json:"has_more"`
		Meta     struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := a.C.Call(ctx, "conversations.history", p, &r); err != nil {
		var se *slack.Error
		if errors.As(err, &se) && se.Code == "channel_not_found" { // (0,O.uI)(e,"channel_not_found"): no threads
			return CanvasCommentsPage{Replies: map[string][]Message{}}, nil
		}
		return CanvasCommentsPage{}, err
	}
	out := CanvasCommentsPage{Replies: map[string][]Message{}, Cursor: r.Meta.NextCursor, HasMore: r.HasMore}
	seen := map[string]bool{}
	for i := range r.Messages {
		m := &r.Messages[i]
		switch {
		case m.Subtype == SubtypeCommentRoot:
			if seen[m.TS] {
				continue
			}
			seen[m.TS] = true
			a.S.Upsert(ch, *m)
			out.Roots = append(out.Roots, a.message(ch, m))
		case (m.Type == "message" || m.Type == "") && m.ThreadTS != "" && m.ThreadTS != m.TS:
			out.Replies[m.ThreadTS] = append(out.Replies[m.ThreadTS], a.message(ch, m))
		}
	}
	return out, nil
}

// SetCommentResolved is updateArchiveState: quip.thread.archive (resolve)
// or quip.thread.unarchive (re-open) on the thread rooted at ts in file
// channel ch. The answer's message replaces the stored root, and an
// "update" event carries it.
func (a *App) SetCommentResolved(ctx context.Context, ch, ts string, resolve bool) error {
	method := "quip.thread.unarchive"
	if resolve {
		method = "quip.thread.archive"
	}
	var r struct {
		Message *slack.Message `json:"message"`
	}
	if err := a.C.Call(ctx, method, url.Values{"channel_id": {ch}, "thread_ts": {ts}}, &r); err != nil {
		return err
	}
	if r.Message != nil && r.Message.TS == ts {
		upd := *r.Message
		if m, ok := a.S.Mutate(ch, ts, func(x *slack.Message) {
			keep := *x
			*x = upd
			if x.Reactions == nil {
				x.Reactions = keep.Reactions
			}
		}); ok {
			a.emitUpdate(ch, &m)
			return nil
		}
		a.emitUpdate(ch, &upd)
		return nil
	}
	if m, ok := a.S.Mutate(ch, ts, func(x *slack.Message) { // no message in the answer: flip the flag
		if x.DocumentComment == nil {
			x.DocumentComment = &slack.MessageDocumentComment{}
		}
		x.DocumentComment.IsArchived = resolve
	}); ok {
		a.emitUpdate(ch, &m)
	}
	return nil
}

// ResolveAllComments is archiveAllThreads: quip.thread.archiveAll
// {channel_id} on canvas fileID's file channel.
func (a *App) ResolveAllComments(ctx context.Context, fileID string) error {
	return a.C.Call(ctx, "quip.thread.archiveAll", url.Values{"channel_id": {CanvasFileChannel(fileID)}}, nil)
}

// CanArchiveComment is canArchiveComment (+IQ1+): the thread's file
// channel is known, the canvas is writeable (isWriteable: edit access and
// not locked), the root has a Quip thread id and at least one reply.
// info is the canvas's files.info.
func CanArchiveComment(info CanvasInfo, root Message) bool {
	return info.Editable && !info.Locked && root.CommentThreadID() != "" && root.ReplyCount > 0
}

// CanResolveAllComments is canArchiveAllComments (comment stream chunk):
// a quip file the viewer can write that isn't locked.
func CanResolveAllComments(info CanvasInfo) bool { return info.Editable && !info.Locked }

// KnownCanvasInfo is the last files.info Clack read for canvas id (the
// canvas view, a comment thread, the comment stream), if any.
func (a *App) KnownCanvasInfo(id string) (CanvasInfo, bool) {
	cs := a.canvases()
	cs.mu.Lock()
	defer cs.mu.Unlock()
	i, ok := cs.infos[id]
	return i, ok
}

func (a *App) rememberCanvasInfo(i CanvasInfo) {
	if i.ID == "" {
		return
	}
	cs := a.canvases()
	cs.mu.Lock()
	cs.infos[i.ID] = i
	if len(cs.infos) > 256 {
		for k := range cs.infos {
			if k != i.ID {
				delete(cs.infos, k)
				break
			}
		}
	}
	cs.mu.Unlock()
}

// IsCanvasComment is isCanvasComment (C3jk l → z1wc k): m is in a file
// channel whose file is a canvas (known from its files.info, or from the
// thread's root being a comment root).
func (a *App) IsCanvasComment(m *Message) bool {
	if m == nil || !strings.HasPrefix(m.Channel, "C") {
		return false
	}
	if c, ok := a.S.Conv(m.Channel); ok && !IsFileChannel(c) {
		return false
	}
	if _, ok := a.KnownCanvasInfo(FileOfChannel(m.Channel)); ok {
		return true
	}
	root := m.ThreadTS
	if root == "" {
		root = m.TS
	}
	r, ok := a.S.Find(m.Channel, root)
	return ok && r.Subtype == SubtypeCommentRoot
}

// CanAddReaction is canAddReaction's file channel rule (vtEK): a message
// in a canvas's file channel can take a reaction when the canvas is
// writeable, or commentable under canvas_comment_only_mode. Until the
// canvas's files.info is known, and for every other message, it can.
func (a *App) CanAddReaction(m *Message) bool {
	if m == nil {
		return false
	}
	if !strings.HasPrefix(m.Channel, "C") {
		return true
	}
	info, ok := a.KnownCanvasInfo(FileOfChannel(m.Channel))
	if !ok {
		return true
	}
	return a.CanReactToComment(info)
}

// CanReactToComment is canAddReaction's canvas comment rule (vtEK): in a
// canvas's file channel, a viewer who can't edit the canvas may react
// only when it is commentable (access comment, write or owner) and
// canvas_comment_only_mode is on.
func (a *App) CanReactToComment(info CanvasInfo) bool {
	if info.Editable && !info.Locked {
		return true
	}
	return info.Commentable() && a.experiment(ExpCanvasCommentOnly) == "on"
}

// ExpCanvasCommentOnly is canvas_comment_only_mode (read in
// experiment_reads_more.go): comment access for canvases (the access
// indicator's "Comment only", reacting to comments with comment access,
// the embedded menus' comment rows).
const ExpCanvasCommentOnly = "canvas_comment_only_mode"

// Commentable is x1Bi isCommentable: access comment, write or owner.
func (i CanvasInfo) Commentable() bool {
	return i.Access == "comment" || i.Access == "write" || i.Access == "owner"
}
