package slick

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
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
		_, _ = taut.Dispatch(h.ctx(), h.tt, "showNotification", args{"Slackbot", "A new slash command /deploy was added to this workspace by Bob."}, native)
		if shown != 0 {
			t.Errorf("a slash-command notice was shown")
		}
		_, _ = taut.Dispatch(h.ctx(), h.tt, "showNotification", args{"alice", "lunch?"}, native)
		if shown == 0 {
			t.Errorf("an ordinary notification was dropped")
		}
	}

	before["Experiments"] = func(h *harness) {
		h.tt.DataWrite("slick:plugin:Experiments", "overrides", `{"clack_test_experiment":"treatment"}`)
	}
	checks["Experiments"] = func(t *testing.T, h *harness) {
		g := h.jsString(`return T.redux.getStore().getState().experiments?.clack_test_experiment?.group ?? ''`)
		if g != "treatment" {
			t.Errorf("override group %q", g)
		}
		h.ui.mu.Lock()
		tabs := len(h.ui.tabs)
		h.ui.mu.Unlock()
		if tabs == 0 {
			t.Error("no Preferences tab")
		}
	}
	checks["HaikuWarning"] = func(t *testing.T, h *harness) {
		if !h.tt.SendCheck(h.ctx(), composerCtx(chGen, string(richText("just a normal message")))) {
			t.Fatal("blocked a normal message")
		}
		bodyKids := func() int {
			var n int
			h.jsInto(`return document.body.children.length`, &n)
			return n
		}
		n0 := bodyKids()
		done := make(chan bool, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			done <- h.tt.SendCheck(ctx, composerCtx(chGen, string(richText("an old silent pond a frog jumps into the pond splash silence again"))))
		}()
		eventually(t, 3*time.Second, "the haiku dialog", func() bool { return bodyKids() > n0 })
		select {
		case ok := <-done:
			t.Errorf("the haiku went out without asking (%v)", ok)
		default:
		}
	}

	before["HcaStatus"] = func(h *harness) {
		h.answer("https://auth.hackclub.com/api/external/check", `{"result":"verified_but_over_18"}`)
	}
	checks["HcaStatus"] = func(t *testing.T, h *harness) {
		h.render(3, "BaseMessageSender", map[string]any{"userId": aliceID})
		h.waitTree(t, 3, "a status class on the sender", func(nodes []any) bool {
			n := findNative(nodes, "BaseMessageSender")
			p, _ := n["p"].(map[string]any)
			c, _ := p["className"].(string)
			return strings.Contains(c, "slick-hca-over-18")
		})
		if !h.wasFetched("https://auth.hackclub.com/api/external/check?slack_id=" + aliceID) {
			t.Error("the main half didn't ask auth.hackclub.com")
		}
	}

	enableWith["CustomFonts"] = map[string]any{"fontFamily": "Comic Neue"}
	checks["CustomFonts"] = func(t *testing.T, h *harness) {
		eventually(t, 3*time.Second, "font style", func() bool {
			return strings.Contains(h.ui.style("plugin:CustomFonts"), "Comic Neue")
		})
	}
	enableWith["StreamerMode"] = map[string]any{"activation": "always"}
	checks["StreamerMode"] = func(t *testing.T, h *harness) {
		type args struct {
			Title   string `json:"title"`
			Message string `json:"message"`
		}
		shown := 0
		native := func(ctx context.Context, a args) (any, error) { shown++; return nil, nil }
		eventually(t, 3*time.Second, "notifications silenced", func() bool {
			before := shown
			_, _ = taut.Dispatch(h.ctx(), h.tt, "showNotification", args{"alice", "secret dm"}, native)
			return shown == before
		})
		if h.ui.style("plugin:StreamerMode:redaction") == "" {
			t.Error("no redaction stylesheet")
		}
	}
	// an admin-only channel the viewer may post in only because they're an admin
	before["RestrictedChannelWarning"] = func(h *harness) {
		self, _ := h.app.S.User(selfID)
		admin := *self
		admin.IsAdmin = true
		h.app.S.PutUsers([]slack.User{admin})
		h.app.S.Self = admin
		props, _ := json.Marshal(map[string]any{"posting_restricted_to": map[string]any{"type": []string{"admin"}}})
		h.app.S.PutConv(slack.Conversation{ID: "C0ADMINS01", Name: "admins-post", IsChannel: true, IsMember: true, NumMembers: 50, Properties: props})
	}
	checks["RestrictedChannelWarning"] = func(t *testing.T, h *harness) {
		text := h.renderText(4, "MessageInputRoadblock", map[string]any{"channelId": "C0ADMINS01"})
		if !strings.Contains(text, "Post anyway") {
			t.Errorf("no way to post anyway: %q", text)
		}
	}

	enableWith["BetterBlock"] = map[string]any{"blockedUsers": map[string]any{aliceID: "alice"}}
	checks["BetterBlock"] = func(t *testing.T, h *harness) {
		var r struct {
			Message []string `json:"message"`
			Slice   []string `json:"slice"`
			Raw     []string `json:"raw"`
		}
		h.jsInto(`
			const st = T.redux.getStore().getState()
			const key = 'message-1700000002.000200-C0GENERAL1'
			return {
				message: st.messages['C0GENERAL1']['1700000002.000200'].reactions[0].users,
				slice: st.reactions[key]?.[0]?.users ?? [],
				raw: T.getRawState().reactions[key]?.[0]?.users ?? [],
			}`, &r)
		if has(r.Message, aliceID) || has(r.Slice, aliceID) || !has(r.Raw, aliceID) || !has(r.Message, selfID) {
			t.Errorf("blocked reactor still shown: %+v", r)
		}
	}
}

// waitTree waits until root's latest tree satisfies cond.
func (h *harness) waitTree(t *testing.T, root uint64, what string, cond func([]any) bool) []any {
	t.Helper()
	var nodes []any
	eventually(t, 5*time.Second, what, func() bool {
		h.ui.mu.Lock()
		tree := h.ui.trees[root]
		h.ui.mu.Unlock()
		nodes = nil
		if json.Unmarshal(tree, &nodes) != nil {
			return false
		}
		return cond(nodes)
	})
	return nodes
}
