package messages

import (
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// voteBlocks are the blocks of the vote bot Karim saw in a thread
// (2026-11-10; the bot's createInteractiveMessage: a section, then one
// actions block per button, plain_text with emoji: true).
const voteBlocks = `[{"type":"section","block_id":"v1","text":{"type":"mrkdwn","text":"Upvotes: 0\nDownvotes: 11"}},` +
	`{"type":"actions","block_id":"v2","elements":[{"type":"button","action_id":"actionId-upvote","value":"upvote","text":{"type":"plain_text","text":":upvote:  Upvote","emoji":true}}]},` +
	`{"type":"actions","block_id":"v3","elements":[{"type":"button","action_id":"actionId-downvote","value":"downvote","text":{"type":"plain_text","text":":downvote:  Downvote","emoji":true}}]}]`

// The thread pane draws a Block Kit message with the channel's renderer:
// the same element host (blockkit.MessageElements) and richview layout, so
// the vote message's two actions blocks are the same runs in both (each
// its own row, as in Slack: .p-actions_block is a flex row of its own,
// 8px under it and .p-actions_block__action 8px over each element), their
// buttons as wide as their 16px emoji and text make them. With
// THREADS_AUDIT_SHOTS_DIR (the threads-audit workflow) it saves the message
// in the channel and in the thread pane, light and dark. Synthetic data.
func TestThreadsAuditBlockKit(t *testing.T) {
	dir := os.Getenv("THREADS_AUDIT_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		app, fs, _ := shotsApp(t)
		app.S.CustomEmoji.Put("upvote", fs.URL+"img/32x32/upvote.png")
		app.S.CustomEmoji.Put("downvote", fs.URL+"img/32x32/downvote.png")
		const ch = "C0VOTE0001"
		app.S.PutConv(slack.Conversation{ID: ch, Name: "meta", IsChannel: true, IsMember: true, NumMembers: 40})
		app.S.PutBot("B0VOTE0001", store.BotInfo{Name: "Vote Bot", Icon: shotAvatar("B0VOTE0001")})
		day := time.Now()
		hour := 10
		if dark { // blockkit keeps a message's element host by channel/ts across hosts: a ts of its own per theme
			hour = 11
		}
		at := func(h, m int) string {
			return fmt.Sprintf("%d.000100", time.Date(day.Year(), day.Month(), day.Day(), h-10+hour, m, 0, 0, time.Local).Unix())
		}
		root := slack.Message{Type: "message", TS: at(10, 0), User: "UANA00001", Text: "Proposal: move standup to 10am",
			Blocks: shotRich(shotSection("Proposal: move standup to 10am"))}
		root.ThreadTS, root.ReplyCount, root.ReplyUsers, root.LatestReply = root.TS, 1, []string{"UBEN00001"}, at(10, 1)
		vote := func(ts string) slack.Message {
			m := slack.Message{Type: "message", Subtype: "bot_message", TS: ts, BotID: "B0VOTE0001", Username: "Vote Bot", Text: "Upvotes: 0 Downvotes: 11",
				Blocks: json.RawMessage(voteBlocks), BotProfile: &slack.BotProfile{ID: "B0VOTE0001", Name: "Vote Bot", AppID: "A0VOTE0001"}}
			m.Edited = &slack.Edited{User: "B0VOTE0001", TS: at(10, 5)}
			return m
		}
		inChan := vote(at(10, 2))
		reply := vote(at(10, 1))
		reply.ThreadTS = root.TS
		app.S.SetLatest(ch, []slack.Message{root, inChan}, false)
		app.S.SetThread(ch, root.TS, []slack.Message{root, reply})
		th := theme.New(dark)
		host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}

		const cw, chH, tw, thH = 1100, 700, 520, 700
		cv := NewChannelView(host, ch)
		cv.Preview = true
		tv := NewThreadView(host, ch, root.TS)
		cops, tops := new(op.Ops), new(op.Ops)
		fc := func() { shotFrame(cv, th, cw, chH, cops) }
		ft := func() { shotFrame(tv, th, tw, thH, tops) }
		loadList(cv, fc)
		slotsOf := func(s *listState, ts string) []image.Rectangle {
			if r := s.rows[ts]; r != nil && r.rv != nil {
				return r.rv.ElementSlots()
			}
			return nil
		}
		until(t, 5*time.Second, "the vote message in both views", func() { fc(); ft() }, func() bool {
			return len(slotsOf(cv.s, inChan.TS)) == 2 && len(slotsOf(tv.s, reply.TS)) == 2
		})
		for i := 0; i < 40; i++ { // the emoji images
			fc()
			ft()
			time.Sleep(10 * time.Millisecond)
		}
		fc()
		ft()
		cs, rs := slotsOf(cv.s, inChan.TS), slotsOf(tv.s, reply.TS)
		for i := range cs {
			if cs[i].Size() != rs[i].Size() {
				t.Errorf("%s: actions block %d is %v in the channel but %v in the thread pane", mode, i+1, cs[i].Size(), rs[i].Size())
			}
		}
		for name, s := range map[string][]image.Rectangle{"channel": cs, "thread": rs} {
			if gap := s[1].Min.Y - s[0].Max.Y; gap != 16 {
				t.Errorf("%s, %s: %dpx between the two actions blocks, want 16 (8px under one, 8px over the next's button)", mode, name, gap)
			}
			if s[0].Dy() != 28 {
				t.Errorf("%s, %s: the actions run is %dpx tall, want one 28px button row", mode, name, s[0].Dy())
			}
		}
		if dir == "" {
			continue
		}
		cimg := pixeltest.Render(t, cw, chH, cops)
		for i, it := range cv.s.items {
			if it.kind == itMsg && it.key == inChan.TS && i < len(cv.s.pos) {
				y0 := cv.s.pos[i] + cv.s.origin.Y
				savePNG(t, filepath.Join(dir, "bk-vote-channel-"+mode+".png"), crop(cimg, max(y0-8, 0), min(y0+cv.s.heights[i]+8, chH)))
			}
		}
		timg := pixeltest.Render(t, tw, thH, tops)
		y0, y1 := rowsSpan(tv.s)
		savePNG(t, filepath.Join(dir, "bk-vote-thread-"+mode+".png"), crop(timg, max(y0+tv.s.origin.Y-8, 0), min(y1+tv.s.origin.Y+8, thH)))
	}
}
