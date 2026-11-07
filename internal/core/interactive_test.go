package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Synthetic fixtures only (scraping policy).

func nextView(t *testing.T, ch <-chan ViewEvent, kind string) ViewEvent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Kind == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %q view event", kind)
		}
	}
}

func noView(t *testing.T, ch <-chan ViewEvent) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected view event %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestContainerJSON(t *testing.T) {
	cases := []struct {
		c    ActionContainer
		want string
	}{
		{ActionContainer{Type: "message", ChannelID: "C1", MessageTS: "1.2", ThreadTS: "1.0"},
			`{"channel_id":"C1","is_ephemeral":false,"message_ts":"1.2","type":"message"}`},
		{ActionContainer{Type: "message", ChannelID: "C1", MessageTS: "1.2", ThreadTS: "1.0", IsEphemeral: true},
			`{"channel_id":"C1","is_ephemeral":true,"message_ts":"1.2","thread_ts":"1.0","type":"message"}`},
		{ActionContainer{Type: "message_attachment", ChannelID: "C1", MessageTS: "1.2", AttachmentID: 2},
			`{"attachment_id":2,"channel_id":"C1","is_ephemeral":false,"message_ts":"1.2","type":"message_attachment"}`},
		{ActionContainer{Type: "view", ViewID: "V1"}, `{"type":"view","view_id":"V1"}`},
	}
	for _, c := range cases {
		if got := string(c.c.JSON()); got != c.want {
			t.Errorf("container %+v:\n got %s\nwant %s", c.c, got, c.want)
		}
	}
}

