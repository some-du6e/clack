package ui

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/ui/popups"
)

// The Edit topic and Edit description inputs hold a stored topic the way
// Slack's useChannelTopicInput / useChannelPurposeInput do (boot-shared
// wD23, QBdp): message_to_contents_string (client-boot L9Xf) shows a
// channel the client knows as #name and one it can't see (Karim's
// #alumni topic, 2026-11-20: a private channel, C0B5XH84KCZ) as #C…, a
// member as @display-name, <!here> as @here, a labelled link as its label
// and &amp; as &. Saving it untouched sends nothing (handleSave's
// value() !== U); saving it edited sends submitChannelTopic's /
// submitChannelPurpose's text (clean_message_text_with_state, client-boot
// DfFm): <#C…|name>, <#C…|C…> for the unknown channel, <@U…>, <!here>,
// the link's label and &amp;.
func TestTopicEditMentions(t *testing.T) {
	e, fs := previewEnv(t)
	var mu sync.Mutex
	var sent []string // "method channel text"
	var badArgs []string
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method != "conversations.setTopic" && method != "conversations.setPurpose" {
			return nil, false
		}
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, method+" "+form.Get("channel")+" "+form.Get("topic")+form.Get("purpose"))
		// submitChannelTopic's and submitChannelPurpose's own arguments
		want := url.Values{"reason": {"submitChannelTopic"}}
		if method == "conversations.setPurpose" {
			want = url.Values{"reason": {"submitChannelPurpose"}, "name_tagging": {"true"}}
		}
		for k := range want {
			if form.Get(k) != want.Get(k) {
				badArgs = append(badArgs, method+" "+k+"="+form.Get(k))
			}
		}
		return map[string]any{"ok": true}, true
	})
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		if len(badArgs) > 0 {
			t.Errorf("wrong arguments: %q", badArgs)
		}
	}()
	calls := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sent...)
	}
	ch := fakeslackMemberChannel(e)
	other := ""
	for _, s := range e.a.model().Sections {
		for _, id := range s.Rows {
			if c := e.a.model().conv(id); id != ch && other == "" && c != nil && c.Kind == "channel" && !c.Preview && c.Name != "" {
				other = id
			}
		}
	}
	if ch == "" || other == "" {
		t.Fatal("no two member channels")
	}
	a := e.a.Core()
	const uid = "U0FAKE00003"
	a.EnsureUsers(context.Background(), []string{uid})
	uname := ""
	e.until("the member's name", 10*time.Second, func() bool {
		if u, ok := a.S.User(uid); ok && u != nil {
			uname = u.Profile.DisplayName
		}
		return uname != ""
	})
	oname := a.S.ChannelName(other)
	stored := "Alums: <#" + other + "> and <#C0B5XH84KCZ>\nAsk <@" + uid + "> or <!here> about <https://example.com/docs|the docs> &amp; more"
	shown := "Alums: #" + oname + " and #C0B5XH84KCZ\nAsk @" + uname + " or @here about the docs & more"
	clean := "Alums: <#" + other + "|" + oname + "> and <#C0B5XH84KCZ|C0B5XH84KCZ>\nAsk <@" + uid + "> or <!here> about the docs &amp; more today"

	// the header's Edit topic
	conv := *e.a.model().conv(ch)
	conv.Topic = stored
	open := func() *popups.TopicDialog {
		t.Helper()
		e.a.openTopicDialog(ch, &conv)
		e.settle(300 * time.Millisecond)
		d, ok := topOverlay[*popups.TopicDialog](e.a)
		if !ok {
			t.Fatal("no Edit topic dialog")
		}
		return d
	}
	d := open()
	if got := d.Text(); got != shown {
		t.Errorf("Edit topic shows\n  %q, want\n  %q", got, shown)
	}
	if !d.Save() {
		t.Error("Save on the untouched topic didn't just close")
	}
	e.settle(300 * time.Millisecond)
	if c := calls(); len(c) != 0 {
		t.Errorf("the untouched topic was sent: %q", c)
	}
	d = open()
	e.typeText(" today")
	e.settle(150 * time.Millisecond)
	d.Save()
	e.until("setTopic", 10*time.Second, func() bool { return len(calls()) >= 1 })
	if c := calls(); c[0] != "conversations.setTopic "+ch+" "+clean {
		t.Errorf("Edit topic sent\n  %q, want\n  %q", c[0], "conversations.setTopic "+ch+" "+clean)
	}

	// channel details' Edit topic and Edit description
	fs.SetTopic(ch, stored)
	fs.SetPurpose(ch, stored)
	o := openEditTopic(t, e, ch)
	if got := o.d.EditText(); got != shown {
		t.Errorf("details' Edit topic shows\n  %q, want\n  %q", got, shown)
	}
	o.d.SaveEdit()
	e.until("the modal closes", 10*time.Second, func() bool { return !o.d.Editing() })
	e.settle(300 * time.Millisecond)
	if c := calls(); len(c) != 1 {
		t.Errorf("details sent the untouched topic: %q", c[1:])
	}
	edit := func(what, method string, start func()) {
		t.Helper()
		n := len(calls())
		start()
		e.settle(400 * time.Millisecond)
		if !o.d.Editing() {
			t.Fatalf("%s didn't open", what)
		}
		if got := o.d.EditText(); got != shown {
			t.Errorf("%s shows\n  %q, want\n  %q", what, got, shown)
		}
		e.typeText(" today")
		e.settle(150 * time.Millisecond)
		o.d.SaveEdit()
		e.until(method, 10*time.Second, func() bool { return len(calls()) > n })
		e.until("the modal closes", 10*time.Second, func() bool { return !o.d.Editing() })
		if c := calls(); c[n] != method+" "+ch+" "+clean || len(c) != n+1 {
			t.Errorf("%s sent\n  %q, want\n  %q", what, c[n:], method+" "+ch+" "+clean)
		}
	}
	edit("details' Edit topic", "conversations.setTopic", o.d.EditTopic)
	edit("Edit description", "conversations.setPurpose", o.d.EditPurpose)
}

