package ui

// Clack's tab rail buttons, for side-by-sides with Slack's tab rail markup
// under Slack's shipping CSS (.github/scripts/rail_ref.py, the font-shots
// workflow's sidebar-rows jobs). With RAIL_SHOTS_DIR set it draws the
// client over fakeslack's Unreads fixtures (synthetic data; the mention in
// #unreads-launch badges Activity) at 1x and 1.25x, dark and light, and
// crops every rail button idle, hovered, selected and after a mouse click
// (rail-<mode>-<scale>x-<key>-<state>.png), with rail.json. At 1x it does
// the same under railShotThemes' account themes
// (rail-<theme>-<mode>-1x-<key>-<state>.png), which rail_ref.py draws with
// Slack's tokens for that theme (testdata/theme-oracle.json, from Slack's
// code).

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

type railShotBtn struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	State  string `json:"state"` // idle, hover, selected, clicked
	File   string `json:"file"`
	Badge  int    `json:"badge"`
	Dot    bool   `json:"dot"`
	Active bool   `json:"active"` // drawn selected
	Top    int    `json:"top"`    // px, the crop's top above the button
	H      int    `json:"h"`      // px, the button's height
	Y      int    `json:"y"`      // px, the crop's top in the window
}

type railShot struct {
	Scale float32           `json:"scale"`
	Dark  bool              `json:"dark"`
	W     int               `json:"w"`   // px, the crop's width (the rail)
	Win   [2]int            `json:"win"` // px, the window (Slack's side draws .p-theme_background over it)
	Btns  []railShotBtn     `json:"btns"`
	Vars  map[string]string `json:"vars,omitempty"`
	Theme string            `json:"theme,omitempty"`    // a theme-oracle.json case ("" Slack's default theme)
	IA    string            `json:"ia_theme,omitempty"` // the account theme every crop was taken under
}

