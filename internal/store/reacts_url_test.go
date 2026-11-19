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
