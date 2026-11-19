package messages

// A component tree that changes what a message says is laid over the
// plugins' view of the message (row.overlay): Censorship's masks under
// ShowRealUser's MessageWrapper, which rewrites a forwarded message's
// attachment author. A message with no raw blocks is sent to the
// components as its doc's blocks (clack-i), and those were compared with
// the view's blocks: wherever the view differed (a mask), the source's
// block came back, and the row drew the masked word. Synthetic data on
// internal/fakeslack.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const overlayAuthor = "Relayed by the test"

// overlayMaskJS masks "job" in ch's messages (the messages slice) and
// rewrites every attachment's author in MessageWrapper's msg.
func overlayMaskJS(ch string) []byte {
	return pluginJS(`
class MaskAndRelay extends TautPlugin {
  static id = 'MaskAndRelay'
  static pluginName = 'Mask and relay (test)'
  static description = 'test: masks a word in the messages slice, rewrites attachment authors in MessageWrapper'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const fix = (t) => (typeof t === 'string' ? t.replace(/job/g, '***') : t)
    const els = (list) => (Array.isArray(list) ? list.map((e) => (e && e.type === 'text' ? { ...e, text: fix(e.text) } : e && e.elements ? { ...e, elements: els(e.elements) } : e)) : list)
    const mask = (ts, m) => (m && typeof m.text === 'string' && m.text.includes('job') ? { ...m, text: fix(m.text), blocks: els(m.blocks) } : m)
    const { patchSlice, mapEntries } = this.api.redux
    patchSlice('messages', (ch, bucket) => (ch === '` + ch + `' && bucket && typeof bucket === 'object' ? mapEntries(bucket, mask) : bucket))
    this.api.patchComponent('MessageWrapper', (Original) => (props) => {
      const m = props.msg
      const msg = React.useMemo(() => (m && Array.isArray(m.attachments) && m.attachments.length
        ? { ...m, attachments: m.attachments.map((a) => ({ ...a, author_name: '` + overlayAuthor + `' })) } : m), [m])
      return React.createElement(Original, { ...props, msg })
    })
  }
}
return MaskAndRelay`)
}

func TestOverlayKeepsTheViewsMasks(t *testing.T) {
	app, _, _ := shotsApp(t)
	const ch = "C0OVERMSK1"
	app.S.PutConv(slack.Conversation{ID: ch, Name: "notices", IsChannel: true, IsMember: true, NumMembers: 12})
	base := time.Now().Add(-time.Hour).Unix()
	textOnly, withBlocks := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60)
	att := []slack.Attachment{{ID: 1, Fallback: "from the relay", AuthorName: "relay", Text: "posted on the relay"}}
	app.S.SetLatest(ch, []slack.Message{
		{Type: "message", TS: textOnly, User: "UANA00001", Text: "the job fair is today", Attachments: att},
		{Type: "message", TS: withBlocks, User: "UANA00001", Text: "the job market", Blocks: shotRich(shotSection("the job market")), Attachments: att},
	}, false)
	patchedEnv(t, app, overlayMaskJS(ch), "MessageWrapper")
	until(t, 5*time.Second, "the messages slice patched", func() {}, func() bool { return tautOf().SlicePatched("messages") })
	v := NewChannelView(coreHost{app: app, th: theme.New(false)}, ch)
	v.Preview = true
	frame := frameFn(v)
	author := func(r *row) string {
		for _, b := range r.m.Doc.Blocks {
			if b.Kind == richtext.AttachmentBlock && b.Att != nil {
				return b.Att.AuthorName
			}
		}
		return ""
	}
	ready := func(ts string) bool {
		r := v.s.rows[ts]
		return r != nil && r.viewed && r.wrapMsg != nil && author(r) == overlayAuthor
	}
	until(t, 10*time.Second, "both rows on the view, under the wrapper's rewrite", frame, func() bool { return ready(textOnly) && ready(withBlocks) })
	var bad []string
	for i := 0; i < 40; i++ {
		frame()
		for _, ts := range []string{textOnly, withBlocks} {
			r := v.s.rows[ts]
			body := richtext.PlainText(r.m.Doc)
			if strings.Contains(body, "job") || !strings.Contains(body, "***") {
				if len(bad) < 6 {
					kind := "with blocks"
					if ts == textOnly {
						kind = "text only"
					}
					bad = append(bad, fmt.Sprintf("frame %d, the message %s: the body reads %q; want the view's mask under the wrapper's change", i, kind, body))
				}
			}
			if a := author(r); a != overlayAuthor {
				bad = append(bad, fmt.Sprintf("frame %d: the attachment's author is %q; want the wrapper's %q", i, a, overlayAuthor))
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, b := range bad {
		t.Error(b)
	}
}
