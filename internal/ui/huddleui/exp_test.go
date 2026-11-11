package huddleui

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/huddle/avperm"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The huddle experiments and audit rows in the UI, against fakeslack and
// the fake Chime only: every test sets the experiment's groups itself
// (as the Experiments plugins can) and checks both sides of each check.

// ovHost records the overlays (modals) the controller opens.
type ovHost struct {
	*notifyRec
	omu sync.Mutex
	ovs []popups.Overlay
}

func (h *ovHost) ShowOverlay(o popups.Overlay) {
	h.omu.Lock()
	h.ovs = append(h.ovs, o)
	h.omu.Unlock()
}

// modal is the newest overlay that's still open, as a Modal.
func (h *ovHost) modal() *popups.Modal {
	h.omu.Lock()
	defer h.omu.Unlock()
	for i := len(h.ovs) - 1; i >= 0; i-- {
		if m, ok := h.ovs[i].(*popups.Modal); ok && !m.Dismissed() {
			return m
		}
	}
	return nil
}

type expEnv struct {
	*inviteEnv
	oh *ovHost
}

func newExpEnv(t *testing.T, prefs map[string]any) *expEnv {
	t.Helper()
	e := newInviteEnv(t, prefs)
	oh := &ovHost{notifyRec: e.h}
	e.c.h = oh
	e.c.Client = expCaller{chime: e.fk.Caller(), slack: e.app.C}
	old := loadMusic // Test Speaker and the lounge music: no CDN in tests
	loadMusic = func(string) ([]int16, error) { return make([]int16, 960), nil }
	t.Cleanup(func() { loadMusic = old })
	return &expEnv{inviteEnv: e, oh: oh}
}

// expCaller sends the huddle's media calls to the fake Chime (rooms.join,
// the transcription, the topic, Invite people) and the rest to fakeslack.
type expCaller struct{ chime, slack huddle.Caller }

func (c expCaller) Call(ctx context.Context, method string, p url.Values, out any) error {
	switch method {
	case "rooms.join", "rooms.startTranscription", "rooms.stopTranscription", "screenhero.rooms.rename", "rooms.notifyMember":
		return c.chime.Call(ctx, method, p, out)
	}
	return c.slack.Call(ctx, method, p, out)
}

func (e *expEnv) set(name, group string) { e.app.SetExperimentGroup(name, group) }

// joinHuddle joins the fake in the test channel and waits for the media.
func (e *expEnv) joinHuddle() *huddle.Session {
	e.t.Helper()
	e.c.Start(hcChannel)
	var s *huddle.Session
	waitFor(e.t, "connected", func() bool {
		s = e.c.Session()
		return s != nil && s.State().Phase == huddle.Connected
	})
	return s
}

// ---- huddles_alone_timeout ----

