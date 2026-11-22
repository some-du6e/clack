package core

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// The org page's search keeps what the store holds for a channel: left
// since boot (dropMembership kept it outside the sidebar, is_member
// false), it isn't fetched again, so Flannel's earlier "member" copy
// never marks it joined.
func TestOrgSearchKeepsALeave(t *testing.T) {
	sz := fakeslack.Small
	sz.ThreadHeader = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	fs.SetSlackConnectHub(fakeslack.SlackConnectHub{Connected: []fakeslack.ExternalTeamFixture{{ID: fakeslack.ExternalTeamID, Name: "Partner Co"}}})
	a := New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	if err := a.Boot(context.Background()); err != nil {
		t.Fatal(err)
	}
	ch := fakeslack.ThreadSharedID
	if c, ok := a.Conv(ch); !ok || !c.Member {
		t.Fatalf("#th-partner-co after boot: %+v (%v), want a member channel", c, ok)
	}
	a.dropMembership(ch, false) // the viewer left
	convs, _, err := a.SearchExternalOrg(context.Background(), fakeslack.ExternalTeamID, "partner", ExternalOrgLimit)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range convs {
		if c.ID == ch {
			found = true
			if c.Member {
				t.Errorf("the search lists #th-partner-co as joined after the leave")
			}
		}
	}
	if !found {
		t.Errorf("the search lost #th-partner-co: %v", convs)
	}
	if c, ok := a.Conv(ch); !ok || c.Member {
		t.Errorf("#th-partner-co after the search: %+v (%v), want it held and not a member", c, ok)
	}
}
