package store

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// The journal keeps what changed after a fetch went out: a mark survives
// an older counts answer, a channel left isn't put back by an older list
// (and one joined again isn't dropped), and a section list newer than an
// answer stays.
func TestJournalKeepsNewerChanges(t *testing.T) {
	s := New("")
	s.SetCounts(&slack.Counts{Channels: []slack.CountEntry{
		{ID: "C1", LastRead: "1.0", Latest: "5.0", HasUnreads: true, MentionCount: 2},
		{ID: "C2", LastRead: "1.0", Latest: "5.0", HasUnreads: true},
	}})
	since := s.Seq()
	s.MarkRead("C1", "5.0") // while the answer below was out
	kept := s.SetCountsSince(&slack.Counts{Channels: []slack.CountEntry{
		{ID: "C1", LastRead: "1.0", Latest: "5.0", HasUnreads: true, MentionCount: 2},
		{ID: "C2", LastRead: "5.0", Latest: "5.0"},
	}}, since)
	if e, _ := s.Count("C1"); kept != 1 || e.HasUnreads || e.MentionCount != 0 {
		t.Errorf("the mark: kept %d, C1 %+v", kept, e)
	}
	if e, _ := s.Count("C2"); e.HasUnreads {
		t.Errorf("C2 should take the answer: %+v", e)
	}
	if !s.CountChangedSince("C1", since) || s.CountChangedSince("C1", s.Seq()) || s.CountChangedSince("C2", since) {
		t.Error("CountChangedSince")
	}

	// Only a new message while the answer was out: Slack's cursor and
	// mention count win (it counts mentions below a partial mark the
	// client can't), and the newer message stays unread.
	s.SetCounts(&slack.Counts{Channels: []slack.CountEntry{{ID: "C7", LastRead: "3.0", Latest: "5.0", HasUnreads: true}}})
	since = s.Seq()
	s.NoteMessage("C7", &slack.Message{TS: "6.0", User: "U2"}, false)
	if n := s.SetCountsSince(&slack.Counts{Channels: []slack.CountEntry{{ID: "C7", LastRead: "3.0", Latest: "5.0", HasUnreads: true, MentionCount: 2}}}, since); n != 1 {
		t.Errorf("merged %d, want 1", n)
	}
	if e, _ := s.Count("C7"); e.MentionCount != 2 || !e.HasUnreads || e.Latest != "6.0" || e.LastRead != "3.0" {
		t.Errorf("a message during the call: %+v, want Slack's 2 mentions, the newer latest, unread", e)
	}
	// a message the answer already counted: the answer whole
	since = s.Seq()
	s.NoteMessage("C7", &slack.Message{TS: "7.0", User: "U2"}, false)
	s.SetCountsSince(&slack.Counts{Channels: []slack.CountEntry{{ID: "C7", LastRead: "7.0", Latest: "7.0"}}}, since)
	if e, _ := s.Count("C7"); e.HasUnreads || e.MentionCount != 0 {
		t.Errorf("an answer that saw the message: %+v", e)
	}

	s.PutConv(slack.Conversation{ID: "C3", Name: "three", IsChannel: true})
	s.PutConv(slack.Conversation{ID: "C4", Name: "four", IsChannel: true})
	since = s.Seq()
	s.DropConv("C3")                                                               // left meanwhile
	s.PutConvLocal(slack.Conversation{ID: "C4", Name: "renamed", IsChannel: true}) // renamed meanwhile
	list := []slack.Conversation{{ID: "C3", Name: "three", IsChannel: true}, {ID: "C4", Name: "four", IsChannel: true}, {ID: "C5", IsChannel: true}}
	if n := s.PutConvsSince(list, since); n != 2 {
		t.Errorf("PutConvsSince skipped %d, want 2", n)
	}
	if _, ok := s.Conv("C3"); ok {
		t.Error("an older list put C3 back")
	}
	if c, _ := s.Conv("C4"); c.Name != "renamed" {
		t.Errorf("an older list undid C4's rename: %q", c.Name)
	}
	if _, ok := s.Conv("C5"); !ok {
		t.Error("C5 wasn't installed")
	}
	added := map[string]*slack.Conversation{"C4": nil, "C6": nil}
	s.PutConv(slack.Conversation{ID: "C6", IsChannel: true})
	if n := s.DropBootConvs(added, []slack.Conversation{{ID: "C5"}}, since); n != 1 {
		t.Errorf("DropBootConvs dropped %d, want 1 (C6; C4 changed here)", n)
	}

	s.SetSections([]slack.Section{{ID: "S1"}})
	since = s.Seq()
	s.SetSections([]slack.Section{{ID: "S1"}, {ID: "S2"}}) // a move's refresh, newer
	if s.SetSectionsSince([]slack.Section{{ID: "S1"}}, since) || len(s.Sections) != 2 {
		t.Errorf("an older section list replaced a newer one: %v", s.Sections)
	}
	if !s.SetSectionsSince([]slack.Section{{ID: "S3"}}, s.Seq()) || s.Sections[0].ID != "S3" {
		t.Error("a list newer than every change wasn't installed")
	}
}

