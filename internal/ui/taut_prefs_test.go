package ui

// Preferences > Taut and the Taut wiring, over a synthetic workspace (no
// live Slack, no window). TestTautNoPluginsStartup always runs;
// TestTautPrefsRender needs Taut's plugins built by the ui-taut workflow
// (TAUT_PLUGINS_DIR) and writes PNGs to TAUT_SHOTS_DIR when set.

import (
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

const (
	ttSelf = "USELF0001"
	ttTeam = "T0266FRGM" // Hack Club, so Hack Club–only plugins run
	ttGen  = "C0GENERAL1"
)

// ttApp is a ui.App over an offline core with a synthetic workspace, its
// Taut started through the real startTaut wiring under CLACK_HOME=home.
func ttApp(t *testing.T, home string) (*App, *core.App) {
	t.Helper()
	t.Setenv("CLACK_HOME", home)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: ttSelf, Team: ttTeam,
		TeamURL: "http://127.0.0.1:1/", EnterpriseURL: "http://127.0.0.1:1/"})
	s := store.New("")
	s.Lock()
	s.TeamID, s.TeamName, s.TeamDomain = ttTeam, "Synthetic Club", "hackclub"
	s.Unlock()
	s.PutUsers([]slack.User{{ID: ttSelf, Name: "me", RealName: "Test Self", Profile: slack.Profile{DisplayName: "self", RealName: "Test Self"}}})
	s.PutConv(slack.Conversation{ID: ttGen, Name: "general", IsChannel: true, IsMember: true, IsGeneral: true, NumMembers: 3})
	ca := core.New(c, s)
	if u, ok := s.User(ttSelf); ok {
		ca.S.Self = *u
	}
	ca.ServeOffline()
	images := imgcache.New(func(ctx context.Context, url string) ([]byte, error) { return nil, errors.New("offline") }, "", nil)
	a := New(Options{ConfigDir: t.TempDir(), NoRealtime: true}, images)
	sh := theme.NewShaper()
	a.th.SetShapers(sh, sh)
	b := &backend{core: ca, app: a}
	a.data = b
	return a, ca
}

func TestTautNoPluginsStartup(t *testing.T) {
	a, _ := ttApp(t, t.TempDir())
	t0 := time.Now()
	a.data.startTaut(a)
	d := time.Since(t0)
	tt := a.Taut()
	if tt == nil {
		t.Fatal("taut didn't start")
	}
	t.Cleanup(func() { a.data.stopTaut() })
	t.Logf("startTaut with no plugins: %v", d)
	if tt.Started() {
		t.Fatal("the JS runtime started with no plugins")
	}

	// Natives run directly when nothing patches the thunk; still no JS.
	ran := map[string]bool{}
	for _, name := range []string{"showNotification", "setUserPrefByApi", "openPrefsModal", "joinChannel"} {
		name := name
		if _, err := taut.Dispatch(context.Background(), tt, name, map[string]any{"x": 1},
			func(ctx context.Context, args map[string]any) (any, error) { ran[name] = true; return nil, nil }); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(ran) != 4 {
		t.Errorf("natives ran: %v", ran)
	}

	// Preferences > Taut reads the model without starting JS.
	s := tt.Settings()
	n := 0
	for _, c := range s.Categories {
		n += len(c.Plugins)
	}
	if n != 0 || s.RuntimeError != "" {
		t.Errorf("settings: %d plugins, runtime error %q", n, s.RuntimeError)
	}
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: pluginsTabID}
	gtx := ttContext(new(op.Ops), image.Pt(800, 560))
	a.slackcFrame(gtx)
	p.layout(gtx)
	p.close()
	if tt.Started() {
		t.Fatal("drawing Preferences > Taut started the JS runtime")
	}
	if a.tautGen != 0 {
		t.Errorf("Attached ran without a runtime (tautGen %d)", a.tautGen)
	}
}

func ttContext(ops *op.Ops, sz image.Point) layout.Context {
	return layout.Context{Ops: ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(sz)}
}

// ttRenderer draws frames into a headless GPU window.
type ttRenderer struct {
	t   *testing.T
	a   *App
	win *headless.Window
	sz  image.Point
	dir string
}

func (r *ttRenderer) frame(w func(gtx layout.Context)) *image.RGBA {
	ops := new(op.Ops)
	gtx := ttContext(ops, r.sz)
	r.a.drain()
	slackc.Pump()
	r.a.slackcFrame(gtx)
	theme.Fill(gtx, image.Rectangle{Max: r.sz}, r.a.th.C.Bg)
	w(gtx)
	if r.win == nil {
		return nil
	}
	if err := r.win.Frame(ops); err != nil {
		r.t.Fatalf("frame: %v", err)
	}
	img := image.NewRGBA(image.Rectangle{Max: r.sz})
	if err := r.win.Screenshot(img); err != nil {
		r.t.Fatalf("screenshot: %v", err)
	}
	return img
}

// save writes the top h pixels (the dialog is 560 tall; plugin pages are
// drawn full height).
func (r *ttRenderer) save(name string, img *image.RGBA, h ...int) {
	if r.dir == "" || img == nil {
		return
	}
	hh := 560
	if len(h) > 0 {
		hh = h[0]
	}
	img = img.SubImage(image.Rect(0, 0, img.Rect.Dx(), min(hh, img.Rect.Dy()))).(*image.RGBA)
	f, err := os.Create(filepath.Join(r.dir, name+".png"))
	if err != nil {
		r.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		r.t.Fatal(err)
	}
}

