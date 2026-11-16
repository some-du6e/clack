package messages

// A plugin's Message patch gets MessageWrapper's msg in its props on every
// render, as in Slack, never what its own last render handed Original.
// Clack used to send it back (the row's msg override, row.message(), as
// Message's own msg prop), so a patch that changes msg saw its own change
// again on the next render: one appending to the text grew it on every
// frame JS answered in time, and a pass-through patch bounced an updated
// row between two revisions (vote_update_test.go). Synthetic data only.

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// appendTextJS patches Message and ThreadReplyGeneric to draw the
// message's text with a suffix.
var appendTextJS = pluginJS(`
class AppendText extends TautPlugin {
  static id = 'AppendText'
  static pluginName = 'Append text (test)'
  static description = 'test: Message draws msg.text with a suffix'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    for (const n of ['Message', 'ThreadReplyGeneric'])
      this.api.patchComponent(n, (Original) => (props) =>
        React.createElement(Original, { ...props, msg: { ...props.msg, text: props.msg.text + ' (patched)' } }))
  }
}
return AppendText`)

func TestMessagePatchGetsWrappersMsg(t *testing.T) {
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: voteChan, Name: "meta", IsChannel: true, IsMember: true, NumMembers: 40})
	app.S.PutBot(voteBotID, store.BotInfo{Name: "Vote Bot", Icon: shotAvatar(voteBotID)})
	base := time.Now().Add(-time.Hour).Unix()
	rootTS, replyTS, chanTS := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60), fmt.Sprintf("%d.000300", base+120)
	root := slack.Message{Type: "message", TS: rootTS, User: "UANA00001", Text: "Proposal: move standup to 10am",
		Blocks: shotRich(shotSection("Proposal: move standup to 10am"))}
	root.ThreadTS, root.ReplyCount, root.ReplyUsers, root.LatestReply = rootTS, 1, []string{"UBEN00001"}, replyTS
	app.S.SetLatest(voteChan, []slack.Message{root, voteMsg(chanTS, "", 1, 0, 0)}, false)
	app.S.SetThread(voteChan, rootTS, []slack.Message{root, voteMsg(replyTS, rootTS, 1, 0, 0)})
	renders, step := patchedEnv(t, app, appendTextJS, "Message", "ThreadReplyGeneric")

	h := coreHost{app: app, th: theme.New(false)}
	cv := NewChannelView(h, voteChan)
	cv.Preview = true
	tv := NewThreadView(h, voteChan, rootTS)
	now := time.Now()
	panes := []struct {
		p    *votePane
		ts   string
		name string // the component its row draws through
	}{
		{&votePane{name: "channel", s: cv.s, v: cv, size: image.Pt(1100, 700), now: &now, step: step}, chanTS, "Message"},
		{&votePane{name: "thread", s: tv.s, v: tv, size: image.Pt(520, 700), now: &now, step: step}, replyTS, "ThreadReplyGeneric"},
	}
	const want = "Upvotes: 0\nDownvotes: 0 (patched)"
	for _, x := range panes {
		until(t, 10*time.Second, x.p.name+": the patched text", x.p.frame, func() bool {
			r := x.p.s.rows[x.ts]
			return r != nil && r.m.Text == want
		})
		for i := 0; i < 20; i++ {
			x.p.frame()
		}
		if r := x.p.s.rows[x.ts]; r.m.Text != want {
			t.Errorf("%s: the row draws %q after 20 more frames; want %q (the patch applied once)", x.p.name, r.m.Text, want)
		}
		for _, s := range renders.sentTexts(x.name) {
			if strings.Contains(s, "(patched)") {
				t.Errorf("%s: Go sent %s its own patched msg as props (%q); its props are MessageWrapper's msg", x.p.name, x.name, s)
				break
			}
		}
	}
}

