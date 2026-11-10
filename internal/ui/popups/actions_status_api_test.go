package popups

import (
	"encoding/json"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// actCalls records the status modal's Slack calls against fakeslack and
// answers the ones fakeslack doesn't know.
type actCalls struct {
	mu    sync.Mutex
	calls []actCall
	dnd   map[string]any // dnd.info's answer
	sched []map[string]any
	reply string // users.profile.get ooo_message
}

type actCall struct {
	method string
	form   url.Values
}

func actHook(t *testing.T, fs *fakeslack.Server) *actCalls {
	t.Helper()
	c := &actCalls{dnd: map[string]any{"ok": true, "dnd_enabled": false, "snooze_enabled": false}}
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		c.mu.Lock()
		defer c.mu.Unlock()
		switch method {
		case "users.profile.set", "users.customStatus.schedule", "users.customStatus.deleteScheduled", "users.customStatus.list",
			"users.profile.get", "dnd.setSnooze", "dnd.endSnooze", "dnd.info":
			c.calls = append(c.calls, actCall{method, f})
		}
		switch method {
		case "dnd.info":
			return c.dnd, true
		case "users.customStatus.schedule":
			ds, _ := strconv.ParseInt(f.Get("date_scheduled"), 10, 64)
			return map[string]any{"ok": true, "scheduled_status": map[string]any{"id": "CS1", "date_scheduled": ds}}, true
		case "users.customStatus.list":
			return map[string]any{"ok": true, "statuses": c.sched, "custom_sections": []any{}}, true
		case "users.profile.get":
			return map[string]any{"ok": true, "profile": map[string]any{"ooo_message": c.reply}}, true
		case "users.profile.set", "users.customStatus.deleteScheduled", "dnd.setSnooze", "dnd.endSnooze":
			return map[string]any{"ok": true}, true
		}
		return nil, false
	})
	return c
}

func (c *actCalls) of(method string) []url.Values {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []url.Values
	for _, x := range c.calls {
		if x.method == method {
			out = append(out, x.form)
		}
	}
	return out
}

func (c *actCalls) reset() { c.mu.Lock(); c.calls = nil; c.mu.Unlock() }

func actProfileOf(t *testing.T, f url.Values) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal([]byte(f.Get("profile")), &p); err != nil {
		t.Fatalf("profile %q: %v", f.Get("profile"), err)
	}
	return p
}

func TestActProfileSetFields(t *testing.T) {
	h, fs := ccEnv(t)
	c := actHook(t, fs)
	ctx := t.Context()
	sv := actStatusSave{Emoji: "no_entry", Text: "Out of office", Canonical: actOOOCanonical, Exp: 1900000000,
		Duration: "custom", IsDnd: true, OOO: actOOOBlocks("Back Monday"), OOOSet: true}
	h.a.SetExperimentGroup("status_improvements", "off")
	if err := actProfileSet(ctx, h.a, sv); err != nil {
		t.Fatal(err)
	}
	p := actProfileOf(t, c.of("users.profile.set")[0])
	want := map[string]any{"status_emoji": ":no_entry:", "status_text": "Out of office", "status_text_canonical": "Out of office",
		"status_expiration": float64(1900000000), "status_duration": "custom", "ooo_message": actOOOBlocks("Back Monday")}
	for k, v := range want {
		if p[k] != v {
			t.Errorf("%s = %#v, want %#v", k, p[k], v)
		}
	}
	if _, ok := p["status_is_dnd"]; ok {
		t.Error("status_is_dnd sent without status_improvements")
	}
	// with the experiment on it is sent; a text-only status gets
	// :speech_balloon:, an edited preset text drops the canonical, an
	// unedited saved reply is left out
	h.a.SetExperimentGroup("status_improvements", "on")
	c.reset()
	if err := actProfileSet(ctx, h.a, actStatusSave{Text: "Lunch!", Canonical: "Commuting", Duration: "30_minutes", IsDnd: true}); err != nil {
		t.Fatal(err)
	}
	p = actProfileOf(t, c.of("users.profile.set")[0])
	if p["status_is_dnd"] != true || p["status_emoji"] != ":speech_balloon:" || p["status_text_canonical"] != "" {
		t.Errorf("profile %v", p)
	}
	if _, ok := p["ooo_message"]; ok {
		t.Error("ooo_message sent with OOOSet false")
	}
	if _, ok := p["status_expiration"]; ok {
		t.Error("status_expiration sent for 0")
	}
}

