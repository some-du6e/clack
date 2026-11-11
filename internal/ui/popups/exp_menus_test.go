package popups

import (
	"context"
	"errors"
	"image"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// emCalls records the fake Slack's API calls.
type emCalls struct {
	mu    sync.Mutex
	calls map[string][]url.Values
}

// wait returns method's first call whose form has every key=value of
// want, waiting up to d.
func (r *emCalls) wait(t *testing.T, method string, want map[string]string, d time.Duration) url.Values {
	t.Helper()
	end := time.Now().Add(d)
	for {
		r.mu.Lock()
		for _, f := range r.calls[method] {
			ok := true
			for k, v := range want {
				if f.Get(k) != v {
					ok = false
				}
			}
			if ok {
				r.mu.Unlock()
				return f
			}
		}
		got := r.calls[method]
		r.mu.Unlock()
		if time.Now().After(end) {
			t.Fatalf("no %s with %v; calls %v", method, want, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// hcEnvOn is hcEnv on a fake Slack the test started.
func hcEnvOn(t *testing.T, fs *fakeslack.Server) *tzHost {
	t.Helper()
	a := core.New(slack.New(fs.Creds()), store.New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	a.EnsureUsers(ctx, []string{fakeslack.SelfID, fakeslack.ProfileID})
	if u, ok := a.S.User(fakeslack.SelfID); ok {
		a.S.Self = *u
	}
	noNet := func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	th := theme.New(false)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	return &tzHost{a: a, th: th, im: imgcache.New(noNet, "", nil)}
}

// emHook records every call in rec and lets the fake answer it.
func emHook(t *testing.T, rec *emCalls) func(method string, form url.Values) (any, bool) {
	return func(method string, form url.Values) (any, bool) {
		rec.mu.Lock()
		rec.calls[method] = append(rec.calls[method], form)
		rec.mu.Unlock()
		return nil, false
	}
}

// emBooted is a fake Slack and a core booted on it, the fake's calls
// recorded.
func emBooted(t *testing.T) (*tzHost, *fakeslack.Server, *emCalls) {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	rec := &emCalls{calls: map[string][]url.Values{}}
	fs.SetHook(emHook(t, rec))
	h := hcEnvOn(t, fs)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- h.a.Boot(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("boot: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("boot timed out")
	}
	return h, fs, rec
}

func emLabels(items []MenuItem) []string {
	var out []string
	for _, it := range items {
		switch {
		case it.Separator:
			out = append(out, "-")
		case len(it.Sub) > 0:
			var sub []string
			for _, s := range it.Sub {
				sub = append(sub, s.Label)
			}
			out = append(out, it.Label+"["+strings.Join(sub, ",")+"]")
		default:
			out = append(out, it.Label)
		}
	}
	return out
}

func emItem(t *testing.T, items []MenuItem, label string) MenuItem {
	t.Helper()
	for _, it := range items {
		if it.Label == label {
			return it
		}
		for _, s := range it.Sub {
			if s.Label == label {
				return s
			}
		}
	}
	t.Fatalf("no %q in %q", label, emLabels(items))
	return MenuItem{}
}

func emHas(items []MenuItem, label string) bool {
	for _, l := range emLabels(items) {
		if l == label || strings.Contains(l, "["+label) || strings.Contains(l, ","+label) {
			return true
		}
	}
	return false
}

// emClip runs a DoCtx row and returns what it put on the clipboard.
func emClip(it MenuItem) string {
	var r input.Router
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Source: r.Source(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(100, 100))}
	it.DoCtx(gtx)
	r.Frame(&ops)
	_, b, _ := r.WriteClipboard()
	return string(b)
}

const emCh = "C0FAKE00001"

// The message menu's rows, as MessageActionsMenu (2rcC) builds them: the
// follow label from the thread's subscription, the pin wording by
// conversation type, the broadcast rows, the workflow row and Report to
// Slack.
func TestMessageMenuSlackRows(t *testing.T) {
	h, _, _ := emBooted(t)
	self := fakeslack.SelfID
	root := core.Message{Channel: emCh, TS: "1700000000.000100", User: fakeslack.ProfileID, ThreadTS: "1700000000.000100", ReplyCount: 2}
	reply := core.Message{Channel: emCh, TS: "1700000000.000200", User: self, ThreadTS: root.TS}
	items := func(m core.Message, where MessageContext) []MenuItem {
		return actMessageItems(h, m, MessageMenuOpts{Where: where})
	}

	// ojBT: the label follows the subscription, not who wrote the message
	own := core.Message{Channel: emCh, TS: "1700000000.000300", User: self}
	if !emHas(items(own, InChannel), "Get notified about new replies") {
		t.Errorf("own, unsubscribed: %q", emLabels(items(own, InChannel)))
	}
	sub := root
	sub.FollowingThread = true
	if !emHas(items(sub, InChannel), "Turn off notifications for replies") {
		t.Errorf("subscribed root: %q", emLabels(items(sub, InChannel)))
	}
	// a reply reads its root's subscription
	h.a.S.SetThread(emCh, root.TS, []slack.Message{
		{TS: root.TS, ThreadTS: root.TS, User: fakeslack.ProfileID, Subscribed: true, Text: "synthetic root"},
		{TS: reply.TS, ThreadTS: root.TS, User: self, Text: "synthetic reply"},
	})
	if !emHas(items(reply, InThread), "Turn off notifications for replies") {
		t.Errorf("reply under a subscribed root: %q", emLabels(items(reply, InThread)))
	}

	// getPinActionLabel: channels vs conversations
	if !emHas(items(own, InChannel), "Pin to channel") {
		t.Errorf("channel pin: %q", emLabels(items(own, InChannel)))
	}
	dm := core.Message{Channel: "D0FAKEDM01", TS: "1700000000.000400", User: self}
	if !emHas(items(dm, InChannel), "Pin to this conversation") {
		t.Errorf("DM pin: %q", emLabels(items(dm, InChannel)))
	}
	dm.Pinned = true
	if !emHas(items(dm, InChannel), "Un-pin from this conversation") {
		t.Errorf("DM unpin: %q", emLabels(items(dm, InChannel)))
	}

	// T: only in a thread container, on your own reply
	if emHas(items(reply, InChannel), "Send reply to this channel") {
		t.Error("Send reply to this channel outside a thread")
	}
	got := emLabels(items(reply, InThread))
	if i := strings.Index(strings.Join(got, "|"), "Connect to apps[No apps]|-|Send reply to this channel"); i < 0 {
		t.Errorf("thread reply: %q", got)
	}
	other := reply
	other.User = fakeslack.ProfileID
	if emHas(items(other, InThread), "Send reply to this channel") {
		t.Error("someone else's reply offers Send reply to this channel")
	}
	dmReply := core.Message{Channel: "D0FAKEDM01", TS: "1700000000.000500", User: self, ThreadTS: "1700000000.000400"}
	if !emHas(items(dmReply, InThread), "Send reply to this conversation") {
		t.Errorf("DM reply: %q", emLabels(items(dmReply, InThread)))
	}
	// a reply already sent to the channel can be taken back instead
	bc := reply
	bc.Broadcast, bc.Subtype = true, "thread_broadcast"
	if l := emLabels(items(bc, InThread)); emHas(items(bc, InThread), "Send reply to this channel") || !emHas(items(bc, InThread), "Remove reply from channel…") {
		t.Errorf("broadcast reply: %q", l)
	}
	if !emHas(items(bc, InChannel), "Remove reply from channel…") {
		t.Errorf("broadcast in the channel: %q", emLabels(items(bc, InChannel)))
	}

	// q: a workflow bot's message with its workflow_id
	wf := core.Message{Channel: emCh, TS: "1700000000.000600", User: fakeslack.IdentityWorkflowID, WorkflowID: "Wf0FAKE0001"}
	// Workflow Builder's bot user (fakeslack serves it only with
	// Size.Identity): straight into the store
	h.a.S.PutUsers([]slack.User{{ID: fakeslack.IdentityWorkflowID, TeamID: fakeslack.TeamID, Name: "standup",
		RealName: "Standup Workflow", IsBot: true, IsWorkflowBot: true}})
	if it := emItem(t, items(wf, InChannel), "Copy workflow ID"); emClip(it) != "Wf0FAKE0001" {
		t.Errorf("copied %q", emClip(it))
	}
	person := wf
	person.User = fakeslack.ProfileID
	if emHas(items(person, InChannel), "Copy workflow ID") {
		t.Error("Copy workflow ID on a person's message")
	}

	// tH: Report to Slack once client.extras says so, not on your own
	if emHas(items(other, InChannel), "Report to Slack") {
		t.Error("Report to Slack without is_content_reporting_enabled")
	}
	h.a.SetContentReporting(true)
	t.Cleanup(func() { h.a.SetContentReporting(false) })
	l := emLabels(items(other, InChannel))
	if len(l) < 2 || l[len(l)-1] != "Report to Slack" || l[len(l)-2] != "-" {
		t.Errorf("others' message: %q", l)
	}
	if emHas(items(own, InChannel), "Report to Slack") {
		t.Error("Report to Slack on your own message")
	}
}

// The rows' actions make Slack's calls: a thread's Mark unread is
// subscriptions.thread.mark with read 0 at the reply; the conversation's
// moves the read cursor; Send reply to this channel is chat.update with
// reply_broadcast; Remove reply… asks first, then chat.delete with
// broadcast_delete; Report to Slack opens the help center's form.
func TestMessageMenuSlackActions(t *testing.T) {
	h, _, rec := emBooted(t)
	self := fakeslack.SelfID
	root := "1700000000.000100"
	reply := core.Message{Channel: emCh, TS: "1700000000.000200", User: self, ThreadTS: root}
	h.a.S.SetThread(emCh, root, []slack.Message{
		{TS: root, ThreadTS: root, User: fakeslack.ProfileID, Text: "synthetic root"},
		{TS: reply.TS, ThreadTS: root, User: self, Text: "synthetic reply"},
	})

	emItem(t, actMessageItems(h, reply, MessageMenuOpts{Where: InThread}), "Mark unread").Do()
	rec.wait(t, "subscriptions.thread.mark", map[string]string{"channel": emCh, "thread_ts": root, "ts": reply.TS, "read": "0"}, 10*time.Second)
	chMsg := core.Message{Channel: emCh, TS: "1700000000.000300", User: fakeslack.ProfileID}
	emItem(t, actMessageItems(h, chMsg, MessageMenuOpts{Where: InChannel}), "Mark unread").Do()
	rec.wait(t, "conversations.mark", map[string]string{"channel": emCh}, 10*time.Second)
	for _, ts := range h.toastList() {
		if strings.Contains(ts, "unread") {
			t.Errorf("Slack doesn't toast Mark unread: %q", ts)
		}
	}

	emItem(t, actMessageItems(h, reply, MessageMenuOpts{Where: InThread}), "Send reply to this channel").Do()
	rec.wait(t, "chat.update", map[string]string{"channel": emCh, "ts": reply.TS, "reply_broadcast": "true"}, 10*time.Second)

	bc := reply
	bc.Broadcast, bc.Subtype = true, "thread_broadcast"
	emItem(t, actMessageItems(h, bc, MessageMenuOpts{Where: InThread}), "Remove reply from channel…").Do()
	md, ok := lastShown[*Modal](h)
	if !ok || md.o.Title != "Remove from channel" {
		t.Fatalf("no Remove from channel dialog (%T)", md)
	}
	var go_ *ModalButton
	for _, b := range md.o.Buttons {
		if b.Label == "Remove message" {
			go_ = b
		}
	}
	if go_ == nil || go_.Kind != BtnPrimary {
		t.Fatalf("buttons %+v", md.o.Buttons)
	}
	md.press(go_)
	rec.wait(t, "chat.delete", map[string]string{"channel": emCh, "ts": reply.TS, "broadcast_delete": "true"}, 10*time.Second)

	var opened string
	prev := openReport
	openReport = func(u string) error { opened = u; return nil }
	t.Cleanup(func() { openReport = prev })
	h.a.SetContentReporting(true)
	t.Cleanup(func() { h.a.SetContentReporting(false) })
	other := core.Message{Channel: emCh, TS: "1700000000.000700", User: fakeslack.ProfileID, ThreadTS: root}
	emItem(t, actMessageItems(h, other, MessageMenuOpts{}), "Report to Slack").Do()
	want := "/help/requests/report-content?u=" + permalink(h, emCh, other.TS, root) + "&type=message"
	if !strings.HasPrefix(opened, "https://") || !strings.HasSuffix(opened, want) {
		t.Errorf("opened %q, want …%s", opened, want)
	}
}

// client.extras' misc says whether content reporting is on.
func TestClientExtrasContentReporting(t *testing.T) {
	h, fs, _ := emBooted(t)
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "client.extras" {
			if form.Get("extras") != "misc" {
				return map[string]any{"ok": false, "error": "invalid_arguments"}, true
			}
			return map[string]any{"ok": true, "misc": map[string]any{"is_content_reporting_enabled": true}}, true
		}
		return nil, false
	})
	if err := h.a.LoadClientExtras(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !h.a.ContentReportingEnabled() {
		t.Error("is_content_reporting_enabled not read")
	}
}

// Session toggles are on for "treatment" and "on" only (V1z/'s default
// treatment groups), and an unassigned toggle is off.
func TestSessionToggleGroups(t *testing.T) {
	h, _, _ := emBooted(t)
	for g, want := range map[string]bool{"on": true, "treatment": true, "off": false, "control": false, "treatment_a": false, "": false} {
		h.a.SetExperimentGroup(ExpNoAutoHighlightSearchMultiSelect, g)
		if got := noAutoHighlight(h); got != want {
			t.Errorf("group %q: %v", g, got)
		}
	}
	h.a.ClearExperimentGroup(ExpNoAutoHighlightSearchMultiSelect)
	if noAutoHighlight(h) {
		t.Error("unassigned toggle on")
	}
}

// The share dialog's Forward button is the v2 Button exactly while
// slack_kit_v2_button is "on"; Copy Link stays the legacy outline button.
func TestSlackKitV2ButtonFollowsExperiment(t *testing.T) {
	h, _, _ := emBooted(t)
	f := newForward(h, core.Message{Channel: emCh, TS: "1700000000.000100", User: fakeslack.ProfileID})
	for _, c := range []struct {
		group string
		want  bool
	}{{"on", true}, {"off", false}, {"", false}, {"on", true}} {
		h.a.SetExperimentGroup(ExpSlackKitV2Button, c.group)
		if got := f.fwd.V2(); got != c.want {
			t.Errorf("group %q: v2 %v", c.group, got)
		}
	}
	if b := f.modal.o.Buttons[0]; b.Label != "Copy Link" || b.V2 != nil {
		t.Errorf("copy button %+v", b)
	}
	// the v2 Button has no 80px minimum and is 36px tall (medium)
	th := h.Theme()
	var c widget.Clickable
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(400, 100)}}
	v2 := buttonV2(gtx, th, &c, BtnPrimary, theme.ButtonV2Medium, "OK", "", false)
	legacy := button(gtx, th, &c, BtnPrimary, BtnMedium, "OK", "", 80, false)
	if v2.Size.Y != 36 || v2.Size.X != textW(gtx, th, 15, theme.Bold, "OK")+24 || legacy.Size.X < 80 {
		t.Errorf("v2 %v, legacy %v", v2.Size, legacy.Size)
	}
	if ic := buttonV2(gtx, th, &c, BtnPrimary, theme.ButtonV2Medium, "", "caret-down", false); ic.Size != image.Pt(36, 36) {
		t.Errorf("icon only %v", ic.Size)
	}
}

// The share dialog's picker is a SelectOptionsList: the first match is
// active at once, or under a11y_no_auto_highlight_search_multi_select
// pseudo-active (Enter still picks it); arrows don't wrap.
func TestForwardPickerAutoHighlight(t *testing.T) {
	h, _, _ := emBooted(t)
	f := newForward(h, core.Message{Channel: emCh, TS: "1700000000.000100", User: fakeslack.ProfileID})
	if len(f.all) == 0 {
		t.Fatal("no targets")
	}
	q := strings.ToLower(f.all[0].title)
	if len(q) > 3 {
		q = q[:3]
	}
	search := func() {
		f.query = "\x00"
		f.to.SetText(q)
		f.refilter()
		if len(f.match) == 0 {
			t.Fatalf("no match for %q", q)
		}
	}
	h.a.SetExperimentGroup(ExpNoAutoHighlightSearchMultiSelect, "off")
	search()
	if f.sel != 0 || f.pseudoRow() != -1 || f.enterRow() != 0 {
		t.Errorf("toggle off: sel %d pseudo %d", f.sel, f.pseudoRow())
	}
	h.a.SetExperimentGroup(ExpNoAutoHighlightSearchMultiSelect, "on")
	search()
	if f.sel != -1 || f.pseudoRow() != 0 || f.enterRow() != 0 {
		t.Errorf("toggle on: sel %d pseudo %d enter %d", f.sel, f.pseudoRow(), f.enterRow())
	}
}

// A basic select's list under the two session toggles: the placeholder
// row ("--Select an option--", picking it clears) when the select opened
// empty, and the selected row active on open unless no-auto-highlight
// leaves none active with the first row pseudo-active.
func TestSelectListToggles(t *testing.T) {
	h, _, _ := emBooted(t)
	labels := []string{"Alpha", "Beta", "Gamma"}
	open := func(sel int, ph string) *dtTimeList {
		return NewSelectList(h, image.Rect(0, 0, 200, 36), labels, sel, SelectListOpts{Placeholder: ph}, func(int) {}).(*dtTimeList)
	}
	h.a.SetExperimentGroup(ExpBasicSelectPlaceholderOption, "off")
	h.a.SetExperimentGroup(ExpNoAutoHighlightSearchMultiSelect, "off")
	l := open(1, "Select an option")
	if len(l.labels) != 3 || l.cur != 1 || l.sel != 1 || l.pseudoRow() != -1 {
		t.Errorf("toggles off: %q cur %d sel %d pseudo %d", l.labels, l.cur, l.sel, l.pseudoRow())
	}
	h.a.SetExperimentGroup(ExpBasicSelectPlaceholderOption, "on")
	var picked []int
	l = NewSelectList(h, image.Rect(0, 0, 200, 36), labels, -1, SelectListOpts{Placeholder: "Select an option"}, func(i int) { picked = append(picked, i) }).(*dtTimeList)
	if len(l.labels) != 4 || l.labels[0] != "--Select an option--" || l.sel != -1 {
		t.Fatalf("placeholder row: %q sel %d", l.labels, l.sel)
	}
	l.pick(0)
	l.pick(2)
	if len(picked) != 2 || picked[0] != -1 || picked[1] != 1 {
		t.Errorf("picked %v, want [-1 1]", picked)
	}
	if l := open(1, ""); len(l.labels) != 3 {
		t.Errorf("no placeholder text, no placeholder row: %q", l.labels)
	}
	h.a.SetExperimentGroup(ExpNoAutoHighlightSearchMultiSelect, "treatment")
	l = open(2, "")
	if l.cur != -1 || l.sel != 2 || l.pseudoRow() != 0 || l.list.Position.First != 0 {
		t.Errorf("no auto highlight: cur %d sel %d pseudo %d first %d", l.cur, l.sel, l.pseudoRow(), l.list.Position.First)
	}
	l.move(1)
	if l.cur != 0 || l.pseudoRow() != -1 {
		t.Errorf("↓ from none: cur %d", l.cur)
	}
}

// The menu's tag and badge slots (Slack Kit MenuItem tag / badgeCount):
// templates carry them both ways, the badge caps at 9+, the colours are
// the styles' tokens, and rows with them lay out.
func TestMenuTagAndBadge(t *testing.T) {
	h, _, _ := emBooted(t)
	th := h.Theme()
	items := []MenuItem{
		{Label: "Every new message", Radio: true, Checked: true, Subtitle: "Get notified for every message", Tag: &MenuTag{Label: "Customized", Style: "informative"}, Do: func() {}},
		{Label: "Message is", BadgeCount: 2, BadgeStyle: "blue", BadgeHighlighted: true, Sub: []MenuItem{{Label: "Pinned", Do: func() {}}}},
		{Label: "Filters", BadgeCount: 12, Do: func() {}},
	}
	m := NewMenuOpts(h, items, MenuOpts{})
	back := menuFromTemplate(th, m.menuTemplate(items), items)
	if back[0].Tag == nil || *back[0].Tag != (MenuTag{"Customized", "informative"}) || back[1].BadgeCount != 2 || back[1].BadgeStyle != "blue" || back[2].BadgeCount != 12 {
		t.Errorf("round trip: %+v", back)
	}
	plugin := []any{slackc.Props{"key": "p", "label": "Plugin row", "badgeCount": 3.0, "tag": slackc.Props{"label": "New", "style": "success"}, "click": func() {}}}
	if got := menuFromTemplate(th, plugin, nil); len(got) != 1 || got[0].BadgeCount != 3 || got[0].Tag == nil || got[0].Tag.Label != "New" {
		t.Errorf("plugin template: %+v", got)
	}
	if s, capped := badgeText(12); s != "9+" || !capped {
		t.Errorf("badge 12: %q", s)
	}
	if s, capped := badgeText(9); s != "9" || capped {
		t.Errorf("badge 9: %q", s)
	}
	if bg, fg := menuTagColors(theme.New(false), "informative"); bg != rgb(0xe3f8ff) || fg != rgb(0x1264a3) {
		t.Errorf("light informative %v %v", bg, fg)
	}
	if bg, fg := menuTagColors(theme.New(true), "informative"); bg != rgba8(0x036696, 0x40) || fg != rgb(0x2ba5ce) {
		t.Errorf("dark informative %v %v", bg, fg)
	}
	if bg, fg := menuBadgeColors(theme.New(false), ""); bg != rgb(0xe01e5a) || fg != rgb(0xffffff) {
		t.Errorf("red badge %v %v", bg, fg)
	}
	if bg, _ := menuBadgeColors(theme.New(false), "blue"); bg != rgba8(0x36c5f0, 0x21) {
		t.Errorf("blue badge %v", bg)
	}
	// a radio row with a description and a tag wraps the description in
	// the narrower label box: no taller than two lines either way
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1000, 800))}
	plain := NewMenuOpts(h, []MenuItem{{Label: items[0].Label, Radio: true, Checked: true, Subtitle: items[0].Subtitle, Do: func() {}}}, MenuOpts{})
	// at 300px the tag narrows the label box enough to wrap the
	// description onto a second line
	if rh, rp := m.rowHeight(gtx, th, 0, 300), plain.rowHeight(gtx, th, 0, 300); rp < 28 || rh <= rp {
		t.Errorf("radio row %d with the tag, %d without", rh, rp)
	}
	e := newMkEnv(t, m) // draws the rows (tag, badge, caret)
	e.frame()
	if menuTagWidth(gtx, th, "Customized") <= 8 || menuBadgeWidth(gtx, th, 12) > 25 {
		t.Errorf("tag %d badge %d", menuTagWidth(gtx, th, "Customized"), menuBadgeWidth(gtx, th, 12))
	}
}

