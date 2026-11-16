package messages

// A converted message's Doc has names in it that Slack's components read
// from the store as they render (Slack re-renders them when the member or
// channel changes): a file's uploader on the line above its files
// (FilesMetaTextWrapper's renderUploader) and a channel-tab slug's tab
// title and channel name (MrkdwnChannelTab). Clack converts a message
// once and keeps it, so a profile change or a channel's tab renamed left
// every row showing the old names: the unpatched list's models and the
// plugins' patched views (taut's conversions) alike, until the next
// history load. Each conversion now records the members and channels it
// read (core.Message.Refs), and a change to one converts again just the
// messages that read it; a names pref (display_real_names) those that
// read any member. Synthetic data on internal/fakeslack.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	refsChan = "C0REFS0001"
	refsOps  = "C0REFS0002"
	refsTab  = "Ct0REFS001"
)

// refsTabz is #ops' tabs with the canvas tab the slugs name, labeled label.
func refsTabz(label string) []any {
	return []any{map[string]any{"id": refsTab, "type": "canvas", "label": label}}
}

// refsUserChange is Cara's profile change as the socket brings it: her
// display name is now name.
func refsUserChange(app *core.App, name string) {
	u := map[string]any{"id": "UCARA0001", "name": "cara", "real_name": "Cara Lindqvist",
		"profile": map[string]any{"display_name": name, "real_name": "Cara Lindqvist", "image_48": shotAvatar("UCARA0001"), "image_72": shotAvatar("UCARA0001")}}
	raw, _ := json.Marshal(map[string]any{"type": "user_change", "user": u})
	app.Inject(slack.Event{Type: "user_change", Raw: raw})
}

// refsTabRename is #ops' channel_updated renaming the tab to label.
func refsTabRename(app *core.App, label string) {
	raw, _ := json.Marshal(map[string]any{"type": "channel_updated", "channel": refsOps,
		"updates": map[string]any{"properties": map[string]any{"tabz": refsTabz(label)}}})
	app.Inject(slack.Event{Type: "channel_updated", Channel: json.RawMessage(`"` + refsOps + `"`), Raw: raw})
}

// refsFile is a PDF Cara owns.
func refsFile(id, title string) map[string]any {
	return map[string]any{"id": id, "name": strings.ToLower(title) + ".pdf", "title": title, "mimetype": "application/pdf",
		"filetype": "pdf", "pretty_type": "PDF", "mode": "hosted", "user": "UCARA0001", "size": 2048}
}

// refsMessages are Ana's message sharing a file Cara owns and Ben's
// message with a slug to #ops' tab, at the ts of i and i+1.
func refsMessages(ts func(int) string, i int, ch string) []map[string]any {
	file := refsFile(fmt.Sprintf("F0REFS%04d", i), "Launch plan")
	slug := []any{map[string]any{"type": "rich_text", "block_id": "r", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
		map[string]any{"type": "text", "text": "Steps are in "},
		map[string]any{"type": "channel", "channel_id": refsOps, "tab_id": refsTab}}}}}}
	return []map[string]any{
		{"type": "message", "channel": ch, "ts": ts(i), "user": "UANA00001", "text": "Here's the plan", "files": []any{file}},
		{"type": "message", "channel": ch, "ts": ts(i + 1), "user": "UBEN00001", "text": "Steps are in <#" + refsOps + ">", "blocks": slug},
	}
}

// refsOwners are the file owners m's Doc names (failure messages).
func refsOwners(m *core.Message) []string {
	var out []string
	for _, b := range m.Doc.Blocks {
		if b.File != nil {
			out = append(out, b.File.Uploader)
		}
	}
	return out
}

func TestRenamesRedrawRows(t *testing.T) {
	t.Run("unpatched", func(t *testing.T) { checkRenames(t, false) })
	// a plugin patching the messages slice: its text patch changes every
	// message, so each row draws the plugin's view, which taut converts;
	// its copy of Ben's notes shares a file Cara owns, which the list's
	// own copy doesn't (only taut's conversion names her, so only its
	// re-conversion can update the row); and two messages only the
	// plugin has (injectMessages), which only taut converts
	t.Run("patched", func(t *testing.T) { checkRenames(t, true) })
}

