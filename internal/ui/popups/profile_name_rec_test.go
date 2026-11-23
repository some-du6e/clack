package popups

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"io"
	"net/url"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/unit"

	vclip "github.com/some-du6e/clack/internal/clip"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/media"
	"github.com/some-du6e/clack/internal/media/goaudio"
	"github.com/some-du6e/clack/internal/media/mediatest"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/videoplayer"
)

// nrCalls records the API calls fakeslack answers, by method.
type nrCalls struct {
	mu sync.Mutex
	m  map[string][]url.Values
}

func (c *nrCalls) get(method string) []url.Values {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]url.Values(nil), c.m[method]...)
}

func recordCalls(fs *fakeslack.Server) *nrCalls {
	c := &nrCalls{m: map[string][]url.Values{}}
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		c.mu.Lock()
		c.m[method] = append(c.m[method], f)
		c.mu.Unlock()
		return nil, false
	})
	return c
}

// useNameMic points the name recorder at dev.
func useNameMic(t *testing.T, dev func() huddle.Device) {
	t.Helper()
	old, oldSound := NameRecDevice, NameRecStartSound
	NameRecDevice, NameRecStartSound = dev, func() {}
	t.Cleanup(func() { NameRecDevice, NameRecStartSound = old, oldSound })
}

// headerElements is the HEADER section a setSections call wrote, by
// element id, each element as JSON.
func headerElements(t *testing.T, ss []url.Values) map[string]string {
	t.Helper()
	if len(ss) == 0 {
		t.Fatal("no users.profile.setSections")
	}
	last := ss[len(ss)-1]
	if last.Get("section") != "PsHEADER" {
		t.Fatalf("setSections section %q", last.Get("section"))
	}
	var els []map[string]any
	if err := json.Unmarshal([]byte(last.Get("elements")), &els); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range els {
		b, _ := json.Marshal(e)
		out[e["element_id"].(string)] = string(b)
	}
	return out
}

// waitDeleted waits for files.delete of id (EditProfileFormPane's e5 isn't
// awaited).
func waitDeleted(t *testing.T, calls *nrCalls, id string) {
	t.Helper()
	for dl := time.Now().Add(5 * time.Second); time.Now().Before(dl); time.Sleep(10 * time.Millisecond) {
		for _, d := range calls.get("files.delete") {
			if d.Get("file") == id {
				return
			}
		}
	}
	t.Errorf("no files.delete of %s: %v", id, calls.get("files.delete"))
}

// record starts the name recorder and waits until it records.
func recordName(t *testing.T, m *EditProfileModal) *vclip.Recorder {
	t.Helper()
	f := m.Form()
	f.RecordName()
	epUntil(t, m, "recording", func() bool { r := f.NameRecorder(); return r != nil && r.State() == vclip.Recording })
	return f.NameRecorder()
}

