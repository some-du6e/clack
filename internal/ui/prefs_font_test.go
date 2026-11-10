package ui

import (
	"context"
	"encoding/json"
	"errors"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/assets/fonts"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uifont"
)

// fakeFonts is an installed-font index where "Arial" is Lato Black (so
// the switch is visible in every measurement), and nothing else exists.
type fakeFonts struct{}

func (fakeFonts) Faces(family string) []uifont.File {
	if strings.EqualFold(family, "Arial") {
		return []uifont.File{{Path: "/f/arial.ttf"}}
	}
	return nil
}

// useFakeFonts points the font loader at fakeFonts and no network.
func useFakeFonts(t *testing.T) {
	t.Helper()
	prev := fontLoader
	dir := t.TempDir()
	fontLoader = func(offline bool) *uifont.Loader {
		return &uifont.Loader{GOOS: runtime.GOOS, CacheDir: dir, Offline: offline, System: fakeFonts{},
			ReadFile: func(p string) ([]byte, error) {
				if p == "/f/arial.ttf" {
					return fonts.Black, nil
				}
				return nil, os.ErrNotExist
			},
			Fetch: func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }}
	}
	t.Cleanup(func() {
		fontLoader = prev
		theme.SetDefaultUIFont(nil)
	})
}

// TestFontPrefRoundTrip: Appearance > Font lists Slack's options in
// Slack's order, a pick switches the UI font (off the UI goroutine, then
// one install) and is kept in ui.json as Slack's option object, and the
// next start draws with it from the first frame.
func TestFontPrefRoundTrip(t *testing.T) {
	useFakeFonts(t)
	a, _, _ := fakePrefsApp(t)
	waitFor(t, a, "the startup font", func() bool { return a.font.applied == uifont.Lato })

	// the select: Slack's labels and order, Lato checked
	items := a.fontItems(a.currentFont())
	var got, want []string
	for _, it := range items {
		got = append(got, it.Label)
		if it.Checked != (it.Label == "Lato (Default)") {
			t.Errorf("%s checked=%v", it.Label, it.Checked)
		}
	}
	for _, o := range uifont.Options(runtime.GOOS) {
		want = append(want, o.Label)
	}
	if strings.Join(got, "|") != strings.Join(want, "|") || got[0] != "Arial" {
		t.Fatalf("font options %q, want %q", got, want)
	}

	// the Appearance page draws it enabled with the current label
	drawFont := func() string {
		p := &prefsModal{a: a, clicks: map[string]*widget.Clickable{}, section: "appearance"}
		var rec []prefsDrawn
		p.rec = &rec
		gtx := ttContext(new(op.Ops), image.Pt(800, 624))
		a.slackcFrame(gtx)
		p.layout(gtx)
		for _, r := range rec {
			if r.Kind == "select" && r.Key == "sel.font" {
				if !r.Enabled {
					t.Error("the font select is greyed")
				}
				return r.Label
			}
		}
		t.Fatal("no font select")
		return ""
	}
	if l := drawFont(); l != "Lato (Default)" {
		t.Errorf("select shows %q", l)
	}

	// pick Arial
	old, gen := a.th.Shaper, theme.FontGen()
	for _, it := range items {
		if it.Label == "Arial" {
			it.Do()
		}
	}
	if a.th.Shaper != old {
		t.Fatal("the shaper changed on the UI goroutine before the load finished")
	}
	waitFor(t, a, "Arial installed", func() bool { return a.font.applied == uifont.Arial })
	if a.th.Shaper == old || theme.FontGen() == gen {
		t.Fatal("no new shaper / FontGen")
	}
	// Lato Black stands in for Arial: 20px "Slack" measures wider than Lato
	gtx := ttContext(new(op.Ops), image.Pt(800, 600))
	newW := a.th.Measure(gtx, 20, theme.Regular, "Slack typeface")
	th0 := theme.Bare(false) // Lato (theme.New follows the installed font now, like a huddle window)
	lsh := latoShaper()
	th0.SetShapers(lsh, lsh)
	if hw := theme.New(false).Measure(gtx, 20, theme.Regular, "Slack typeface"); hw != newW {
		t.Errorf("a window opened now (theme.New) measures %d, the app %d", hw, newW)
	}
	if oldW := th0.Measure(gtx, 20, theme.Regular, "Slack typeface"); newW <= oldW {
		t.Errorf("Arial (Lato Black here) measures %d, Lato %d", newW, oldW)
	}
	if l := drawFont(); l != "Arial" {
		t.Errorf("select shows %q after the pick", l)
	}

	// ui.json holds Slack's object
	a.flushPrefs()
	b, err := os.ReadFile(filepath.Join(a.opts.ConfigDir, "ui.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		T map[string]any `json:"client_typeface"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.T["label"] != "Arial" || raw.T["value"] != "arial" || raw.T["fontFamily"] != "Arial" {
		t.Errorf("ui.json client_typeface %v", raw.T)
	}

	// a restart reads it and builds the font before the first frame
	a2, _, _ := fakePrefsApp(t)
	a2.opts.ConfigDir = a.opts.ConfigDir
	a2.loadPrefs()
	if a2.currentFont().Value != uifont.Arial {
		t.Fatalf("restart: font %+v", a2.currentFont())
	}
	sh, o, pending := startupShaper(a2.prefs.ClientTypeface)
	if pending || o.Value != uifont.Arial {
		t.Fatalf("startup: %v pending=%v", o.Value, pending)
	}
	th := theme.Bare(false)
	th.SetShapers(sh, sh)
	if w := th.Measure(gtx, 20, theme.Regular, "Slack typeface"); w != newW {
		t.Errorf("startup shaper measures %d, the switched one %d", w, newW)
	}

	// back to Lato
	for _, it := range a.fontItems(a.currentFont()) {
		if it.Label == "Lato (Default)" {
			it.Do()
		}
	}
	waitFor(t, a, "Lato again", func() bool { return a.font.applied == uifont.Lato })
	if w := a.th.Measure(gtx, 20, theme.Regular, "Slack typeface"); w != th0.Measure(gtx, 20, theme.Regular, "Slack typeface") {
		t.Errorf("Lato again measures %d", w)
	}
}

// TestFontPickLatestWins: a slow load finishing after a newer pick is
// dropped.
func TestFontPickLatestWins(t *testing.T) {
	useFakeFonts(t)
	a, _, _ := fakePrefsApp(t)
	arial, _ := uifont.Find(runtime.GOOS, uifont.Arial)
	a.setClientTypeface(arial)
	a.setClientTypeface(uifont.Default())
	waitFor(t, a, "both loads", func() bool { return a.font.loading == "" })
	if a.font.applied != uifont.Lato && a.font.applied != "" {
		t.Errorf("applied %q, want the last pick (lato)", a.font.applied)
	}
	if a.currentFont().Value != uifont.Lato {
		t.Errorf("pref %q", a.currentFont().Value)
	}
}