// getHuddleAloneTimeoutStep, case by case.
func TestAloneTimeoutStep(t *testing.T) {
	t0 := time.Unix(1700000000, 0)
	th, resp := 10*time.Minute, time.Minute
	step := func(cur aloneState, alone, shown bool, win string, now time.Time) (aloneStepKind, aloneState) {
		return aloneTimeoutStep(cur, alone, shown, win, now, th, resp)
	}
	if k, _ := step(aloneState{}, false, false, "main", t0); k != aloneNone {
		t.Fatalf("not alone, nothing: %v", k)
	}
	k, st := step(aloneState{}, true, false, "main", t0)
	if k != aloneUpdate || !st.since.Equal(t0) {
		t.Fatalf("alone starts the clock: %v %+v", k, st)
	}
	if k, _ := step(st, true, false, "main", t0.Add(9*time.Minute)); k != aloneNone {
		t.Fatalf("9 minutes: %v", k)
	}
	k, st = step(st, true, false, "main", t0.Add(10*time.Minute))
	if k != alonePrompt || !st.deadline.Equal(t0.Add(11*time.Minute)) || st.promptWin != "main" {
		t.Fatalf("10 minutes prompts with 60 s: %v %+v", k, st)
	}
	if k, _ := step(st, true, true, "main", t0.Add(10*time.Minute+30*time.Second)); k != aloneNone {
		t.Fatalf("waiting for the answer: %v", k)
	}
	if k, _ := step(st, true, false, "huddle", t0.Add(10*time.Minute+30*time.Second)); k != alonePrompt {
		t.Fatalf("the huddle window opened: prompt there: %v", k)
	}
	if k, _ := step(st, true, true, "main", t0.Add(11*time.Minute)); k != aloneLeave {
		t.Fatalf("deadline with the prompt up leaves: %v", k)
	}
	if k, _ := step(st, true, false, "main", t0.Add(11*time.Minute)); k != alonePrompt {
		t.Fatalf("deadline with no prompt prompts again: %v", k)
	}
	if k, st2 := step(st, false, true, "main", t0.Add(10*time.Minute+5*time.Second)); k != aloneUpdate || !st2.deadline.IsZero() || !st2.since.IsZero() {
		t.Fatalf("someone joined: reset: %v %+v", k, st2)
	}
	if got := stillHereText(1); !strings.Contains(got, "in 1 second unless") {
		t.Fatal(got)
	}
	if got := stillHereText(45); !strings.Contains(got, "in 45 seconds unless") {
		t.Fatal(got)
	}
	if th, re := aloneThresholds(true); th != 30*time.Second || re != 15*time.Second {
		t.Fatalf("shortened: %v %v", th, re)
	}
}

func shortAlone(t *testing.T) {
	oldEvery, oldTh := aloneEvery, aloneThresholds
	aloneEvery = 40 * time.Millisecond
	aloneThresholds = func(bool) (time.Duration, time.Duration) { return 300 * time.Millisecond, 600 * time.Millisecond }
	t.Cleanup(func() { aloneEvery, aloneThresholds = oldEvery, oldTh })
}

// Alone in the huddle: "Are you still there?", and with no answer you
// leave; with the experiment off nothing asks.
func TestAloneTimeoutLeaves(t *testing.T) {
	shortAlone(t)
	e := newExpEnv(t, nil)
	e.set(ExpAloneTimeout, "on")
	e.fk.SoloRoom()
	s := e.joinHuddle()
	waitFor(t, "the prompt", func() bool { return e.oh.modal() != nil })
	if n := remotePeers(s.State()); n != 0 {
		t.Fatalf("remote peers %d", n)
	}
	waitFor(t, "leaving at the deadline", func() bool { return e.c.Session() == nil })
}

func TestAloneTimeoutStillHere(t *testing.T) {
	shortAlone(t)
	e := newExpEnv(t, nil)
	e.set(ExpAloneTimeout, "on")
	e.fk.SoloRoom()
	s := e.joinHuddle()
	var m *popups.Modal
	waitFor(t, "the prompt", func() bool { m = e.oh.modal(); return m != nil })
	m.Dismiss() // closing it is "I’m Still Here"
	e.c.alone.mu.Lock()
	st := e.c.alone.st
	e.c.alone.mu.Unlock()
	if st.since.IsZero() || !st.deadline.IsZero() {
		t.Fatalf("still here: %+v", st)
	}
	if e.c.Session() != s {
		t.Fatal("left after I’m Still Here")
	}
	s.Leave()
}

func TestAloneTimeoutOff(t *testing.T) {
	shortAlone(t)
	e := newExpEnv(t, nil)
	e.set(ExpAloneTimeout, "off")
	e.fk.SoloRoom()
	s := e.joinHuddle()
	time.Sleep(1200 * time.Millisecond)
	if e.oh.modal() != nil || e.c.Session() != s {
		t.Fatal("the alone timeout ran with the experiment off")
	}
	s.Leave()
}

// ---- huddles_av_button_permission_handling ----

