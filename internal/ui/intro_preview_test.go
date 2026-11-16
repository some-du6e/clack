package ui

import (
	"bytes"
	"image"
	"image/png"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
	"weak"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"

	"github.com/some-du6e/clack/internal/demo"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
	"github.com/some-du6e/clack/internal/ui/uifont"
)

// The renderer in tests: this test binary as TestIntroPreviewChild, or,
// with CLACK_INTRO_PREVIEW_BIN set (the macos and windows-shell jobs), a
// built Clack's own `intro-preview`, the way a release runs it.
func introPreviewTestCommand(args ...string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	if bin := os.Getenv("CLACK_INTRO_PREVIEW_BIN"); bin != "" {
		cmd = exec.Command(bin, append([]string{"intro-preview"}, args...)...)
		configureChild(cmd)
	} else {
		cmd = exec.Command(os.Args[0], append([]string{"-test.run=^TestIntroPreviewChild$", "-test.count=1", "--"}, args...)...)
		cmd.Env = append(os.Environ(), "CLACK_INTRO_PREVIEW_CHILD=1")
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd, nil
}

// TestIntroPreviewChild is the renderer's process in tests.
func TestIntroPreviewChild(t *testing.T) {
	if os.Getenv("CLACK_INTRO_PREVIEW_CHILD") == "" {
		t.Skip("the preview renderer's process (introPreviewTestCommand)")
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	os.Exit(RunIntroPreview(args, os.Stdout, os.Stderr))
}

// usePreviewRenderer turns the renderer on for this test; its reports
// arrive on the channel (nil: no reports asked for).
func usePreviewRenderer(t *testing.T, reports bool) chan *introPreviewReport {
	t.Helper()
	oldCmd, oldHook := introPreviewCommand, introPreviewReportHook
	introPreviewCommand = introPreviewTestCommand
	var ch chan *introPreviewReport
	if reports {
		ch = make(chan *introPreviewReport, 4)
		introPreviewReportHook = func(r *introPreviewReport) { ch <- r }
	}
	t.Cleanup(func() { introPreviewCommand, introPreviewReportHook = oldCmd, oldHook })
	return ch
}

// waitReal draws frames without moving the virtual clock (the beat stays
// where it is) until ok, for at most d of real time: the renderer works
// in real time.
func waitReal(t *testing.T, e *introEnv, what string, d time.Duration, ok func() bool) {
	t.Helper()
	dl := time.Now().Add(d)
	for !ok() {
		if time.Now().After(dl) {
			t.Fatalf("timed out after %v waiting for %s", d, what)
		}
		e.frame(0)
		time.Sleep(5 * time.Millisecond)
	}
}

// writeShot saves img (cropped to r unless r is empty) as dir/name.png.
func writeShot(t *testing.T, dir, name string, img *image.RGBA, r image.Rectangle) {
	t.Helper()
	var out image.Image = img
	if !r.Empty() {
		out = img.SubImage(r.Intersect(img.Bounds()))
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, out); err != nil {
		t.Fatal(err)
	}
}

// ochin is Slack's Ochin theme as ia_theme stores it (theme/account_test.go).
const ochin = `{"primary":{"palette":"ocean"},"highlight1":{"palette":"ocean"},"highlight2":{"palette":"cilantro"},"important":{"palette":"mojito"},"brightness":5,"sidebarInverted":false,"useCustomHex":false}`

// startPreviewBeat replays the introduction over a fresh window (its
// account theme Ochin, Arial, fr-FR), goes to "your Slack, 1:1" and waits
// for the renderer's frame and (reports) its report. base is the
// goroutine count before the introduction.
func startPreviewBeat(t *testing.T, dark bool, reports chan *introPreviewReport) (*App, *introEnv, *introPreviewReport, int) {
	t.Helper()
	a, _, _ := fakePrefsApp(t)
	a.motionOverride = -1
	if dark {
		a.th.SetDark(true)
	}
	// the window's account theme, font and locale, none of them defaults
	a.prefs.AcctTheme = ochin
	a.setAcct(acctFromPrefs(ochin, false))
	a.prefs.ClientTypeface = &uifont.Option{Label: "Arial", Value: uifont.Arial, FontFamily: "Arial"}
	a.prefs.Locale = "fr-FR"
	e := newIntroEnv(t, a, image.Pt(1440, 900))
	e.run(300 * time.Millisecond)
	runtime.GC()
	base := runtime.NumGoroutine() // before the introduction (and its renderer)
	a.replayIntro()
	e.frame(time.Second / 60)
	e.started()
	in := a.intro
	e.key(key.NameRightArrow)
	e.key(key.NameRightArrow)
	if in.cur != beatSlack {
		t.Fatalf("beat %d, want your Slack (%d)", in.cur, beatSlack)
	}
	waitReal(t, e, "the preview's frame", 90*time.Second, func() bool { return in.preview.ready() || in.preview.failed() })
	if in.preview.failed() {
		if w, err := pixeltest.NewWindow(64, 64); err != nil {
			t.Skipf("no GPU for the renderer: %v", err)
		} else {
			w.Release()
		}
		t.Fatal("the preview renderer failed (its log is above)")
	}
	var rep *introPreviewReport
	if reports != nil {
		select {
		case rep = <-reports:
		case <-time.After(15 * time.Second):
			t.Fatal("no report from the renderer")
		}
	}
	return a, e, rep, base
}

// endIntro closes the introduction (Esc, then Enter on You're all set).
func endIntro(t *testing.T, a *App, e *introEnv) {
	t.Helper()
	e.key(key.NameEscape)
	e.run(1500 * time.Millisecond)
	e.key(key.NameReturn)
	e.run(time.Second)
	if a.intro != nil {
		t.Fatal("the introduction is still up")
	}
}

// "It's your Slack, 1:1." shows the real app: the renderer, its own
// process, boots the demo workspace and draws the shell (sidebar sections,
// #general's header, messages and composer, a thread) in the window's
// account theme, font and locale, and the card draws its frame. The
// renderer calls only reads, never badges the Dock, never runs a plugin
// and (macOS) never becomes an app; clicks and keys on the card do nothing;
// after the introduction the preview is gone: unreachable, its goroutines
// ended, no folder left. With LOGIN_SHOTS_DIR set (login-shots, and the
// macos and windows-shell jobs) the beat is saved, light and dark:
// intro-slack-<mode>.png and its card, -card.png.
func TestIntroSlackPreview(t *testing.T) {
	dir := os.Getenv("LOGIN_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		t.Run(mode, func(t *testing.T) {
			reports := usePreviewRenderer(t, true)
			a, e, rep, base := startPreviewBeat(t, dark, reports)
			in := a.intro
			e.run(1200 * time.Millisecond) // the card has risen, the image faded in

			var rec []introDrawn
			in.rec = &rec
			e.frame(time.Second / 60)
			in.rec = nil
			full := false
			for _, d := range rec {
				if d.name == "slack-preview" && d.a > 0.99 {
					full = true
				}
			}
			if !full {
				t.Errorf("the preview isn't fully drawn: %+v", rec)
			}

			// what the renderer drew: the app's own components
			t.Logf("renderer: %.1fs, timeout %v, route %q, sections %q, %d rows, thread %v, list %s, composer %s, unread %v, font %q, locale %q, home %q, appkit %+v",
				rep.Elapsed, rep.Timeout, rep.Route, rep.Sections, rep.Rows, rep.Thread, rep.List, rep.Composer, rep.Unread, rep.Font, rep.Locale, rep.Home, rep.AppKit)
			t.Logf("drawn: %q", rep.Drawn)
			if rep.Timeout {
				t.Error("the renderer never settled")
			}
			if rep.Team != demo.TeamName || !slices.Contains(rep.Sections, "Sales floor") || rep.Rows == 0 {
				t.Errorf("the sidebar: team %q, sections %q, %d rows", rep.Team, rep.Sections, rep.Rows)
			}
			if !rep.Thread || rep.List != "ui.channelList" || rep.Composer != "ui.composerBox" {
				t.Errorf("#general: thread %v, list %s, composer %s (want the real list, composer and a thread)", rep.Thread, rep.List, rep.Composer)
			}
			for _, want := range []string{demo.TeamName, "general"} {
				if !slices.Contains(rep.Drawn, want) {
					t.Errorf("%q isn't drawn", want)
				}
			}
			for _, s := range rep.Drawn {
				if strings.Contains(s, "new message") {
					t.Errorf("#general opens with a %q pill", s)
				}
			}
			if rep.Unread {
				t.Error("#general starts unread")
			}
			// the window's theme: its mode, its account theme's chrome (the
			// demo account's own is the default aubergine), its font and locale
			bg, ch := a.th.C.Bg, a.th.C.ChromeA
			if rep.Dark != dark || rep.Bg != [3]uint8{bg.R, bg.G, bg.B} || rep.Chrome != [3]uint8{ch.R, ch.G, ch.B} {
				t.Errorf("the renderer's theme: dark %v, panel %v, chrome %v; the window's: dark %v, panel %v, chrome %v", rep.Dark, rep.Bg, rep.Chrome, dark, bg, ch)
			}
			if rep.Font != uifont.Arial || rep.Locale != "fr-FR" {
				t.Errorf("the renderer's font %q and locale %q, want the window's (arial, fr-FR)", rep.Font, rep.Locale)
			}
			if len(rep.Dock) != 0 {
				t.Errorf("the renderer asked for Dock badges %q", rep.Dock)
			}
			if rep.Taut || rep.Slick {
				t.Errorf("the renderer started a plugin system (taut %v, slick %v)", rep.Taut, rep.Slick)
			}
			if runtime.GOOS == "darwin" {
				if k := rep.AppKit; k == nil || k.NSApp || k.Policy == 0 || k.Active {
					t.Errorf("the renderer is an app to AppKit (Dock icon, activation): %+v", k)
				}
			}
			var calls []string
			for m, n := range rep.Calls {
				calls = append(calls, m)
				if n > 0 && !slices.Contains(previewReads, m) {
					t.Errorf("the renderer called %s ×%d (not a read the preview may make)", m, n)
				}
			}
			sort.Strings(calls)
			t.Logf("calls: %q; refused (never sent): %q", calls, rep.Refused)

			if dir != "" {
				win, err := pixeltest.NewWindow(e.sz.X, e.sz.Y)
				if err != nil {
					t.Fatalf("headless window: %v", err)
				}
				e.win = win
				e.frame(time.Second / 60)
				e.win = nil
				win.Release()
				writeShot(t, dir, "intro-slack-"+mode, e.img, image.Rectangle{})
				writeShot(t, dir, "intro-slack-"+mode+"-card", e.img, in.preview.card)
			}

			// inert: clicks on the card, keys and text change nothing
			card := in.preview.card
			for _, pt := range []image.Point{
				{card.Min.X + card.Dx()*45/100, card.Max.Y - card.Dy()/14},
				{card.Min.X + card.Dx()*12/100, card.Min.Y + card.Dy()/2},
				{card.Max.X - card.Dx()/8, card.Min.Y + card.Dy()/3},
			} {
				fp := f32.Pt(float32(pt.X), float32(pt.Y))
				e.r.Queue(
					pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: fp},
					pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: fp},
					pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: fp},
				)
				e.frame(time.Second / 60)
			}
			e.r.Queue(key.EditEvent{Text: "hello from the introduction"})
			e.r.Queue(key.Event{Name: "K", Modifiers: key.ModShortcut, State: key.Press}, key.Event{Name: "K", Modifiers: key.ModShortcut, State: key.Release})
			e.frame(time.Second / 60)
			e.frame(time.Second / 60)
			if len(a.overlays) != 0 || in.cur != beatSlack {
				t.Errorf("input on the card: %d overlays, beat %d", len(a.overlays), in.cur)
			}

			// gone with the introduction
			wp := weak.Make(in.preview)
			in = nil
			endIntro(t, a, e)
			dl := time.Now().Add(15 * time.Second)
			for {
				runtime.GC()
				n := runtime.NumGoroutine()
				if wp.Value() == nil && n <= base {
					break
				}
				if time.Now().After(dl) {
					buf := make([]byte, 1<<20)
					t.Fatalf("after the introduction: preview reachable %v, %d goroutines (%d before):\n%s",
						wp.Value() != nil, n, base, buf[:runtime.Stack(buf, true)])
				}
				e.frame(0)
				time.Sleep(20 * time.Millisecond)
			}
			if ms, _ := filepath.Glob(filepath.Join(os.TempDir(), previewDirPrefix+"*")); len(ms) != 0 {
				t.Errorf("renderer folders left: %q", ms)
			}
		})
	}
}

