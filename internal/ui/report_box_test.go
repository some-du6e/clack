package ui

// The report box (report_box.go) and the opt-in crash reports, on the
// fake Slack and a fake Sentry (internal/sentry/sentrytest, on loopback:
// nothing here reaches Sentry or Slack).

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/sentry"
	"github.com/some-du6e/clack/internal/sentry/sentrytest"
	"github.com/some-du6e/clack/internal/ui/inspect"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// reportEnv is the client on the fake, the inspect tool's gate off, a
// message with a reaction in view, and a fake Sentry.
type reportEnv struct {
	*perfEnv
	fake   *sentrytest.Server
	ch, ts string
	pill   messages.ReactionRect
	leaks  []string // what no report may carry: the message, its sender's names, the session's secrets, ids
	mu     sync.Mutex
	calls  []string
}

const reportMessage = "the launch moved to friday, keep it quiet"

func newReportEnv(t *testing.T, canSend bool) *reportEnv {
	t.Helper()
	t.Setenv("CLACK_INSPECT", "")
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	ids := fs.UserIDs()
	re := &reportEnv{ch: fs.ChannelIDs()[0], fake: sentrytest.Start(t)}
	re.ts, _ = fs.PostQuiet(re.ch, ids[1], reportMessage)
	if !fs.ReactQuiet(re.ch, re.ts, "tada", ids[2]) {
		t.Fatal("no message to react to")
	}
	fs.SetHook(func(method string, _ url.Values) (any, bool) {
		re.mu.Lock()
		re.calls = append(re.calls, method)
		re.mu.Unlock()
		return nil, false
	})
	e := startEnv(t, fs, t.TempDir())
	re.perfEnv = e
	if e.a.insp != nil {
		t.Fatal("the inspect tool is on: the report box must work without its gate")
	}
	if canSend {
		c, err := sentry.New(re.fake.DSN(), "v0.0.0-test")
		if err != nil {
			t.Fatal(err)
		}
		e.a.opts.Sentry = c
	}
	e.waitReady()
	e.openChannel(re.ch)
	e.until("history", 20*time.Second, func() bool { return e.loadedMessages(re.ch) > 0 })
	e.toLatest()
	e.until("the reaction pill", 20*time.Second, func() bool {
		cv := e.a.convos[re.ch+"/"]
		if cv == nil {
			return false
		}
		l, ok := cv.list.(channelList)
		if !ok {
			return false
		}
		for _, r := range l.ReactionRects() {
			if r.TS == re.ts && r.Name == "tada" {
				re.pill = r
				return true
			}
		}
		return false
	})
	cr := fs.Creds()
	re.leaks = []string{reportMessage, "launch moved", "friday", "keep it quiet", re.ch, re.ts, "tada", cr.Token, cr.Cookie, e.home}
	if h, err := os.UserHomeDir(); err == nil && len(h) > 3 {
		re.leaks = append(re.leaks, h)
	}
	if u, ok := e.a.Core().S.RawUser(ids[1]); ok {
		for _, n := range []string{u.Name, u.RealName, u.Profile.DisplayName, u.Profile.RealName} {
			if len(n) > 2 {
				re.leaks = append(re.leaks, n)
			}
		}
	}
	return re
}

// hyperR presses the report box's shortcut.
func (e *perfEnv) hyperR() {
	f := reportFilter()
	e.key(f.Name, f.Required)
}

// openBox points at the pill, presses Hyper+R and runs the capture's
// frames: one recorded, the pick's (shot after it, as App.Run does after
// every frame), then the box.
func (e *reportEnv) openBox(t *testing.T) *popups.ReportBox {
	t.Helper()
	r := e.pill.R
	e.move(f32.Pt(float32(r.Min.X+r.Dx()/2), float32(r.Min.Y+r.Dy()/2)))
	e.frame()
	e.hyperR()
	e.frame() // Hyper+R: the capture starts, this frame is recorded
	if !e.a.reportActive() {
		t.Fatal("Hyper+R started no capture")
	}
	e.frame() // the pick: what the pointer is over in the recorded frame
	e.a.inspectAfterFrame(&e.ops, e.size)
	e.until("the report box", 10*time.Second, func() bool { return e.a.rbox != nil && !e.a.rbox.Dismissed() })
	if e.a.reportActive() {
		t.Error("the capture's tool still runs with the box open")
	}
	return e.a.rbox
}

