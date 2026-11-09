package fakeslack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
)

func TestFakeServesBootMethods(t *testing.T) {
	s, err := Start(Small)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := slack.New(s.Creds())
	ctx := context.Background()
	convs, err := c.Conversations(ctx)
	if err != nil || len(convs) != Small.Channels+Small.DMs {
		t.Fatalf("conversations: %d, %v", len(convs), err)
	}
	em, err := c.EmojiList(ctx)
	if err != nil || len(em) < Small.Emoji {
		t.Fatalf("emoji: %d, %v", len(em), err)
	}
	us, err := c.UsersInfo(ctx, []string{SelfID, "U0FAKE00003"})
	if err != nil || len(us) != 2 {
		t.Fatalf("edge users/info: %d, %v", len(us), err)
	}
	p1, err := c.History(ctx, convs[1].ID, slack.HistoryOpts{Limit: 50})
	if err != nil || len(p1.Messages) != 50 || !p1.HasMore {
		t.Fatalf("history page 1: %d more=%v %v", len(p1.Messages), p1.HasMore, err)
	}
	p2, err := c.History(ctx, convs[1].ID, slack.HistoryOpts{Limit: 100, Latest: p1.Messages[49].TS})
	if err != nil || len(p2.Messages) != 70 || p2.HasMore {
		t.Fatalf("history page 2: %d more=%v %v", len(p2.Messages), p2.HasMore, err)
	}
	if !slackTSLess(p2.Messages[0].TS, p1.Messages[49].TS) {
		t.Fatal("page 2 not older than page 1")
	}
	if b, err := s.Fetch(ctx, s.URL+"img/32x16/x.png"); err != nil || len(b) < 50 {
		t.Fatalf("image: %d bytes, %v", len(b), err)
	}
	if b, err := s.Fetch(ctx, "https://a.slack-edge.com/production-standard-emoji-assets/16.0/apple-medium/1f600.png"); err != nil || len(b) < 50 {
		t.Fatalf("external image stand-in: %d bytes, %v", len(b), err)
	}
}

func TestGallerySurfaces(t *testing.T) {
	s, err := Start(Gallery)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c := slack.New(s.Creds())
	ctx := context.Background()
	convs, err := c.Conversations(ctx)
	if err != nil || len(convs) != Small.Channels+Small.DMs+2 {
		t.Fatalf("conversations: %d, %v", len(convs), err)
	}
	var evs []map[string]any
	s.Emit = func(ev map[string]any) { evs = append(evs, ev) }
	var res struct {
		OK     bool              `json:"ok"`
		Error  string            `json:"error"`
		Errors map[string]string `json:"errors"`
		Action string            `json:"response_action"`
	}
	call := func(m string, p url.Values) {
		res.OK, res.Error, res.Errors, res.Action = false, "", nil, ""
		raw, err := c.Raw(ctx, m, p)
		var se *slack.Error
		if errors.As(err, &se) && len(se.Body) > 0 {
			raw = se.Body
		}
		_ = json.Unmarshal(raw, &res)
	}
	call("apps.actions.v2.execute", url.Values{"app_id": {AppAppID}, "action_id": {"SURFA1"}, "client_token": {"web-1"}})
	if len(evs) != 1 || evs[0]["type"] != "view_opened" || evs[0]["client_token"] != "web-1" {
		t.Fatalf("execute: %v", evs)
	}
	for i, want := range []string{"errors", "push"} {
		call("views.submit", url.Values{"view_id": {SurfTicketView}, "client_token": {fmt.Sprint("web-s", i)}})
		if got := res.Action; i == 0 && (res.Error != "validation_failed" || res.Errors["title"] == "") || i == 1 && got != want {
			t.Errorf("submit %d: %+v", i, res)
		}
	}
	call("views.submit", url.Values{"view_id": {"VSURF00002"}, "client_token": {"web-u"}})
	if res.Action != "update" {
		t.Errorf("update: %+v", res)
	}
	call("views.submit", url.Values{"view_id": {"VSURF00002"}, "client_token": {"web-c"}})
	if res.Action != "clear" {
		t.Errorf("clear: %+v", res)
	}
}