// railShotThemes are the account themes the rail is shot under too:
// Karim's #FED4BE as Slack's Custom theme picker stores it (brightness 2,
// from the colour's tone: the light scope's dark ink) and as Slack's
// four-hex Import does (the Aubergine preset's brightness 6: the inverted
// scope's white ink over the pale gradient, Slack's own), and a dark
// custom colour.
var railShotThemes = []struct {
	name, ia string
	modes    []bool // dark
}{
	{"karim picker", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":2,"sidebarInverted":false,"useCustomHex":true}`, []bool{false, true}},
	{"karim import", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine","custom":"#611F69"},"highlight2":{"palette":"jade","custom":"#2EB67D"},"important":{"palette":"aubergine","custom":"#E01E5A"},"brightness":6,"sidebarInverted":false,"useCustomHex":true}`, []bool{false}},
	// the same Import saved from a light client: sidebarInverted kept (Slack's setIaThemePrefByApi)
	{"karim import light save", `{"primary":{"palette":"aubergine","custom":"#FED4BE"},"highlight1":{"palette":"aubergine","custom":"#611F69"},"highlight2":{"palette":"jade","custom":"#2EB67D"},"important":{"palette":"aubergine","custom":"#E01E5A"},"brightness":6,"sidebarInverted":true,"useCustomHex":true}`, []bool{false}},
	{"dark custom", `{"primary":{"palette":"aubergine","custom":"#4A154B"},"highlight1":{"palette":"aubergine"},"highlight2":{"palette":"jade"},"important":{"palette":"aubergine"},"brightness":5,"sidebarInverted":true,"useCustomHex":true}`, []bool{false}},
}

func TestRailShots(t *testing.T) {
	dir := os.Getenv("RAIL_SHOTS_DIR")
	if dir == "" {
		t.Skip("RAIL_SHOTS_DIR unset (the font-shots workflow's sidebar-rows jobs)")
	}
	t.Setenv("CLACK_BENCH_GPU", "1")
	var shots []railShot
	for _, scale := range []float32{1, 1.25} {
		sz := fakeslack.Small
		sz.Unreads = true
		fs, err := fakeslack.Start(sz)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { fs.Close() })
		// the classic Activity badge (no activity inbox in the fake) reads
		// client.counts' channel_badges, as Slack's 3x6A does: the server's
		// count for #unreads-launch's one mention of the viewer
		fs.SetChannelBadges(map[string]int{"channels": 1})
		e := startEnv(t, fs, t.TempDir())
		if e.gpu == nil {
			t.Fatal("no GPU renderer")
		}
		e.scale = scale
		e.a.railRects = map[string]image.Rectangle{}
		e.waitReady()
		e.until("the Activity badge", 20*time.Second, func() bool { n, _ := e.a.railBadge("activity"); return n > 0 })
		ch := fakeslack.JoinBannerOldID
		away := func() { e.movePtr(image.Pt(e.size.X-40, e.size.Y-40)) }
		closeAll := func() {
			for _, o := range e.a.overlays {
				if d, ok := o.(dismisser); ok {
					d.Dismiss()
				}
			}
			e.a.overlays = nil
			e.a.peek.menu = nil // the More peek (its menu was among the overlays)
		}
		shoot := func(dark bool, themeName, ia string) {
			e.useAccountTheme(ia, dark) // theme_contrast_shots_test.go
			mode := "light"
			if dark {
				mode = "dark"
			}
			if themeName != "" {
				mode = strings.ReplaceAll(themeName, " ", "-") + "-" + mode
			}
			sh := railShot{Scale: scale, Dark: dark, W: e.context().Dp(70), Win: [2]int{e.size.X, e.size.Y}, Vars: e.a.scopeVars, Theme: themeName, IA: ia}
			want, set := theme.ParseIATheme(ia)
			for _, key := range []string{"home", "dms", "activity", "browse", "platform"} {
				e.openChannel(ch)
				e.settle(200 * time.Millisecond)
				if _, ok := e.a.railRects[key]; !ok {
					t.Logf("no %s button in the rail, skipped", key)
					continue
				}
				// the route that selects it (More and Agents & tools have none)
				route := map[string]string{"home": "c/" + ch, "dms": "dms", "activity": "activity"}[key]
				for _, state := range []string{"idle", "hover", "selected", "clicked"} {
					closeAll()
					switch {
					case state == "selected" && route == "":
						continue
					case state == "selected":
						e.a.Navigate(route)
					case key == "home":
						e.a.Navigate("dms") // Home idle and hovered: another tab open
					default:
						e.openChannel(ch)
					}
					e.settle(200 * time.Millisecond)
					r := e.a.railRects[key]
					mid := image.Pt((r.Min.X+r.Max.X)/2, r.Min.Y+e.context().Dp(26))
					switch state {
					case "hover":
						// shot within the peeks' 300 ms trigger delay
						// (rail_tab_peek.go, rail_peek.go): the Activity
						// peek or the More menu would cover the crop's edge
						e.movePtr(mid)
						e.settle(120 * time.Millisecond)
					case "clicked":
						// a mouse click, then the pointer leaves: what the
						// click leaves behind (Slack: no focus ring)
						e.click(mid)
						e.frame()
						away()
						e.settle(400 * time.Millisecond)
					default:
						away()
						e.settle(400 * time.Millisecond)
					}
					if e.a.acct.set != set || e.a.acct.t != want {
						t.Fatalf("%s %s %s: the account theme isn't %q any more (core's users.prefs answer?)", mode, key, state, ia)
					}
					r = e.a.railRects[key]
					img := image.NewRGBA(image.Rectangle{Max: e.size})
					if err := e.gpu.Screenshot(img); err != nil {
						t.Fatal(err)
					}
					top := e.context().Dp(6)
					crop := img.SubImage(image.Rect(0, r.Min.Y-top, sh.W, r.Max.Y+e.context().Dp(2)))
					name := fmt.Sprintf("rail-%s-%gx-%s-%s.png", mode, scale, key, state)
					f, err := os.Create(filepath.Join(dir, name))
					if err != nil {
						t.Fatal(err)
					}
					if err := png.Encode(f, crop); err != nil {
						t.Fatal(err)
					}
					f.Close()
					n, dot := e.a.railBadge(key)
					active := e.a.activeRail() == key
					if key == "browse" || key == "platform" {
						active = false
					}
					sh.Btns = append(sh.Btns, railShotBtn{Key: key, Label: e.a.railTabFor(railID(e.a, key)).label, State: state, File: name,
						Badge: n, Dot: dot, Active: active, Top: top, H: r.Dy(), Y: r.Min.Y - top})
				}
			}
			closeAll()
			shots = append(shots, sh)
		}
		for _, dark := range []bool{true, false} {
			shoot(dark, "", "")
		}
		if scale != 1 {
			continue
		}
		for _, th := range railShotThemes {
			for _, dark := range th.modes {
				shoot(dark, th.name, th.ia)
			}
		}
		e.a.applyAccountPrefs(core.Prefs{})
	}
	b, _ := json.MarshalIndent(shots, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "rail.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// railID is the rail tab id behind key (railKey's inverse over the shown tabs).
func railID(a *App, key string) string {
	for id := range a.model().Rail.Tabs {
		if railKey(id) == key {
			return id
		}
	}
	return key
}