// called lists the methods the fake Slack was called with that start
// with prefix.
func (e *reportEnv) called(prefix string) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, m := range e.calls {
		if strings.HasPrefix(m, prefix) {
			out = append(out, m)
		}
	}
	return out
}

// without is xs less x.
func without(xs []string, x string) []string {
	var out []string
	for _, s := range xs {
		if s != x {
			out = append(out, s)
		}
	}
	return out
}

// rowsOf are a box's rows by key.
func rowsOf(b *popups.ReportBox) map[string]string {
	out := map[string]string{}
	for _, r := range b.Rows() {
		out[r.Key] = r.Value
	}
	return out
}

// noLeaks fails for each of e.leaks in what.
func (e *reportEnv) noLeaks(t *testing.T, what string, b []byte) {
	t.Helper()
	for _, l := range e.leaks {
		if l != "" && bytes.Contains(b, []byte(l)) {
			t.Errorf("%s carries %q", what, l)
		}
	}
}

// Hyper+R over the reaction pill, the inspect tool off: the box shows
// what the inspect tool's pick would report about the pill (its component,
// element, region, page, ids as tags) and has a shot of the whole window,
// off until ticked. Nothing goes out until Send, and Send sends one
// feedback whose comment is the box's and whose report holds no message
// text, name, token, cookie, id or home path. The pill never sees the
// keys or a click.
func TestReportBoxHyperR(t *testing.T) {
	shots, _ := fakeShots(t)
	e := newReportEnv(t, true)
	box := e.openBox(t)
	rows := rowsOf(box)
	pr := e.pill.R
	for _, w := range []struct{ key, want string }{
		{"component", "UnstyledButton"},
		{"element", "button.c-button-unstyled.c-reaction[data-qa=reactji]"},
		{"rect", "x " + itoa(pr.Min.X) + " y " + itoa(pr.Min.Y) + " w " + itoa(pr.Dx()) + " h " + itoa(pr.Dy()) + " dp"},
		{"region", "primary"},
		{"page", "c/ch:* (public)"},
		{"ids", "*channelId=ch:*msg.ts=ts:*"},
		{"window", "light · * · 1440x900 dp at 1x"},
		{"clack", "* · " + runtime.GOOS + "/" + runtime.GOARCH + " · *"},
	} {
		if got := rows[w.key]; !globMatch(w.want, got) {
			t.Errorf("box row %s = %q, want %q", w.key, got, w.want)
		}
	}
	if _, ok := rows["capture"]; ok {
		t.Errorf("a pick's report has a capture row: %q", rows["capture"])
	}
	if *shots != 1 || len(fakeShotRegions) != 1 || fakeShotRegions[0] != (image.Rectangle{Max: e.size}) {
		t.Fatalf("shots %d of %v, want one of the whole window %v", *shots, fakeShotRegions, e.size)
	}
	if box.Shot() != nil {
		t.Error("the screenshot is attached before the box is ticked")
	}
	if box.Unavailable() || box.CanSend() {
		t.Errorf("unavailable %v, Send enabled %v with no comment", box.Unavailable(), box.CanSend())
	}
	box.SetComment("   ")
	if box.CanSend() {
		t.Error("Send enabled for a comment of spaces")
	}

	// nothing goes out before Send: not on open, not while typing
	box.SetComment("the tada pill sits 1px low")
	e.settle(400 * time.Millisecond)
	if n := len(e.fake.Envelopes()); n != 0 || box.SendCalls() != 0 {
		t.Fatalf("%d envelopes (%d sends) before Send", n, box.SendCalls())
	}
	if !box.CanSend() {
		t.Fatal("Send disabled with a comment")
	}
	box.Send()
	envs := e.fake.Wait(t, 1, 15*time.Second)
	e.until("the box closing after the send", 10*time.Second, func() bool { return box.Dismissed() })
	if !strings.Contains(e.a.toast.text, "Report sent") {
		t.Errorf("toast %q", e.a.toast.text)
	}
	e.settle(200 * time.Millisecond)
	if n := len(e.fake.Envelopes()); n != 1 {
		t.Fatalf("%d envelopes for one Send", n)
	}
	env := envs[0]
	if len(env.Items) != 1 || env.Items[0].Type() != "feedback" {
		t.Fatalf("items %v: want one feedback, no screenshot (unticked)", env.Items)
	}
	ev, err := env.Items[0].JSON()
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := ev["contexts"].(map[string]any)
	fb, _ := ctx["feedback"].(map[string]any)
	rep, _ := ctx["report"].(map[string]any)
	if fb["message"] != "the tada pill sits 1px low" || rep["component"] != "UnstyledButton" || rep["region"] != "primary" {
		t.Errorf("feedback %v, report %v", fb, rep)
	}
	if _, ok := rep["screenshot"]; ok {
		t.Error("a screenshot row with no screenshot")
	}
	// the comment itself names the reaction: that's the reporter's own
	// words; nothing else of the message may be there
	e.leaks = without(e.leaks, "tada")
	e.noLeaks(t, "the report", env.Raw)
	if got := e.called("reactions."); len(got) != 0 {
		t.Errorf("the capture reached the pill: %v", got)
	}
}