func TestActScheduleStatusCall(t *testing.T) {
	h, fs := ccEnv(t)
	c := actHook(t, fs)
	sv := actStatusSave{Emoji: "palm_tree", Text: "Vacationing", Canonical: "Vacationing", Start: 1900000000, Exp: 1900086400,
		IsDnd: true, OOOSet: true}
	if err := actScheduleStatus(t.Context(), h.a, sv); err != nil {
		t.Fatal(err)
	}
	f := c.of("users.customStatus.schedule")[0]
	want := url.Values{"emoji": {":palm_tree:"}, "text": {"Vacationing"}, "text_canonical": {"Vacationing"}, "date_scheduled": {"1900000000"},
		"date_expire": {"1900086400"}, "is_dnd": {"true"}, "ooo_message": {""}, "duration": {"custom"}}
	for k, v := range want {
		if f.Get(k) != v[0] {
			t.Errorf("%s = %q, want %q", k, f.Get(k), v[0])
		}
	}
	if f.Has("custom_status_id") {
		t.Error("custom_status_id on a new one")
	}
	c.reset()
	sv.EditID = "CS9"
	if err := actScheduleStatus(t.Context(), h.a, sv); err != nil {
		t.Fatal(err)
	}
	if got := c.of("users.customStatus.schedule")[0].Get("custom_status_id"); got != "CS9" {
		t.Errorf("edit: custom_status_id %q", got)
	}
}