// wrapperFieldsJS patches MessageWrapper to hand Original a msg with a
// field of its own and without bot_profile, and Message and
// ThreadReplyGeneric to draw what their msg says about both.
var wrapperFieldsJS = pluginJS(`
class WrapperFields extends TautPlugin {
  static id = 'WrapperFields'
  static pluginName = 'Wrapper fields (test)'
  static description = 'test: Message reads the fields MessageWrapper set'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    this.api.patchComponent('MessageWrapper', (Original) => (props) => {
      const msg = React.useMemo(() => {
        const m = { ...props.msg, clackFlag: 'from-wrapper' }
        delete m.bot_profile
        return m
      }, [props.msg])
      return React.createElement(Original, { ...props, msg })
    })
    for (const n of ['Message', 'ThreadReplyGeneric'])
      this.api.patchComponent(n, (Original) => (props) =>
        React.createElement(Original, { ...props, msg: { ...props.msg,
          text: props.msg.text + ' [' + props.msg.clackFlag + ', bot_profile ' + ('bot_profile' in props.msg) + ']' } }))
  }
}
return WrapperFields`)

// Message (ThreadReplyGeneric in a thread pane) gets MessageWrapper's msg
// as the wrapper's patch passed it, the very object (Slack's MessageWrapper
// renders Message with {...this.props}). Clack rebuilt it from the row's
// message: a field of the plugin's own was gone and the bot_profile the
// wrapper took out was back. Synthetic data only.
func TestMessagePatchGetsWrappersExactMsg(t *testing.T) {
	app, _, _ := shotsApp(t)
	app.S.PutConv(slack.Conversation{ID: voteChan, Name: "meta", IsChannel: true, IsMember: true, NumMembers: 40})
	app.S.PutBot(voteBotID, store.BotInfo{Name: "Vote Bot", Icon: shotAvatar(voteBotID)})
	base := time.Now().Add(-time.Hour).Unix()
	rootTS, replyTS, chanTS := fmt.Sprintf("%d.000100", base), fmt.Sprintf("%d.000200", base+60), fmt.Sprintf("%d.000300", base+120)
	root := slack.Message{Type: "message", TS: rootTS, User: "UANA00001", Text: "Proposal: move standup to 10am",
		Blocks: shotRich(shotSection("Proposal: move standup to 10am"))}
	root.ThreadTS, root.ReplyCount, root.ReplyUsers, root.LatestReply = rootTS, 1, []string{"UBEN00001"}, replyTS
	app.S.SetLatest(voteChan, []slack.Message{root, voteMsg(chanTS, "", 1, 0, 0)}, false)
	app.S.SetThread(voteChan, rootTS, []slack.Message{root, voteMsg(replyTS, rootTS, 1, 0, 0)})
	_, step := patchedEnv(t, app, wrapperFieldsJS, "MessageWrapper", "Message", "ThreadReplyGeneric")

	h := coreHost{app: app, th: theme.New(false)}
	cv := NewChannelView(h, voteChan)
	cv.Preview = true
	tv := NewThreadView(h, voteChan, rootTS)
	now := time.Now()
	panes := []struct {
		p  *votePane
		ts string
	}{
		{&votePane{name: "channel", s: cv.s, v: cv, size: image.Pt(1100, 700), now: &now, step: step}, chanTS},
		{&votePane{name: "thread", s: tv.s, v: tv, size: image.Pt(520, 700), now: &now, step: step}, replyTS},
	}
	const want = "Upvotes: 0\nDownvotes: 0 [from-wrapper, bot_profile false]"
	for _, x := range panes {
		got := ""
		for dl := time.Now().Add(10 * time.Second); got != want && time.Now().Before(dl); time.Sleep(15 * time.Millisecond) {
			x.p.frame()
			if r := x.p.s.rows[x.ts]; r != nil {
				got = r.m.Text
			}
		}
		if got != want {
			t.Errorf("%s: the row draws %q; want %q (the msg MessageWrapper's patch handed Original, as Message's patch got it)", x.p.name, got, want)
		}
	}
}
