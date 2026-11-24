package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/expandable"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// Slack's expandables store (5vqP, Ua4u addObjects): no state reads as
// none; a collapse is a stored false; an empty key is skipped; a value it
// already holds changes nothing (no event, no new generation).
func TestExpandablesStore(t *testing.T) {
	a, _ := testApp(t)
	events, stop := a.Subscribe()
	defer stop()
	if _, ok := a.Expanded("inline_files_msg_1_2F1Channel"); ok {
		t.Fatal("state before any write")
	}
	gen := a.ExpandablesGen()
	a.SetExpanded("inline_files_msg_1_2F1Channel", false)
	if on, ok := a.Expanded("inline_files_msg_1_2F1Channel"); !ok || on {
		t.Errorf("after a collapse: %v %v, want a stored false", on, ok)
	}
	if a.ExpandablesGen() == gen {
		t.Error("the generation didn't move")
	}
	select {
	case ev := <-events:
		if ev.Type != "expandables" {
			t.Errorf("event %q", ev.Type)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no expandables event")
	}
	gen = a.ExpandablesGen()
	a.SetExpanded("inline_files_msg_1_2F1Channel", false)
	a.BulkSetExpanded(map[string]bool{"": true})
	if a.ExpandablesGen() != gen {
		t.Error("an unchanged value (or an empty key) moved the generation")
	}
	if _, ok := a.Expanded(""); ok {
		t.Error("the empty key was stored")
	}
	a.BulkSetExpanded(map[string]bool{"inline_files_msg_1_2F1Channel": true, "block_image_1.2_b": false})
	if on, _ := a.Expanded("inline_files_msg_1_2F1Channel"); !on {
		t.Error("bulkSetExpanded didn't expand")
	}
	if on, ok := a.Expanded("block_image_1.2_b"); !ok || on {
		t.Error("bulkSetExpanded didn't collapse")
	}
}

// The store is the account's across restarts (persistenceEnabled,
// persistImmediately): written as it changes, read back at the next
// start under its account, the state set before the read kept. A key
// that carries an image URL (an attachment without an id) stays in
// memory.
func TestExpandablesPersist(t *testing.T) {
	dir := t.TempDir()
	creds := slack.Creds{Token: "xoxc-test", UserID: "USELF", Team: "T1", TeamURL: "http://127.0.0.1:1/"}
	a := New(slack.New(creds), store.New(dir))
	b := New(slack.New(creds), store.New(dir))
	other := New(slack.New(slack.Creds{Token: "xoxc-test", UserID: "UOTHER", Team: "T1"}), store.New(dir))
	t.Cleanup(func() { // the writes end before the directory goes
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			if !a.expandablesWriting() && !b.expandablesWriting() && !other.expandablesWriting() {
				return
			}
		}
	})
	if b.SetExpanded("block_image_1.2_b", false); b.expandablesWriting() { // set before the disk's copy is read
		t.Error("a write before the disk's copy was read (it would replace the file)")
	}
	a.loadExpandables("T1", "USELF")
	urlKey := "img_msg_1.2https://e.test/a.pngChannel"
	a.SetExpanded("inline_files_msg_1_2F1Channel", false)
	a.SetExpanded("block_image_1.2_b", true)
	a.SetExpanded(urlKey, false)
	path := filepath.Join(dir, store.ExpandablesFile)
	var disk struct {
		Expanded map[string]bool `json:"expanded"`
	}
	for end := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		b, err := os.ReadFile(path)
		if err == nil && json.Unmarshal(b, &disk) == nil && len(disk.Expanded) == 2 {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("expandables.json: %s %v", b, err)
		}
	}
	if _, ok := disk.Expanded[urlKey]; ok {
		t.Error("a key with an image URL went to disk")
	}
	b.loadExpandables("T1", "USELF")
	if on, ok := b.Expanded("inline_files_msg_1_2F1Channel"); !ok || on {
		t.Errorf("the collapse didn't survive a restart: %v %v", on, ok)
	}
	if on, _ := b.Expanded("block_image_1.2_b"); on {
		t.Error("the disk's copy replaced this session's state")
	}
	other.loadExpandables("T1", "UOTHER")
	if _, ok := other.Expanded("inline_files_msg_1_2F1Channel"); ok {
		t.Error("another account read the file")
	}
}