// Edit description past its 250 characters: Texty only warns (no cap),
// Slack refuses the save (too_long), and ChannelPurposeDialog stays open
// with what was typed (Gro7 over FlDO: onGo's rejection never closes it).
func TestEditDescriptionTooLong(t *testing.T) {
	e, fs := previewEnv(t)
	var mu sync.Mutex
	var sent []string
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method != "conversations.setPurpose" {
			return nil, false
		}
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, form.Get("purpose"))
		return map[string]any{"ok": false, "error": "too_long"}, true
	})
	ch := fakeslackMemberChannel(e)
	fs.SetPurpose(ch, "Short description")
	e.a.OpenDetails(ch)
	var o detailsOverlay
	e.until("Edit description", 10*time.Second, func() bool {
		var ok bool
		if o, ok = topOverlay[detailsOverlay](e.a); !ok {
			return false
		}
		o.d.EditPurpose()
		return o.d.Editing()
	})
	e.settle(400 * time.Millisecond)
	long := " " + strings.Repeat("x", 260)
	e.typeText(long)
	e.settle(150 * time.Millisecond)
	want := "Short description" + long
	if got := o.d.EditText(); got != want {
		t.Fatalf("the input holds %d characters, want %d (no cap)", len([]rune(got)), len([]rune(want)))
	}
	o.d.SaveEdit()
	e.until("setPurpose", 10*time.Second, func() bool { mu.Lock(); defer mu.Unlock(); return len(sent) > 0 })
	e.settle(500 * time.Millisecond)
	if !o.d.Editing() {
		t.Fatal("the refused description closed the dialog")
	}
	if got := o.d.EditText(); got != want {
		t.Errorf("after the refusal the input holds %q", got)
	}
}