func checkRenames(t *testing.T, patched bool) {
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: refsChan, Name: "launch", IsChannel: true, IsMember: true, NumMembers: 12})
	props, _ := json.Marshal(map[string]any{"tabz": refsTabz("Runbook")})
	app.S.PutConv(slack.Conversation{ID: refsOps, Name: "ops", IsChannel: true, IsMember: true, NumMembers: 8, Properties: props})
	base := time.Now().Add(-time.Hour).Unix()
	ts := func(i int) string { return fmt.Sprintf("%d.%06d", base+int64(i)*60, 100+i) }
	// Ben's notes: a file in them only in the plugin's copy
	notesMsg := map[string]any{"type": "message", "channel": refsChan, "ts": ts(4), "user": "UBEN00001", "text": "Notes from the sync"}
	var msgs []slack.Message
	for _, m := range append(refsMessages(ts, 0, refsChan), notesMsg) {
		b, _ := json.Marshal(m)
		var sm slack.Message
		if err := json.Unmarshal(b, &sm); err != nil {
			t.Fatal(err)
		}
		msgs = append(msgs, sm)
	}
	app.S.SetLatest(refsChan, msgs, false)
	files, slugs, stored := []string{ts(0)}, []string{ts(1)}, []string{ts(0), ts(1)}
	if patched {
		inj, _ := json.Marshal(refsMessages(ts, 2, refsChan))
		notes, _ := json.Marshal(refsFile("F0REFSNOTE", "Sync notes"))
		patchedEnv(t, app, pluginJS(`
class InjectRefs extends TautPlugin {
  static id = 'InjectRefs'
  static pluginName = 'Inject refs (test)'
  static description = 'test: a text patch, a file only its copy of a message has, messages only it has'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const msgs = `+string(inj)+`
    this.api.messages.injectMessages(() => msgs)
    this.api.messages.patchMessageText((t) => t.replace(/plan/g, 'PLAN').replace(/Steps/g, 'STEPS').replace(/sync/g, 'SYNC'))
    const notes = `+string(notes)+`
    const share = (ts, m) => (ts === '`+ts(4)+`' && m ? { ...m, files: [notes] } : m)
    const { patchSlice, mapEntries } = this.api.redux
    patchSlice('messages', (ch, bucket) => (ch === '`+refsChan+`' && bucket && typeof bucket === 'object' ? mapEntries(bucket, share) : bucket))
  }
}
return InjectRefs`))
		until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
		files, slugs, stored = append(files, ts(2), ts(4)), append(slugs, ts(3)), append(stored, ts(4))
	}
	h := coreHost{app: app, th: theme.New(false)}
	v := NewChannelView(h, refsChan)
	v.Preview = true
	frame := frameFn(v)
	// rows reports whether every row draws want (and, for the files, not
	// gone, the owner's old name before the separator)
	rows := func(keys []string, want, gone string) func() bool {
		return func() bool {
			for _, k := range keys {
				s := rowText(v.s.rows[k])
				if !strings.Contains(s, want) || gone != "" && strings.Contains(s, gone) {
					return false
				}
			}
			return true
		}
	}
	texts := func() string {
		var out []string
		for _, k := range append(append([]string(nil), files...), slugs...) {
			s := fmt.Sprintf("%s: %q", k, rowText(v.s.rows[k]))
			if r := v.s.rows[k]; r != nil {
				s += fmt.Sprintf(" (viewed %v, conversion %d, rev %d, src owners %v)", r.viewed, r.viewConv, r.rev, refsOwners(&r.src))
			}
			if t := tautOf(); t != nil {
				mv, conv, ok := t.MessageViewConv(refsChan, k, false)
				s += fmt.Sprintf(" view ok %v conversion %d owners %v", ok, conv, refsOwners(&mv))
			}
			out = append(out, s)
		}
		if t := tautOf(); t != nil {
			for _, m := range t.InjectedMessages(refsChan) {
				out = append(out, fmt.Sprintf("injected %s owners %v", m.TS, refsOwners(&m)))
			}
		}
		return strings.Join(out, "; ")
	}
	wait := func(what string, cond func() bool) {
		t.Helper()
		dl := time.Now().Add(8 * time.Second)
		for !cond() {
			if time.Now().After(dl) {
				t.Fatalf("%s: timed out; the rows draw %s", what, texts())
			}
			frame()
			time.Sleep(15 * time.Millisecond)
		}
	}
	wait("the file owner's name", rows(files, "cara | ", ""))
	wait("the tab slug", rows(slugs, "Runbook in #ops", ""))
	if patched {
		// the rows of the messages core has draw the plugin's views
		wait("the plugin's text", rows([]string{ts(0)}, "PLAN", ""))
		for _, k := range stored {
			if r := v.s.rows[k]; r == nil || !r.viewed {
				t.Errorf("the row of %s doesn't draw the plugin's view: %s", k, texts())
			}
		}
	}

	// Cara renames herself: the rows of her file say so (she sent none of
	// them, so no row drew her)
	refsUserChange(app, "kit.l")
	wait("the file owner's new name", rows(files, "kit.l | ", "cara | "))

	// #ops' tab renamed: the slugs to it follow
	refsTabRename(app, "Playbook")
	wait("the renamed tab in its slugs", rows(slugs, "Playbook in #ops", "Runbook"))

	// display_real_names: the files name their owner by her full name
	if err := app.SetMultiplePrefs(context.Background(), map[string]any{"display_real_names": true}); err != nil {
		t.Fatal(err)
	}
	wait("the owner's full name under display_real_names", rows(files, "Cara Lindqvist | ", "kit.l | "))
}

