package core

// Benchmarks of the sidebar snapshot on a 1700-conversation workspace
// (α.89's freeze report: 1665 conversations held the UI goroutine 2 s in
// sections, through a collate.New per group DM). Synthetic content only.
// This file uses only APIs the base has, so bench-compare.yml's core
// suite runs it on both sides.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

var benchWords = []string{"alpha", "Beta", "gamma", "Delta", "épée", "zeta", "Eta", "theta", "iota", "kappa", "lambda", "mu"}

// bigApp is a booted App with 1100 channels, 300 DMs and 300 group DMs
// (its Members known, the sortName path that collated per call), a
// Starred, a custom, the Channels and the DMs section.
func bigApp(tb testing.TB) *App {
	tb.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":false,"error":"not_in_test"}`))
	}))
	tb.Cleanup(srv.Close)
	c := slack.New(slack.Creds{Token: "xoxc-test", UserID: "USELF", Team: "T1", TeamURL: srv.URL + "/"})
	c.EdgeURL = srv.URL + "/edge/"
	s := store.New("")
	users := []slack.User{{ID: "USELF", Name: "me", Profile: slack.Profile{DisplayName: "Me"}}}
	for i := 0; i < 300; i++ {
		w := benchWords[i%len(benchWords)]
		users = append(users, slack.User{ID: fmt.Sprintf("U%04d", i), Name: fmt.Sprintf("person%d", i),
			Profile: slack.Profile{DisplayName: fmt.Sprintf("%s %d", w, i)}})
	}
	s.PutUsers(users)
	var custom []string
	for i := 0; i < 1100; i++ {
		id := fmt.Sprintf("C%05d", i)
		s.PutConv(slack.Conversation{ID: id, Name: fmt.Sprintf("%s-%d", benchWords[i%len(benchWords)], i), IsChannel: true, IsMember: true})
		if i%20 == 0 {
			custom = append(custom, id)
		}
	}
	for i := 0; i < 300; i++ {
		s.PutConv(slack.Conversation{ID: fmt.Sprintf("D%05d", i), IsIM: true, User: fmt.Sprintf("U%04d", i), IsMember: true})
		s.PutConv(slack.Conversation{ID: fmt.Sprintf("G%05d", i), IsMPIM: true, IsMember: true,
			Name:    fmt.Sprintf("mpdm-person%d--person%d--me-1", i, (i+7)%300),
			Members: []string{"USELF", fmt.Sprintf("U%04d", i), fmt.Sprintf("U%04d", (i+7)%300), fmt.Sprintf("U%04d", (i+31)%300)}})
	}
	a := New(c, s)
	a.S.Self = *mustUser(s, "USELF")
	cust := slack.Section{ID: "L0CUSTOM", Name: "Projects", Type: "standard", Next: "L0CHANNELS"}
	cust.ChannelIDsPage.ChannelIDs = custom
	a.S.Lock()
	a.S.Sections = []slack.Section{{ID: "L0STARS", Type: "stars", Next: "L0CUSTOM"}, cust,
		{ID: "L0CHANNELS", Type: "channels", Next: "L0DMS"}, {ID: "L0DMS", Type: "direct_messages"}}
	a.S.Unlock()
	close(a.ready)
	return a
}

func BenchmarkSortName1700(b *testing.B) {
	a := bigApp(b)
	convs := a.snapshotNow().Convs
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range convs {
			a.sortName(c)
		}
	}
}

func BenchmarkSections1700(b *testing.B) {
	a := bigApp(b)
	convs := a.snapshotNow().Convs
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.sections(convs)
	}
}

func BenchmarkSnapshot1700(b *testing.B) {
	a := bigApp(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.snapshotNow()
	}
}
