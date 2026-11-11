package popups

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// maUntil drains popups' callbacks until cond holds (10s at most).
func maUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for {
		drain()
		if cond() {
			return
		}
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// maBooted is emBooted on a fake with the workflow fixtures.
func maBooted(t *testing.T) (*tzHost, *fakeslack.Server, *emCalls) {
	t.Helper()
	size := fakeslack.Small
	size.Workflows = true
	fs, err := fakeslack.Start(size)
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

func maForwarding(h *tzHost, on bool) {
	h.a.SetTeamPrefs(map[string]json.RawMessage{"private_message_forwarding": json.RawMessage(strconv.FormatBool(on))})
}

func maShareRow(items []MenuItem) (MenuItem, bool) {
	for _, it := range items {
		if it.Separator {
			break
		}
		if it.Key == "share_message" {
			return it, true
		}
	}
	return MenuItem{}, false
}

// The right-click menu's share row is the toolbar's share action (6bWJ):
// "Share message…" with S, or "Forward message…" with F when the team
// allows private message forwarding; the file wording for a one-file
// message; none for an automated message under
// system_messages_redesign_m1; its id is share_message.
func TestContextMenuShareRowLikeSlack(t *testing.T) {
	at := image.Pt(100, 100)
	m := core.Message{Channel: "C1", TS: "1700000000.000100", User: "UOTHER", Text: "synthetic"}
	bare := &tzHost{th: theme.New(false)}
	if it, ok := maShareRow(NewMessageContextMenu(bare, at, m, InChannel, MessageMenuOpts{}).Items()); !ok || it.Label != "Share message…" || it.Hint != "S" {
		t.Errorf("no forwarding: %+v %v", it, ok)
	}
	h, _, _ := emBooted(t)
	maForwarding(h, true)
	m.Channel = emCh
	if it, ok := maShareRow(NewMessageContextMenu(h, at, m, InChannel, MessageMenuOpts{}).Items()); !ok || it.Label != "Forward message…" || it.Hint != "F" {
		t.Errorf("forwarding: %+v %v", it, ok)
	}
	maForwarding(h, false)
	if it, ok := maShareRow(NewMessageContextMenu(h, at, m, InChannel, MessageMenuOpts{}).Items()); !ok || it.Label != "Share message…" || it.Hint != "S" {
		t.Errorf("forwarding off: %+v %v", it, ok)
	}
	sys := m
	sys.Subtype = "channel_join"
	h.a.SetExperimentGroup("system_messages_redesign_m1", "on")
	if it, ok := maShareRow(NewMessageContextMenu(h, at, sys, InChannel, MessageMenuOpts{}).Items()); ok {
		t.Errorf("an automated message has a share row: %+v", it)
	}
	h.a.SetExperimentGroup("system_messages_redesign_m1", "off")
	if _, ok := maShareRow(NewMessageContextMenu(h, at, sys, InChannel, MessageMenuOpts{}).Items()); !ok {
		t.Error("the redesign off: no share row")
	}
	h.a.ClearExperimentGroup("system_messages_redesign_m1")
	tomb := m
	tomb.Tombstone = true
	if _, ok := maShareRow(NewMessageContextMenu(h, at, tomb, InChannel, MessageMenuOpts{}).Items()); ok {
		t.Error("a tombstone can be shared")
	}
}

// The share dialog's chrome, as MessageShareDialog has it: e3's title by
// the forwarding pref and the channel, the go button "Share"/"Forward",
// Copy Link at the left whose tooltip says what the link is for and then
// "Copied!" until it hides (no toast), the options list's 32px rows
// scrolling past 200px, and the footer under the experiments: Save Draft
// (forwarding_save_as_draft) and the schedule caret joined to Forward
// (schedule_send_forwards).
func TestForwardDialogChromeLikeSlack(t *testing.T) {
	h, _, _ := emBooted(t)
	m := core.Message{Channel: emCh, TS: "1700000000.000100", User: fakeslack.ProfileID, Text: "synthetic"}
	f := newForward(h, m)
	if f.title() != "Share this message" || f.fwd.Label != "Share" {
		t.Errorf("no forwarding: %q / %q", f.title(), f.fwd.Label)
	}
	if !strings.Contains(f.copyTip(), "To share, paste it") {
		t.Errorf("tip %q", f.copyTip())
	}
	maForwarding(h, true)
	f = newForward(h, m)
	if c, _ := h.a.Conv(emCh); c.Kind == "channel" && f.title() != "Forward this message" {
		t.Errorf("public channel: %q", f.title())
	}
	if f.fwd.Label != "Forward" || !strings.Contains(f.copyTip(), "To forward, paste it") {
		t.Errorf("forwarding: %q / %q", f.fwd.Label, f.copyTip())
	}
	dm := newForward(h, core.Message{Channel: "D0FAKEDM99", TS: "1700000000.000200", User: fakeslack.ProfileID})
	if dm.title() != "Forward this private message" {
		t.Errorf("DM: %q", dm.title())
	}
	// Copy Link: the left of the footer, "Copied!" after a click until the
	// tooltip goes, and no toast
	if !f.copy.Left || f.copy.Label != "Copy Link" || f.copy.V2 != nil {
		t.Errorf("copy button %+v", f.copy)
	}
	f.copyLink = true
	var ops op.Ops
	gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Now: time.Now(), Constraints: layout.Exact(image.Pt(472, 600))}
	f.layout(gtx)
	if f.copyTip() != "Copied!" {
		t.Errorf("after a click %q", f.copyTip())
	}
	for _, s := range h.toastList() {
		if strings.Contains(s, "copied") || strings.Contains(s, "Copied") {
			t.Errorf("a toast %q", s)
		}
	}
	f.copy.TipHidden()
	if f.copyTip() == "Copied!" {
		t.Error("Copied! stays after the tooltip hid")
	}
	// the options list: 32px rows, 200px at most
	th := h.Theme()
	for _, n := range []int{1, 4, 6, 7, 30} {
		f.match = f.match[:0]
		for i := 0; i < n && i < len(f.all); i++ {
			f.match = append(f.match, i)
		}
		ops.Reset()
		d := f.suggestions(gtx, th, colorsOf(th))
		want := minInt(len(f.match)*actFwdRowH, actFwdListMax) + 12
		if d.Size.Y != want {
			t.Errorf("%d matches: %dpx, want %d", len(f.match), d.Size.Y, want)
		}
	}
	// the footer by experiment
	labels := func(f *actForward) string {
		var out []string
		for _, b := range f.modal.o.Buttons {
			l := b.Label
			if b == f.caret {
				l = "caret"
				if !b.Joined {
					l += "(apart)"
				}
			}
			out = append(out, l)
		}
		return strings.Join(out, "|")
	}
	h.a.SetExperimentGroup(ExpScheduleSendForwards, "off")
	h.a.SetExperimentGroup(ExpForwardingSaveAsDraft, "off")
	if got := labels(newForward(h, m)); got != "Copy Link|Forward" {
		t.Errorf("both off: %q", got)
	}
	h.a.SetExperimentGroup(ExpScheduleSendForwards, "on")
	if got := labels(newForward(h, m)); got != "Copy Link|Forward|caret" {
		t.Errorf("schedule_send_forwards: %q", got)
	}
	h.a.SetExperimentGroup(ExpForwardingSaveAsDraft, "on")
	g := newForward(h, m)
	if got := labels(g); got != "Copy Link|Save Draft|Forward|caret" {
		t.Errorf("both on: %q", got)
	}
	// the caret is off with Forward (no destination)
	if g.caret.enabled() || g.fwd.enabled() {
		t.Error("Forward and its caret on without a destination")
	}
	g.chips = append(g.chips, &actFwdChip{t: &g.all[0]})
	if !g.caret.enabled() || !g.fwd.enabled() {
		t.Error("Forward and its caret off with a destination")
	}
}

// Save Draft (saveDraft): a draft holding the note and the permalink as a
// link, for the destination, or with none yet (destinations []); the
// dialog closes without a word.
func TestForwardSaveDraftLikeSlack(t *testing.T) {
	h, _, rec := emBooted(t)
	h.a.SetExperimentGroup(ExpForwardingSaveAsDraft, "on")
	m := core.Message{Channel: emCh, TS: "1700000000.000100", User: fakeslack.ProfileID, Text: "synthetic"}
	f := newForward(h, m)
	f.saveDraft()
	got := rec.wait(t, "drafts.create", map[string]string{"destinations": "[]"}, 10*time.Second)
	if !strings.Contains(got.Get("blocks"), `"type":"link"`) || !strings.Contains(got.Get("blocks"), f.link) {
		t.Errorf("blocks %s", got.Get("blocks"))
	}
	f = newForward(h, m)
	f.note.SetText("synthetic note")
	f.chips = append(f.chips, &actFwdChip{t: &f.all[0]})
	f.saveDraft()
	dest, _ := json.Marshal([]map[string]string{{"channel_id": f.all[0].id}})
	got = rec.wait(t, "drafts.create", map[string]string{"destinations": string(dest)}, 10*time.Second)
	if !strings.Contains(got.Get("blocks"), "synthetic note\\n") {
		t.Errorf("the note isn't before the link: %s", got.Get("blocks"))
	}
	for _, s := range h.toastList() {
		if strings.Contains(strings.ToLower(s), "draft") {
			t.Errorf("Slack says nothing, Clack toasted %q", s)
		}
	}
}

// The schedule caret: ScheduleSendMenu for the destination; a preset
// schedules the forward as a draft (drafts.create, then date_scheduled)
// and says "Message scheduled"; Custom time saves the draft first, opens
// the time dialog over the share dialog under updated_scheduled_sends_desktop
// (the share dialog stays until the time is picked) or after it closes,
// and schedules that draft.
func TestForwardScheduleLikeSlack(t *testing.T) {
	h, _, rec := emBooted(t)
	h.a.SetExperimentGroup(ExpScheduleSendForwards, "on")
	prevMenu, prevDialog := ScheduleSendMenu, OpenScheduleDialog
	t.Cleanup(func() { ScheduleSendMenu, OpenScheduleDialog = prevMenu, prevDialog })
	m := core.Message{Channel: emCh, TS: "1700000000.000100", User: fakeslack.ProfileID, Text: "synthetic"}

	at := time.Now().Add(3 * time.Hour).Truncate(time.Minute)
	var menuCh string
	ScheduleSendMenu = func(_ Host, _ image.Rectangle, ch string, pick func(time.Time), _ func()) {
		menuCh = ch
		pick(at)
	}
	f := newForward(h, m)
	f.chips = append(f.chips, &actFwdChip{t: &f.all[0]})
	f.caret.Do()
	if menuCh != f.all[0].id {
		t.Errorf("the menu is for %q", menuCh)
	}
	if !f.modal.done {
		t.Error("the share dialog stays after a preset")
	}
	rec.wait(t, "drafts.update", map[string]string{"date_scheduled": strconv.FormatInt(at.Unix(), 10)}, 10*time.Second)
	maUntil(t, "the toast", func() bool {
		for _, s := range h.toastList() {
			if s == "Message scheduled" {
				return true
			}
		}
		return false
	})

	// Custom time, stacked
	h.a.SetExperimentGroup("updated_scheduled_sends_desktop", "on")
	var pickCustom func(time.Time)
	ScheduleSendMenu = func(_ Host, _ image.Rectangle, _ string, _ func(time.Time), custom func()) { custom() }
	OpenScheduleDialog = func(_ Host, _ string, pick func(time.Time)) { pickCustom = pick }
	g := newForward(h, m)
	g.note.SetText("custom synthetic")
	g.chips = append(g.chips, &actFwdChip{t: &g.all[0]})
	g.caret.Do()
	maUntil(t, "the custom time dialog", func() bool { return pickCustom != nil })
	if g.modal.done {
		t.Error("stacked: the share dialog closed before the time was picked")
	}
	later := time.Now().Add(26 * time.Hour).Truncate(time.Minute)
	pickCustom(later)
	if !g.modal.done {
		t.Error("the share dialog stays after Schedule Message (closeAll)")
	}
	rec.wait(t, "drafts.update", map[string]string{"date_scheduled": strconv.FormatInt(later.Unix(), 10)}, 10*time.Second)

	// not stacked: the share dialog goes first
	h.a.SetExperimentGroup("updated_scheduled_sends_desktop", "off")
	pickCustom = nil
	k := newForward(h, m)
	k.chips = append(k.chips, &actFwdChip{t: &k.all[0]})
	k.caret.Do()
	if !k.modal.done {
		t.Error("not stacked: the share dialog stays open")
	}
	maUntil(t, "the custom time dialog", func() bool { return pickCustom != nil })
}

// maFlagEnv is a booted fake whose org has content review on.
func maFlagEnv(t *testing.T) (*tzHost, *fakeslack.Server, *emCalls) {
	t.Helper()
	h, fs, rec := emBooted(t)
	fs.SetEnterpriseLockedPrefs(map[string]any{"content_review_enabled": true})
	if err := h.a.LoadEnterprisePrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	// ed's cross-org rule, pinned: the source team's org (not the author's)
	h.a.SetExperimentGroup(ExpFlagXorgRefinement, "control")
	h.a.SetExperimentGroup(ExpFlagXorgExternalTeamCheck, "off")
	return h, fs, rec
}

// ed, "Flag this message…": someone else's message on an Enterprise team
// whose org has content review, first in the last group, red when
// highlighted, in Report to Slack's place; a name, topic or description
// message that still matches the channel flags it instead.
func TestMessageMenuFlagRowLikeSlack(t *testing.T) {
	h, fs, _ := maFlagEnv(t)
	h.a.SetContentReporting(true)
	other := core.Message{Channel: emCh, TS: "1700000000.000700", User: fakeslack.ProfileID, Text: "synthetic"}
	items := actMessageItems(h, other, MessageMenuOpts{})
	l := emLabels(items)
	if len(l) < 2 || l[len(l)-1] != "Flag this message…" || l[len(l)-2] != "-" || emHas(items, "Report to Slack") {
		t.Fatalf("others' message: %q", l)
	}
	flag := emItem(t, items, "Flag this message…")
	if flag.Key != "flag_message" || !flag.DangerHighlight || flag.Danger || flag.Icon != "warning" {
		t.Errorf("row %+v", flag)
	}
	own := other
	own.User = fakeslack.SelfID
	if emHas(actMessageItems(h, own, MessageMenuOpts{}), "Flag this message…") {
		t.Error("your own message can be flagged")
	}
	tomb := other
	tomb.Tombstone = true
	if emHas(actMessageItems(h, tomb, MessageMenuOpts{}), "Flag this message…") {
		t.Error("a tombstone can be flagged")
	}
	// a topic message that still matches the channel's topic
	h.a.S.PutConv(slack.Conversation{ID: "C0FLAGTOP1", Name: "flags", IsChannel: true, IsMember: true, Topic: &slack.Purpose{Value: "synthetic topic"}})
	topic := core.Message{Channel: "C0FLAGTOP1", TS: "1700000000.000800", User: fakeslack.ProfileID, Subtype: "channel_topic", Topic: "synthetic topic"}
	if !emHas(actMessageItems(h, topic, MessageMenuOpts{}), "Flag channel topic") {
		t.Errorf("topic message: %q", emLabels(actMessageItems(h, topic, MessageMenuOpts{})))
	}
	topic.Topic = "an older synthetic topic"
	if !emHas(actMessageItems(h, topic, MessageMenuOpts{}), "Flag this message…") {
		t.Errorf("stale topic message: %q", emLabels(actMessageItems(h, topic, MessageMenuOpts{})))
	}
	// flag_xorg_refinement with the external team check: the author's org
	// decides; someone from another org has no flag row (no channel team
	// allows_flag_content for them that Clack knows of)
	h.a.SetExperimentGroup(ExpFlagXorgRefinement, "treatment")
	h.a.SetExperimentGroup(ExpFlagXorgExternalTeamCheck, "on")
	h.a.S.PutUsers([]slack.User{
		{ID: "U0FLAGORG1", TeamID: fakeslack.TeamID, Name: "inorg", EnterpriseUser: &slack.EnterpriseUser{EnterpriseID: fakeslack.EnterpriseID}},
		{ID: "U0FLAGEXT1", TeamID: "T0OTHERORG", Name: "outside", EnterpriseUser: &slack.EnterpriseUser{EnterpriseID: "E0OTHERORG"}},
	})
	inOrg := other
	inOrg.User = "U0FLAGORG1"
	if !emHas(actMessageItems(h, inOrg, MessageMenuOpts{}), "Flag this message…") {
		t.Errorf("refinement, same org: %q", emLabels(actMessageItems(h, inOrg, MessageMenuOpts{})))
	}
	ext := other
	ext.User = "U0FLAGEXT1"
	if emHas(actMessageItems(h, ext, MessageMenuOpts{}), "Flag this message…") {
		t.Error("refinement, another org: a flag row")
	}
	h.a.SetExperimentGroup(ExpFlagXorgRefinement, "control")
	h.a.SetExperimentGroup(ExpFlagXorgExternalTeamCheck, "off")

	// no content review: Report to Slack is back
	fs.SetEnterpriseLockedPrefs(map[string]any{})
	if err := h.a.LoadEnterprisePrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	items = actMessageItems(h, other, MessageMenuOpts{})
	if emHas(items, "Flag this message…") || !emHas(items, "Report to Slack") {
		t.Errorf("without content review: %q", emLabels(items))
	}
}

// FlagMessageDialog: Submit needs a reason, opens the speed bump, whose
// Submit calls moderation.flags.create; then "Message has been flagged"
// with the other steps and no footer. A failure shows Slack's alert. A
// channel topic flag sends channel_details and closes once done.
func TestFlagMessageDialogLikeSlack(t *testing.T) {
	h, fs, rec := maFlagEnv(t)
	other := core.Message{Channel: emCh, TS: "1700000000.000700", User: fakeslack.ProfileID, Text: "synthetic"}
	f := openFlagDialog(h, other)
	if f == nil {
		t.Fatal("no dialog with content review on")
	}
	if f.title() != "Flag this message" || !strings.HasPrefix(f.info(), "Report this message to your organization’s Content Admins.") ||
		f.placeholder() != "I’d like you to review this inappropriate message because…" {
		t.Errorf("texts %q / %q / %q", f.title(), f.info(), f.placeholder())
	}
	if f.submit.enabled() {
		t.Error("Submit without a reason")
	}
	f.reason.SetText("   ")
	if f.submit.enabled() {
		t.Error("Submit with a blank reason")
	}
	f.reason.SetText(strings.Repeat("x", flagMaxChars+1))
	if f.submit.enabled() {
		t.Error("Submit past 560 characters")
	}
	f.reason.SetText("a synthetic reason")
	if !f.submit.enabled() {
		t.Fatal("Submit off with a reason")
	}
	f.submit.Do()
	if !f.bump {
		t.Fatal("no speed bump")
	}
	if b := f.bumpText(); len(b) != 2 || !strings.HasPrefix(b[1], "Are you sure you want to flag this message") {
		t.Errorf("speed bump %q", b)
	}
	f.send()
	rec.wait(t, "moderation.flags.create", map[string]string{"channel_id": emCh, "item_id": other.TS, "type": "message", "reason": "a synthetic reason"}, 10*time.Second)
	maUntil(t, "the submitted state", func() bool { return f.done })
	if f.title() != "Message has been flagged" || len(f.modal.o.Buttons) != 0 || f.modal.done {
		t.Errorf("submitted: %q, %d buttons, closed %v", f.title(), len(f.modal.o.Buttons), f.modal.done)
	}
	if len(fs.FlagCalls()) != 1 {
		t.Errorf("%d flag calls", len(fs.FlagCalls()))
	}

	// a failure: the alert, still the form
	fs.SetHook(func(method string, form url.Values) (any, bool) {
		if method == "moderation.flags.create" {
			return map[string]any{"ok": false, "error": "internal_error"}, true
		}
		return nil, false
	})
	g := openFlagDialog(h, other)
	g.reason.SetText("another synthetic reason")
	g.submit.Do()
	g.send()
	maUntil(t, "the failure", func() bool { return g.failed })
	if g.done || g.sending || len(g.modal.o.Buttons) == 0 {
		t.Errorf("failed: done %v, sending %v", g.done, g.sending)
	}
	fs.SetHook(emHook(t, rec))

	// a channel topic: channel_details, and the dialog closes
	h.a.S.PutConv(slack.Conversation{ID: "C0FLAGTOP2", Name: "flags-two", IsChannel: true, IsMember: true, Topic: &slack.Purpose{Value: "synthetic topic"}})
	topic := core.Message{Channel: "C0FLAGTOP2", TS: "1700000000.000900", User: fakeslack.ProfileID, Subtype: "channel_topic", Topic: "synthetic topic"}
	k := openFlagDialog(h, topic)
	if k.kind != "channel_topic" || k.title() != "Flag channel topic" || k.placeholder() != "Please provide details" || k.channelContent() != "synthetic topic" {
		t.Errorf("topic dialog: %q %q %q %q", k.kind, k.title(), k.placeholder(), k.channelContent())
	}
	k.reason.SetText("a synthetic topic reason")
	k.submit.Do()
	k.send()
	rec.wait(t, "moderation.flags.create", map[string]string{"channel_id": "C0FLAGTOP2", "type": "channel_details", "channel_details_flagged": `["channel_topic"]`}, 10*time.Second)
	maUntil(t, "the topic dialog to close", func() bool { return k.modal.done })
}

// The workflow rows: "View workflow details" (t7) for a workflow bot's
// message with a trigger_id opens the details view; "Copy workflow ID"
// (q) for a trigger-only message fetches the trigger's preview as the
// menu opens and copies its workflow.
func TestMessageMenuWorkflowRowsLikeSlack(t *testing.T) {
	h, _, rec := maBooted(t)
	h.a.S.PutUsers([]slack.User{{ID: fakeslack.IdentityWorkflowID, TeamID: fakeslack.TeamID, Name: "standup",
		RealName: "Standup Workflow", IsBot: true, IsWorkflowBot: true}})
	var opened [2]string
	prev := OpenWorkflowDetails
	OpenWorkflowDetails = func(tr, ch string) { opened = [2]string{tr, ch} }
	t.Cleanup(func() { OpenWorkflowDetails = prev })
	m := core.Message{Channel: emCh, TS: "1700000000.000600", User: fakeslack.IdentityWorkflowID, TriggerID: fakeslack.TriggerDeploy}
	items := actMessageItems(h, m, MessageMenuOpts{})
	l := strings.Join(emLabels(items), "|")
	if !strings.Contains(l, "-|View workflow details|Copy workflow ID") {
		t.Fatalf("rows %q", l)
	}
	emItem(t, items, "View workflow details").Do()
	if opened != [2]string{fakeslack.TriggerDeploy, emCh} {
		t.Errorf("opened %v", opened)
	}
	rec.wait(t, "workflows.triggers.preview", map[string]string{"trigger_ids": fakeslack.TriggerDeploy}, 10*time.Second)
	cp := emItem(t, items, "Copy workflow ID")
	maUntil(t, "the preview", func() bool { return emClip(cp) == "Wf0FAKE01" })
	// a person's message has neither
	person := m
	person.User = fakeslack.ProfileID
	if p := actMessageItems(h, person, MessageMenuOpts{}); emHas(p, "View workflow details") || emHas(p, "Copy workflow ID") {
		t.Errorf("a person's message: %q", emLabels(p))
	}
	// a workflow_id copies itself, no preview needed
	byID := m
	byID.TriggerID, byID.WorkflowID = "", "Wf0FAKE77"
	p := actMessageItems(h, byID, MessageMenuOpts{})
	if emHas(p, "View workflow details") || emClip(emItem(t, p, "Copy workflow ID")) != "Wf0FAKE77" {
		t.Errorf("workflow_id only: %q", emLabels(p))
	}
}

var _ = errors.New