func TestBlockActionSend(t *testing.T) {
	a, f := testApp(t)
	f.reply["blocks.actions"] = `{"ok":true}`
	ix := a.Interactive()
	m := &Message{TS: "1700000000.000100", Channel: "C1", BotID: "B1", BotProfile: &BotProfile{ID: "B1", AppID: "A1", TeamID: "T9"}}
	b := ix.MessageContainer(m, 0)
	b.Action = json.RawMessage(`{"type":"button","block_id":"b1","action_id":"go","text":{"type":"plain_text","text":"Go"},"value":"v"}`)
	b.State = json.RawMessage(`{"b2":{"c":{"type":"checkboxes","selected_options":[]}}}`)
	tok, err := ix.BlockActionSend(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	p := f.last("blocks.actions")
	if p.Get("service_id") != "B1" || p.Get("app_id") != "A1" || p.Get("service_team_id") != "T9" {
		t.Errorf("ids: %v", p)
	}
	if !strings.HasPrefix(tok, "web-") || p.Get("client_token") != tok {
		t.Errorf("token %q vs %q", tok, p.Get("client_token"))
	}
	if !strings.HasPrefix(p.Get("actions"), `[{"type":"button"`) || !strings.Contains(p.Get("container"), `"is_ephemeral":false`) {
		t.Errorf("payload: %s %s", p.Get("actions"), p.Get("container"))
	}
	if p.Get("state") != `{"values":{"b2":{"c":{"type":"checkboxes","selected_options":[]}}}}` {
		t.Errorf("state: %s", p.Get("state"))
	}

	f.reply["blocks.actions"] = `{"ok":false,"error":"post_error","message":"App took too long to respond. Please try again, or contact the app's developer."}`
	_, err = ix.BlockActionSend(context.Background(), b)
	ae, ok := err.(*ActionError)
	if !ok || ae.Code != "post_error" || !strings.Contains(ae.Message, "took too long") {
		t.Errorf("post_error: %#v", err)
	}
	f.reply["blocks.actions"] = `{"ok":false,"error":"invalid_container"}`
	_, err = ix.BlockActionSend(context.Background(), b)
	if ae, ok := err.(*ActionError); !ok || ae.Message != ActionErrorText("invalid_container") {
		t.Errorf("invalid_container: %#v", err)
	}
}

func TestViewEvents(t *testing.T) {
	a, f := testApp(t)
	f.reply["blocks.actions"] = `{"ok":true}`
	ix := a.Interactive()
	ch, stop := ix.Subscribe()
	defer stop()
	tok, err := ix.BlockActionSend(context.Background(), BlockAction{ServiceID: "B1", Container: ActionContainer{Type: "message"},
		Action: json.RawMessage(`{"type":"button"}`)})
	if err != nil {
		t.Fatal(err)
	}
	// someone else's click (another client of the same account): ignored
	ix.handle(rtm("view_opened", map[string]any{"view_id": "VX", "client_token": "web-1", "timeout_range": 10000,
		"view": map[string]any{"id": "VX", "type": "modal"}}))
	noView(t, ch)
	// ours, nested view
	ix.handle(rtm("view_opened", map[string]any{"view_id": "V1", "view_type": "modal", "client_token": tok, "timeout_range": 10000,
		"view": map[string]any{"id": "V1", "type": "modal", "root_view_id": "V1", "private_metadata": "pm", "callback_id": "cb",
			"title": map[string]any{"type": "plain_text", "text": "T"}, "blocks": []any{}}}))
	ev := nextView(t, ch, "opened")
	if ev.View.ID != "V1" || ev.View.PrivateMetadata != "pm" || ev.View.CallbackID != "cb" || ev.ClientToken != tok {
		t.Errorf("opened: %+v", ev.View)
	}
	// flat (older payloads)
	ix.handle(rtm("view_opened", map[string]any{"view_id": "V2", "previous_view_id": "V1", "client_token": tok, "timeout_range": 10000,
		"type": "modal", "title": map[string]any{"type": "plain_text", "text": "T2"}, "blocks": []any{}}))
	ev = nextView(t, ch, "opened")
	if ev.View.ID != "V2" || ev.View.PreviousViewID != "V1" || len(ev.View.Title) == 0 {
		t.Errorf("flat: %+v", ev.View)
	}
	ix.handle(rtm("view_updated", map[string]any{"view_id": "V1", "view": map[string]any{"id": "V1", "type": "modal", "hash": "h2"}}))
	if ev = nextView(t, ch, "updated"); ev.View.Hash != "h2" {
		t.Errorf("updated: %+v", ev.View)
	}
	// App Home publish
	ix.handle(rtm("view_opened", map[string]any{"view_id": "VH", "view_type": "home", "channel_id": "D1",
		"view": map[string]any{"id": "VH", "type": "home", "blocks": []any{}}}))
	if ev = nextView(t, ch, "home"); ev.Channel != "D1" || ix.Home("D1") == nil {
		t.Errorf("home: %+v", ev)
	}
	// ephemeral messages
	ix.handle(rtm("message", map[string]any{"channel": "C1", "ts": "5.5", "is_ephemeral": true, "thread_ts": "5.0", "text": "x"}))
	nextView(t, ch, "ephemeral")
	if !ix.Ephemeral("C1", "5.5") || ix.Ephemeral("C1", "5.6") {
		t.Error("ephemeral flags")
	}
	c := ix.MessageContainer(&Message{Channel: "C1", TS: "5.5", BotID: "B1"}, 0).Container
	if !c.IsEphemeral || c.ThreadTS != "5.0" {
		t.Errorf("ephemeral container: %+v", c)
	}
}

func TestSubmitView(t *testing.T) {
	a, f := testApp(t)
	ix := a.Interactive()
	f.reply["views.submit"] = `{"ok":false,"error":"validation_failed","errors":{"b1":"Too short"}}`
	r, err := ix.SubmitView(context.Background(), "V1", json.RawMessage(`{"b1":{"a":{"type":"plain_text_input","value":"x"}}}`))
	if err != nil || r.ResponseAction != "errors" || r.Errors["b1"] != "Too short" {
		t.Fatalf("errors: %+v %v", r, err)
	}
	p := f.last("views.submit")
	if p.Get("view_id") != "V1" || p.Get("state") != `{"values":{"b1":{"a":{"type":"plain_text_input","value":"x"}}}}` || !strings.HasPrefix(p.Get("client_token"), "web-") {
		t.Errorf("params: %v", p)
	}
	f.reply["views.submit"] = `{"ok":true,"response_action":"update","view":{"id":"V1","type":"modal","hash":"h3","blocks":[]}}`
	r, err = ix.SubmitView(context.Background(), "V1", nil)
	if err != nil || r.ResponseAction != "update" || r.View == nil || r.View.Hash != "h3" {
		t.Fatalf("update: %+v %v", r, err)
	}
	f.reply["views.submit"] = `{"ok":true,"toast_message":"Saved"}`
	if r, _ = ix.SubmitView(context.Background(), "V1", nil); r.Toast != "Saved" || r.ResponseAction != "" {
		t.Errorf("toast: %+v", r)
	}
	f.reply["views.submit"] = `{"ok":false,"error":"view_not_found"}`
	if r, _ = ix.SubmitView(context.Background(), "V1", nil); r.Code != "view_not_found" || r.ViewError == "" {
		t.Errorf("failure: %+v", r)
	}
	f.reply["views.close"] = `{"ok":true}`
	if err := ix.CloseView(context.Background(), "V2", "V1"); err != nil {
		t.Fatal(err)
	}
	if p := f.last("views.close"); p.Get("view_id") != "V2" || p.Get("root_view_id") != "V1" {
		t.Errorf("close: %v", p)
	}
}

func TestShortcutsAndHome(t *testing.T) {
	a, f := testApp(t)
	ix := a.Interactive()
	f.reply["apps.actions.v2.list"] = `{"ok":true,"app_actions":[{"app_id":"A1","app_name":"Tool","team_id":"T1","icons":{"image_32":"https://img.example/i.png"},
		"actions":[{"action_id":"X1","app_id":"A1","name":"Inspect","desc":"d","callback_id":"cb","type":"message_action"},
		{"action_id":"X2","app_id":"A1","name":"New","desc":"","callback_id":"cb2","type":"global_action"}]}]}`
	ss, err := ix.Shortcuts(context.Background())
	if err != nil || len(ss) != 2 || ss[0].AppName != "Tool" || ss[0].Icon == "" {
		t.Fatalf("shortcuts: %+v %v", ss, err)
	}
	f.reply["apps.actions.v2.execute"] = `{"ok":true}`
	if _, err := ix.RunShortcut(context.Background(), ss[0], "C1", "1.2"); err != nil {
		t.Fatal(err)
	}
	p := f.last("apps.actions.v2.execute")
	if p.Get("action_id") != "X1" || p.Get("app_id") != "A1" || p.Get("context") != `{"channel_id":"C1","message_ts":"1.2"}` {
		t.Errorf("execute: %v", p)
	}
	f.reply["conversations.info"] = `{"ok":true,"channel":{"id":"D1"},"app_home":{"app_id":"A1","app_installed_team_id":"T1","conversation_id":"D1",
		"home_tab_enabled":true,"messages_tab_enabled":true,"messages_tab_read_only_enabled":false,"home_view_id":"VH"},
		"home_view":{"id":"VH","type":"home","bot_id":"B1","blocks":[{"type":"divider"}]}}`
	h, v, err := ix.AppHome(context.Background(), "D1")
	if err != nil || !h.HomeTab || h.AppID != "A1" || v == nil || v.ID != "VH" || v.ChannelID != "D1" {
		t.Fatalf("home: %+v %+v %v", h, v, err)
	}
	if p := f.last("conversations.info"); p.Get("return_app_home") != "true" {
		t.Errorf("info params: %v", p)
	}
}