// Every tooltip takes theme's colours: dark #212428 tips with #f8f8f8
// text, black shortcut chips (KeyboardKey's tooltip variant) in both
// modes.
func TestTooltipColorsFromTheme(t *testing.T) {
	for _, dark := range []bool{false, true} {
		th := theme.New(dark)
		cl := colorsOf(th)
		if cl.Tooltip != th.C.Tooltip || cl.TooltipText != th.C.TooltipText {
			t.Errorf("dark %v: popups %v %v, theme %v %v", dark, cl.Tooltip, cl.TooltipText, th.C.Tooltip, th.C.TooltipText)
		}
		if cl.Kbd != rgb(0x000000) || cl.KbdText != th.C.TooltipText {
			t.Errorf("dark %v: chip %v %v", dark, cl.Kbd, cl.KbdText)
		}
	}
	if c := colorsOf(theme.New(true)).Tooltip; c != rgb(0x212428) {
		t.Errorf("dark tooltip %v", c)
	}
}

// ChannelNotificationPrefMenu tags the setting in effect "Customized"
// when the channel's advanced settings depart from it (n9iP Xq).
func TestNotifyMenuCustomizedTag(t *testing.T) {
	for i, c := range []struct {
		n             core.ChannelNotify
		cur, setting  string
		muted, wanted bool
	}{
		{core.ChannelNotify{}, "everything", "everything", false, false},
		{core.ChannelNotify{FollowAllThreads: true}, "everything", "everything", false, true},
		{core.ChannelNotify{FollowAllThreads: true}, "everything", "mentions_dms", false, false}, // not the setting in effect
		{core.ChannelNotify{FollowAllThreads: true}, "mute", "everything", true, false},
		{core.ChannelNotify{SuppressAtChannel: true}, "mentions_dms", "mentions_dms", false, true},
		{core.ChannelNotify{SuppressAtChannel: true}, "everything", "everything", false, false},
		{core.ChannelNotify{BadgeAllUnreads: true}, "everything", "everything", false, true},
		{core.ChannelNotify{Desktop: "everything", Mobile: "nothing"}, "everything", "everything", false, true},
		{core.ChannelNotify{Desktop: "everything", Mobile: "default"}, "everything", "everything", false, false},
		{core.ChannelNotify{Desktop: "everything", Mobile: "everything"}, "everything", "everything", false, false},
	} {
		got := notifyCustomized(c.n, c.cur, c.setting, c.muted)
		if (got != nil) != c.wanted || got != nil && *got != (MenuTag{"Customized", "informative"}) {
			t.Errorf("case %d: %+v", i, got)
		}
	}
}