// With the screenshot ticked it goes with the report as a PNG of the
// whole window, and the report says so.
func TestReportBoxScreenshot(t *testing.T) {
	fakeShots(t)
	e := newReportEnv(t, true)
	box := e.openBox(t)
	box.SetShot(true)
	if box.Shot() == nil {
		t.Fatal("ticked, the box has no screenshot")
	}
	box.SetComment("see the screenshot")
	box.Send()
	env := e.fake.Wait(t, 1, 15*time.Second)[0]
	if len(env.Items) != 2 || env.Items[1].Type() != "attachment" || env.Items[1].Header["filename"] != "window.png" {
		t.Fatalf("items %v, want the feedback and window.png", env.Items)
	}
	img, err := png.Decode(bytes.NewReader(env.Items[1].Payload))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Size() != e.size {
		t.Errorf("the screenshot is %v, want the window's %v", img.Bounds().Size(), e.size)
	}
	ev, _ := env.Items[0].JSON()
	rep, _ := ev["contexts"].(map[string]any)["report"].(map[string]any)
	tags, _ := ev["tags"].(map[string]any)
	if rep["screenshot"] != "the window, nothing blurred" || tags["screenshot"] != "yes" {
		t.Errorf("report %v, tags %v", rep, tags)
	}
}

// "Blur parts of it" is the inspect review over the box's screenshot: a
// rect dragged over it, then Enter, and the screenshot the box sends has
// nothing of what was under it; Esc in a second review leaves it as it
// was.
func TestReportBoxBlur(t *testing.T) {
	// the fake shot: white, with a black block in the middle
	prev := inspect.Shoot
	inspect.Shoot = func(_ op.CallOp, region image.Rectangle, k float32) (*image.RGBA, error) {
		img := image.NewRGBA(image.Rectangle{Max: inspect.ShotSize(region, k)})
		draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
		b := img.Bounds()
		draw.Draw(img, image.Rect(b.Dx()/2-40, b.Dy()/2-40, b.Dx()/2+40, b.Dy()/2+40), image.NewUniform(color.Black), image.Point{}, draw.Src)
		return img, nil
	}
	t.Cleanup(func() { inspect.Shoot = prev })
	e := newReportEnv(t, true)
	box := e.openBox(t)
	box.SetShot(true)
	shot := box.Shot()
	mid := image.Pt(shot.Bounds().Dx()/2, shot.Bounds().Dy()/2)
	if c := shot.RGBAAt(mid.X, mid.Y); c.R != 0 {
		t.Fatalf("the fake shot's middle is %v", c)
	}
	box.Blur()
	e.frame()
	if !e.a.rpt.Reviewing() {
		t.Fatal("Blur parts of it opened no review")
	}
	if e.a.toast.text != "" && strings.Contains(e.a.toast.text, "Inspect") {
		t.Errorf("toast %q", e.a.toast.text)
	}
	e.frame()
	v := e.a.rpt.ReviewView()
	if v.Empty() {
		t.Fatal("the review drew no shot")
	}
	// drag over the block (the view shows the shot scaled into the window)
	toView := func(p image.Point) image.Point {
		sb := shot.Bounds()
		return image.Pt(v.Min.X+p.X*v.Dx()/sb.Dx(), v.Min.Y+p.Y*v.Dy()/sb.Dy())
	}
	from, to := toView(mid.Sub(image.Pt(60, 60))), toView(mid.Add(image.Pt(60, 60)))
	e.drag(from, to)
	e.release(to)
	e.frame()
	if marks, _, ok := e.a.rpt.Marks(); !ok || len(marks) != 1 {
		t.Fatalf("marks %v (review %v) after the drag", marks, ok)
	}
	if box.CanSend() {
		t.Error("Send enabled with the review over the window")
	}
	e.settle(450 * time.Millisecond) // a region's review takes Enter once it has been on screen a while
	e.key(key.NameReturn, 0)
	e.until("the blurred shot back in the box", 10*time.Second, func() bool { return box.Blurred() == 1 })
	if e.a.rpt.Active() {
		t.Error("the review still runs")
	}
	got := box.Shot()
	if got == shot {
		t.Fatal("the box kept the shot as it was")
	}
	for _, p := range []image.Point{mid, mid.Add(image.Pt(-35, -35)), mid.Add(image.Pt(35, 35))} {
		if c := got.RGBAAt(p.X, p.Y); c.R < 0xf0 || c.G < 0xf0 || c.B < 0xf0 {
			t.Errorf("the blurred shot at %v is %v: the block under the rect is still there", p, c)
		}
	}
	// a second review left with Esc changes nothing
	box.Blur()
	e.frame()
	e.frame()
	e.key(key.NameEscape, 0)
	e.frame()
	if e.a.rpt.Active() || box.Shot() != got || box.Blurred() != 1 {
		t.Errorf("after Esc: review %v, shot kept %v, blurred %d", e.a.rpt.Active(), box.Shot() == got, box.Blurred())
	}
	box.SetComment("blurred")
	box.Send()
	env := e.fake.Wait(t, 1, 15*time.Second)[0]
	if len(env.Items) != 2 || env.Items[1].Type() != "attachment" {
		t.Fatalf("items %v, want the feedback and the blurred screenshot", env.Items)
	}
	ev, _ := env.Items[0].JSON()
	rep, _ := ev["contexts"].(map[string]any)["report"].(map[string]any)
	if rep["screenshot"] != "the window, 1 part blurred by the reporter" {
		t.Errorf("report's screenshot row %q", rep["screenshot"])
	}
	img, err := png.Decode(bytes.NewReader(env.Items[1].Payload))
	if err != nil {
		t.Fatal(err)
	}
	if r, _, _, _ := img.At(mid.X, mid.Y).RGBA(); r < 0xf000 {
		t.Error("the sent screenshot still has the block the reporter blurred")
	}
}