// settle draws frames until cond holds (async editors, panels, actions).
func (r *ttRenderer) settle(w func(gtx layout.Context), d time.Duration, cond func() bool) *image.RGBA {
	deadline := time.Now().Add(d)
	for {
		r.frame(w)
		if cond == nil || cond() || time.Now().After(deadline) {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	return r.frame(w)
}

func TestTautPrefsRender(t *testing.T) {
	pdir := os.Getenv("TAUT_PLUGINS_DIR")
	if pdir == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the ui-taut workflow builds Taut's plugins and runs this")
	}
	a, _ := ttApp(t, t.TempDir())
	a.data.startTaut(a)
	tt := a.Taut()
	if tt == nil {
		t.Fatal("taut didn't start")
	}
	t.Cleanup(func() { a.data.stopTaut() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	files, _ := filepath.Glob(filepath.Join(pdir, "*.js"))
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no plugins in " + pdir)
	}
	t0 := time.Now()
	var ids []string
	for _, f := range files {
		code, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		id, err := tt.Import(ctx, code, "")
		if err != nil {
			t.Errorf("import %s: %v", filepath.Base(f), err)
			continue
		}
		ids = append(ids, id)
	}
	t.Logf("imported %d plugins in %v", len(ids), time.Since(t0))

	// Installed but all off: a fresh start reads metadata only, no JS.
	for _, id := range ids {
		if err := tt.SetEnabled(ctx, id, false); err != nil {
			t.Errorf("disable %s: %v", id, err)
		}
	}
	a.data.stopTaut()
	t0 = time.Now()
	a.data.startTaut(a)
	t.Logf("startTaut with %d plugins installed, none on: %v", len(ids), time.Since(t0))
	if tt = a.Taut(); tt == nil {
		t.Fatal("taut didn't restart")
	}
	if tt.Started() {
		t.Error("the JS runtime started with every plugin turned off")
	}

	shots := os.Getenv("TAUT_SHOTS_DIR")
	r := &ttRenderer{t: t, a: a, sz: image.Pt(800, 1400), dir: shots}
	if shots != "" {
		w, err := pixeltest.NewWindow(r.sz.X, r.sz.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		r.win = w
		defer w.Release()
	}
	p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: pluginsTabID}
	defer p.close()
	draw := func(gtx layout.Context) { p.layout(gtx) }
	page := func() *tautPage { return p.taut }
	// tall draws the Taut page alone, 600 wide and full height, so whole
	// plugin pages (editors, data panels) fit in one shot.
	tall := func(gtx layout.Context) {
		gtx.Constraints = layout.Exact(image.Pt(600, r.sz.Y))
		p.taut.layout(gtx)
	}

	for _, dark := range []bool{false, true} {
		a.th.SetDark(dark)
		mode := "light"
		if dark {
			mode = "dark"
		}
		r.save("taut-plugins-installed-off-"+mode, r.settle(draw, 0, nil))
		// custom CSS, config.json and About open from Plugin settings
		p.section = pluginSettingsTabID
		r.save("taut-plugin-settings-"+mode, r.settle(draw, 0, nil))
		page().go_("advanced", "")
		r.save("taut-config-json-"+mode, r.settle(draw, 0, nil))
		page().go_("appearance", "")
		r.save("taut-custom-css-"+mode, r.settle(draw, 0, nil))
		page().go_("about", "")
		r.save("taut-about-"+mode, r.settle(draw, 0, nil))
		page().go_("main", "")
		p.section = pluginsTabID
	}
	a.th.SetDark(false)

	// Enable everything (Taut's acceptance does the same), then every
	// plugin page: options, React editors, data panels.
	t0 = time.Now()
	for _, id := range ids {
		if err := tt.SetEnabled(ctx, id, true); err != nil {
			t.Errorf("enable %s: %v", id, err)
		}
	}
	t.Logf("enabled %d plugins in %v", len(ids), time.Since(t0))
	if !tt.Started() {
		t.Fatal("enabling plugins didn't start the runtime")
	}
	r.settle(draw, 20*time.Second, func() bool {
		for _, c := range tt.Settings().Categories {
			for _, pl := range c.Plugins {
				if pl.Enabled && !pl.Running && pl.Error == "" {
					return false
				}
			}
		}
		return true
	})
	r.save("taut-plugins-installed-on-light", r.frame(draw))
	p.plugins.s.query.SetText("emoji")
	r.save("taut-plugins-search-light", r.frame(draw))
	p.plugins.s.query.SetText("")

	var editors, panels, kinds = 0, 0, map[string]int{}
	for _, c := range tt.Settings().Categories {
		for _, pl := range c.Plugins {
			if pl.Error != "" {
				t.Logf("%s: %s", pl.ID, pl.Error)
			}
			page().go_("plugin", pl.ID)
			wantSubs := 0
			for _, o := range pl.Options {
				kinds[o.Kind.Type]++
				if o.HasEditor {
					wantSubs++
					editors++
				}
			}
			if pl.HasDataPanel && pl.Running {
				wantSubs++
				panels++
			}
			img := r.settle(tall, 10*time.Second, func() bool {
				ready := 0
				for _, s := range page().subs {
					if s.Ready() {
						ready++
					}
				}
				return ready >= wantSubs
			})
			for k, s := range page().subs {
				if !s.Ready() {
					t.Errorf("%s: %s never rendered", pl.ID, k)
				}
			}
			r.save("taut-plugin-"+safeName(pl.ID), img, r.sz.Y)
		}
	}
	t.Logf("option kinds %v; %d editors, %d data panels", kinds, editors, panels)
	page().go_("main", "")

	// Dark plugin page for the record.
	a.th.SetDark(true)
	if len(ids) > 0 {
		page().go_("plugin", ids[0])
		r.save("taut-plugin-first-dark", r.settle(tall, 5*time.Second, nil), r.sz.Y)
	}
	a.th.SetDark(false)
}

func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		return '_'
	}, s)
}