// Name recording, as Slack's EditAudioButton and NamePronunciationAudioRecorder
// do it: Record Audio Clip opens the recorder; ✓ stops it, and the
// recording uploads at once as a private file (files.getUploadURL
// {filename audio_name_pronunciation.m4a, length, subtype
// slack_name_pronunciation}, completeUpload with the subtype, shared
// nowhere) while the form's Save waits; once uploaded Save writes its id
// into the HEADER section's Name Recording element (setProfileHeaderFields:
// file {fileId}), and deletes nothing.
func TestEditProfileRecordsNameRecording(t *testing.T) {
	h, fs := epEnv(t, false)
	calls := recordCalls(fs)
	useNameMic(t, func() huddle.Device { return huddle.NewToneDevice(440) })
	m := NewEditProfileModal(h, EditProfileOpts{})
	f := m.Form()
	epUntil(t, m, "the header section", func() bool { return !f.secsLoading })
	if id, rec, up, _ := f.Recording(); id != "" || rec || up || f.nr.mode != nrButton {
		t.Fatalf("before recording: %q %v %v mode %d", id, rec, up, f.nr.mode)
	}
	r := recordName(t, m)
	if _, rec, _, _ := f.Recording(); !rec {
		t.Error("not recording")
	}
	time.Sleep(800 * time.Millisecond)
	f.StopName()
	epUntil(t, m, "the recording attached", func() bool { return f.nr.mode == nrFile })
	if f.canSave() && f.nr.uploading {
		t.Error("Save on while the recording uploads (onFileUploadStart)")
	}
	epUntil(t, m, "the upload", func() bool { id, _, up, _ := f.Recording(); return id != "" && !up })
	if r.State() != vclip.Stopped {
		t.Errorf("recorder %s", r.State())
	}
	id, _, _, failed := f.Recording()
	if failed || !f.canSave() || !f.dirty() {
		t.Fatalf("after the upload: failed %v, can save %v, dirty %v", failed, f.canSave(), f.dirty())
	}
	var up fakeslack.UploadedFile
	for _, u := range fs.Uploads() {
		if u.ID == id {
			up = u
		}
	}
	a := up.URLArgs
	if up.Name != NameRecFileName || a.Get("subtype") != NameRecSubtype || a.Get("length") == "0" || a.Get("length") == "" ||
		!up.Completed || up.CompleteSubtype != NameRecSubtype {
		t.Errorf("the upload %q %v, completed %v subtype %q", up.Name, a, up.Completed, up.CompleteSubtype)
	}
	if len(fs.Shared()) != 0 {
		t.Errorf("the recording was shared: %+v", fs.Shared())
	}
	f.Save()
	epUntil(t, m, "the save", m.Dismissed)
	els := headerElements(t, calls.get("users.profile.setSections"))
	if want := `{"element_id":"PeRECORD","file":{"fileId":"` + id + `"}}`; els["PeRECORD"] != want {
		t.Errorf("Name Recording written %s, want %s", els["PeRECORD"], want)
	}
	if d := calls.get("files.delete"); len(d) != 0 {
		t.Errorf("deleted %v", d)
	}
}

// The recorder's 10 s limit stops and attaches the recording by itself
// (here shortened); removing a recording made in the form deletes it at
// once (onChangeFile(null): e5(eK)) and brings the button back; the form's
// Cancel deletes a recording it didn't save (handleCancel), and writes
// nothing.
func TestEditProfileNameRecordingLimitRemoveCancel(t *testing.T) {
	h, fs := epEnv(t, false)
	calls := recordCalls(fs)
	useNameMic(t, func() huddle.Device { return huddle.NewToneDevice(440) })
	old := nameRecLimit
	nameRecLimit = 1500 * time.Millisecond
	t.Cleanup(func() { nameRecLimit = old })
	m := NewEditProfileModal(h, EditProfileOpts{})
	f := m.Form()
	epUntil(t, m, "the header section", func() bool { return !f.secsLoading })
	r := recordName(t, m)
	if !r.Warning() { // 1.5 s left is inside the last 5 s
		t.Error("no warning inside the last 5 s")
	}
	epUntil(t, m, "the limit's upload", func() bool { id, _, up, _ := f.Recording(); return id != "" && !up })
	first, _, _, _ := f.Recording()
	f.RemoveName()
	if id, _, _, _ := f.Recording(); id != "" || f.nr.mode != nrButton || f.nrBusy {
		t.Fatalf("after remove: %q mode %d busy %v", id, f.nr.mode, f.nrBusy)
	}
	waitDeleted(t, calls, first)
	recordName(t, m)
	time.Sleep(500 * time.Millisecond)
	f.StopName()
	epUntil(t, m, "the second upload", func() bool { id, _, up, _ := f.Recording(); return id != "" && !up })
	second, _, _, _ := f.Recording()
	f.m.press(f.m.o.Buttons[0]) // Cancel
	if !m.Dismissed() {
		t.Fatal("Cancel didn't close")
	}
	waitDeleted(t, calls, second)
	if ss := calls.get("users.profile.setSections"); len(ss) != 0 {
		t.Errorf("Cancel wrote the header: %v", ss)
	}
}