// A build without the DSN (this test binary, a local build): the box
// opens and shows what a report would hold, says it can't be sent, and
// Send stays off.
func TestReportBoxUnavailable(t *testing.T) {
	fakeShots(t)
	e := newReportEnv(t, false)
	box := e.openBox(t)
	if !box.Unavailable() {
		t.Fatal("a build with no DSN says reports can be sent")
	}
	if rows := rowsOf(box); rows["component"] != "UnstyledButton" {
		t.Errorf("the unavailable box shows %v", rows)
	}
	box.SetComment("hello")
	if box.CanSend() {
		t.Error("Send enabled in a build that can't send")
	}
	box.Send()
	e.settle(300 * time.Millisecond)
	if box.SendCalls() != 0 || len(e.fake.Envelopes()) != 0 || box.Dismissed() {
		t.Errorf("sends %d, envelopes %d, closed %v", box.SendCalls(), len(e.fake.Envelopes()), box.Dismissed())
	}
}

// A failed send keeps the box with the error, and the comment; the next
// Send goes again.
func TestReportBoxSendFails(t *testing.T) {
	fakeShots(t)
	e := newReportEnv(t, true)
	box := e.openBox(t)
	e.fake.Answer(500)
	box.SetComment("try twice")
	box.Send()
	e.until("the error", 15*time.Second, func() bool { return box.Err() != "" })
	if box.Dismissed() {
		t.Fatal("a failed send closed the box")
	}
	e.fake.Answer(200)
	box.Send()
	e.until("the box closing", 15*time.Second, func() bool { return box.Dismissed() })
	if n := len(e.fake.Envelopes()); n != 2 {
		t.Errorf("%d envelopes for two sends", n)
	}
}

