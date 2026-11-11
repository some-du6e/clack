package core

import (
	"context"
	"reflect"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// SearchInThread is Slack's searchInThread: search.inline with
// find_in_thread, the channel and thread_ts, 100 a page, following
// next_cursor to the end, and the matches newest first. Against the
// fake's thread header fixtures (synthetic), one match a page.
func TestSearchInThreadPages(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	fs.FindInThreadPage = 1
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	ctx := context.Background()
	// "Launch checklist for Friday", "Docs are ready", "Ship it"
	got, err := a.SearchInThread(ctx, fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, "S")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{fakeslack.ThreadReplyTS(1), fakeslack.ThreadReplyTS(0), fakeslack.ThreadRootTS}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("matches %v, want %v (newest first)", got, want)
	}
	forms := fs.SearchForms("search.inline")
	if len(forms) != 3 {
		t.Fatalf("%d search.inline calls, want 3 (a page each)", len(forms))
	}
	for i, f := range forms {
		for k, v := range map[string]string{"query": "S", "channel": fakeslack.ThreadPublicID, "thread_ts": fakeslack.ThreadRootTS,
			"find_in_thread": "true", "count": "100"} {
			if f.Get(k) != v {
				t.Errorf("call %d: %s=%q, want %q", i, k, f.Get(k), v)
			}
		}
		if wantCursor := []string{"", "fit:1", "fit:2"}[i]; f.Get("cursor") != wantCursor {
			t.Errorf("call %d: cursor %q, want %q", i, f.Get("cursor"), wantCursor)
		}
	}
	if got, err := a.SearchInThread(ctx, fakeslack.ThreadPublicID, fakeslack.ThreadRootTS, "nothing like it"); err != nil || len(got) != 0 {
		t.Errorf("no match: %v %v", got, err)
	}
}
