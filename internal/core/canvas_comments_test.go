package core

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

func commentsApp(t *testing.T) (*App, *fakeslack.Server) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	return a, fs
}

func rootTSs(ms []Message) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.TS)
	}
	return out
}

// The comment stream is documentHistoryFetcher on the canvas's file
// channel: the arguments Slack sends, the roots per document_stream with
// their latest replies, the read state; resolving is quip.thread.archive
// / unarchive and archiveAll.
func TestCanvasComments(t *testing.T) {
	a, fs := commentsApp(t)
	ctx := context.Background()
	if ch := CanvasFileChannel(fakeslack.CanvasChannel); ch != fakeslack.FileChanLaunch || FileOfChannel(ch) != fakeslack.CanvasChannel {
		t.Fatalf("file channel %q", ch)
	}
	p, err := a.CanvasComments(ctx, fakeslack.CanvasChannel, CommentsLatest, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rootTSs(p.Roots), []string{fakeslack.CommentUnread, fakeslack.CommentOpen, fakeslack.CommentReacted}; !equalStrings(got, want) {
		t.Fatalf("Most recent roots %v, want %v (open threads, latest reply first)", got, want)
	}
	calls := fs.CommentHistoryCalls()
	if len(calls) != 1 {
		t.Fatalf("%d history calls", len(calls))
	}
	for k, v := range map[string]string{"channel": fakeslack.FileChanLaunch, "limit": "20", "document_stream": "latest_reply",
		"include_pin_count": "true", "inclusive": "true", "no_user_profile": "true", "include_free_team_extra_messages": "true",
		"include_date_joined": "false"} {
		if g := calls[0].Get(k); g != v {
			t.Errorf("history %s=%q, want %q", k, g, v)
		}
	}
	for _, r := range p.Roots {
		if !r.IsCommentRoot() || r.CommentThreadID() == "" || r.Channel != fakeslack.FileChanLaunch {
			t.Errorf("root %s: subtype %q thread %q channel %q", r.TS, r.Subtype, r.CommentThreadID(), r.Channel)
		}
		reps := p.Replies[r.TS]
		if len(reps) != 1 || reps[0].TS != r.LatestReply {
			t.Errorf("root %s: replies %v, want its latest %s", r.TS, rootTSs(reps), r.LatestReply)
		}
	}
	if read, ok := p.Roots[0].CommentRead(); !ok || read {
		t.Errorf("CommentUnread: read %v ok %v", read, ok)
	}
	if read, ok := p.Roots[1].CommentRead(); !ok || !read {
		t.Errorf("CommentOpen: read %v ok %v", read, ok)
	}
	for f, want := range map[string][]string{CommentsUnread: {fakeslack.CommentUnread}, CommentsResolved: {fakeslack.CommentResolved}} {
		p, err := a.CanvasComments(ctx, fakeslack.CanvasChannel, f, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := rootTSs(p.Roots); !equalStrings(got, want) {
			t.Errorf("%s: roots %v, want %v", f, got, want)
		}
	}
	// a canvas without a file channel: no threads, no error
	if p, err := a.CanvasComments(ctx, "F0NOCOMMENTS", CommentsLatest, ""); err != nil || len(p.Roots) != 0 {
		t.Errorf("no file channel: %v %v", p.Roots, err)
	}

	// Resolve thread / Re-open thread
	if err := a.SetCommentResolved(ctx, fakeslack.FileChanLaunch, fakeslack.CommentOpen, true); err != nil {
		t.Fatal(err)
	}
	if err := a.SetCommentResolved(ctx, fakeslack.FileChanLaunch, fakeslack.CommentResolved, false); err != nil {
		t.Fatal(err)
	}
	cc := fs.CommentCalls()
	if len(cc) != 2 || cc[0].Get("method") != "quip.thread.archive" || cc[0].Get("channel_id") != fakeslack.FileChanLaunch ||
		cc[0].Get("thread_ts") != fakeslack.CommentOpen || cc[1].Get("method") != "quip.thread.unarchive" || cc[1].Get("thread_ts") != fakeslack.CommentResolved {
		t.Fatalf("quip.thread calls %v", cc)
	}
	p, _ = a.CanvasComments(ctx, fakeslack.CanvasChannel, CommentsResolved, "")
	if got := rootTSs(p.Roots); !equalStrings(got, []string{fakeslack.CommentOpen}) {
		t.Errorf("resolved after the swap: %v", got)
	}
	if !p.Roots[0].CommentResolved() {
		t.Error("the resolved root's is_archived")
	}

	// canArchiveComment: a writeable, unlocked canvas, a thread id, a reply
	info := CanvasInfo{ID: fakeslack.CanvasChannel, Editable: true, Access: "owner"}
	if !CanArchiveComment(info, p.Roots[0]) {
		t.Error("CanArchiveComment on the owner's canvas")
	}
	locked := info
	locked.Locked = true
	if CanArchiveComment(locked, p.Roots[0]) || CanResolveAllComments(locked) {
		t.Error("a locked canvas's threads resolve")
	}
	noReply := p.Roots[0]
	noReply.ReplyCount = 0
	if CanArchiveComment(info, noReply) {
		t.Error("a thread without comments resolves")
	}

	// canAddReaction: comment access reacts under canvas_comment_only_mode
	a.SetExperimentGroup(ExpCanvasCommentOnly, "on")
	view := CanvasInfo{ID: "F0CMTVIEW1", Access: "read"}
	comment := CanvasInfo{ID: "F0CMTCOMM1", Access: "comment"}
	if a.CanReactToComment(view) || !a.CanReactToComment(comment) || !a.CanReactToComment(info) {
		t.Error("CanReactToComment: read no, comment and owner yes")
	}
	a.rememberCanvasInfo(view)
	if a.CanAddReaction(&Message{Channel: CanvasFileChannel(view.ID), TS: "1.000001"}) {
		t.Error("a view-only canvas's comment takes a reaction")
	}
	if !a.CanAddReaction(&Message{Channel: "C0FAKE00000", TS: "1.000001"}) {
		t.Error("an ordinary channel's message takes no reaction")
	}
	a.SetExperimentGroup(ExpCanvasCommentOnly, "off")
	if a.CanReactToComment(comment) {
		t.Error("comment access reacts without canvas_comment_only_mode")
	}

	// isCanvasComment: a file channel of a known canvas
	if a.IsCanvasComment(&Message{Channel: fakeslack.FileChanLaunch, TS: fakeslack.CommentOpen}) {
		t.Error("a comment counts as a canvas comment before the canvas is known")
	}
	a.rememberCanvasInfo(CanvasInfo{ID: fakeslack.CanvasChannel})
	if !a.IsCanvasComment(&Message{Channel: fakeslack.FileChanLaunch, TS: "1762510100.000200", ThreadTS: fakeslack.CommentOpen}) {
		t.Error("a comment in the Launch plan's file channel isn't a canvas comment")
	}
	if a.IsCanvasComment(&Message{Channel: "C0FAKE00000", TS: "1.000001"}) {
		t.Error("an ordinary channel's message is a canvas comment")
	}

	// Resolve all threads
	if err := a.ResolveAllComments(ctx, fakeslack.CanvasChannel); err != nil {
		t.Fatal(err)
	}
	if cc := fs.CommentCalls(); cc[len(cc)-1].Get("method") != "quip.thread.archiveAll" || cc[len(cc)-1].Get("channel_id") != fakeslack.FileChanLaunch {
		t.Errorf("archiveAll call %v", cc[len(cc)-1])
	}
	if p, _ := a.CanvasComments(ctx, fakeslack.CanvasChannel, CommentsLatest, ""); len(p.Roots) != 0 {
		t.Errorf("open threads after Resolve all: %v", rootTSs(p.Roots))
	}
}
