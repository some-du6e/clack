package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// introApp is a signed-out first launch: the introduction in front of
// the sign-in card. dir is the config folder ("" a new one).
func introApp(t *testing.T, dir string, login func(slack.SignInInput) error) *App {
	t.Helper()
	if dir == "" {
		dir = t.TempDir()
	}
	images := imgcache.New(func(ctx context.Context, url string) ([]byte, error) { return nil, errors.New("offline") }, "", nil)
	a := New(Options{ConfigDir: dir, NoRealtime: true}, images)
	sh := theme.NewShaper()
	a.th.SetShapers(sh, sh)
	a.motionOverride = -1 // full motion, whatever this machine's setting
	if login == nil {
		login = func(slack.SignInInput) error { return nil }
	}
	a.SetLogin(login)
	return a
}

// introEnv draws whole frames through a real input.Router on a virtual
// clock, counting the frames that asked for another one at once.
type introEnv struct {
	t     *testing.T
	a     *App
	r     input.Router
	ops   op.Ops
	now   time.Time
	sz    image.Point
	wakes int
	win   *headless.Window // set: frames are rendered too
	img   *image.RGBA
}

func newIntroEnv(t *testing.T, a *App, sz image.Point) *introEnv {
	return &introEnv{t: t, a: a, now: time.Now(), sz: sz}
}

func (e *introEnv) frame(dt time.Duration) {
	e.now = e.now.Add(dt)
	e.ops.Reset()
	gtx := layout.Context{Ops: &e.ops, Now: e.now, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(e.sz), Source: e.r.Source()}
	e.a.drain()
	slackc.Pump()
	e.a.frame(gtx)
	e.r.Frame(&e.ops)
	if at, ok := e.r.WakeupTime(); ok && !at.After(e.now.Add(5*time.Millisecond)) {
		e.wakes++
	}
	if e.win != nil {
		if err := e.win.Frame(&e.ops); err != nil {
			e.t.Fatalf("frame: %v", err)
		}
		if e.img == nil {
			e.img = image.NewRGBA(image.Rectangle{Max: e.sz})
		}
		if err := e.win.Screenshot(e.img); err != nil {
			e.t.Fatalf("screenshot: %v", err)
		}
	}
}

// run draws frames at 60 fps for d.
func (e *introEnv) run(d time.Duration) {
	const step = time.Second / 60
	for t := time.Duration(0); t < d; t += step {
		e.frame(step)
	}
}

func (e *introEnv) key(name key.Name) {
	e.r.Queue(key.Event{Name: name, State: key.Press}, key.Event{Name: name, State: key.Release})
	e.frame(time.Second / 60)
	e.frame(time.Second / 60)
}

// started draws until the sequence's clock runs.
func (e *introEnv) started() {
	e.t.Helper()
	for i := 0; i < 60 && (e.a.intro == nil || e.a.intro.t0.IsZero()); i++ {
		e.frame(time.Second / 60)
	}
	if e.a.intro == nil || e.a.intro.t0.IsZero() {
		e.t.Fatal("the introduction never started")
	}
}

// idleWakes is how many of n frames asked for another frame at once.
func (e *introEnv) idleWakes(n int) int {
	e.wakes = 0
	for i := 0; i < n; i++ {
		e.frame(50 * time.Millisecond)
	}
	return e.wakes
}