func TestMicPermissionHandling(t *testing.T) {
	e := newExpEnv(t, nil)
	denied := huddle.State{Mic: huddle.MicDenied}
	e.set(ExpAVButtonPermissions, "on")
	ctls := e.c.micCtls(denied, false)
	if m := ctls[0]; !m.warn || !m.dim || m.tip != "Microphone permission denied" || m.icon != "mic-off" {
		t.Fatalf("denied, on: %+v", m)
	}
	if ctls[1].id != "micmenu" || ctls[1].tip != "Audio settings" || ctls[1].seg != segCaret {
		t.Fatalf("the caret: %+v", ctls[1])
	}
	e.c.micClick(nil, denied, e.oh)
	m := e.oh.modal()
	if m == nil {
		t.Fatal("no DeviceWarningModal")
	}
	m.Dismiss()
	e.set(ExpAVButtonPermissions, "off")
	if m := e.c.micCtls(denied, false)[0]; m.warn || m.dim || m.tip != "Mute mic" {
		t.Fatalf("denied, off (the permission reads granted): %+v", m)
	}
	s := e.joinHuddle()
	e.c.micClick(s, s.State(), e.oh)
	if !s.State().Muted || e.oh.modal() != nil {
		t.Fatal("off: the click didn't toggle the mute")
	}
	if c := e.c.micCtls(s.State(), true); c[0].icon != "mic-off" || c[0].tip != "Unmute mic" || c[1].icon != "caret-down" || !c[1].on {
		t.Fatalf("muted with the menu open: %+v", c)
	}
	s.Leave()
}

// ---- the Audio settings menu (mic and speaker switching) ----

func TestAudioSettingsMenu(t *testing.T) {
	old := listAudioDevices
	listAudioDevices = func(capture bool) ([]huddle.AudioDevice, error) {
		if capture {
			return []huddle.AudioDevice{{ID: "m1", Name: "Built-in Microphone", Default: true}, {ID: "m2", Name: "USB Mic"}}, nil
		}
		return []huddle.AudioDevice{{ID: "o1", Name: "Speakers", Default: true}, {ID: "o2", Name: "Headphones"}}, nil
	}
	defer func() { listAudioDevices = old; huddle.SelectAudioDevices("", "") }()
	e := newExpEnv(t, nil)
	e.set(ExpNoiseSuppAwareness, "on")
	s := e.joinHuddle()
	items := e.c.audioMenu(s)
	want := "# Microphone | Built-in Microphone | USB Mic | --- | # Speaker | Speakers | Headphones | --- | Test Speaker | --- | Enable noise suppression | --- | Audio & video preferences"
	if got := labels(items); got != want {
		t.Fatalf("menu\n got %s\nwant %s", got, want)
	}
	if !items[1].Radio || !items[1].Checked || items[2].Checked {
		t.Fatalf("the default mic is the checked one: %+v %+v", items[1], items[2])
	}
	items[2].Do() // USB Mic
	if mic, _ := huddle.SelectedAudioDevices(); mic != "m2" {
		t.Fatalf("picked %q", mic)
	}
	items = e.c.audioMenu(s)
	if items[1].Checked || !items[2].Checked {
		t.Fatal("the pick isn't the checked one")
	}
	items[6].Do() // Headphones
	if _, out := huddle.SelectedAudioDevices(); out != "o2" {
		t.Fatalf("picked speaker %q", out)
	}
	e.set(ExpNoiseSuppAwareness, "off")
	if got := labels(e.c.audioMenu(s)); strings.Contains(got, "noise suppression") {
		t.Fatalf("NS row without the experiment: %s", got)
	}
	s.Leave()
}

// ---- huddles_switch_microphone_helper ----