// isAttachmentExpanded (FDIC), isImageTooLarge (WGgb) and
// ExpandableContainer's disclosure rule (pOD6): the stored state first;
// else collapsed with expand_inline_imgs false, or an image over 20 MB
// under obey_inline_img_limit, or an unverified outsider's in a DM.
func TestAttachmentExpandedRules(t *testing.T) {
	a, _ := testApp(t)
	const key, big = "img_msg_1.21Channel", 0x1400000 + 1
	if !a.AttachmentExpanded(key, 0, "", "C1") || a.ImageTooLarge(key, big) {
		t.Error("defaults: collapsed (or too large without obey_inline_img_limit)")
	}
	a.mergePrefs(map[string]json.RawMessage{"obey_inline_img_limit": json.RawMessage("true")})
	if !a.ImageTooLarge(key, big) || a.ImageTooLarge(key, big-1) || a.AttachmentExpanded(key, big, "", "C1") {
		t.Error("20 MB limit")
	}
	a.SetExpanded(key, true) // "expand it anyway"
	if a.ImageTooLarge(key, big) || !a.AttachmentExpanded(key, big, "", "C1") {
		t.Error("the stored state didn't win over the limit")
	}
	a.mergePrefs(map[string]json.RawMessage{"expand_inline_imgs": json.RawMessage("false")})
	if !a.AttachmentExpanded(key, 0, "", "C1") {
		t.Error("the stored state didn't win over expand_inline_imgs")
	}
	if a.AttachmentExpanded("img_msg_1.22Channel", 0, "", "C1") || a.ImageTooLarge("img_msg_1.22Channel", big) {
		t.Error("expand_inline_imgs false: expanded, or too large (isImageTooLarge needs inline images on)")
	}
}

// shouldCollapseFromUnverifiedUser (Ns/U): only in a DM or a group DM,
// for a member of a team that isn't the viewer's, isn't in the viewer's
// enterprise and isn't verified (team.info), unless the viewer allowed
// them (allowed_unfurl_senders). An unknown team is read with team.info
// and counts once it answered. The disclosure shows only without state.
func TestCollapseFromUnverifiedUser(t *testing.T) {
	a, f := testApp(t)
	a.S.PutUsers([]slack.User{{ID: "UEXT", Name: "ext", TeamID: "T9"}, {ID: "UNEW", Name: "new", TeamID: "T8"},
		{ID: "UOWN", Name: "own", TeamID: "T1"}})
	a.S.PutConv(slack.Conversation{ID: "D9", IsIM: true, User: "UEXT"})
	a.S.PutConv(slack.Conversation{ID: "G9", IsMPIM: true})
	a.S.PutConv(slack.Conversation{ID: "D8", IsIM: true, User: "UNEW"})
	a.setTeamRecord("T9", teamRecord{})
	for _, c := range []struct {
		sender, ch string
		want       bool
	}{
		{"UEXT", "D9", true}, {"UEXT", "G9", true}, {"UEXT", "C1", false}, {"UOWN", "D9", false}, {"", "D9", false},
	} {
		if got := a.CollapseFromUnverifiedUser(c.sender, c.ch); got != c.want {
			t.Errorf("%s in %s: %v, want %v", c.sender, c.ch, got, c.want)
		}
	}
	if a.AttachmentExpanded("img_msg_1.21Channel", 0, "UEXT", "D9") || !a.UnverifiedDisclosure("img_msg_1.21Channel", 0, "UEXT", "D9") {
		t.Error("the unverified sender's preview isn't hidden behind the disclosure")
	}
	a.SetExpanded("img_msg_1.21Channel", true) // "just show this one"
	if !a.AttachmentExpanded("img_msg_1.21Channel", 0, "UEXT", "D9") || a.UnverifiedDisclosure("img_msg_1.21Channel", 0, "UEXT", "D9") {
		t.Error("just show this one didn't show it")
	}
	a.setTeamRecord("T9", teamRecord{verified: true})
	if a.CollapseFromUnverifiedUser("UEXT", "D9") {
		t.Error("a verified team")
	}
	a.C.Creds.EnterpriseID = "E1"
	a.setTeamRecord("T9", teamRecord{enterpriseID: "E1"})
	if a.CollapseFromUnverifiedUser("UEXT", "D9") {
		t.Error("a team of the viewer's enterprise")
	}
	a.setTeamRecord("T9", teamRecord{})

	// an unknown team: team.info, then the rule
	f.reply["team.info"] = `{"ok":true,"team":{"id":"T8","name":"Elsewhere","is_verified":false}}`
	if a.CollapseFromUnverifiedUser("UNEW", "D8") {
		t.Error("an unknown team counted before team.info answered")
	}
	for end := time.Now().Add(5 * time.Second); !a.CollapseFromUnverifiedUser("UNEW", "D8"); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("team.info for T8: %d calls", countCalls(f, "team.info"))
		}
	}
	if p := f.last("team.info"); p.Get("team") != "T8" {
		t.Errorf("team.info %v", p)
	}

	// Allow all previews from them
	f.reply["users.prefs.set"] = `{"ok":true}`
	if err := a.AllowUnfurlsFrom(t.Context(), "UEXT"); err != nil {
		t.Fatal(err)
	}
	if p := f.last("users.prefs.set"); p.Get("name") != "allowed_unfurl_senders" || p.Get("value") != "UEXT" {
		t.Errorf("users.prefs.set %v", p)
	}
	if a.CollapseFromUnverifiedUser("UEXT", "D9") {
		t.Error("an allowed sender")
	}
	a.mergePrefs(map[string]json.RawMessage{"allowed_unfurl_senders": json.RawMessage(`"UEXT,UNEW"`)})
	if a.CollapseFromUnverifiedUser("UNEW", "D8") {
		t.Error("the second allowed sender")
	}
}