// treeState is every file under dir: its bytes and modification time.
func treeState(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		v := fi.ModTime().UTC().Format(time.RFC3339Nano)
		if !d.IsDir() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			v += " " + string(b)
		}
		out[rel] = v
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The renderer never touches the user's config folder: with CLACK_HOME
// holding an enabled Taut plugin that writes its storage on start, its
// config, user CSS, stored data and secrets, creds.json and Slick's
// switches, nothing in it changes (bytes, mtimes, no new file), the
// renderer's CLACK_HOME is its own, and no plugin system ran.
func TestIntroPreviewPluginHome(t *testing.T) {
	home := t.TempDir()
	plugin := `(() => {
const TautPlugin = globalThis.TautPlugin, opt = globalThis.TautOpt;
class Marker extends TautPlugin {
	static id = 'Marker'; static pluginName = 'Marker'; static description = ''; static authors = []
	static defaultConfig = { enabled: true }
	async start() {
		this.s = this.api.storage.store('k', {n: 0})
		await this.s.ready
		await this.s.update((v) => ({n: v.n + 1}))
		await this.api.secrets.store('key', '').set('from the renderer')
	}
}
return Marker
})()`
	files := map[string]string{
		"taut/plugins/Marker.js":        plugin,
		"taut/config.json":              `{"plugins":{"Marker":{"enabled":true}}}`,
		"taut/user.css":                 `.p-ia4_client { outline: 1px solid red }`,
		"taut/storage/plugin~Old~store": `{"a":"b"}`,
		"taut/secrets/plugin~Old~sec":   `{"token":"xoxc-user"}`,
		"creds.json":                    `{"token":"xoxc-user","cookie":"xoxd-user"}`,
		"slick/switches.json":           `{"enabled":["SomeTheme"]}`,
		"slick/settings.json":           `{}`,
	}
	for name, body := range files {
		p := filepath.Join(home, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour) // mtimes a write would move
	_ = filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error { return os.Chtimes(p, old, old) })
	before := treeState(t, home)
	t.Setenv("CLACK_HOME", home)
	t.Setenv("YOLOSLACK_HOME", home)

	reports := usePreviewRenderer(t, true)
	a, e, rep, _ := startPreviewBeat(t, false, reports)
	if rep.Taut || rep.Slick {
		t.Errorf("the renderer started a plugin system (taut %v, slick %v)", rep.Taut, rep.Slick)
	}
	if rep.Home == "" || rep.Home == home || !strings.HasPrefix(filepath.Base(filepath.Dir(rep.Home)), previewDirPrefix) {
		t.Errorf("the renderer's CLACK_HOME is %q, not its own folder", rep.Home)
	}
	endIntro(t, a, e)
	dl := time.Now().Add(15 * time.Second)
	for {
		ms, _ := filepath.Glob(filepath.Join(os.TempDir(), previewDirPrefix+"*"))
		if len(ms) == 0 {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("renderer folders left: %q", ms)
		}
		e.frame(0)
		time.Sleep(20 * time.Millisecond)
	}
	after := treeState(t, home)
	for k, v := range after {
		if b, ok := before[k]; !ok {
			t.Errorf("the renderer made %s in the user's folder", k)
		} else if b != v {
			t.Errorf("the renderer changed %s in the user's folder", k)
		}
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			t.Errorf("the renderer removed %s from the user's folder", k)
		}
	}
}

