package core

import (
	"testing"

	"github.com/some-du6e/clack/internal/lists"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// What a jump into a canvas's file channel opens, as Slack's
// jumpToMessage → lFpV openDocWithThreadTs reads it from the message at
// the jump's thread, else at its ts.
func TestCanvasJumpOf(t *testing.T) {
	for _, c := range []struct {
		name        string
		m           slack.Message
		sec, thread string
	}{
		{"a mention: its section", slack.Message{TS: "1.1", Subtype: "document_mention",
			DocumentMention: &slack.MessageDocumentMention{FileID: "F1", SectionID: "temp:C:S1"}}, "temp:C:S1", ""},
		{"a comment root with replies: its thread", slack.Message{TS: "2.1", ThreadTS: "2.1", Subtype: "document_comment_root", ReplyCount: 2}, "", "2.1"},
		{"a comment root without replies: just the canvas (suppressCommentThread)", slack.Message{TS: "3.1", ThreadTS: "3.1", Subtype: "document_comment_root"}, "", ""},
		{"a reply: just the canvas", slack.Message{TS: "2.2", ThreadTS: "2.1"}, "", ""},
	} {
		if sec, thread := canvasJumpOf(&c.m); sec != c.sec || thread != c.thread {
			t.Errorf("%s: section %q thread %q, want %q %q", c.name, sec, thread, c.sec, c.thread)
		}
	}
}

func TestCanvasOfJumpAndCanvasJump(t *testing.T) {
	a, _ := testApp(t)
	const fch, fid = "C0FILE0001", "F0FILE0001"
	mention := slack.Message{Type: "message", TS: "10.1", Subtype: "document_mention",
		DocumentMention: &slack.MessageDocumentMention{FileID: fid, SectionID: "temp:C:S1"}}
	root := slack.Message{Type: "message", TS: "20.1", ThreadTS: "20.1", Subtype: "document_comment_root", ReplyCount: 1}
	reply := slack.Message{Type: "message", TS: "20.5", ThreadTS: "20.1"}

	if _, ok, wait := a.CanvasOfJump("C1", ""); ok || wait {
		t.Errorf("an ordinary channel: canvas %v, wait %v", ok, wait)
	}
	// a file channel the store doesn't hold, its mention held: the message
	// names its canvas
	a.S.PutFetched(fch, []slack.Message{mention, root, reply}, a.S.LiveSeq())
	if f, ok, wait := a.CanvasOfJump(fch, "10.1"); !ok || wait || f != fid {
		t.Errorf("an unstored file channel's held mention: %q %v %v, want %s", f, ok, wait, fid)
	}
	// stored, files.info not answered, no mention to go by: wait
	a.S.PutConv(slack.Conversation{ID: fch, IsChannel: true, IsGroup: true, IsPrivate: true, IsFile: true, IsMember: true})
	if f, ok, wait := a.CanvasOfJump(fch, "20.5"); ok || !wait || f != fid {
		t.Errorf("before files.info: %q %v %v, want to wait on %s", f, ok, wait, fid)
	}
	// a list's file channel is no canvas
	a.S.PutCanvasRef(fid, richtext.CanvasRef{Kind: "list", Status: "normal"})
	if _, ok, wait := a.CanvasOfJump(fch, "20.5"); ok || wait {
		t.Errorf("a list's file channel: canvas %v, wait %v", ok, wait)
	}
	a.S.PutCanvasRef(fid, richtext.CanvasRef{Kind: "canvas", Status: "normal"})
	if f, ok, _ := a.CanvasOfJump(fch, ""); !ok || f != fid {
		t.Errorf("a canvas's file channel: %q %v", f, ok)
	}
	for _, c := range []struct {
		name, thread, ts, sec, comment string
		known                          bool
	}{
		{"the mention", "", "10.1", "temp:C:S1", "", true},
		{"a thread link at the mention", "10.1", "", "temp:C:S1", "", true},
		{"a reply with its thread: the comment thread", "20.1", "20.5", "", "20.1", true},
		{"a reply alone: just the canvas", "", "20.5", "", "", true},
		{"a message the store lacks", "", "99.9", "", "", false},
	} {
		sec, comment, known := a.CanvasJump(fch, c.thread, c.ts)
		if sec != c.sec || comment != c.comment || known != c.known {
			t.Errorf("%s: section %q comment %q known %v, want %q %q %v", c.name, sec, comment, known, c.sec, c.comment, c.known)
		}
	}
}

// FileChannelKind is what Slack's RedirectFileChannel reads of a file
// channel: a canvas's, a list's, or neither. A held document_mention
// names a canvas only until files.info says otherwise; a stored file
// channel whose files.info hasn't answered waits. ListRecordOf is the
// record a list's comment message belongs to: its thread root's
// slack_list, else the loaded list's record with that thread_ts.
func TestFileChannelKind(t *testing.T) {
	a, _ := testApp(t)
	const fch, fid = "C0FILE0002", "F0FILE0002"
	mention := slack.Message{Type: "message", TS: "10.1", Subtype: "document_mention",
		DocumentMention: &slack.MessageDocumentMention{FileID: fid, SectionID: "temp:C:S1"}}
	root := slack.Message{Type: "message", TS: "30.1", ThreadTS: "30.1", Subtype: "list_record_comment",
		SlackList: &slack.MessageSlackList{ListID: fid, ListRecordID: "Rec0001"}}
	reply := slack.Message{Type: "message", TS: "30.5", ThreadTS: "30.1"}

	if _, kind, wait := a.FileChannelKind("C1", "10.1"); kind != "" || wait {
		t.Errorf("an ordinary channel: %q, wait %v", kind, wait)
	}
	if _, kind, wait := a.FileChannelKind(fch, ""); kind != "" || wait {
		t.Errorf("a file channel neither the store nor files.info knows: %q, wait %v; want neither", kind, wait)
	}
	a.S.PutFetched(fch, []slack.Message{mention, root, reply}, a.S.LiveSeq())
	a.S.PutConv(slack.Conversation{ID: fch, IsChannel: true, IsGroup: true, IsPrivate: true, IsFile: true, IsMember: true})
	if f, kind, wait := a.FileChannelKind(fch, "10.1"); kind != "canvas" || wait || f != fid {
		t.Errorf("a held mention before files.info: %q %q %v, want the canvas %s", f, kind, wait, fid)
	}
	if f, kind, wait := a.FileChannelKind(fch, ""); kind != "" || !wait || f != fid {
		t.Errorf("before files.info: %q %q %v, want to wait on %s", f, kind, wait, fid)
	}
	a.S.PutCanvasRef(fid, richtext.CanvasRef{Kind: "list", Status: "normal"})
	if f, kind, wait := a.FileChannelKind(fch, "10.1"); kind != "list" || wait || f != fid {
		t.Errorf("a held mention whose file files.info says is a list: %q %q %v, want the list", f, kind, wait)
	}
	if _, ok, wait := a.CanvasOfJump(fch, "10.1"); ok || wait {
		t.Errorf("CanvasOfJump on a list's file channel: canvas %v, wait %v", ok, wait)
	}
	for _, c := range []struct{ name, thread, ts, rec string }{
		{"a reply in the record's thread", "30.1", "30.5", "Rec0001"},
		{"the thread's root", "", "30.1", "Rec0001"},
		{"a message of no record", "", "10.1", ""},
		{"a message the store lacks", "", "99.9", ""},
	} {
		if rec := a.ListRecordOf(fch, c.thread, c.ts); rec != c.rec {
			t.Errorf("%s: record %q, want %q", c.name, rec, c.rec)
		}
	}
	st := a.lists()
	st.mu.Lock()
	st.docs[fid] = &ListDoc{Records: []lists.Record{{ID: "Rec0002", ThreadTS: "40.1"}}}
	st.mu.Unlock()
	if rec := a.ListRecordOf(fch, "40.1", "40.7"); rec != "Rec0002" {
		t.Errorf("a reply whose root the store lacks: record %q, want the loaded list's Rec0002", rec)
	}
	a.S.PutCanvasRef(fid, richtext.CanvasRef{Kind: "", Status: "deleted"})
	if _, kind, wait := a.FileChannelKind(fch, ""); kind != "" || wait {
		t.Errorf("a file that's gone: %q, wait %v; want neither (the conversation)", kind, wait)
	}
}