// The first launch: Enter, Space and → move on, ← goes back, Esc skips to
// the sign-in card, which comes in and then nothing moves. The next launch
// goes straight to sign-in.
func TestIntroFirstRun(t *testing.T) {
	dir := t.TempDir()
	a := introApp(t, dir, nil)
	if a.intro == nil || a.intro.mode != introFirstRun {
		t.Fatal("no introduction on the first launch")
	}
	e := newIntroEnv(t, a, image.Pt(1440, 900))
	e.started()
	in := a.intro
	if in.cur != beatLogo {
		t.Fatalf("starts on beat %d", in.cur)
	}
	steps := []struct {
		k    key.Name
		want int
	}{
		{key.NameReturn, beatNative}, {key.NameRightArrow, beatSlack}, {key.NameLeftArrow, beatNative},
		{key.NameSpace, beatSlack}, {key.NameEnter, beatPlugins},
	}
	for _, s := range steps {
		e.key(s.k)
		if in.cur != s.want {
			t.Fatalf("after %v: beat %d, want %d", s.k, in.cur, s.want)
		}
		e.run(300 * time.Millisecond)
	}
	if e.wakes == 0 {
		t.Error("the beats asked for no frames while they moved")
	}
	e.key(key.NameEscape)
	if !in.signIn || in.cur != beatFinal {
		t.Fatalf("Esc: sign-in %v, beat %d", in.signIn, in.cur)
	}
	if !a.prefs.IntroSeen {
		t.Error("the introduction isn't marked seen")
	}
	e.run(2 * time.Second)
	if a.intro != nil {
		t.Fatal("the introduction is still there after handing over")
	}
	if c := a.login.card; c.Empty() || !c.In(image.Rectangle{Max: e.sz}) {
		t.Errorf("sign-in card %v", c)
	}
	if !e.a.login.focused {
		t.Error("the sign-in card didn't take the focus")
	}
	if w := e.idleWakes(40); w > 2 {
		t.Errorf("idle on the sign-in card: %d of 40 frames asked for another at once", w)
	}
	a.flushPrefs()
	b, err := os.ReadFile(filepath.Join(dir, "ui.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		IntroSeen bool `json:"intro_seen"`
	}
	if json.Unmarshal(b, &saved) != nil || !saved.IntroSeen {
		t.Errorf("ui.json %s", b)
	}
	if again := introApp(t, dir, nil); again.intro != nil {
		t.Error("the introduction played again on the second launch")
	}
}

// Left alone, the beats move on by themselves and end on the sign-in
// card; Skip does the same at once.
func TestIntroAutoAdvanceAndSkip(t *testing.T) {
	a := introApp(t, "", nil)
	e := newIntroEnv(t, a, image.Pt(1280, 800))
	e.started()
	in := a.intro
	seen := map[int]bool{}
	for i := 0; i < 60*40 && a.intro != nil; i++ {
		seen[in.cur] = true
		e.frame(time.Second / 60)
	}
	for b := beatLogo; b <= beatFinal; b++ {
		if !seen[b] {
			t.Errorf("beat %d never showed", b)
		}
	}
	if a.intro != nil {
		t.Fatal("the introduction didn't end by itself")
	}
	b := introApp(t, "", nil)
	e = newIntroEnv(t, b, image.Pt(1280, 800))
	e.started()
	b.intro.skipBtn.Click()
	e.frame(time.Second / 60)
	if !b.intro.signIn {
		t.Fatal("Skip didn't go to the sign-in card")
	}
}

// Reduced motion: every piece only fades (no offset, scale or blur),
// across the whole sequence; with motion on, pieces do move.
func TestIntroReducedMotion(t *testing.T) {
	for _, reduced := range []bool{true, false} {
		a := introApp(t, "", nil)
		if reduced {
			a.motionOverride = 1
		}
		e := newIntroEnv(t, a, image.Pt(1440, 900))
		e.started()
		in := a.intro
		var rec []introDrawn
		in.rec = &rec
		moved, faded := 0, 0
		for i := 0; i < 30*40 && a.intro != nil; i++ {
			rec = rec[:0]
			e.frame(time.Second / 30)
			for _, d := range rec {
				still := d.dx == 0 && d.dy == 0 && (d.s == 1 || d.s == 0) && d.blur == 0
				if !still {
					moved++
					if reduced && moved < 5 {
						t.Errorf("reduced motion: %s moved (dx %.1f dy %.1f scale %.3f blur %.2f) on beat %d", d.name, d.dx, d.dy, d.s, d.blur, in.cur)
					}
				}
				if d.a > 0.05 && d.a < 0.95 {
					faded++
				}
			}
		}
		if faded == 0 {
			t.Errorf("reduced %v: nothing faded", reduced)
		}
		if !reduced && moved == 0 {
			t.Error("with motion on, nothing moved")
		}
	}
}

// A sign-in link from the browser during the introduction goes straight
// to the sign-in card and signs in.
func TestIntroSignInWithLink(t *testing.T) {
	var mu sync.Mutex
	var got []slack.SignInInput
	a := introApp(t, "", func(in slack.SignInInput) error {
		mu.Lock()
		got = append(got, in)
		mu.Unlock()
		return errors.New("that sign-in link has expired")
	})
	e := newIntroEnv(t, a, image.Pt(1280, 800))
	e.started()
	a.SignInWithLink("slack://T08PSQ7BQ/magic-login/abc123")
	e.frame(time.Second / 60)
	if a.intro == nil || !a.intro.signIn {
		t.Fatal("the introduction didn't step aside for the link")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || got[0].Link == "" {
		t.Errorf("login got %v", got)
	}
}

// Preferences > Clack > Replay introduction plays the sequence over the
// signed-in app: the app's shortcuts are inert under it, Esc goes to
// "You're all set", Enter closes it, and then the app is as idle as it
// was before.
func TestIntroReplay(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	a.motionOverride = -1
	e := newIntroEnv(t, a, image.Pt(1280, 800))
	e.run(500 * time.Millisecond)
	base := e.idleWakes(40)

	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	defer p.close()
	rows := pageRows(t, a, p, clackTabID)
	found := false
	for _, r := range rows {
		if r.Kind == "button" && r.Key == "replay_intro" && r.Label == "Replay" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no Replay button on the Clack tab: %+v", rows)
	}
	p.clicks["replay_intro"].Click()
	pageRows(t, a, p, clackTabID)
	e.frame(time.Second / 60) // the posted replayIntro runs
	if a.intro == nil || a.intro.mode != introReplay {
		t.Fatal("Replay didn't start the introduction")
	}
	e.started()
	in := a.intro
	// the app's own shortcuts don't fire under it
	e.r.Queue(key.Event{Name: "K", Modifiers: key.ModShortcut, State: key.Press}, key.Event{Name: "K", Modifiers: key.ModShortcut, State: key.Release})
	e.frame(time.Second / 60)
	e.frame(time.Second / 60)
	if len(a.overlays) != 0 {
		t.Errorf("a shortcut opened %d overlays under the introduction", len(a.overlays))
	}
	e.key(key.NameRightArrow)
	if in.cur != beatNative {
		t.Fatalf("→: beat %d", in.cur)
	}
	e.key(key.NameEscape)
	if in.cur != beatFinal || in.signIn {
		t.Fatalf("Esc: beat %d (sign-in %v), want You're all set", in.cur, in.signIn)
	}
	e.run(1500 * time.Millisecond)
	if a.intro == nil {
		t.Fatal("You're all set closed by itself")
	}
	e.key(key.NameReturn)
	if !in.closing {
		t.Fatal("Enter didn't close You're all set")
	}
	e.run(time.Second)
	if a.intro != nil {
		t.Fatal("the overlay is still up")
	}
	if w := e.idleWakes(40); w > base+2 {
		t.Errorf("after the replay %d of 40 frames asked for another at once (%d before it)", w, base)
	}
	if a.login != nil {
		t.Error("the replay signed out")
	}
}

// The springs: critically damped never overshoots, underdamped
// overshoots a little and settles, kick peaks at 1, and a springVal keeps
// its velocity when the target moves.
func TestSprings(t *testing.T) {
	for _, s := range []spring{springSmooth, springSnappy, springGentle} {
		for x := 0.0; x < s.settle()*1.5; x += 0.002 {
			if v := s.at(x); v > 1+1e-9 || v < -1e-9 {
				t.Fatalf("%+v at %.3f = %f: a critically damped spring left [0,1]", s, x, v)
			}
		}
		if v := s.at(s.settle()); v < 1-motionEps*1.01 {
			t.Errorf("%+v: %f at its settle time", s, v)
		}
	}
	for _, s := range []spring{springBouncy, springPop} {
		peak := 0.0
		for x := 0.0; x < s.settle(); x += 0.001 {
			peak = max(peak, s.at(x))
		}
		if peak <= 1.01 || peak > 1.2 {
			t.Errorf("%+v overshoots to %f", s, peak)
		}
		if v := s.at(s.settle()); v < 1-motionEps*1.5 || v > 1+motionEps*1.5 {
			t.Errorf("%+v: %f at its settle time", s, v)
		}
	}
	peak := 0.0
	for x := 0.0; x < 0.2; x += 0.0005 {
		peak = max(peak, springShake.kick(x))
	}
	if peak < 0.99 || peak > 1.01 {
		t.Errorf("kick peaks at %f", peak)
	}
	now := time.Now()
	sv := springVal{s: springSmooth}
	sv.get(now, 0)
	sv.get(now.Add(100*time.Millisecond), 1)
	x1, _ := sv.get(now.Add(200*time.Millisecond), 1)
	v1 := sv.v
	x2, moving := sv.get(now.Add(220*time.Millisecond), 0) // reversed mid-flight
	if !moving || v1 <= 0 || x2 < x1 {
		t.Errorf("reversing jumped: %f (v %f) then %f", x1, v1, x2)
	}
	if x, m := sv.get(now.Add(5*time.Second), 0); m || x != 0 {
		t.Errorf("not at rest: %f %v", x, m)
	}
}

// TestIntroVideo renders the introduction headless at 60 fps and encodes
// it with ffmpeg (the login-shots workflow's intro-video job, which sets
// INTRO_VIDEO_DIR): the first launch through the sign-in card and its
// error shake, the replay over the signed-in app, and reduced motion,
// plus key frames as PNGs.
func TestIntroVideo(t *testing.T) {
	dir := os.Getenv("INTRO_VIDEO_DIR")
	if dir == "" {
		t.Skip("INTRO_VIDEO_DIR not set: the login-shots workflow's intro-video job renders this")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Fatal("ffmpeg: ", err)
	}
	sz := image.Pt(1440, 900)
	win, err := pixeltest.NewWindow(sz.X, sz.Y)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	defer win.Release()
	record := func(name string, fps int, e *introEnv, script func(step func(d time.Duration), keyframe func(string))) {
		t.Helper()
		e.win = win
		out := filepath.Join(dir, name+".mp4")
		cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-f", "rawvideo", "-pix_fmt", "rgba",
			"-s", fmt.Sprintf("%dx%d", sz.X, sz.Y), "-r", fmt.Sprint(fps), "-i", "-",
			"-c:v", "libx264", "-preset", "medium", "-crf", "16", "-pix_fmt", "yuv420p", "-movflags", "+faststart", out)
		cmd.Stderr = os.Stderr
		w, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		frames := 0
		t0 := time.Now()
		step := func(d time.Duration) {
			dt := time.Second / time.Duration(fps)
			for x := time.Duration(0); x < d; x += dt {
				e.frame(dt)
				if _, err := w.Write(e.img.Pix); err != nil {
					t.Fatal(err)
				}
				frames++
			}
		}
		keyframe := func(label string) {
			if e.img == nil {
				return
			}
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%04d-%s.png", name, frames, label)))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, e.img); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
		script(step, keyframe)
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Wait(); err != nil {
			t.Fatalf("ffmpeg: %v", err)
		}
		t.Logf("%s: %d frames at %d fps (%.1f s of video) in %v", out, frames, fps, float64(frames)/float64(fps), time.Since(t0).Round(time.Second))
		e.win = nil
	}

	// the first launch, left alone, then a wrong link pasted on the card
	a := introApp(t, "", nil)
	e := newIntroEnv(t, a, sz)
	record("intro", 60, e, func(step func(time.Duration), kf func(string)) {
		step(400 * time.Millisecond)
		marks := []struct {
			at    time.Duration
			label string
		}{
			{150 * time.Millisecond, "mark-arriving"}, {500 * time.Millisecond, "mark-landing"},
			{900 * time.Millisecond, "clack"}, {1800 * time.Millisecond, "wordmark"},
		}
		last := time.Duration(0)
		for _, m := range marks {
			step(m.at - last)
			last = m.at
			kf(m.label)
		}
		for a.intro != nil && !a.intro.signIn {
			cur := a.intro.cur
			step(time.Second / 60)
			if in := a.intro; in != nil && in.cur != cur && !in.signIn {
				b := in.cur
				step(350 * time.Millisecond)
				kf(fmt.Sprintf("beat%d-entering", b))
				step(1500 * time.Millisecond)
				kf(fmt.Sprintf("beat%d", b))
			}
		}
		step(500 * time.Millisecond)
		kf("signin-entering")
		step(1500 * time.Millisecond)
		kf("signin")
		// a link that isn't one: the card shakes its head
		e.r.Queue(key.EditEvent{Text: "slack://not-a-link"})
		step(300 * time.Millisecond)
		e.r.Queue(key.Event{Name: key.NameReturn, State: key.Press}, key.Event{Name: key.NameReturn, State: key.Release})
		step(120 * time.Millisecond)
		kf("signin-shake")
		step(1600 * time.Millisecond)
		kf("signin-error")
	})

	// the replay over the signed-in app, ending on You're all set
	b, _, _ := fakePrefsApp(t)
	b.motionOverride = -1
	e = newIntroEnv(t, b, sz)
	record("intro-replay", 60, e, func(step func(time.Duration), kf func(string)) {
		step(time.Second)
		kf("app")
		b.replayIntro()
		step(300 * time.Millisecond)
		kf("fading-in")
		for b.intro != nil && b.intro.cur != beatFinal {
			step(time.Second / 60)
		}
		step(400 * time.Millisecond)
		kf("all-set-entering")
		step(1600 * time.Millisecond)
		kf("all-set")
		e.r.Queue(key.Event{Name: key.NameReturn, State: key.Press}, key.Event{Name: key.NameReturn, State: key.Release})
		step(250 * time.Millisecond)
		kf("closing")
		step(time.Second)
		kf("back")
	})

	// reduced motion: the same sequence, fades only
	c := introApp(t, "", nil)
	c.motionOverride = 1
	e = newIntroEnv(t, c, sz)
	record("intro-reduced", 30, e, func(step func(time.Duration), kf func(string)) {
		for i := 0; c.intro != nil && i < 40; i++ {
			step(time.Second)
			if i%4 == 1 {
				kf(fmt.Sprintf("t%02d", i))
			}
		}
		step(time.Second)
		kf("signin")
	})
}
