package messages

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// woHost records what the card's clicks ask the ui root for.
type woHost struct {
	nilHost
	opened []WorkObjectOpen
}

func (h *woHost) OpenWorkObject(o WorkObjectOpen) { h.opened = append(h.opened, o) }
func (h *woHost) ShareWorkObject(WorkObjectOpen) bool {
	return false
}

func woAttachment(t *testing.T, appUnfurl string) *richtext.Attachment {
	t.Helper()
	ent := map[string]any{"work_object_type": "external", "entity_id": "W:A0TEST:T1", "app_id": "A0TEST",
		"external_url": "https://tracker.example.com/T1", "layouts": map[string]any{"compact": map[string]any{"title": map[string]any{"text": "T1"}}}}
	raw, _ := json.Marshal(ent)
	m := slack.Message{Type: "message", TS: "1.000", Attachments: []slack.Attachment{{ID: 1, FromURL: "https://tracker.example.com/T1",
		AppUnfurlURL: appUnfurl, WorkObjectEntity: raw}}}
	for _, b := range richtext.FromMessage(nilRes{}, &m).Blocks {
		if b.Att != nil && b.Att.WorkObject != nil {
			return b.Att
		}
	}
	t.Fatal("no work object attachment")
	return nil
}

// nilRes resolves nothing.
type nilRes struct{}

func (nilRes) UserName(string) string                   { return "" }
func (nilRes) ChannelName(string) string                { return "" }
func (nilRes) UsergroupName(string) string              { return "" }
func (nilRes) Emoji(string) (url, text string, ok bool) { return "", "", false }
func (nilRes) SelfID() string                           { return "U0SELF" }

// ExternalWorkObjectEntity's clicks: the card and Preview open the
// flexpane on Details, Comments and View conversations on their tabs;
// Open opens app_unfurl_url when the attachment has one (pFiZ B), else
// external_url.
func TestWorkObjectClickRouting(t *testing.T) {
	var urls []string
	prev := openURL
	openURL = func(u string) { urls = append(urls, u) }
	t.Cleanup(func() { openURL = prev })
	h := &woHost{}
	r := &richHandler{h: h, m: &core.Message{Channel: "C1", TS: "1.000"}}
	att := woAttachment(t, "https://tracker.example.com/T1?via=slack")
	for _, c := range []struct {
		a   richview.WorkObjectAction
		tab string
	}{{richview.WOClick, "details"}, {richview.WOPreview, "details"}, {richview.WOComments, "comments"}, {richview.WOConversations, "threads"}} {
		r.WorkObject(richview.WorkObjectEvent{Att: att, Action: c.a})
		if n := len(h.opened); n == 0 || h.opened[n-1].Tab != c.tab || h.opened[n-1].Channel != "C1" || h.opened[n-1].TS != "1.000" ||
			h.opened[n-1].Entity.EntityID != "W:A0TEST:T1" {
			t.Fatalf("action %d: opened %+v, want the %s tab", c.a, h.opened, c.tab)
		}
	}
	r.WorkObject(richview.WorkObjectEvent{Att: att, Action: richview.WOOpen})
	r.WorkObject(richview.WorkObjectEvent{Att: woAttachment(t, ""), Action: richview.WOOpen})
	if len(urls) != 2 || urls[0] != "https://tracker.example.com/T1?via=slack" || urls[1] != "https://tracker.example.com/T1" {
		t.Errorf("Open opened %q", urls)
	}
	// Share without a share dialog does nothing
	r.WorkObject(richview.WorkObjectEvent{Att: att, Action: richview.WOShare})
	if len(h.opened) != 4 || len(urls) != 2 {
		t.Errorf("Share did something: %+v %q", h.opened, urls)
	}
}