// The profile's own recording shows as the file when the form opens
// (initialNamePronunciationFileId), playable; removing it changes the
// form, and Save deletes it (handleSubmit's e5(ea)) and writes the element
// null.
func TestEditProfileNameRecordingReplacesTheProfiles(t *testing.T) {
	h, fs := epEnv(t, false)
	calls := recordCalls(fs)
	els := `[{"element_id":"PeRECORD","file":{"fileId":"` + fakeslack.NameRecordingID + `"}}]`
	if err := h.a.C.Call(context.Background(), "users.profile.setSections", url.Values{"user": {fakeslack.SelfID}, "section": {"PsHEADER"}, "elements": {els}}, nil); err != nil {
		t.Fatal(err)
	}
	m := NewEditProfileModal(h, EditProfileOpts{})
	f := m.Form()
	epUntil(t, m, "the profile's recording", func() bool { return f.nr.mode == nrFile && f.nr.file != nil })
	if id, _, _, _ := f.Recording(); id != fakeslack.NameRecordingID || f.dirty() {
		t.Fatalf("opened with %q, dirty %v", id, f.dirty())
	}
	if f.nr.uploadedFile() == nil || f.nr.uploadedFile().URL == "" {
		t.Error("the profile's recording has no url_private to play")
	}
	f.RemoveName()
	if d := calls.get("files.delete"); len(d) != 0 {
		t.Errorf("the profile's recording deleted before Save: %v", d)
	}
	if !f.dirty() {
		t.Fatal("removing the recording isn't a change")
	}
	f.Save()
	epUntil(t, m, "the save", m.Dismissed)
	waitDeleted(t, calls, fakeslack.NameRecordingID)
	if got := headerElements(t, calls.get("users.profile.setSections"))["PeRECORD"]; got != `{"element_id":"PeRECORD","file":null}` {
		t.Errorf("Name Recording written %s, want null", got)
	}
}

// nrDeadMic is a microphone that won't open.
type nrDeadMic struct{}

func (nrDeadMic) Start(func([]int16), func([]int16)) error { return errors.New("no microphone") }
func (nrDeadMic) Close()                                   {}

// A microphone that won't open (no permission, no device) is
// MicrophoneAccessErrorDialog; the recorder stays up for its ×, which
// brings the button back.
func TestEditProfileNameRecordingMicError(t *testing.T) {
	h, _ := epEnv(t, false)
	useNameMic(t, func() huddle.Device { return nrDeadMic{} })
	m := NewEditProfileModal(h, EditProfileOpts{})
	f := m.Form()
	epUntil(t, m, "the header section", func() bool { return !f.secsLoading })
	f.RecordName()
	epUntil(t, m, "the dialog", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		for _, o := range h.shown {
			if md, ok := o.(*Modal); ok && md.o.Title == nsStoriesAV.T("Can’t access microphone") {
				return true
			}
		}
		return false
	})
	if f.nr.mode != nrRecording {
		t.Errorf("mode %d after the error, want the recorder", f.nr.mode)
	}
	f.CancelName()
	if f.nr.mode != nrButton || f.nr.rec != nil {
		t.Errorf("after ×: mode %d", f.nr.mode)
	}
}

// nrHost is the test host with the window's players.
type nrHost struct {
	*tzHost
	vids *videoplayer.Controller
}

func (h *nrHost) Videos() *videoplayer.Controller { return h.vids }

// nrPlayers is the window's players on the Go audio backend with no sound
// card (it plays on the clock), every URL reading Robin's recording;
// opened has each URL read.
func nrPlayers(t *testing.T, th *theme.Theme) (ctl *videoplayer.Controller, opened *sync.Map) {
	t.Helper()
	_, opener := mediatest.Source(fakeslack.NameRecordingID, mediatest.TonesOpusFrag, false)
	noCard := func(func([]int16)) (io.Closer, error) { return nil, errors.New("no sound card (the test's)") }
	opened = new(sync.Map)
	ctl = videoplayer.New(videoplayer.Env{
		Manager: media.NewManager(media.NewChain(nil, &goaudio.Backend{Output: noCard, Logf: t.Logf})),
		Open: func(ctx context.Context, u string, off int64) (io.ReadCloser, media.Info, error) {
			opened.Store(u, true)
			return opener.Open(ctx, off)
		},
		Repaint: func() {},
		Theme:   func() *theme.Theme { return th },
	})
	t.Cleanup(ctl.CloseAll)
	return ctl, opened
}