// Quitting while a renderer still works stops it and removes its folder
// before the window goes: the open introduction's, and one an
// introduction just closed (Esc) left exiting. Run's DestroyEvent is what
// calls shutdownIntro.
func TestIntroPreviewQuit(t *testing.T) {
	src, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(src, []byte("case app.DestroyEvent:"))
	if i < 0 || !bytes.Contains(src[i:min(len(src), i+400)], []byte("a.shutdownIntro()")) {
		t.Error("Run's DestroyEvent doesn't call a.shutdownIntro()")
	}
	usePreviewRenderer(t, false)
	for _, closedFirst := range []bool{false, true} {
		a, _, _ := fakePrefsApp(t)
		a.motionOverride = -1
		e := newIntroEnv(t, a, image.Pt(1280, 800))
		e.run(200 * time.Millisecond)
		a.replayIntro()
		e.frame(time.Second / 60)
		e.started()
		p := a.intro.preview
		var dir string
		waitReal(t, e, "the renderer running", 60*time.Second, func() bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			dir = p.dir
			return p.proc != nil || p.fail
		})
		if p.fail {
			t.Skip("the renderer didn't start")
		}
		if closedFirst { // the introduction is gone; its renderer is still exiting
			a.intro.close()
			a.intro = nil
		}
		a.shutdownIntro()
		p.mu.Lock()
		proc := p.proc
		p.mu.Unlock()
		if proc != nil {
			t.Errorf("closed first %v: the renderer is still running after shutdown", closedFirst)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("closed first %v: the renderer's folder %s is still there (%v)", closedFirst, dir, err)
		}
	}
}