// crashOutput is a crash.log's crash for the next start, as a dev build
// writes it: a panic quoting a message's text, a token, a channel id,
// and paths under the home directory.
func crashOutput(home string) string {
	return `panic: render "the launch moved to friday, keep it quiet" in C0123ABCD45 (xoxc-111-222-333)

goroutine 42 [running]:
github.com/some-du6e/clack/internal/ui/messages.(*List).layout(0xc000123400)
	` + home + `/src/clack/internal/ui/messages/list.go:812 +0x1f4
created by github.com/some-du6e/clack/internal/ui.(*App).Run in goroutine 1
	` + home + `/src/clack/internal/ui/app.go:700 +0x2a5
`
}

// crashApp is an App of its own (no window, no Slack) sending to fake,
// its config in dir.
func crashApp(t *testing.T, dir string, fake *sentrytest.Server) *App {
	t.Helper()
	c, err := sentry.New(fake.DSN(), "v0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	images := imgcache.New(func(context.Context, string) ([]byte, error) { return nil, errors.New("offline") }, "", nil)
	a := New(Options{ConfigDir: dir, NoRealtime: true, Sentry: c}, images)
	t.Cleanup(a.release)
	return a
}

// Send crash reports is off by default, and off nothing is sent, ever:
// not the crash, and not later once it's turned on (crash.log started
// afresh at that start: the crash isn't kept). On, the next start sends
// one event, scrubbed: the crash's type, its message with what it quoted
// left out, the crashing goroutine's frames, no home path.
func TestCrashReportsOptIn(t *testing.T) {
	home, _ := os.UserHomeDir()
	if len(home) < 3 {
		home = "/home/someone"
	}
	fake := sentrytest.Start(t)
	dir := t.TempDir()
	crash := sentry.Crash{Output: crashOutput(home), Header: "clack v0.0.0-test linux/amd64 pid 7, started 2026-11-24T10:00:00Z",
		Exit: "applog: previous run ended without saying why (clack v0.0.0-test linux/amd64 pid 7, started 2026-11-24T10:00:00Z): no exit line"}

	// off (the default): nothing
	a := crashApp(t, dir, fake)
	if a.prefs.CrashReports || a.crashOn.Load() {
		t.Fatal("crash reports are on by default")
	}
	if a.ReportCrash(crash) {
		t.Error("ReportCrash sent with the pref off")
	}
	// the next start, the pref on now, has no crash to send (crash.log
	// started afresh when the crash was read)
	a.setCrashReports(true)
	a.flushPrefs() // ui.json written before the next start reads it
	b := crashApp(t, dir, fake)
	if !b.crashOn.Load() {
		t.Fatal("ui.json didn't keep crash_reports")
	}
	if b.ReportCrash(sentry.Crash{Exit: crash.Exit}) {
		t.Error("a start with no crash sent a report")
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(fake.Envelopes()); n != 0 {
		t.Fatalf("%d envelopes with the pref off, or no crash", n)
	}

	// on: the crash's next start sends one event
	if !b.ReportCrash(crash) {
		t.Fatal("ReportCrash didn't send with the pref on")
	}
	envs := fake.Wait(t, 1, 10*time.Second)
	if len(envs) != 1 || len(envs[0].Items) != 1 || envs[0].Items[0].Type() != "event" {
		t.Fatalf("envelopes %v", envs)
	}
	ev, err := envs[0].Items[0].JSON()
	if err != nil {
		t.Fatal(err)
	}
	exc, _ := ev["exception"].(map[string]any)
	vals, _ := exc["values"].([]any)
	if ev["level"] != "fatal" || len(vals) != 1 {
		t.Fatalf("event %v", ev)
	}
	x, _ := vals[0].(map[string]any)
	if x["type"] != "panic" || x["value"] != `render "…" in <id> (xoxc-REDACTED)` {
		t.Errorf("exception %q %q", x["type"], x["value"])
	}
	st, _ := x["stacktrace"].(map[string]any)
	if fr, _ := st["frames"].([]any); len(fr) != 2 {
		t.Errorf("frames %v", st["frames"])
	}
	raw := envs[0].Raw
	for _, bad := range []string{"launch moved", "friday", "keep it quiet", "xoxc-111", "C0123ABCD45", home + "/", "0xc000123400"} {
		if bytes.Contains(raw, []byte(bad)) {
			t.Errorf("the crash report carries %q", bad)
		}
	}

	// off again: nothing more
	b.setCrashReports(false)
	if b.ReportCrash(crash) {
		t.Error("sent after turning it off")
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(fake.Envelopes()); n != 1 {
		t.Errorf("%d envelopes after turning it off", n)
	}
}

// Preferences > Clack's third row is Send crash reports, off by default;
// it's greyed in a build that can't send, and live in one that can.
func TestCrashReportsRow(t *testing.T) {
	a, _, _ := fakePrefsApp(t)
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}}
	defer p.close()
	row := func() prefsDrawn {
		for _, r := range pageRows(t, a, p, clackTabID) {
			if r.Key == "crash_reports" {
				return r
			}
		}
		t.Fatal("no Send crash reports row")
		return prefsDrawn{}
	}
	if r := row(); r.Kind != "check" || r.On || r.Enabled {
		t.Errorf("no DSN: %+v, want an unticked, greyed box", r)
	}
	p.clicks["crash_reports"].Click()
	row()
	if a.prefs.CrashReports {
		t.Error("the greyed row turned crash reports on")
	}
	fake := sentrytest.Start(t)
	c, err := sentry.New(fake.DSN(), "v0.0.0-test")
	if err != nil {
		t.Fatal(err)
	}
	a.opts.Sentry = c
	if r := row(); !r.Enabled || r.On {
		t.Errorf("with a DSN: %+v, want an unticked live box", r)
	}
	p.clicks["crash_reports"].Click()
	row() // the click's frame
	if !a.prefs.CrashReports || !a.crashOn.Load() {
		t.Fatalf("after a click: pref %v, ReportCrash's %v", a.prefs.CrashReports, a.crashOn.Load())
	}
	if r := row(); !r.On {
		t.Errorf("after a click: %+v, want it ticked", r)
	}
}
