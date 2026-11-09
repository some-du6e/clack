package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

func TestBootSnapshotRoundTripAndDrop(t *testing.T) {
	root := t.TempDir()
	dir := AccountDir(root, "T0FAKE", "U0SELF")
	if want := filepath.Join(root, "v1", "T0FAKE-U0SELF"); dir != want {
		t.Fatalf("AccountDir = %s, want %s", dir, want)
	}
	if d := AccountDir(root, "../../etc", ""); filepath.Dir(filepath.Dir(d)) != root {
		t.Fatalf("AccountDir left the root: %s", d)
	}
	s := New(dir)
	d := &BootSnapshot{TeamID: "T0FAKE", SelfID: "U0SELF",
		Convs:  []slack.Conversation{{ID: "C1", Name: "general", IsChannel: true}},
		Counts: []slack.CountEntry{{ID: "C1", HasUnreads: true, MentionCount: 2}},
		Users:  []slack.User{{ID: "U0SELF", Name: "me"}}, Emoji: map[string]string{"party": "https://emoji.slack-edge.com/T/party/x.gif"}}
	if err := s.WriteBoot(d); err != nil {
		t.Fatal(err)
	}
	att := json.RawMessage(`[{"id":1,"fallback":"x","image_width":640,"image_height":480,"some_field_the_struct_lacks":{"a":1}}]`)
	if err := s.WriteHistory(&HistorySnapshot{Channel: "C1", Msgs: []slack.Message{{TS: "1.000001", User: "U0SELF", RawAttachments: att}}}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		for _, n := range []string{"boot.json", "history.json"} {
			fi, err := os.Stat(filepath.Join(dir, n))
			if err != nil || fi.Mode().Perm() != 0o600 {
				t.Fatalf("%s: %v mode %v, want 0600", n, err, fi)
			}
		}
	}

	s2 := New(dir)
	got := s2.ReadBoot()
	if got == nil || len(got.Convs) != 1 || got.Counts[0].MentionCount != 2 {
		t.Fatalf("ReadBoot = %+v", got)
	}
	s2.InstallBoot(got, Prefs{EmojiStyle: "apple"}, map[string]bool{})
	if c, ok := s2.Conv("C1"); !ok || c.Name != "general" {
		t.Fatal("InstallBoot didn't install the conversation")
	}
	if e, _ := s2.Count("C1"); !e.HasUnreads {
		t.Fatal("InstallBoot dropped the cached counts")
	}
	if s2.EmojiFull() {
		t.Fatal("a snapshot's emoji slice counts as the whole map")
	}
	h := s2.ReadHistory()
	if h == nil || len(h.Msgs) != 1 || !strings.Contains(string(h.Msgs[0].RawAttachments), "some_field_the_struct_lacks") {
		t.Fatalf("history.json lost the attachments as Slack sent them: %+v", h)
	}
	if !s2.SetCachedLatest(h.Channel, h.Msgs, h.HasMore) {
		t.Fatal("history.json didn't install")
	}
	if s2.SetCachedLatest("C1", nil, false) {
		t.Fatal("a cached page replaced a loaded timeline")
	}
	// the network's list drops what Slack no longer lists
	s2.ReplaceConvs([]slack.Conversation{{ID: "C2", Name: "random"}})
	if _, ok := s2.Conv("C1"); ok {
		t.Fatal("ReplaceConvs kept a conversation the network no longer lists")
	}

	// another version's file is ignored
	b, _ := os.ReadFile(filepath.Join(dir, "boot.json"))
	old := append([]byte(`{"v":0,`), b[len(`{"v":1,`):]...)
	if err := os.WriteFile(filepath.Join(dir, "boot.json"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	if New(dir).ReadBoot() != nil {
		t.Fatal("a v0 snapshot was read")
	}

	// sign-out: everything goes and nothing comes back
	_ = os.MkdirAll(filepath.Join(dir, "img", "ab"), 0o700)
	_ = os.WriteFile(filepath.Join(dir, "img", "ab", "x"), []byte("png"), 0o600)
	_ = s.SaveCache()
	s.DropDisk()
	if err := s.WriteBoot(d); err == nil {
		t.Fatal("WriteBoot wrote after DropDisk")
	}
	_ = s.SaveCache()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		es, _ := os.ReadDir(dir)
		t.Fatalf("the account dir survived sign-out: %v %v", err, es)
	}
}

func TestPurgeOldVersions(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"v0/T-U", "v1/T-U", "vX", "other"} {
		_ = os.MkdirAll(filepath.Join(root, d), 0o700)
	}
	PurgeOldVersions(root)
	for d, want := range map[string]bool{"v0": false, "v1": true, "vX": true, "other": true} {
		_, err := os.Stat(filepath.Join(root, d))
		if (err == nil) != want {
			t.Errorf("%s exists=%v, want %v", d, err == nil, want)
		}
	}
}
