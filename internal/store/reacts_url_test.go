package store

// A reaction's url (reaction_added's: a Slack Connect partner's custom
// emoji) survives a fetch that was out when it came: the replay puts it
// back with the reaction. Synthetic data only.

import (
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

func TestReactionURLSurvivesAFetch(t *testing.T) {
	s := New("")
	const ch, ts = "C0LIVE0009", "900.000001"
	const url = "https://emoji.slack-edge.com/T0PARTNER/partner-dance/4567ef.png"
	since := s.LiveSeq()
	if _, found, _ := s.ApplyReactionNotedURL(ch, ts, "partner-dance", "U0PARTNER1", url, true); found {
		t.Fatal("the store held the message before its fetch")
	}
	got := s.PutFetched(ch, []slack.Message{{Type: "message", TS: ts, Text: "a synthetic message"}}, since)
	if len(got) != 1 || len(got[0].Reactions) != 1 || got[0].Reactions[0].URL != url || got[0].Reactions[0].Name != "partner-dance" {
		t.Fatalf("the fetched copy's reactions %+v, want partner-dance with its url", got[0].Reactions)
	}
	// malformed: nothing noted, nothing changed
	if _, found, changed := s.ApplyReactionNotedURL(ch, ts, "tada", "", "", true); found || changed {
		t.Errorf("an add by nobody: found %v, changed %v", found, changed)
	}
}

// An existing reaction gains a url the way cBAo sets h.url before its
// isEqual: an add by someone listed bringing one changes only the url, an
// add by someone else counts them and takes theirs, and the same url
// again changes nothing.
func TestReactionGainsAURL(t *testing.T) {
	const x, y = "https://emoji.slack-edge.com/T0PARTNER/partner-wave/x.png", "https://emoji.slack-edge.com/T0PARTNER/partner-wave/y.png"
	m := slack.Message{TS: "900.000002", Reactions: []slack.Reaction{{Name: "partner-wave", Count: 1, Users: []string{"U0A"}}}}
	if !ApplyReactionURL(&m, "partner-wave", "U0A", x, true) || m.Reactions[0].URL != x || m.Reactions[0].Count != 1 {
		t.Fatalf("a listed person's add with a url: %+v", m.Reactions)
	}
	if !ApplyReactionURL(&m, "partner-wave", "U0B", y, true) || m.Reactions[0].URL != y || m.Reactions[0].Count != 2 {
		t.Fatalf("another person's add with a url: %+v", m.Reactions)
	}
	if ApplyReactionURL(&m, "partner-wave", "U0A", y, true) || m.Reactions[0].Count != 2 {
		t.Fatalf("the same url again changed %+v", m.Reactions)
	}
}
