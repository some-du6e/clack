package messages

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// UserPronouns' thread-pane patch, as both variants ship it: the header
// with omitLinebreak, the pronouns after it, then the line break.
const threadPronounsPlugin = `(() => {
const TautPlugin = globalThis.TautPlugin
class ThreadPronouns extends TautPlugin {
  static id = 'ThreadPronouns'
  static pluginName = 'Thread Pronouns'
  static description = 'UserPronouns thread header patch'
  static authors = ['jeremy']
  static defaultConfig = { enabled: true }
  start() {
    const R = React
    const Pronouns = ({ userId }) => {
      const m = this.api.members.useMember(userId)
      const p = String(m?.profile?.pronouns ?? '').trim()
      return p ? R.createElement('span', { className: 'x-thread-pronouns' }, p) : null
    }
    this.api.patchComponent('ThreadSenderAndTimestampGeneric', (Original) => (props) => {
      if (props.adjacent || props.visible === false || !props.msg?.user) return R.createElement(Original, props)
      return R.createElement(R.Fragment, null,
        R.createElement(Original, { ...props, omitLinebreak: true }),
        R.createElement(Pronouns, { userId: props.msg.user }),
        props.omitLinebreak ? null : R.createElement('br'))
    })
  }
}
return ThreadPronouns
})()`

// The thread pane's sender line is ThreadSenderAndTimestampGeneric: a
// plugin's siblings of it (UserPronouns) render there, in the pane.
func TestPluginThreadSenderSiblings(t *testing.T) {
	_, _, tt, u, h := pluginEnv(t)
	install(t, tt, []byte(threadPronounsPlugin))
	until(t, 5*time.Second, "ThreadSenderAndTimestampGeneric patched", func() {}, func() bool {
		return slackc.Patched("ThreadSenderAndTimestampGeneric")
	})
	tv := NewThreadView(h, tGen, tsFair)
	ft := frameFn(tv)
	until(t, 8*time.Second, "the thread loaded", ft, func() bool { return len(tv.s.snap.msgs) > 0 })
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && !u.anyTree("x-thread-pronouns") {
		ft()
		time.Sleep(15 * time.Millisecond)
	}
	if !u.anyTree("x-thread-pronouns") {
		u.mu.Lock()
		for r, tr := range u.trees {
			t.Logf("root %d: %.400s", r, tr)
		}
		u.mu.Unlock()
		t.Fatal("no tree with the root sender's pronouns in the thread pane")
	}
}

// A plugin that re-renders the body from msg (BetterBlock's filtered
// text): it renames every text element and keeps everything else.
const filterBlocksPlugin = `(() => {
const TautPlugin = globalThis.TautPlugin
class FilterBlocks extends TautPlugin {
  static id = 'FilterBlocks'
  static pluginName = 'Filter Blocks'
  static description = 'rewrites text elements in msg.blocks'
  static authors = ['jeremy']
  static defaultConfig = { enabled: true }
  start() {
    const R = React
    const walk = (v) => Array.isArray(v) ? v.map(walk) : v && typeof v === 'object'
      ? Object.fromEntries(Object.entries(v).map(([k, x]) => [k, k === 'text' && v.type === 'text' ? x.replace('see', 'saw') : walk(x)])) : v
    this.api.patchComponent('Blocks', (Original) => (props) => {
      const blocks = props.msg?.blocks
      if (!Array.isArray(blocks)) return R.createElement(Original, props)
      const mention = JSON.stringify(blocks).includes('"message_mention"')
      return R.createElement(R.Fragment, null,
        R.createElement(Original, { ...props, msg: { ...props.msg, blocks: walk(blocks) } }),
        mention ? R.createElement('span', { className: 'x-saw-message-mention' }) : null)
    })
  }
}
return FilterBlocks
})()`

// A message_mention reaches JS as Slack sends it (the msg prop carries the
// message's real blocks), and the body a plugin returns from them keeps
// the pill.
func TestPluginMessageMentionBlocks(t *testing.T) {
	app, _, tt, u, h := pluginEnv(t)
	const ch, ts = "C0MENTIONS", "1700000200.000100"
	perma := "https://hackclub.slack.com/archives/" + tGen + "/p1700000001000100"
	blocks := `[{"type":"rich_text","block_id":"mm1","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"text","text":"see "},{"type":"message_mention","message_ts":"` + tsFair + `","channel_id":"` + tGen + `","author_id":"UALICE","text":"` + perma + `","url":"` + perma + `"}]}]}]`
	app.S.PutConv(slack.Conversation{ID: ch, Name: "mentions", IsChannel: true, IsMember: true, NumMembers: 2})
	app.S.SetLatest(ch, []slack.Message{{Type: "message", TS: ts, User: "UBOB", Text: "see " + perma, Blocks: json.RawMessage(blocks)}}, false)
	install(t, tt, []byte(filterBlocksPlugin))
	until(t, 5*time.Second, "Blocks patched", func() {}, func() bool { return slackc.Patched("Blocks") })
	v := NewChannelView(h, ch)
	v.Preview = true
	f := frameFn(v)
	drawn := func() string {
		if r := v.s.rows[ts]; r != nil && r.rv != nil {
			return r.rv.TextRange(0, r.rv.PlainLen())
		}
		return ""
	}
	until(t, 8*time.Second, "the row", f, func() bool { return drawn() != "" })
	for dl := time.Now().Add(3 * time.Second); time.Now().Before(dl) && !strings.Contains(drawn(), "saw "); {
		f()
		time.Sleep(15 * time.Millisecond)
	}
	if !u.anyTree("x-saw-message-mention") {
		t.Error("msg.blocks reached the plugin without the message_mention element")
	}
	if !strings.Contains(drawn(), "saw ") {
		t.Fatalf("the row doesn't draw the plugin's msg.blocks: %q", drawn())
	}
	var kinds []richtext.SpanKind
	var plain string
	for _, b := range v.s.rows[ts].rv.Doc().Blocks {
		for _, sp := range b.Spans {
			kinds = append(kinds, sp.Kind)
			plain += sp.Text
		}
	}
	if !slices.Contains(kinds, richtext.MessageMention) {
		t.Errorf("the plugin's body lost the message_mention pill: spans %v %q", kinds, plain)
	}
}
