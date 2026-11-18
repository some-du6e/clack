package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
)

// A canvas mention is named from the store by the id Quip's export holds,
// the way Slack's canvas draws each mention control from its model (Quip's
// hI and hu views): "#name" for a channel the viewer can see, member or
// not; the lock and the name for a private one they're in; "Private
// channel" (dimmed, not a link) for one neither flannel nor
// conversations.genericInfo knows; "@name" for a person, "@handle" for a
// group; Pending (the skeleton) while a lookup is out, never the id.
func TestNameCanvasMention(t *testing.T) {
	a, fs := canvasApp(t)
	var member, private string
	for _, c := range fs.ChannelLinkCases() {
		switch c.Name {
		case "member":
			member = c.ID
		case "private member":
			private = c.ID
		}
	}
	if member == "" || private == "" {
		t.Fatalf("fixture channels: member %q private %q", member, private)
	}
	cv, ok := a.Conv(member)
	if !ok || cv.Name == "" {
		t.Fatalf("no member channel %s", member)
	}
	pv, ok := a.Conv(private)
	if !ok || pv.Name == "" {
		t.Fatalf("no private channel %s", private)
	}
	named := func(kind richtext.SpanKind, id string) richtext.Span {
		s := richtext.Span{Kind: kind, ID: id, Text: "#" + id}
		if kind != richtext.Channel {
			s.Text = "@" + id
		}
		a.NameCanvasMention(&s)
		return s
	}
	if s := named(richtext.Channel, member); s.Text != "#"+cv.Name || s.Pending || s.Off || s.Lock {
		t.Errorf("member channel: %+v, want #%s", s, cv.Name)
	}
	if s := named(richtext.Channel, private); s.Text != pv.Name || !s.Lock || s.Off || s.Pending {
		t.Errorf("private channel: %+v, want the lock and %s", s, pv.Name)
	}
	// a public channel the viewer isn't in: the skeleton until edge
	// channels/info answers, then its name
	if s := named(richtext.Channel, fakeslack.PreviewChannelID); !s.Pending || s.Text != "" {
		t.Errorf("non-member channel before its lookup: %+v, want Pending with no text", s)
	}
	// one neither flannel nor conversations.genericInfo knows
	const gone = "C0NOSUCH0001"
	named(richtext.Channel, gone)
	deadline := time.Now().Add(15 * time.Second)
	for {
		p, g := named(richtext.Channel, fakeslack.PreviewChannelID), named(richtext.Channel, gone)
		if !p.Pending && !g.Pending {
			if p.Text != "#lounge-preview" || p.Off || p.Lock {
				t.Errorf("non-member channel: %+v, want #lounge-preview", p)
			}
			if g.Text != richtext.PrivateChannelLabel || !g.Off || !g.Lock {
				t.Errorf("unknown channel: %+v, want the Private channel chip", g)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("lookups never answered: %+v %+v", p, g)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := fs.CallCount("conversations.genericInfo"); n == 0 {
		t.Error("flannel's failed id never went to conversations.genericInfo")
	}
	// people and groups
	self := named(richtext.User, fakeslack.SelfID)
	if !self.Self || self.Pending || len(self.Text) < 2 || self.Text[0] != '@' || self.Text == "@"+fakeslack.SelfID {
		t.Errorf("self: %+v", self)
	}
	deadline = time.Now().Add(15 * time.Second)
	for s := named(richtext.User, fakeslack.ProfileID); s.Pending; s = named(richtext.User, fakeslack.ProfileID) {
		if time.Now().After(deadline) {
			t.Fatalf("member never loaded: %+v", s)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s := named(richtext.User, fakeslack.ProfileID); s.Self || s.Text == "@"+fakeslack.ProfileID || s.Text == "" {
		t.Errorf("person: %+v", s)
	}
	// groups: the skeleton until the group list is read (core reads it 5 s
	// after boot), then the handle; an id the list lacks is hu's "unknown
	// group"
	deadline = time.Now().Add(20 * time.Second)
	for {
		loaded := a.S.UsergroupsLoaded()
		s := named(richtext.Usergroup, fakeslack.WorkspaceGroupID)
		if s.Text == "@snackcrew" && !s.Pending {
			break
		}
		if !loaded && (!s.Pending || s.Text != "") {
			t.Fatalf("group before the list loaded: %+v, want Pending", s)
		}
		if time.Now().After(deadline) {
			t.Fatalf("group: %+v, want @snackcrew", s)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if s := named(richtext.Usergroup, "S0NOSUCH001"); s.Text != "@unknown group" || s.Pending {
		t.Errorf("unknown group: %+v", s)
	}
	// unchanged: NameCanvasMention reports nothing to do
	s := named(richtext.Channel, member)
	if a.NameCanvasMention(&s) {
		t.Error("a named mention named again reported a change")
	}
}

// Slack's fetchRawChannelsById asks conversations.genericInfo for the ids
// flannel's channels/info fails, and only what neither knows is hidden.
func TestResolveChannelsGenericInfoFallback(t *testing.T) {
	a, fs := canvasApp(t)
	fs.SetEdgeHook(func(method string, body map[string]json.RawMessage) (any, bool) {
		if method != "edge/channels/info" {
			return nil, false
		}
		var up map[string]int
		_ = json.Unmarshal(body["updated_ids"], &up)
		failed := []string{}
		for id := range up {
			failed = append(failed, id)
		}
		return map[string]any{"ok": true, "results": []any{}, "failed_ids": failed}, true
	})
	got := a.resolveChannels(t.Context(), []string{fakeslack.PreviewChannelID, "C0NOSUCH0002"})
	if len(got) != 2 {
		t.Errorf("resolved %v", got)
	}
	if n := a.S.ChannelName(fakeslack.PreviewChannelID); n != "lounge-preview" {
		t.Errorf("flannel's failed id, answered by genericInfo: name %q", n)
	}
	if acc := a.S.ChannelAccess(fakeslack.PreviewChannelID); acc != richtext.ChannelVisible {
		t.Errorf("its access %v", acc)
	}
	if acc := a.S.ChannelAccess("C0NOSUCH0002"); acc != richtext.ChannelHidden {
		t.Errorf("unknown to both: access %v, want hidden", acc)
	}
}
