package core

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A conversation the store holds named by its own id (a stub whose
// payload had no name; the id is also what store.ChannelName answers for
// a channel it can't name) isn't named: ResolveConv looks it up again,
// and the member set keeps the name conversations.info has. A named one
// is answered from the store.
func TestResolveConvRenamesIDStub(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	a := New(slack.New(fs.Creds()), store.New(""))
	t.Cleanup(a.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	id := fakeslack.ThreadPrivateID // th-secret-plans
	a.S.PutConv(slack.Conversation{ID: id, Name: id, IsGroup: true, IsPrivate: true, IsMember: true})
	if c, _ := a.Conv(id); named(c) {
		t.Fatalf("a conversation named by its id counts as named: %+v", c)
	}
	c, ok := a.ResolveConv(ctx, id)
	if !ok || c.Name != "th-secret-plans" || c.Kind != "private" || !c.Member {
		t.Fatalf("ResolveConv(%s) = %+v %v, want the private channel th-secret-plans", id, c, ok)
	}
	if _, member := a.S.Conv(id); !member {
		t.Error("the lookup moved the channel out of the viewer's conversations")
	}
	if n := a.S.ChannelName(id); n != "th-secret-plans" {
		t.Errorf("ChannelName(%s) = %q after the lookup, want th-secret-plans", id, n)
	}

	// named already: no second lookup changes it
	a.S.PutConv(slack.Conversation{ID: id, Name: "kept-name", IsGroup: true, IsPrivate: true, IsMember: true})
	if c, ok := a.ResolveConv(ctx, id); !ok || c.Name != "kept-name" {
		t.Errorf("a named conversation: %+v %v, want the stored name", c, ok)
	}
}
