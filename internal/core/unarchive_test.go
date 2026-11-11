package core

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// canUserUnarchiveChannel: an archived channel with UNARCHIVE_CHANNEL
// (permissions/info, loaded in the background); never a DM or a channel
// that isn't archived.
func TestCanUnarchive(t *testing.T) {
	a, f := testApp(t)
	f.reply["info"] = `{"ok":true,"allowed":["UNARCHIVE_CHANNEL"]}` // the edge path permissions/info
	a.S.PutConv(slack.Conversation{ID: "C9", Name: "old", IsChannel: true, IsArchived: true})
	if a.CanUnarchive("C9") {
		t.Fatal("true before the permission loaded")
	}
	deadline := time.Now().Add(5 * time.Second)
	for !a.CanUnarchive("C9") {
		if time.Now().After(deadline) {
			t.Fatal("the permission never loaded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if a.CanUnarchive("C1") || a.CanUnarchive("D1") {
		t.Error("an unarchived channel or a DM can be unarchived")
	}
	// an archived channel the viewer isn't in (the store's ext list)
	a.S.PutExtConv(slack.Conversation{ID: "C8", Name: "older", IsChannel: true, IsArchived: true})
	deadline = time.Now().Add(5 * time.Second)
	for !a.CanUnarchive("C8") {
		if time.Now().After(deadline) {
			t.Fatal("an ext archived channel never became unarchivable")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