// nrShot saves v's name row (under the 256px photo: 280 to 480 of the
// pane, the name, its button and the title) at scale 2 as name.png under
// PROFILE_SHOTS_DIR (own-profile.yml), when that's set; no tofu.
func nrShot(t *testing.T, v *ProfileView, name string) {
	t.Helper()
	dir := os.Getenv("PROFILE_SHOTS_DIR")
	if dir == "" {
		return
	}
	const s = 2
	win := image.Pt(pvW*s, 480*s)
	var ops op.Ops
	frame := func() {
		ops.Reset()
		bg := colorsOf(v.h.Theme()).Bg
		if v.h.Theme().Dark {
			bg = rgb(0x1a1d21)
		}
		paint.Fill(&ops, bg)
		gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: s, PxPerSp: s}, Constraints: layout.Exact(win)}
		v.Layout(gtx)
	}
	for i := 0; i < 3; i++ {
		frame()
		drain()
		time.Sleep(10 * time.Millisecond)
	}
	stop := theme.StartTofuScan()
	frame()
	if st := stop(); st.Notdef > 0 {
		t.Errorf("%s: %d .notdef glyphs", name, st.Notdef)
	}
	epSaveShot(t, dir, name, stCrop(pixeltest.Render(t, win.X, win.Y, &ops), image.Rect(0, 280*s, win.X, win.Y)))
}

// Screenshots, light and dark (PROFILE_SHOTS_DIR, own-profile.yml): the
// name recorder recording, and the profile's recording on the form's tile
// with the window's player.
func TestProfileNameRecordingShots(t *testing.T) {
	dir := os.Getenv("PROFILE_SHOTS_DIR")
	if dir == "" {
		t.Skip("PROFILE_SHOTS_DIR unset (own-profile.yml sets it)")
	}
	useNameMic(t, func() huddle.Device { return huddle.NewToneDevice(440) })
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		h, _ := epEnv(t, dark)
		m := NewEditProfileModal(h, EditProfileOpts{})
		f := m.Form()
		epUntil(t, m, "the header section", func() bool { return !f.secsLoading })
		recordName(t, m)
		time.Sleep(1200 * time.Millisecond) // a second on the timer, some dots
		epShot(t, h, dir, "edit-profile-name-recorder-"+mode, m, func() *Modal { return m.m })
		f.CancelName()
		m.Close()

		els := `[{"element_id":"PeRECORD","file":{"fileId":"` + fakeslack.NameRecordingID + `"}}]`
		if err := h.a.C.Call(context.Background(), "users.profile.setSections", url.Values{"user": {fakeslack.SelfID}, "section": {"PsHEADER"}, "elements": {els}}, nil); err != nil {
			t.Fatal(err)
		}
		ctl, _ := nrPlayers(t, h.th)
		hn := &nrHost{tzHost: h, vids: ctl}
		mt := NewEditProfileModal(hn, EditProfileOpts{})
		ft := mt.Form()
		epUntil(t, mt, "the profile's recording", func() bool { return ft.nr.mode == nrFile && ft.nr.file != nil })
		epShot(t, h, dir, "edit-profile-name-recording-"+mode, mt, func() *Modal { return mt.m })
		mt.Close()
	}
}

// nrFrame lays v out once.
func nrFrame(v *ProfileView) {
	ops := new(op.Ops)
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(400, 1400)), Now: time.Now()}
	v.Layout(gtx)
	drain()
}