func TestNoAudioDetected(t *testing.T) {
	oldEvery, oldList := micCheckEvery, listAudioDevices
	micCheckEvery = 10 * time.Millisecond
	listAudioDevices = func(capture bool) ([]huddle.AudioDevice, error) {
		return []huddle.AudioDevice{{ID: "a"}, {ID: "b"}}, nil
	}
	defer func() { micCheckEvery, listAudioDevices = oldEvery, oldList }()
	for _, on := range []bool{true, false} {
		e := newExpEnv(t, nil)
		if on {
			e.set(ExpSwitchMicHelper, "on")
		} else {
			e.set(ExpSwitchMicHelper, "off")
		}
		// a microphone that opens but sends only silence
		e.c.Tweak = func(o *huddle.Options) {
			o.Device = nil
			o.MicAccess = func() <-chan avperm.Status {
				ch := make(chan avperm.Status, 1)
				ch <- avperm.Authorized
				return ch
			}
			o.OpenAudio = func(func(string, ...any)) (huddle.Device, error) { return huddle.NewToneDevice(0), nil }
		}
		s := e.joinHuddle()
		if on {
			waitFor(t, "NoAudioDetected", e.c.noAudioAlert)
			e.c.closeNoAudio()
			if e.c.noAudioAlert() {
				t.Fatal("closing kept it")
			}
		} else {
			time.Sleep(500 * time.Millisecond)
			if e.c.noAudioAlert() {
				t.Fatal("NoAudioDetected with the experiment off")
			}
		}
		s.Leave()
	}
}

// ---- SpeakingWhenMuted: "You’re muted." ----

func TestMutedBanner(t *testing.T) {
	oldEvery, oldFor := micCheckEvery, mutedBannerFor
	micCheckEvery, mutedBannerFor = 10*time.Millisecond, 400*time.Millisecond
	defer func() { micCheckEvery, mutedBannerFor = oldEvery, oldFor }()
	e := newExpEnv(t, map[string]any{"huddles_mute_by_default": true})
	e.c.Tweak = func(o *huddle.Options) {
		o.Device = nil
		o.MicAccess = func() <-chan avperm.Status {
			ch := make(chan avperm.Status, 1)
			ch <- avperm.Authorized
			return ch
		}
		o.OpenAudio = func(func(string, ...any)) (huddle.Device, error) { return huddle.NewToneDevice(440), nil }
	}
	s := e.joinHuddle()
	waitFor(t, "You’re muted.", func() bool { return e.c.mutedBanner(s) })
	waitFor(t, "its 60 s", func() bool { return !e.c.mutedBanner(s) && !s.SpeakingWhileMuted() })
	// canShowMutedBanner is false now: talking muted again shows nothing
	time.Sleep(1500 * time.Millisecond)
	if e.c.mutedBanner(s) {
		t.Fatal("the banner came back after its timeout")
	}
	s.Leave()
}

// ---- captions: startEventLog / stopEventLog ----

func TestClosedCaptionsTranscription(t *testing.T) {
	e := newExpEnv(t, nil)
	s := e.joinHuddle()
	e.c.ToggleClosedCaptions(s)
	waitFor(t, "rooms.startTranscription and the start event", func() bool {
		c := s.State().Captions
		return s.State().CaptionsOn && len(c) > 0 && c[0].Start && len(fakeCalls(e.fk, "rooms.startTranscription")) == 1
	})
	if ev := captionEvents(s.State().Captions); len(ev) == 0 || !ev[0].begin || ev[0].text != "Captions have been turned on." {
		t.Fatalf("events %+v", ev)
	}
	e.c.ToggleClosedCaptions(s)
	waitFor(t, "rooms.stopTranscription", func() bool {
		return !s.State().CaptionsOn && len(fakeCalls(e.fk, "rooms.stopTranscription")) == 1
	})
	// a team that doesn't allow transcriptions: the toggle does nothing
	e.app.SetTeamPrefs(map[string]json.RawMessage{"allow_huddles_transcriptions": json.RawMessage(`false`)})
	e.c.ToggleClosedCaptions(s)
	time.Sleep(200 * time.Millisecond)
	if s.State().CaptionsOn || len(fakeCalls(e.fk, "rooms.startTranscription")) != 1 {
		t.Fatal("captions turned on without allow_huddles_transcriptions")
	}
	s.Leave()
}

// huddles_cc_by_default turns them on as your audio connects.
func TestCaptionsByDefault(t *testing.T) {
	e := newExpEnv(t, map[string]any{"huddles_cc_by_default": true})
	s := e.joinHuddle()
	waitFor(t, "the start", func() bool { return len(fakeCalls(e.fk, "rooms.startTranscription")) == 1 })
	waitFor(t, "the event log on", func() bool { on, loading := s.EventLog(); return on && !loading })
	if !s.State().CaptionsOn {
		t.Fatal("not shown")
	}
	e.c.Leave() // stopHuddle runs stopEventLog first
	waitFor(t, "stopHuddle's stopEventLog", func() bool { return len(fakeCalls(e.fk, "rooms.stopTranscription")) == 1 })
}

