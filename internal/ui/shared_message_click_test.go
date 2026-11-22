package ui

// Karim (α.112, Windows, 2026-11-20): "clicking the message doesnt
// highlight nor jumps to it". The thread pane showed a reply with a link
// to a reply in another thread of the same channel, which Slack unfurls
// as a shared message (BaseMessageAttachmentSlackMessage in card layout:
// the avatar, the author and time, "From a thread in #channel", the
// message). Slack's card click (handleCardClick, unless on a link or a
// button), its context and its time all go to
// handleAttachmentFooterNavigateClick (boot-shared YFww): for a reply,
// toThread({channelId, threadTs, replyTs, highlightRoot: true}), the
// conversation at the thread's root, highlighted, and the thread pane at
// the linked reply in its own thread. The link in the text is
// openPermalink's jumpToMessage, the same route. The flexpane's sync
// (client-boot eCxs, "sync thread flexpane") flashes the reply only when
// the primary didn't already show its channel (with nested secondary
// views on, as on Karim's account): from the same channel's pane the root
// flashes in the conversation, the reply doesn't. Clack's α.112 opened
// the reply's own ts as a thread (c/<C>/t/<reply>): nothing to draw and
// nothing highlighted. Synthetic data on the fake.

import (
	"encoding/json"
	"image"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

func TestSharedMessageOpensItsReply(t *testing.T) {
	for _, c := range []struct {
		name string
		card bool
	}{{"the shared message's card", true}, {"the link to it in the text", false}} {
		t.Run(c.name, func(t *testing.T) {
			e := threadHeaderEnv(t, false)
			pub := fakeslack.ThreadPublicID
			author, other := e.fs.UserIDs()[2], e.fs.UserIDs()[3]
			rootA, _ := e.fs.PostQuiet(pub, other, "synthetic: the thread the pane shows")
			e.fs.PostReplyQuiet(pub, rootA, author, "synthetic: a reply in it")
			rootB, _ := e.fs.PostQuiet(pub, author, "synthetic: another thread")
			reply := e.fs.PostReplyQuiet(pub, rootB, author, "HERE!!!!!!")
			link := "https://fakeclub.slack.com/archives/" + pub + "/p" + strings.ReplaceAll(reply, ".", "") + "?thread_ts=" + rootB + "&cid=" + pub
			att := slack.Attachment{ID: 1, IsMsgUnfurl: true, ChannelID: pub, TS: json.RawMessage(strconv.Quote(reply)), AuthorID: author,
				Text: "HERE!!!!!!", Fallback: "HERE!!!!!!", FromURL: link, OriginalURL: link}
			mine := e.fs.PostReplyAttachmentsQuiet(pub, rootA, other, "this message is cool <"+link+">", []slack.Attachment{att})
			e.openChannel(pub)
			e.a.OpenThread(pub, rootA)
			var p image.Point
			e.until("the reply with the shared message in the thread pane", 20*time.Second, func() bool {
				var at []image.Rectangle
				for _, lr := range e.mcLinks(pub, rootA) {
					if lr.TS == mine && strings.Contains(lr.URL, "/p"+strings.ReplaceAll(reply, ".", "")) {
						at = append(at, lr.R) // the text's link, then the card's own (its time, its context)
					}
				}
				if len(at) < 2 {
					return false
				}
				if r := at[0]; !c.card {
					p = image.Pt(r.Min.X+r.Dx()/2, r.Min.Y+r.Dy()/2)
				} else {
					r := at[len(at)-1] // the card's last link: its message is under it
					p = image.Pt(r.Min.X+4, r.Max.Y+12)
				}
				return true
			})
			e.clickAt(p, time.Second)
			e.until("the linked reply's thread in the pane", 20*time.Second, func() bool {
				s := &e.a.secondary
				return e.a.router.Current() == Route("c/"+pub+"/t/"+rootB) && s.channel == pub && s.ts == rootB
			})
			sharedJumpLanded(t, e, pub, rootB, reply)
		})
	}
}