// A member's name recording on their profile, as Slack's Name draws
// NamePronunciationAudioPlayer after the name: the button once the file
// has its url_private (files.info); a click plays the recording from the
// start, a click while it plays pauses it, and it keeps no progress. A
// profile without a recording has no button.
func TestProfileNameRecordingButton(t *testing.T) {
	h0, _ := pfEnv(t)
	ctl, opened := nrPlayers(t, h0.th)
	h := &nrHost{tzHost: h0, vids: ctl}
	v := NewProfileView(h, fakeslack.ProfileID)
	for dl := time.Now().Add(10 * time.Second); v.recFile == nil; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("Robin's recording never loaded")
		}
		nrFrame(v)
	}
	nrFrame(v)
	if _, ok := ctl.TileState("profile", "name", fakeslack.ProfileID, fakeslack.NameRecordingID); !ok {
		t.Fatal("no NamePronunciationAudioPlayer after Robin's name")
	}
	nrShot(t, v, "profile-name-recording-light")
	rec, _ := v.recording(fakeslack.NameRecordingID)
	ctl.ToggleNameSound("profile", fakeslack.ProfileID, rec)
	for dl := time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		nrFrame(v)
		st, _ := ctl.TileState("profile", "name", fakeslack.ProfileID, fakeslack.NameRecordingID)
		if st.Playing && st.Elapsed > 300*time.Millisecond {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("the recording didn't play: %+v", st)
		}
	}
	nrShot(t, v, "profile-name-recording-playing-light") // the animated icon
	if _, ok := opened.Load(rec.URL); !ok {
		t.Errorf("the player didn't read url_private %s", rec.URL)
	}
	ctl.ToggleNameSound("profile", fakeslack.ProfileID, rec)
	for dl := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		nrFrame(v)
		st, _ := ctl.TileState("profile", "name", fakeslack.ProfileID, fakeslack.NameRecordingID)
		if !st.Playing {
			break
		}
		if time.Now().After(dl) {
			t.Fatal("a click while it plays didn't pause it")
		}
	}
	if _, ok := ctl.Progress(fakeslack.NameRecordingID); ok {
		t.Error("the name recording kept a progress")
	}
	// your own profile here has no recording: no button
	self := NewProfileView(h, fakeslack.SelfID)
	for i := 0; i < 40; i++ {
		nrFrame(self)
		time.Sleep(10 * time.Millisecond)
	}
	if f, _ := self.recording(""); f != nil || self.recFile != nil {
		t.Error("a button on a profile without a recording")
	}
}

// Tab and Shift+Tab walk every stop of Edit profile and wrap (react-modal's
// scopeTab), the name recording's Record Audio Clip among them, between
// Pronouns and Name pronunciation, and each stop keeps the focus. The
// field's stop was its editor, which never lays out: Gio dropped the focus
// at the frame's end, the next Tab wrapped to Full name, and Name
// pronunciation, Time zone, Cancel and Save were out of reach.
func TestEditProfileTabOrder(t *testing.T) {
	h, _ := epEnv(t, false)
	m := NewEditProfileModal(h, EditProfileOpts{})
	f := m.Form()
	var r input.Router
	var ops op.Ops
	now := time.Now()
	frame := func() {
		ops.Reset()
		gtx := layout.Context{Ops: &ops, Source: r.Source(), Now: now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(1440, 900))}
		m.Layout(gtx)
		r.Frame(&ops)
		drain()
		now = now.Add(16 * time.Millisecond)
	}
	for dl := time.Now().Add(10 * time.Second); f.secsLoading || f.grantsLoading; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(dl) {
			t.Fatal("the form never loaded")
		}
		frame()
	}
	for i := 0; i < 4; i++ {
		frame()
	}
	stops := f.m.stops()
	if !slices.Contains(stops, event.Tag(&f.nr.recBtn)) {
		t.Error("Record Audio Clip isn't a stop")
	}
	focused := func() int {
		for i, s := range stops {
			if r.Source().Focused(s) {
				return i
			}
		}
		return -1
	}
	if got := focused(); got != 0 {
		t.Fatalf("on open the focus is on stop %d, want Full name's (0)", got)
	}
	tab := func(mods key.Modifiers) {
		r.Queue(key.Event{Name: key.NameTab, Modifiers: mods, State: key.Press})
		frame()
		r.Queue(key.Event{Name: key.NameTab, Modifiers: mods, State: key.Release})
		frame()
		frame()
	}
	n := len(stops)
	for k := 1; k <= n; k++ {
		tab(0)
		if got := focused(); got != k%n {
			t.Fatalf("Tab %d: the focus is on stop %d, want %d of %d", k, got, k%n, n)
		}
	}
	for k := 1; k <= n; k++ {
		tab(key.ModShift)
		if got, want := focused(), (n-k%n)%n; got != want {
			t.Fatalf("Shift+Tab %d: the focus is on stop %d, want %d of %d", k, got, want, n)
		}
	}
}

