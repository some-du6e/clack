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
// messages that read it. Synthetic data on internal/fakeslack.

import (
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

// refsMessages are Ana's message sharing a file Cara owns and Ben's
// message with a slug to #ops' tab, at the ts of i and i+1.
func refsMessages(ts func(int) string, i int, ch string) []map[string]any {
	file := map[string]any{"id": fmt.Sprintf("F0REFS%04d", i), "name": "plan.pdf", "title": "Launch plan", "mimetype": "application/pdf",
		"filetype": "pdf", "pretty_type": "PDF", "mode": "hosted", "user": "UCARA0001", "size": 2048}
	slug := []any{map[string]any{"type": "rich_text", "block_id": "r", "elements": []any{map[string]any{"type": "rich_text_section", "elements": []any{
		map[string]any{"type": "text", "text": "Steps are in "},
		map[string]any{"type": "channel", "channel_id": refsOps, "tab_id": refsTab}}}}}}
	return []map[string]any{
		{"type": "message", "channel": ch, "ts": ts(i), "user": "UANA00001", "text": "Here's the plan", "files": []any{file}},
		{"type": "message", "channel": ch, "ts": ts(i + 1), "user": "UBEN00001", "text": "Steps are in <#" + refsOps + ">", "blocks": slug},
	}
}

func TestRenamesRedrawRows(t *testing.T) {
	t.Run("unpatched", func(t *testing.T) { checkRenames(t, false) })
	// a plugin patching the messages slice: the rows draw its views, which
	// taut converts; and two messages only the plugin has
	// (injectMessages), which only taut converts
	t.Run("patched", func(t *testing.T) { checkRenames(t, true) })
}

func checkRenames(t *testing.T, patched bool) {
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: refsChan, Name: "launch", IsChannel: true, IsMember: true, NumMembers: 12})
	props, _ := json.Marshal(map[string]any{"tabz": refsTabz("Runbook")})
	app.S.PutConv(slack.Conversation{ID: refsOps, Name: "ops", IsChannel: true, IsMember: true, NumMembers: 8, Properties: props})
	base := time.Now().Add(-time.Hour).Unix()
	ts := func(i int) string { return fmt.Sprintf("%d.%06d", base+int64(i)*60, 100+i) }
	var msgs []slack.Message
	for _, m := range refsMessages(ts, 0, refsChan) {
		b, _ := json.Marshal(m)
		var sm slack.Message
		if err := json.Unmarshal(b, &sm); err != nil {
			t.Fatal(err)
		}
		msgs = append(msgs, sm)
	}
	app.S.SetLatest(refsChan, msgs, false)
	files, slugs := []string{ts(0)}, []string{ts(1)}
	if patched {
		inj, _ := json.Marshal(refsMessages(ts, 2, refsChan))
		patchedEnv(t, app, pluginJS(`
class InjectRefs extends TautPlugin {
  static id = 'InjectRefs'
  static pluginName = 'Inject refs (test)'
  static description = 'test: messages only the plugin has, naming a file owner and a channel tab'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const msgs = `+string(inj)+`
    this.api.messages.injectMessages(() => msgs)
  }
}
return InjectRefs`))
		until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
		files, slugs = append(files, ts(2)), append(slugs, ts(3))
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
			out = append(out, fmt.Sprintf("%s: %q", k, rowText(v.s.rows[k])))
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

	// Cara renames herself: the rows of her file say so (she sent none of
	// them, so no row drew her)
	refsUserChange(app, "kit.l")
	wait("the file owner's new name", rows(files, "kit.l | ", "cara | "))

	// #ops' tab renamed: the slugs to it follow
	refsTabRename(app, "Playbook")
	wait("the renamed tab in its slugs", rows(slugs, "Playbook in #ops", "Runbook"))
}