func TestActListAndDeleteScheduled(t *testing.T) {
	h, fs := ccEnv(t)
	c := actHook(t, fs)
	now := int64(1900000000)
	c.sched = []map[string]any{
		{"id": "R1", "emoji": ":tada:", "text": "recent", "date_created": now - 100}, // a recent one
		{"id": "S2", "emoji": ":bus:", "text": "later", "date_scheduled": now + 7200, "date_expire": now + 9000},
		{"id": "S1", "emoji": ":no_entry:", "text": "Out of office", "text_canonical": "Out of office", "date_scheduled": now + 3600, "date_expire": now + 86400, "is_dnd": true},
		{"id": "S0", "emoji": ":bus:", "text": "started", "date_scheduled": now - 60},
	}
	l, err := actListScheduled(t.Context(), h.a, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 2 || l[0].ID != "S1" || l[1].ID != "S2" || !l[0].IsDnd || l[0].TextCanonical != actOOOCanonical {
		t.Fatalf("list %+v", l)
	}
	if got := c.of("users.customStatus.list")[0].Get("statuses_count_per_section"); got != "0" {
		t.Errorf("statuses_count_per_section %q", got)
	}
	if err := actDeleteScheduled(t.Context(), h.a, "S1"); err != nil {
		t.Fatal(err)
	}
	if got := c.of("users.customStatus.deleteScheduled")[0].Get("custom_status_id"); got != "S1" {
		t.Errorf("delete id %q", got)
	}
}

func TestActClearStatusCall(t *testing.T) {
	h, fs := ccEnv(t)
	c := actHook(t, fs)
	exp := time.Now().Add(time.Hour).Unix()
	c.dnd = map[string]any{"ok": true, "snooze_enabled": true, "snooze_endtime": exp + 20}
	ended, err := actClearStatus(t.Context(), h.a, "", exp)
	if err != nil || !ended {
		t.Fatalf("ended %v err %v", ended, err)
	}
	p := actProfileOf(t, c.of("users.profile.set")[0])
	if v, ok := p["status_text_canonical"]; !ok || v != nil {
		t.Errorf("status_text_canonical %#v (want null)", v)
	}
	if p["status_text"] != "" || p["status_emoji"] != "" || p["status_expiration"] != float64(0) {
		t.Errorf("profile %v", p)
	}
	if len(c.of("dnd.endSnooze")) != 1 {
		t.Error("the matching snooze wasn't ended")
	}
	// a snooze that isn't the status's stays
	c.reset()
	if ended, _ := actClearStatus(t.Context(), h.a, "", exp-3600); ended || len(c.of("dnd.endSnooze")) != 0 {
		t.Error("an unrelated snooze was ended")
	}
}

func TestActApplyDND(t *testing.T) {
	h, fs := ccEnv(t)
	c := actHook(t, fs)
	now := time.Unix(1900000000, 0)
	exp := now.Add(90 * time.Minute).Unix()
	off := core.DNDStatus{}
	on := core.DNDStatus{SnoozeEnabled: true, SnoozeEnd: now.Add(10 * time.Minute).Unix()}
	cases := []struct {
		name         string
		st           core.DNDStatus
		d            actDNDSave
		snooze       string // num_minutes, "" none
		end, stillOn bool
	}{
		{"pause", off, actDNDSave{Pause: true, Exp: exp}, "90", false, true},
		{"pause, no expiry", off, actDNDSave{Pause: true}, "60480", false, true},
		{"no pause, not snoozed", off, actDNDSave{Exp: exp}, "", false, false},
		{"no pause ends a snooze", on, actDNDSave{Exp: exp}, "", true, false},
		{"pause past the snooze extends it", on, actDNDSave{Pause: true, Exp: exp}, "90", false, true},
		{"pause inside the snooze keeps it", on, actDNDSave{Pause: true, Exp: now.Add(5 * time.Minute).Unix()}, "", false, true},
		{"Vacationing pauses by itself", off, actDNDSave{PresetDnd: true, Picked: true, Exp: exp}, "90", false, true},
	}
	for _, tc := range cases {
		c.reset()
		got, err := actApplyDND(t.Context(), h.a, tc.st, tc.d, now)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		sn := c.of("dnd.setSnooze")
		if tc.snooze == "" && len(sn) > 0 || tc.snooze != "" && (len(sn) != 1 || sn[0].Get("num_minutes") != tc.snooze) {
			t.Errorf("%s: setSnooze %v, want %q", tc.name, sn, tc.snooze)
		}
		if e := len(c.of("dnd.endSnooze")) > 0; e != tc.end {
			t.Errorf("%s: endSnooze %v, want %v", tc.name, e, tc.end)
		}
		if got != tc.stillOn {
			t.Errorf("%s: snoozed %v, want %v", tc.name, got, tc.stillOn)
		}
	}
}

func TestActOOOBlocksRoundTrip(t *testing.T) {
	if actOOOBlocks("  ") != "" {
		t.Error("empty reply should clear")
	}
	raw := actOOOBlocks("Back on Monday.")
	if raw != `[{"elements":[{"elements":[{"text":"Back on Monday.","type":"text"}],"type":"rich_text_section"}],"type":"rich_text"}]` {
		t.Errorf("blocks %s", raw)
	}
	if got := actOOOPlain(raw); got != "Back on Monday." {
		t.Errorf("plain %q", got)
	}
	two := `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Away "},{"type":"emoji","name":"palm_tree"}]},` +
		`{"type":"rich_text_section","elements":[{"type":"text","text":"Ask "},{"type":"user","user_id":"U1"}]}]}]`
	if got := actOOOPlain(two); got != "Away :palm_tree:\nAsk <@U1>" {
		t.Errorf("two sections %q", got)
	}
	if actCanonicalToSave("Commuting", "Commuting") != "Commuting" || actCanonicalToSave("Commuting", "Bus") != "" ||
		actCanonicalToSave(actOOOCanonical, "OOO till Mon") != actOOOCanonical {
		t.Error("resolveTextCanonicalToSave")
	}
}

// The modal end to end: Out of office starting later is scheduled with its
// automatic reply; one starting now goes through users.profile.set.
func TestActStatusModalSaves(t *testing.T) {
	h, fs := ccEnv(t)
	c := actHook(t, fs)
	s := actNewStatusState(h, false)
	ccUntil(t, s.modal, "the loads", func() bool { return len(c.of("users.profile.get")) > 0 && len(c.of("users.customStatus.list")) > 0 })
	s.pickPreset("no_entry", "Out of office", actOOOCanonical, actDurCustom)
	if !s.ooo || s.dur != actDurCustom || s.replyText == "" {
		t.Fatalf("ooo %v dur %d reply %q", s.ooo, s.dur, s.replyText)
	}
	// start tomorrow
	start := time.Now().Add(24 * time.Hour).Truncate(time.Minute)
	end := start.Add(48 * time.Hour)
	s.sDay.Day, s.sTime.Mins = dtDay(start), dtMins(start)
	s.eDay.Day, s.eTime.Mins = dtDay(end), dtMins(end)
	s.rangeChanged()
	if !s.canSave() || !s.save() {
		t.Fatal("couldn't save")
	}
	ccUntil(t, s.modal, "the schedule call", func() bool { return len(c.of("users.customStatus.schedule")) > 0 })
	f := c.of("users.customStatus.schedule")[0]
	if f.Get("date_scheduled") != strconv.FormatInt(s.startTime().Unix(), 10) || f.Get("date_expire") != strconv.FormatInt(s.endTime().Unix(), 10) ||
		f.Get("text_canonical") != actOOOCanonical || f.Get("emoji") != ":no_entry:" {
		t.Errorf("schedule form %v", f)
	}
	if got := actOOOPlain(f.Get("ooo_message")); got != actOOOReply(s.endTime(), time.Now()) {
		t.Errorf("ooo_message %q", got)
	}
	ccUntil(t, s.modal, "the toast", func() bool {
		for _, x := range h.toastList() {
			if x == "Status scheduled." {
				return true
			}
		}
		return false
	})
	if len(c.of("users.profile.set")) != 0 {
		t.Error("a scheduled status also went through users.profile.set")
	}

	// starting now: users.profile.set, with an edited reply
	s = actNewStatusState(h, false)
	c.reset()
	s.pickPreset("no_entry", "Out of office", actOOOCanonical, actDurCustom)
	s.reply.SetText("Gone fishing")
	s.replyText = "Gone fishing"
	if !s.save() {
		t.Fatal("couldn't save now")
	}
	ccUntil(t, s.modal, "users.profile.set", func() bool { return len(c.of("users.profile.set")) > 0 })
	p := actProfileOf(t, c.of("users.profile.set")[0])
	if p["ooo_message"] != actOOOBlocks("Gone fishing") || p["status_duration"] != "custom" || p["status_expiration"] != float64(s.endTime().Unix()) {
		t.Errorf("profile %v", p)
	}
	if len(c.of("users.customStatus.schedule")) != 0 {
		t.Error("a status starting now was scheduled")
	}
}

// The Scheduled section loads, its rows edit (custom_status_id) and the
// limit of five blocks a sixth.
func TestActStatusModalScheduledSection(t *testing.T) {
	h, fs := ccEnv(t)
	c := actHook(t, fs)
	now := time.Now().Unix()
	for i := 0; i < actMaxScheduled; i++ {
		c.sched = append(c.sched, map[string]any{"id": "S" + strconv.Itoa(i), "emoji": ":bus:", "text": "Trip " + strconv.Itoa(i),
			"date_scheduled": now + int64(3600*(i+1)), "date_expire": now + int64(3600*(i+2)), "duration": "custom"})
	}
	s := actNewStatusState(h, false)
	ccUntil(t, s.modal, "the scheduled list", func() bool { return len(s.sched) == actMaxScheduled })
	s.pickPreset("bus", "Commuting", "Commuting", actDurCustom)
	start := time.Now().Add(26 * time.Hour)
	s.sDay.Day, s.sTime.Mins = dtDay(start), dtMins(start)
	s.eDay.Day, s.eTime.Mins = dtDay(start.Add(time.Hour)), dtMins(start.Add(time.Hour))
	if s.canSave() {
		t.Error("a sixth scheduled status can be saved")
	}
	s.editScheduled(s.sched[2])
	if s.editID != "S2" || s.saveBtn.Label != "Update" || s.modal.o.TitleFunc() != "Edit status" {
		t.Fatalf("editing: id %q label %q", s.editID, s.saveBtn.Label)
	}
	if !s.canSave() || !s.save() {
		t.Fatal("couldn't update")
	}
	ccUntil(t, s.modal, "the edit call", func() bool { return len(c.of("users.customStatus.schedule")) > 0 })
	if got := c.of("users.customStatus.schedule")[0].Get("custom_status_id"); got != "S2" {
		t.Errorf("custom_status_id %q", got)
	}
	if got := actScheduledRange(now+3600, now+7200, time.Unix(now, 0)); got == "" {
		t.Error("empty range label")
	}
}