// A mark made before the warm install (the restored channel, under the
// boot cover) keeps its own count, and every other cached count still
// installs: they used to be dropped whole.
func TestInstallBootDataMergesCounts(t *testing.T) {
	s := New("")
	s.MarkRead("C1", "9.0")
	added := s.InstallBootData(&BootData{
		Convs:         []slack.Conversation{{ID: "C1", IsChannel: true}, {ID: "C2", IsChannel: true}},
		Counts:        []slack.CountEntry{{ID: "C1", LastRead: "1.0", Latest: "9.0", HasUnreads: true}, {ID: "C2", LastRead: "1.0", Latest: "9.0", HasUnreads: true, MentionCount: 3}},
		ThreadsUnread: true, ThreadMentions: 2,
	})
	if len(added) != 2 {
		t.Errorf("added %d conversations", len(added))
	}
	if e, _ := s.Count("C1"); e.HasUnreads || e.LastRead != "9.0" {
		t.Errorf("the mark lost to the boot data: %+v", e)
	}
	if e, _ := s.Count("C2"); !e.HasUnreads || e.MentionCount != 3 {
		t.Errorf("C2's cached count wasn't installed: %+v", e)
	}
	if !s.ThreadsUnread || s.ThreadMentions != 2 {
		t.Error("the cached thread state wasn't installed")
	}
	s.SetCounts(&slack.Counts{})
	s.InstallBootData(&BootData{ThreadsUnread: true, ThreadMentions: 5})
	if s.ThreadMentions == 5 {
		t.Error("the boot data's thread state replaced client.counts'")
	}
}

// Sign-out deletes the warm boot's files at once, before the rest, and
// leaves the signed-out mark until DropDisk finishes; a start finishes a
// sign-out the process didn't live through and drops other accounts' warm
// files (SweepAccounts).
func TestSignOutAndSweep(t *testing.T) {
	root := t.TempDir()
	acct := func(name string) string { return AccountDir(root, name, "U1") }
	write := func(dir string, names ...string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, "img"), 0o700); err != nil {
			t.Fatal(err)
		}
		for _, n := range names {
			if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	a := acct("TA")
	write(a, "meta.json", BootDataFile, HeadersFile)
	s := New(a)
	s.SignOutNow()
	if exists(filepath.Join(a, BootDataFile)) || exists(filepath.Join(a, HeadersFile)) {
		t.Error("SignOutNow left a warm boot file")
	}
	if !exists(filepath.Join(a, SignedOutMark)) || !exists(filepath.Join(a, "meta.json")) {
		t.Error("SignOutNow should leave the mark and the rest (DropDisk's)")
	}
	if err := s.WriteFile(BootDataFile, []byte("{}")); err == nil || exists(filepath.Join(a, BootDataFile)) {
		t.Error("a write after sign-out")
	}
	s.DropDisk()
	if exists(a) {
		t.Error("DropDisk left the account directory")
	}

	keep, other, quit := acct("TK"), acct("TO"), acct("TQ")
	write(keep, "meta.json", BootDataFile, HeadersFile, SignedOutMark) // signed out, then the same account signed in again
	write(other, "meta.json", BootDataFile, HeadersFile)               // the account switcher's saved account
	write(quit, "meta.json", BootDataFile, SignedOutMark)              // signed out, then the process ended
	SweepAccounts(root, keep)
	if !exists(filepath.Join(keep, BootDataFile)) || !exists(filepath.Join(keep, HeadersFile)) || !exists(filepath.Join(keep, "meta.json")) {
		t.Error("the signed-in account's directory lost files")
	}
	if exists(filepath.Join(keep, SignedOutMark)) {
		t.Error("the signed-in account's stale mark stayed")
	}
	if !exists(filepath.Join(other, BootDataFile)) || !exists(filepath.Join(other, HeadersFile)) || !exists(filepath.Join(other, "meta.json")) {
		t.Error("another signed-in account lost its warm boot files")
	}
	if exists(filepath.Join(quit, "meta.json")) || exists(filepath.Join(quit, SignedOutMark)) {
		t.Error("an interrupted sign-out wasn't finished")
	}
	es, _ := os.ReadDir(filepath.Join(root, "v1"))
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	if !slices.Contains(names, filepath.Base(keep)) || slices.Contains(names, filepath.Base(quit)) {
		t.Errorf("v1 after the sweep: %v", names)
	}
}

// A session that ended deletes the warm boot's files, and a save already
// past its own checks doesn't write them again (the flag WriteFile reads
// under its lock); other cache files still save. ClearSignedOut takes a
// stale mark away.
func TestWarmFilesGoneForGood(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	for _, n := range []string{BootDataFile, HeadersFile} {
		if err := s.WriteFile(n, []byte("{}")); err != nil {
			t.Fatal(err)
		}
	}
	s.RemoveWarmFiles()
	for _, n := range []string{BootDataFile, HeadersFile} {
		if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
			t.Errorf("%s still there", n)
		}
		if err := s.WriteFile(n, []byte("{}")); err == nil {
			t.Errorf("%s written after RemoveWarmFiles", n)
		}
	}
	if err := s.WriteFile("channel_managers.json", []byte("{}")); err != nil {
		t.Errorf("another file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, SignedOutMark), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s.ClearSignedOut()
	if _, err := os.Stat(filepath.Join(dir, SignedOutMark)); !os.IsNotExist(err) {
		t.Error("ClearSignedOut left the mark")
	}
}