// Saving a new recording over the profile's own never deletes a file the
// profile still names. When users.profile.setSections fails, the profile
// keeps its recording: nothing is deleted, and Cancel then deletes only the
// new one. When the header is written but users.profile.set refuses a
// field (the form stays open on its error), the profile's recording is the
// new one from then: the replaced one goes, and Cancel deletes nothing.
func TestEditProfileNameRecordingSaveFailures(t *testing.T) {
	// a header write that fails
	h, fs := epEnv(t, false)
	calls := recordCalls(fs)
	useNameMic(t, func() huddle.Device { return huddle.NewToneDevice(440) })
	newOverOld := func(h *tzHost) (*EditProfileModal, string) {
		t.Helper()
		els := `[{"element_id":"PeRECORD","file":{"fileId":"` + fakeslack.NameRecordingID + `"}}]`
		if err := h.a.C.Call(context.Background(), "users.profile.setSections", url.Values{"user": {fakeslack.SelfID}, "section": {"PsHEADER"}, "elements": {els}}, nil); err != nil {
			t.Fatal(err)
		}
		m := NewEditProfileModal(h, EditProfileOpts{})
		f := m.Form()
		epUntil(t, m, "the profile's recording", func() bool { return f.nr.mode == nrFile && f.nr.file != nil })
		f.RemoveName()
		recordName(t, m)
		time.Sleep(600 * time.Millisecond)
		f.StopName()
		epUntil(t, m, "the new recording's upload", func() bool { id, _, up, _ := f.Recording(); return id != "" && !up })
		id, _, _, _ := f.Recording()
		return m, id
	}
	deletedOf := func(calls *nrCalls, id string) bool {
		for _, d := range calls.get("files.delete") {
			if d.Get("file") == id {
				return true
			}
		}
		return false
	}
	m, fresh := newOverOld(h)
	f := m.Form()
	fs.SetFault("users.profile.setSections", fakeslack.Fault{Error: "fatal_error"})
	f.Save()
	epUntil(t, m, "the failed save", func() bool { return !f.Saving() })
	for i := 0; i < 30; i++ {
		epFrame(m)
		time.Sleep(10 * time.Millisecond)
	}
	if deletedOf(calls, fakeslack.NameRecordingID) {
		t.Error("the profile's recording was deleted though its header write failed")
	}
	fs.SetFault("users.profile.setSections", fakeslack.Fault{})
	f.m.press(f.m.o.Buttons[0]) // Cancel
	waitDeleted(t, calls, fresh)
	if deletedOf(calls, fakeslack.NameRecordingID) {
		t.Error("Cancel deleted the recording the profile names")
	}

	// a header written, then a field refused: the form stays open
	h2, fs2 := epEnv(t, false)
	calls2 := &nrCalls{m: map[string][]url.Values{}}
	fs2.SetHook(func(method string, fv url.Values) (any, bool) {
		calls2.mu.Lock()
		calls2.m[method] = append(calls2.m[method], fv)
		calls2.mu.Unlock()
		if method == "users.profile.set" {
			return map[string]any{"ok": false, "error": "invalid_name_specials", "field": "real_name"}, true
		}
		return nil, false
	})
	m2, fresh2 := newOverOld(h2)
	f2 := m2.Form()
	f2.Save()
	epUntil(t, m2, "the field error", func() bool { return !f2.Saving() && f2.FieldError("real_name") != "" })
	if m2.Dismissed() {
		t.Fatal("the form closed on the field error")
	}
	waitDeleted(t, calls2, fakeslack.NameRecordingID) // the header names the new one now
	f2.m.press(f2.m.o.Buttons[0])                     // Cancel
	for i := 0; i < 30; i++ {
		epFrame(m2)
		time.Sleep(10 * time.Millisecond)
	}
	if deletedOf(calls2, fresh2) {
		t.Errorf("Cancel deleted %s, which the profile's header names since the save", fresh2)
	}
}
