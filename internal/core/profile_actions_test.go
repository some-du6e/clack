package core

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
)

// A profile field filter rides search.modules people's custom_fields as
// Slack's browser sends it ({"<id>": [value]}); without one it stays {}.
func TestPeopleBrowseCustomFields(t *testing.T) {
	if got := peopleBrowseParams(PeopleBrowseOpts{}).Get("custom_fields"); got != "{}" {
		t.Errorf("no filter: %q", got)
	}
	p := peopleBrowseParams(PeopleBrowseOpts{CustomFields: map[string][]string{"Xf01ABC": {"Fixture Wrangler"}}})
	if got := p.Get("custom_fields"); got != `{"Xf01ABC":["Fixture Wrangler"]}` {
		t.Errorf("custom_fields %q", got)
	}
}

// The calls behind the profile surfaces, on the fake: edge
// channels/membership, apps.permissions.internal.add/denyAdd and
// search.enterprise's user group query.
func TestProfileActionCalls(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	a := New(slack.New(fs.Creds()), store.New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	forms := map[string]url.Values{}
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		switch method {
		case "apps.permissions.internal.add", "apps.permissions.internal.denyAdd", "search.enterprise":
			forms[method] = form
			if method == "apps.permissions.internal.add" {
				return map[string]any{"ok": true, "scope_info": []map[string]any{{"scopes": []string{"chat:write"}, "short_description": "Send messages", "is_dangerous": true}}}, true
			}
			if method == "search.enterprise" {
				return map[string]any{"ok": true, "num_found": 1, "next_cursor_mark": "abc", "items": []map[string]any{{"id": "U0PFA", "name": "pf"}}}, true
			}
			return map[string]any{"ok": true}, true
		}
		return nil, false
	})
	var edgeBody map[string]json.RawMessage
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		edgeBody = body
		return map[string]any{"ok": true, "members": []string{"U1"}, "non_members": []string{"U2"}}, true
	})
	m, err := a.ChannelMembership(ctx, "C1", []string{"U1", "U2"})
	if err != nil || !m["U1"] || m["U2"] || len(m) != 2 {
		t.Fatalf("membership %v %v", m, err)
	}
	if string(edgeBody["channel"]) != `"C1"` || string(edgeBody["as_admin"]) != "false" {
		t.Errorf("channels/membership body %v", edgeBody)
	}
	scopes, err := a.AddAppToChannel(ctx, "U0APP", "C1", false)
	if err != nil || len(scopes) != 1 || !scopes[0].Dangerous || scopes[0].Short != "Send messages" {
		t.Fatalf("scopes %+v %v", scopes, err)
	}
	if f := forms["apps.permissions.internal.add"]; f.Get("app_user") != "U0APP" || f.Get("channel") != "C1" || f.Get("did_confirm") != "false" {
		t.Errorf("add form %v", f)
	}
	if err := a.DenyAddAppToChannel(ctx, "U0APP", "C1"); err != nil || forms["apps.permissions.internal.denyAdd"].Get("app_user") != "U0APP" {
		t.Errorf("denyAdd %v %v", forms["apps.permissions.internal.denyAdd"], err)
	}
	us, next, n, err := a.UsergroupMembers(ctx, "S1", "rob", "")
	if err != nil || len(us) != 1 || us[0].ID != "U0PFA" || next != "abc" || n != 1 {
		t.Fatalf("members %v %q %d %v", us, next, n, err)
	}
	var q struct {
		Type    string              `json:"type"`
		Clauses []map[string]string `json:"clauses"`
	}
	f := forms["search.enterprise"]
	if err := json.Unmarshal([]byte(f.Get("query")), &q); err != nil || q.Type != "and" || len(q.Clauses) != 3 ||
		q.Clauses[1]["type"] != "belongs_to_usergroup_id" || q.Clauses[1]["value"] != "S1" || q.Clauses[2]["type"] != "fuzzy_with_email" {
		t.Errorf("query %s (%v)", f.Get("query"), err)
	}
	if f.Get("cursor_mark") != "*" || f.Get("count") != "100" || f.Get("include_bots") != "0" {
		t.Errorf("search.enterprise form %v", f)
	}
}

// togglePriorityUserById's error toasts.
func TestVIPError(t *testing.T) {
	tooMany := &slack.Error{Code: "too_many_vips", Body: json.RawMessage(`{"ok":false,"error":"too_many_vips","max_vip_limit":50}`)}
	for _, c := range []struct {
		err  error
		want string
	}{
		{&slack.Error{Code: "user_not_found"}, "User is not eligible to be added as VIP."},
		{tooMany, "robin could not be added to VIP. You can only have a maximum of 50 VIPs."},
		{&slack.Error{Code: "ratelimited"}, "Hmm, something went wrong. Try again?"},
		{context.DeadlineExceeded, ""},
	} {
		if got := VIPError(c.err, "robin"); got != c.want {
			t.Errorf("%v: %q, want %q", c.err, got, c.want)
		}
	}
}
