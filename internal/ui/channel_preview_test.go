package ui

// Channel previews and posting restrictions against internal/fakeslack
// (synthetic workspace; nothing talks to Slack): opening a channel the
// viewer isn't in shows Slack's JoinChannelPane and a sidebar row, Join
// calls conversations.join and brings the composer back, an archived
// channel shows the disabled bar, and a posting-restricted one the
// announce-only roadblock. With PREVIEW_SHOTS_DIR set (the preview-shots
// workflow) each state is saved as a screenshot.

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/io/key"

	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
)

func previewEnv(t *testing.T) (*perfEnv, *fakeslack.Server) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if dir := os.Getenv("PREVIEW_SHOTS_DIR"); dir != "" && e.gpu == nil {
		w, err := headless.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	return e, fs
}

// shot saves the last frame (PREVIEW_SHOTS_DIR only).
func (e *perfEnv) shot(name string) {
	dir := os.Getenv("PREVIEW_SHOTS_DIR")
	if dir == "" || e.gpu == nil {
		return
	}
	img := image.NewRGBA(image.Rectangle{Max: e.size})
	if err := e.gpu.Screenshot(img); err != nil {
		e.tb.Fatalf("screenshot: %v", err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		e.tb.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		e.tb.Fatal(err)
	}
}

func (e *perfEnv) sidebarHas(id string) bool {
	for _, s := range e.a.model().Sections {
		for _, r := range s.Rows {
			if r == id {
				return true
			}
		}
	}
	return false
}

func TestChannelPreviewJoin(t *testing.T) {
	e, fs := previewEnv(t)
	ch := fakeslack.PreviewChannelID
	e.openChannel(ch)
	c := e.a.convoFor(ch, "")
	e.until("join pane", 20*time.Second, func() bool { return e.a.footerFor(c) == footPreview && c.jp != nil && !c.jp.joinRect.Empty() })
	e.until("preview row in the sidebar", 10*time.Second, func() bool { return e.sidebarHas(ch) })
	if ci := e.a.convOf(ch); ci == nil || ci.Purpose == "" {
		t.Errorf("preview conv %+v", ci)
	}
	if c.jp.caretRect.Empty() {
		t.Error("temp_join_channel is on in the fake: no Join… caret")
	}
	e.settle(200 * time.Millisecond)
	e.shot("channel-preview")

	// the caret opens Slack's "Join…" menu
	e.click(c.jp.caretRect.Min.Add(c.jp.caretRect.Size().Div(2)))
	e.settle(150 * time.Millisecond)
	e.shot("channel-preview-join-menu")
	e.key(key.NameEscape, 0)
	e.settle(100 * time.Millisecond)

	// Join: conversations.join, then the composer replaces the pane
	e.click(c.jp.joinRect.Min.Add(c.jp.joinRect.Size().Div(2)))
	e.until("conversations.join", 10*time.Second, func() bool { return len(fs.JoinCalls()) > 0 })
	if j := fs.JoinCalls()[0]; j.Channel != ch || j.ExpirationTS != "" {
		t.Errorf("join call %+v", j)
	}
	e.until("composer back", 10*time.Second, func() bool { return e.a.footerFor(c) == footNone })
	e.until("member row", 10*time.Second, func() bool {
		ci := e.a.model().conv(ch)
		return ci != nil && !ci.Preview && e.sidebarHas(ch)
	})
	e.settle(200 * time.Millisecond)
	e.shot("channel-preview-joined")

	// leaving a never-joined preview drops its row
	other := e.a.convoFor(fakeslack.ArchivedChannelID, "")
	e.openChannel(fakeslack.ArchivedChannelID)
	e.until("archived bar", 20*time.Second, func() bool { return e.a.footerFor(other) == footArchived })
	e.settle(200 * time.Millisecond)
	e.shot("channel-archived")
	e.openChannel(fs.ChannelIDs()[1])
	e.until("archived row gone", 10*time.Second, func() bool { return !e.sidebarHas(fakeslack.ArchivedChannelID) })
}

func TestChannelPreviewTempJoin(t *testing.T) {
	e, fs := previewEnv(t)
	ch := fakeslack.PreviewChannelID
	e.openChannel(ch)
	c := e.a.convoFor(ch, "")
	e.until("join pane", 20*time.Second, func() bool { return e.a.footerFor(c) == footPreview && c.jp != nil && !c.jp.caretRect.Empty() })
	e.a.openTempJoinMenu(c)
	e.frame()
	e.a.joinPreview(c, "one_week")
	e.until("conversations.join", 10*time.Second, func() bool { return len(fs.JoinCalls()) > 0 })
	j := fs.JoinCalls()[0]
	if j.ExpirationTS == "" {
		t.Fatalf("temp join without expiration_ts: %+v", j)
	}
	e.until("temporary section", 10*time.Second, func() bool {
		for _, s := range e.a.model().Sections {
			if s.Type == "temporary_channels" {
				return strings.Contains(strings.Join(s.Rows, ","), ch)
			}
		}
		return false
	})
}

func TestPostingRestrictedRoadblock(t *testing.T) {
	e, fs := previewEnv(t)
	ch := fs.ChannelIDs()[2]
	svc := e.a.data.core
	cv, ok := svc.S.Conv(ch)
	if !ok {
		t.Fatal("no member channel")
	}
	cp := *cv
	cp.Properties = json.RawMessage(`{"posting_restricted_to":{"type":["admin"]},"threads_restricted_to":{"type":["admin"]}}`)
	svc.S.PutConv(cp)
	e.openChannel(ch)
	c := e.a.convoFor(ch, "")
	rb := func() string {
		if r, ok := c.comp.(interface{ RoadblockMessage() string }); ok {
			return r.RoadblockMessage()
		}
		return ""
	}
	e.until("announce-only roadblock", 10*time.Second, func() bool { return rb() == announceOnlyMessage })
	e.settle(200 * time.Millisecond)
	e.shot("channel-posting-restricted")

	// thread-only: the channel composer is blocked, thread replies aren't
	cp.Properties = json.RawMessage(`{"posting_restricted_to":{"type":["admin"]}}`)
	svc.S.PutConv(cp)
	c.postAt = time.Time{}
	e.until("thread-only still blocks the channel composer", 5*time.Second, func() bool { return rb() == announceOnlyMessage })
	tc := e.a.convoFor(ch, "1762516800.000100")
	e.frame()
	e.a.composerRoadblock(tc, tc.comp)
	if r, ok := tc.comp.(interface{ RoadblockMessage() string }); ok && r.RoadblockMessage() != "" {
		t.Errorf("thread reply composer blocked in a thread-only channel: %q", r.RoadblockMessage())
	}

	// lifted: the composer is back
	cp.Properties = nil
	svc.S.PutConv(cp)
	c.postAt = time.Time{}
	e.until("composer unblocked", 5*time.Second, func() bool { return rb() == "" })
}