// A renderer whose window has gone (its stdout's reader closed before the
// frame) isn't killed by SIGPIPE: the write fails, it tears down, removes
// its own folder and exits by itself.
func TestIntroPreviewWindowGone(t *testing.T) {
	dir, err := os.MkdirTemp("", previewDirPrefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removePreviewDir(dir) })
	if err := os.WriteFile(filepath.Join(dir, "ui.json"), []byte(`{"last_channel":"`+demo.GeneralID+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	logf, err := os.CreateTemp(t.TempDir(), "renderer-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer logf.Close()
	cmd, _ := introPreviewTestCommand("-dir", dir, "-w", "330", "-h", "165", "-scale", "0.275")
	base := cmd.Env
	if base == nil {
		base = os.Environ()
	}
	cmd.Env = rendererEnv(base, filepath.Join(dir, "home"))
	cmd.Stderr = logf
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stdout.Close() // the window is gone before the frame
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(90 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the renderer didn't exit after its window went")
	}
	log, _ := os.ReadFile(logf.Name())
	t.Logf("renderer log:\n%s", log)
	if bytes.Contains(log, []byte("headless window")) {
		t.Skip("no GPU for the renderer")
	}
	if !bytes.Contains(log, []byte("intro-preview:")) {
		t.Errorf("the renderer didn't report its failed write")
	}
	if st, ok := cmd.ProcessState.Sys().(interface{ Signaled() bool }); ok && st.Signaled() {
		t.Errorf("the renderer was killed by a signal (%v), so its cleanup never ran", cmd.ProcessState)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the renderer's folder %s is still there (%v)", dir, err)
	}
}
