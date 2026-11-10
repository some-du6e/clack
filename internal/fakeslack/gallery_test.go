package fakeslack

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

type galleryRes struct{}

func (galleryRes) UserName(string) string              { return "someone" }
func (galleryRes) ChannelName(string) string           { return "general" }
func (galleryRes) UsergroupName(string) string         { return "team" }
func (galleryRes) SelfID() string                      { return SelfID }
func (galleryRes) Emoji(string) (string, string, bool) { return "", "", false }

// TestGallery: the blockkit-gallery channel serves every registered part,
// oldest first, each message's blocks valid; only the invalid-block
// fixture renders Slack's error fallback; the Show more thread replies.
func TestGallery(t *testing.T) {
	s, err := Start(Small)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, ok := s.conv(GalleryChannelID); !ok {
		t.Fatal("no gallery channel")
	}
	hist := s.galleryHistory()
	gs := s.Gallery()
	if len(hist) != len(gs) || len(gs) < 15 {
		t.Fatalf("history %d, gallery %d", len(hist), len(gs))
	}
	for i, g := range gs {
		m := hist[len(hist)-1-i]
		if m.Text == "" && len(m.Attachments) == 0 { // attachment-only bot messages may have none
			t.Errorf("%s: no fallback text", g.Name)
		}
		d := richtext.FromMessage(galleryRes{}, &m)
		bad := 0
		for _, b := range d.Blocks {
			if b.Kind == richtext.Unsupported {
				bad++
			}
		}
		if want := map[bool]int{true: 2}[g.Name == "layout/invalid and unknown"]; strings.HasPrefix(g.Name, "layout/") && bad != want {
			t.Errorf("%s: %d unsupported blocks, want %d", g.Name, bad, want)
		}
	}
	ts := s.GalleryTS("layout/show more")
	out := s.historyPage(url.Values{"channel": {GalleryChannelID}, "limit": {"200"}})
	var page struct{ Messages []slack.Message }
	_ = json.Unmarshal(out.([]byte), &page)
	if len(page.Messages) != len(gs) {
		t.Errorf("conversations.history: %d messages", len(page.Messages))
	}
	reps, ok := s.galleryReplies(ts)
	if !ok || len(reps) != 2 || reps[1].ThreadTS != ts || len(reps[1].Blocks) == 0 {
		t.Errorf("replies %+v", reps)
	}
}
