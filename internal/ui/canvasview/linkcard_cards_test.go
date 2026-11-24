package canvasview

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// a container that unfurled with nothing of its own: the card goes by its
// URL alone (a Slack permalink)
func bareContainer() *slack.Message {
	return unfurledAs(&slack.Message{Type: "message"}, true)
}

func waitKind(s *shooter, v *View, kind, has string) {
	softWait(s, v, 10*time.Second, func() bool {
		k, _, _ := cardState(v)
		return k == kind && (has == "" || strings.Contains(embedText(v), has))
	})
}

func wantCard(t *testing.T, v *View, kind string, has ...string) {
	t.Helper()
	k, _, _ := cardState(v)
	txt := embedText(v)
	if k != kind {
		t.Errorf("the card is %q (%q), want %s", k, txt, kind)
	}
	for _, h := range has {
		if !strings.Contains(txt, h) {
			t.Errorf("the %s card's text %q has no %q", kind, txt, h)
		}
	}
	noLabel(t, v)
}

// TestRenderLinkCardMessage: a message permalink's card is MessageUnfurl:
// the sender, the channel and "{date} at {time}" over the message's text,
// read from the store after messages.list by id; a message Slack answers
// without is EmptyUnfurl's "This message has been deleted or does not
// exist.".
func TestRenderLinkCardMessage(t *testing.T) {
	t.Run("known", func(t *testing.T) {
		h, fs := newEnv(t, false)
		ch, user := fs.ChannelIDs()[0], fs.UserIDs()[1]
		ts, _ := fs.PostQuiet(ch, user, "A synthetic message for the card")
		link := "https://fake.slack.com/archives/" + ch + "/p" + strings.ReplaceAll(ts, ".", "")
		_, _, open := cardEnvOn(t, h, fs, link, bareContainer(), cardTS, nil)
		v, s := open(Full, image.Pt(1100, 900))
		waitKind(s, v, "message", "A synthetic message for the card")
		wantCard(t, v, "message", "A synthetic message for the card", h.a.S.UserName(user), " at ")
	})
	t.Run("gone", func(t *testing.T) {
		h, fs := newEnv(t, false)
		link := "https://fake.slack.com/archives/" + fs.ChannelIDs()[0] + "/p1762500000999999"
		_, _, open := cardEnvOn(t, h, fs, link, bareContainer(), cardTS, nil)
		v, s := open(Full, image.Pt(1100, 900))
		waitKind(s, v, "empty", "This message has been deleted or does not exist.")
		wantCard(t, v, "empty", "This message has been deleted or does not exist.")
	})
}

// TestRenderLinkCardMember: a /team/U… link's card is UserProfileUnfurl,
// drawn once users.profile.getSections answered.
func TestRenderLinkCardMember(t *testing.T) {
	h, fs := newEnv(t, false)
	user := fs.UserIDs()[1]
	_, _, open := cardEnvOn(t, h, fs, "https://fake.slack.com/team/"+user, bareContainer(), cardTS, nil)
	v, s := open(Full, image.Pt(1100, 900))
	waitKind(s, v, "member", "")
	wantCard(t, v, "member")
	if got := memberShown(v); got != user {
		t.Errorf("the card shows member %q, want %q", got, user)
	}
}

// TestRenderLinkCardFile: a /files/… link's card is EmbeddedFileUnfurl: a
// PDF without a thumbnail is the generic FileCard, a hosted image with a
// thumbnail the image card, and a file files.info doesn't know is
// EmptyUnfurl's "This file was not found.".
func TestRenderLinkCardFile(t *testing.T) {
	for _, c := range []struct {
		name, id, kind, has string
	}{
		{"generic", fakeslack.CanvasFile, "file-generic", "Quarterly report"},
		{"image", fakeslack.CanvasImage, "file-image", ""},
		{"not found", "F0NOSUCHFIL1", "empty", "This file was not found."},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, fs := newEnv(t, false)
			link := "https://fake.slack.com/files/U0FAKE00001/" + c.id + "/file"
			_, _, open := cardEnvOn(t, h, fs, link, bareContainer(), cardTS, nil)
			v, s := open(Full, image.Pt(1100, 900))
			waitKind(s, v, c.kind, c.has)
			if c.has != "" {
				wantCard(t, v, c.kind, c.has)
			} else {
				wantCard(t, v, c.kind)
			}
		})
	}
}

// TestRenderLinkCardCanvas: a /docs/T…/F… link's card is
// EmbeddedCanvasUnfurl: the canvas's title over its preview (built from
// its export); a canvas files.info doesn't know is NoAccessCanvas's "This
// canvas was not found.".
func TestRenderLinkCardCanvas(t *testing.T) {
	for _, c := range []struct {
		name, id, kind, has string
	}{
		{"shown", fakeslack.CanvasChannel, "canvas", "Launch plan"},
		{"not found", "F0NOSUCHCAN1", "canvas-no-access", "This canvas was not found."},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, fs := newEnv(t, false)
			link := "https://fake.slack.com/docs/" + fakeslack.TeamID + "/" + c.id
			_, _, open := cardEnvOn(t, h, fs, link, bareContainer(), cardTS, nil)
			v, s := open(Full, image.Pt(1100, 900))
			waitKind(s, v, c.kind, c.has)
			wantCard(t, v, c.kind, c.has)
		})
	}
}