func fakeCalls(fk *huddle.Fake, method string) []huddle.FakeCall {
	var out []huddle.FakeCall
	for _, c := range fk.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// ---- topics ----

func TestHuddleTopic(t *testing.T) {
	e := newExpEnv(t, nil)
	s := e.joinHuddle()
	it := e.c.topicItem(s, s.State(), e.oh)
	if it.Label != "Add topic" {
		t.Fatalf("label %q", it.Label)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Rename(ctx, "Planning the launch"); err != nil {
		t.Fatal(err)
	}
	r := fakeCalls(e.fk, "screenhero.rooms.rename")
	if len(r) != 1 || r[0].Params.Get("room") != huddle.FakeRoomID || r[0].Params.Get("room_name") != "Planning the launch" {
		t.Fatalf("rename %+v", r)
	}
	st := s.State()
	if title, sub := e.c.windowTitle(st); title != "Planning the launch" || sub != "2 people" {
		t.Fatalf("title %q / %q", title, sub)
	}
	if it := e.c.topicItem(s, st, e.oh); it.Label != "Edit topic" {
		t.Fatalf("label %q", it.Label)
	}
	it.Do()
	m := e.oh.modal()
	if m == nil {
		t.Fatal("no topic dialog")
	}
	m.Dismiss()
	s.Leave()
}

// ---- huddles_preview_v2 ----

func TestJoinPreviewNeedsPreviewV2(t *testing.T) {
	e := newShotEnv(t, false)
	setPrefs(e.h.app, map[string]any{"huddles_show_join_preview": true})
	e.h.app.SetExperimentGroup(ExpPreviewV2, "on")
	if !e.c.shouldPreview(hcChannel, EntryHeaderButton) {
		t.Fatal("no preview with the pref and the experiment on")
	}
	e.h.app.SetExperimentGroup(ExpPreviewV2, "off")
	if e.c.shouldPreview(hcChannel, EntryHeaderButton) {
		t.Fatal("a preview without huddles_preview_v2")
	}
	// no microphone or camera permission forces it (with the pref off)
	e.h.app.SetExperimentGroup(ExpPreviewV2, "on")
	setPrefs(e.h.app, map[string]any{"huddles_show_join_preview": false})
	old := permissionsGranted
	permissionsGranted = func() bool { return false }
	defer func() { permissionsGranted = old }()
	if !e.c.shouldPreview(hcChannel, EntryHeaderButton) {
		t.Fatal("no forced preview without device permissions")
	}
	e.h.app.SetExperimentGroup(ExpPreviewV2, "off")
	if e.c.shouldPreview(hcChannel, EntryHeaderButton) {
		t.Fatal("forced preview without huddles_preview_v2")
	}
}

// ---- huddle_join_from_preview ----

func TestJoinFromPreview(t *testing.T) {
	e := newExpEnv(t, nil)
	const pv = "C0PREVIEW1"
	e.app.S.PutExtConv(slack.Conversation{ID: pv, Name: "previewing", IsChannel: true, NumMembers: 40}) // known, not joined
	if cv, ok := e.app.Conv(pv); !ok || !cv.Preview {
		t.Fatalf("%s isn't a preview: %+v", pv, cv)
	}
	e.set(ExpJoinFromPreview, "on")
	if !e.c.SupportsHuddles(pv) {
		t.Fatal("on: no huddles in a previewed channel")
	}
	e.set(ExpJoinFromPreview, "off")
	if e.c.SupportsHuddles(pv) {
		t.Fatal("off: huddles in a previewed channel")
	}
	if !e.c.SupportsHuddles(hcChannel) {
		t.Fatal("off: a member channel lost huddles")
	}
	e.c.Begin(pv, EntryHeaderButton)
	time.Sleep(200 * time.Millisecond)
	if e.c.Active() {
		t.Fatal("off: Begin started a huddle in a previewed channel")
	}
}

// ---- huddles_missed_notification ----

func TestMissedNotificationExperiment(t *testing.T) {
	for _, on := range []bool{true, false} {
		e := newExpEnv(t, nil)
		e.c.MissedAfter = 100 * time.Millisecond
		if on {
			e.set(ExpMissedNotification, "on")
		} else {
			e.set(ExpMissedNotification, "off")
		}
		e.invite(dmChannel)
		e.shown("the invite")
		time.Sleep(600 * time.Millisecond)
		missed := false
		for _, n := range e.h.notifications() {
			if strings.Contains(n.Title, "Huddle missed") {
				missed = true
			}
		}
		if missed != on {
			t.Fatalf("on %v: Huddle missed shown %v (%+v)", on, missed, e.h.notifications())
		}
	}
}

// ---- huddles_miles_davis ----

func TestMilesDavisSongs(t *testing.T) {
	e := newExpEnv(t, map[string]any{"huddles_music_player_song_key": "so_what"})
	e.set(ExpMilesDavis, "on")
	if got := e.c.loungeSong(); got != "so_what-a599bd1.mp3" {
		t.Fatalf("on: %s", got)
	}
	e.set(ExpMilesDavis, "off")
	if got := e.c.loungeSong(); got != loungeSongs["jazz"] {
		t.Fatalf("off: %s", got)
	}
}

// ---- huddles_awareness_timestamp: raised hands by when they went up ----

func TestGalleryOrder(t *testing.T) {
	now := time.Now()
	st := huddle.State{Participants: []huddle.Participant{
		{UserID: "USELF", Self: true},
		{UserID: "U1", AttendeeID: "a1", InMedia: true},
		{UserID: "U2", AttendeeID: "a2", InMedia: true, Camera: true},
		{UserID: "U3", AttendeeID: "a3", InMedia: true, Sticker: "raised_hand", StickerTS: 2000, StickerAt: now},
		{UserID: "U4", AttendeeID: "a4", InMedia: true, Sticker: "raised_hand::skin-tone-2", StickerTS: 1000, StickerAt: now},
		{UserID: "U5", AttendeeID: "a5", InMedia: true, Sharing: true},
		{UserID: "U6", AttendeeID: "a6", InMedia: true, Sticker: "raised_hand", StickerAt: now.Add(-time.Hour)}, // no field_timestamp_ms
	}}
	order := func(queue bool) string {
		var ids []string
		for _, p := range galleryOrder(st, queue) {
			ids = append(ids, p.UserID)
		}
		return strings.Join(ids, " ")
	}
	// U6 sent no field_timestamp_ms: last of the raised hands
	if got := order(false); got != "USELF U4 U3 U6 U5 U2 U1" {
		t.Fatalf("by timestamp: %s", got)
	}
	// the queue (huddles_raised_hand_queue) falls back to when Clack
	// received the entry (an hour ago, in ms since 1970: after 1000 and 2000)
	if got := order(true); got != "USELF U4 U3 U6 U5 U2 U1" {
		t.Fatalf("queue: %s", got)
	}
	// someone who's both sharing and on camera is placed once, as a sharer
	st.Participants[5].Camera = true
	if got := order(false); got != "USELF U4 U3 U6 U5 U2 U1" {
		t.Fatalf("dedupe: %s", got)
	}
}

// ---- huddles_screenshare_popout ----

func TestScreensharePopoutGate(t *testing.T) {
	e := newExpEnv(t, nil)
	e.set(ExpScreensharePopout, "off")
	if e.c.popoutOn() {
		t.Fatal("off: the popout button shows")
	}
	e.set(ExpScreensharePopout, "on")
	if !e.c.popoutOn() {
		t.Fatal("on: no popout button")
	}
	// the bookkeeping (no window: NoWindow keeps it from opening one)
	e.c.OpenPopout("att-remote#content", huddle.FakeRemoteUser)
	if e.c.popped("att-remote#content") {
		t.Fatal("NoWindow opened a popout")
	}
	w := &window{c: e.c, popout: "att-remote#content"}
	e.c.popout.wins = map[string]*window{w.popout: w}
	if !e.c.popped(w.popout) {
		t.Fatal("not popped")
	}
	e.c.closePopout(w.popout)
	if e.c.popped(w.popout) {
		t.Fatal("still popped after closing")
	}
}

// ---- huddles_multi_device and huddles_awareness_timestamp reach the session ----

func TestMediaExperiments(t *testing.T) {
	e := newExpEnv(t, nil)
	e.set(ExpAwarenessTimestamp, "on")
	e.set(ExpMultiDevice, "treatment")
	if x := e.c.mediaExperiments(); !x.AwarenessTimestamp || !x.MultiDevice {
		t.Fatalf("%+v", x)
	}
	e.set(ExpAwarenessTimestamp, "off")
	e.set(ExpMultiDevice, "control")
	if x := e.c.mediaExperiments(); x.AwarenessTimestamp || x.MultiDevice {
		t.Fatalf("%+v", x)
	}
}

// ---- huddles_multi_device: the multi-device modal ----

func TestMultiDeviceModal(t *testing.T) {
	e := newExpEnv(t, nil)
	// the room lists you (another device) while this client isn't in it
	e.c.mu.Lock()
	e.c.live[hcChannel] = liveRoom{ID: huddle.FakeRoomID, Channel: hcChannel, Participants: []string{huddle.FakeSelfUser, huddle.FakeRemoteUser}}
	e.c.mu.Unlock()
	if n, others := e.c.huddlingElsewhere(hcChannel); n != 1 || len(others) != 1 {
		t.Fatalf("elsewhere: %d %v", n, others)
	}
	e.set(ExpMultiDevice, "control")
	e.c.Begin(hcChannel, EntryHeaderButton)
	waitFor(t, "control: a plain join", func() bool { return len(fakeCalls(e.fk, "rooms.join")) == 1 })
	if e.oh.modal() != nil {
		t.Fatal("control asked about devices")
	}
	if j := fakeCalls(e.fk, "rooms.join"); j[0].Params.Get("multidevice") != "false" {
		t.Fatalf("multidevice %q", j[0].Params.Get("multidevice"))
	}
	connected := func() bool { s := e.c.Session(); return s != nil && s.State().Phase == huddle.Connected }
	waitFor(t, "connected", connected) // leaving mid-negotiation is a fake Chime error
	e.c.Leave()
	waitFor(t, "left", func() bool { return !e.c.Active() })

	e.set(ExpMultiDevice, "treatment")
	e.c.mu.Lock()
	e.c.live[hcChannel] = liveRoom{ID: huddle.FakeRoomID, Channel: hcChannel, Participants: []string{huddle.FakeSelfUser, huddle.FakeRemoteUser}}
	e.c.mu.Unlock()
	e.c.Begin(hcChannel, EntryHeaderButton)
	var m *popups.Modal
	waitFor(t, "the multi-device modal", func() bool { m = e.oh.modal(); return m != nil })
	if len(fakeCalls(e.fk, "rooms.join")) != 1 {
		t.Fatal("joined before answering")
	}
	// "Use both devices" (the modal's pick): rooms.join with multidevice true
	m.Dismiss()
	e.c.mu.Lock()
	e.c.multi = &pendingMulti{channel: hcChannel, multi: true}
	e.c.mu.Unlock()
	e.c.begin(hcChannel, EntryHeaderButton)
	waitFor(t, "the multi-device join", func() bool { return len(fakeCalls(e.fk, "rooms.join")) == 2 })
	if j := fakeCalls(e.fk, "rooms.join"); j[1].Params.Get("multidevice") != "true" {
		t.Fatalf("Use both devices: multidevice %q", j[1].Params.Get("multidevice"))
	}
	waitFor(t, "connected again", connected)
	e.c.Leave()
	waitFor(t, "left again", func() bool { return !e.c.Active() })
}