// /collapse and /expand (229d, s/xk): every message held for the channel
// (its history and the threads read), in a thread only the thread's,
// gets its previews' keys (getMessageExpandableKeys with the command's
// view context) set at once; in a channel Slackbot says so, and the next
// command doesn't take it away.
func TestExpandCommand(t *testing.T) {
	a, _ := testApp(t)
	msgs := []slack.Message{
		{Type: "message", TS: "1700000000.000100", User: "U1", Files: []slack.File{{ID: "F1"}, {ID: "F2"}}},
		{Type: "message", TS: "1700000000.000200", User: "U1", Attachments: []slack.Attachment{{ID: 1, ImageURL: "https://e.test/a.png"}}},
		{Type: "message", TS: "1700000000.000300", User: "U1", Blocks: json.RawMessage(`[{"type":"image","block_id":"pic","image_url":"https://e.test/b.png"}]`)},
		{Type: "message", TS: "1700000000.000400", User: "U1", Text: "a root", ThreadTS: "1700000000.000400", ReplyCount: 1},
	}
	a.S.PrependOlder("C1", msgs, false)
	a.S.SetThread("C1", "1700000000.000400", []slack.Message{msgs[3],
		{Type: "message", TS: "1700000000.000500", User: "U2", ThreadTS: "1700000000.000400", Files: []slack.File{{ID: "F3"}}}})
	keys := func(vc string, ms ...slack.Message) []string {
		var out []string
		for i := range ms {
			out = append(out, expandable.MessageKeys(&ms[i], vc)...)
		}
		return out
	}
	reply := slack.Message{TS: "1700000000.000500", ThreadTS: "1700000000.000400", Files: []slack.File{{ID: "F3"}}}
	all := append(keys("Channel", msgs...), keys("Channel", reply)...)
	if len(all) != 4 { // the files, the attachment's image, the image block, the reply's files
		t.Fatalf("keys %q", all)
	}
	ts := a.ExpandCommand("C1", "", "Channel", false)
	for _, k := range all {
		if on, ok := a.Expanded(k); !ok || on {
			t.Errorf("/collapse: %s %v %v", k, on, ok)
		}
	}
	ix := a.Interactive()
	if ts == "" || !ix.Ephemeral("C1", ts) {
		t.Fatalf("no Slackbot message (%q)", ts)
	}
	found := ""
	a.S.VisitMessages("C1", func(string) {}, func(m *slack.Message) {
		if m.TS == ts {
			found = m.Text
		}
	})
	if found != "I’ve collapsed all files in this channel for you." {
		t.Errorf("Slackbot said %q", found)
	}
	a.ClearCommandFeedback(false)
	if !ix.Ephemeral("C1", ts) {
		t.Error("the next command took the note away")
	}
	// /expand in the thread: the thread's keys in the thread's view, no note
	if got := a.ExpandCommand("C1", "1700000000.000400", "Thread", true); got != "" {
		t.Errorf("a note in the thread (%q)", got)
	}
	for _, k := range keys("Thread", reply) {
		if on, ok := a.Expanded(k); !ok || !on {
			t.Errorf("/expand in the thread: %s %v %v", k, on, ok)
		}
	}
	for _, k := range keys("Thread", msgs[:3]...) {
		// a block's key has no view context: the channel's /collapse
		// set it, the thread's /expand leaves it
		on, ok := a.Expanded(k)
		if strings.HasSuffix(k, "Thread") && ok || !strings.HasSuffix(k, "Thread") && on {
			t.Errorf("/expand in the thread set a channel message's %s (%v %v)", k, on, ok)
		}
	}
	if on, _ := a.Expanded(all[3]); on {
		t.Error("the thread's /expand reached the channel view's key")
	}
	if slices.ContainsFunc(all, func(k string) bool { return strings.HasSuffix(k, "Thread") }) {
		t.Error("channel keys end with the thread's context")
	}
}
