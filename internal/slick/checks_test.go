package slick

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/taut"
)

// Per-plugin behaviour checks for the acceptance suite: each proves the
// plugin does its thing through the same Go API the UI uses (patched
// renders, Redux views, thunk routing, send transforms and checks, styles).

func richText(text string) json.RawMessage {
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": text}}},
	}}})
	return b
}

func init() {
	enableWith["Nicknames"] = map[string]any{"names": map[string]any{aliceID: "Ally Nick"}}
	enableWith["Censorship"] = map[string]any{"terms": "job, employment"}

	before["ClearURLs"] = func(h *harness) {
		h.answer("https://raw.githubusercontent.com/ClearURLs/Rules/", `{"providers":{"globalRules":{"urlPattern":".*","rules":["utm_[a-z]+"],"rawRules":[],"exceptions":[],"redirections":[]}}}`)
	}

	checks["IgnoreTime"] = func(t *testing.T, h *harness) {
		if h.ui.style("plugin:IgnoreTime:ignore-time") == "" {
			t.Fatal("no stylesheet")
		}
	}
	checks["HumanCount"] = func(t *testing.T, h *harness) {
		n := findNative(h.render(1, "BaseAvatarStack", map[string]any{"channelId": chGen, "memberCount": 5}), "BaseAvatarStack")
		if n == nil {
			t.Fatal("no native BaseAvatarStack")
		}
		if p, _ := n["p"].(map[string]any); p["memberCount"] == float64(5) {
			t.Errorf("memberCount unchanged: %v", p)
		}
	}
	checks["Censorship"] = func(t *testing.T, h *harness) {
		var text string
		eventually(t, 3*time.Second, "censored view", func() bool {
			m, ok := h.tt.MessageView(chGen, "1700000001.000100")
			text = m.Text
			return ok
		})
		if strings.Contains(text, "job") || !strings.Contains(text, "fair") {
			t.Errorf("censored text %q", text)
		}
	}
	checks["Nicknames"] = func(t *testing.T, h *harness) {
		eventually(t, 3*time.Second, "nickname", func() bool {
			m, ok := h.tt.MemberView(aliceID)
			return ok && m.DisplayName == "Ally Nick"
		})
	}
	checks["SilentTyping"] = func(t *testing.T, h *harness) {
		type args struct {
			ChannelID string `json:"channelId"`
		}
		calls := 0
		native := func(ctx context.Context, a args) (any, error) { calls++; return nil, nil }
		if _, err := taut.Dispatch(h.ctx(), h.tt, "currentUserStartedTyping", args{chGen}, native); err != nil {
			t.Fatal(err)
		}
		if calls != 0 {
			t.Errorf("typing went out while silenced")
		}
	}
	checks["NotShitMarkdown"] = func(t *testing.T, h *harness) {
		out := h.tt.TransformSendBlocks(h.ctx(), richText("say **hi** now"))
		if !strings.Contains(string(out), `"bold":true`) || strings.Contains(string(out), "**") {
			t.Errorf("not bolded: %s", out)
		}
	}
	checks["ClearURLs"] = func(t *testing.T, h *harness) {
		in := richText("see https://example.com/page?utm_source=news&id=7")
		var out json.RawMessage
		eventually(t, 5*time.Second, "rules loaded", func() bool {
			out = h.tt.TransformSendBlocks(h.ctx(), in)
			return !strings.Contains(string(out), "utm_source")
		})
		if !strings.Contains(string(out), "id=7") {
			t.Errorf("cleaned too much: %s", out)
		}
		if !h.wasFetched("https://raw.githubusercontent.com/ClearURLs/Rules/") {
			t.Errorf("the main half didn't fetch the rules")
		}
	}
	checks["UserPronouns"] = func(t *testing.T, h *harness) {
		text := h.renderText(2, "ThreadSenderAndTimestampGeneric", map[string]any{"msg": map[string]any{"user": aliceID, "ts": "1700000001.000100", "channel": chGen}})
		if !strings.Contains(text, "she/her") {
			t.Errorf("no pronouns in %q", text)
		}
	}

	enableWith["OfficeHours"] = map[string]any{"schedule": []any{map[string]any{"days": []any{0, 1, 2, 3, 4, 5, 6}, "start": "00:00", "end": "23:59", "status": "invisible"}}}
	quiet["OfficeHours"] = "acts on a timer through userAPI"
	checks["OfficeHours"] = func(t *testing.T, h *harness) {
		eventually(t, 5*time.Second, "users.setPresence away", func() bool {
			for _, f := range h.api.called("users.setPresence") {
				if f.Get("presence") == "away" {
					return true
				}
			}
			return false
		})
	}
	quiet["oneko"] = "a DOM element and a document listener"
	checks["oneko"] = func(t *testing.T, h *harness) {
		if !h.jsBool(`return !!document.getElementById('oneko') && document.body.contains(document.getElementById('oneko'))`) {
			t.Fatal("no cat in the document")
		}
		if !h.tt.Bridge().WantsEvent("mousemove") {
			t.Error("the UI won't forward mousemove")
		}
	}
	checks["NoTrack"] = func(t *testing.T, h *harness) {
		var r struct {
			Blocked bool `json:"blocked"`
			Other   int  `json:"other"`
		}
		h.jsInto(`
			const blocked = await fetch('https://slackb.com/beacon/x').then(() => false, () => true)
			const other = await fetch('https://example.org/x').then((r) => r.status, () => -1)
			return { blocked, other }`, &r)
		if !r.Blocked || r.Other != 404 {
			t.Errorf("%+v", r)
		}
	}
	checks["ShutUpSlackbot"] = func(t *testing.T, h *harness) {
		type args struct {
			Title   string `json:"title"`
			Message string `json:"message"`
		}
		shown := 0
		native := func(ctx context.Context, a args) (any, error) { shown++; return nil, nil }
		_, _ = taut.Dispatch(h.ctx(), h.tt, "showNotification", args{"Slackbot", "Bob has been using the /deploy slash command. When people enter /deploy, it runs Deploy Bot."}, native)
		if shown != 0 {
			t.Errorf("a slash-command notice was shown")
		}
		_, _ = taut.Dispatch(h.ctx(), h.tt, "showNotification", args{"alice", "lunch?"}, native)
		if shown != 1 {
			t.Errorf("an ordinary notification was dropped")
		}
	}
}