// A rename that comes while a model's load is out finds none of the
// load's messages to convert again, and the load's answer, converted
// before the rename, lands with the old name: the model converts again
// what the rename changed once the load is in (afterLoad).
func TestRenameDuringLoad(t *testing.T) {
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: refsChan, Name: "launch", IsChannel: true, IsMember: true, NumMembers: 12})
	base := time.Now().Add(-time.Hour).Unix()
	ts := func(i int) string { return fmt.Sprintf("%d.%06d", base+int64(i)*60, 100+i) }
	b, _ := json.Marshal(refsMessages(ts, 0, refsChan)[0])
	var sm slack.Message
	if err := json.Unmarshal(b, &sm); err != nil {
		t.Fatal(err)
	}
	app.S.SetLatest(refsChan, []slack.Message{sm}, false)
	hb := hubFor(coreHost{app: app, th: theme.New(false)})
	m := hb.channel(refsChan)
	m.mu.Lock()
	m.loading = true // a load is out
	m.mu.Unlock()
	answer := app.MessageFromSlack(refsChan, &sm) // converted as it came, before the rename
	if got := refsOwners(&answer); len(got) != 1 || got[0] != "cara" {
		t.Fatalf("the file's owner before the rename: %v", got)
	}

	refsUserChange(app, "kit.l")
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.mu.Lock()
		seen := !m.reconv.Empty()
		m.mu.Unlock()
		if seen {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the rename never reached the loading model")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// the load lands as ensureLoaded lands it
	m.mu.Lock()
	m.msgs = []entry{{m: answer, rev: m.bump()}}
	m.loading = false
	m.rev++
	m.mu.Unlock()
	hb.afterLoad(m)
	m.mu.Lock()
	got := refsOwners(&m.msgs[0].m)
	m.mu.Unlock()
	if len(got) != 1 || got[0] != "kit.l" {
		t.Errorf("the load's copy, converted before the rename, names %v; want [kit.l]", got)
	}
}
