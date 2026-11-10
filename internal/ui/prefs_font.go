package ui

import (
	"context"
	"image"
	"image/color"
	"log"
	"runtime"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/uifont"
)

// Preferences > Appearance > Font (Slack's ThemeFontTypefaceContainer;
// the rules and their evidence are in internal/ui/uifont).
//
// The choice is per device, like Slack's localConfig "client-typeface":
// ui.json keeps the option object (prefs.ClientTypeface). Picking one
// loads its faces off the UI goroutine (CDN files from the cache dir or
// the network, system files from disk), builds a shaper with them as the
// UI family and installs it in one frame (Theme.SetUIShaper): every view
// lays its text out again with the new font, once.

// fontState is the font row's loader state (UI goroutine only).
type fontState struct {
	seq      int          // the latest requested font; older loads are dropped
	loading  string       // the option value being loaded, "" when idle
	applied  string       // the option value the installed shaper draws
	preview  *text.Shaper // the menu's previews: one family per option (nil until loaded)
	previews bool         // the preview load has started
}

// fontLoader is a uifont.Loader for this OS and the cache dir.
var fontLoader = func(offline bool) *uifont.Loader {
	return &uifont.Loader{GOOS: runtime.GOOS, CacheDir: uifont.DefaultCacheDir(), Offline: offline}
}

// fontShaper builds the shaper for o. pending reports that offline left
// out a CDN file that isn't cached yet.
func fontShaper(ctx context.Context, o uifont.Option, offline bool) (sh *text.Shaper, f *uifont.Font, err error) {
	f, err = fontLoader(offline).Load(ctx, o)
	if err != nil {
		return nil, nil, err
	}
	if f.UI == nil {
		return latoShaper(), f, nil
	}
	sh, err = theme.NewShaperWith(f.UI)
	if err != nil {
		log.Printf("ui: font %s: %v; Lato instead", o.Value, err)
		return latoShaper(), f, nil
	}
	return sh, f, nil
}

// latoShaper is a shaper with the embedded Lato as the UI family.
func latoShaper() *text.Shaper {
	sh, _ := theme.NewShaperWith(nil)
	return sh
}

// startupShaper is New's shaper: the chosen font when everything it needs
// is on this computer already, else Lato (pending: load it online next).
func startupShaper(stored *uifont.Option) (sh *text.Shaper, o uifont.Option, pending bool) {
	o = uifont.Current(stored)
	if !uifont.UsesThemeFont(o) {
		return latoShaper(), o, false
	}
	sh, f, err := fontShaper(context.Background(), o, true)
	if err != nil || f.Pending {
		return latoShaper(), o, true
	}
	log.Printf("ui: font %s (%s)", o.Label, f.Family)
	theme.SetDefaultUIFont(f.UI)
	return sh, o, false
}

// currentFont is the option the font row shows.
func (a *App) currentFont() uifont.Option { return uifont.Current(a.prefs.ClientTypeface) }

// setClientTypeface is setPersistentCustomTypeface: keep the option on
// this device and switch the UI font.
func (a *App) setClientTypeface(o uifont.Option) {
	cp := o
	a.prefs.ClientTypeface = &cp
	a.savePrefs()
	a.loadUIFont(o)
}

// loadUIFont builds o's shaper off the UI goroutine and installs it.
func (a *App) loadUIFont(o uifont.Option) {
	a.font.seq++
	seq := a.font.seq
	a.font.loading = o.Value
	a.Invalidate()
	go func() {
		t := time.Now()
		sh, f, err := fontShaper(context.Background(), o, false)
		if err != nil {
			log.Printf("ui: font %s: %v", o.Value, err)
			sh = nil
		}
		a.Post(func() {
			if seq != a.font.seq { // a newer pick won
				return
			}
			a.font.loading = ""
			if sh == nil {
				return
			}
			var u *theme.UIFont
			if f != nil {
				u = f.UI
			}
			a.installUIShaper(sh, u, o.Value)
			if f != nil {
				log.Printf("ui: font %s (%s) in %v", o.Label, f.Family, time.Since(t).Round(time.Millisecond))
			}
		})
	}()
}

// installUIShaper swaps the UI font in (UI goroutine).
func (a *App) installUIShaper(sh *text.Shaper, u *theme.UIFont, value string) {
	theme.SetDefaultUIFont(u)
	a.th.SetUIShaper(sh)
	a.font.applied = value
	a.Invalidate()
}

// loadFontPreviews loads every option's regular face for the menu, which
// draws each option in its own font (Slack's renderItem fontFamily).
func (a *App) loadFontPreviews() {
	if a.font.previews {
		return
	}
	a.font.previews = true
	opts := uifont.Options(runtime.GOOS)
	go func() {
		l := fontLoader(false)
		var faces []theme.FaceSlot
		for _, o := range opts {
			if !uifont.UsesThemeFont(o) {
				continue
			}
			f, err := l.Load(context.Background(), o)
			if err != nil || f.UI == nil {
				continue
			}
			s := f.UI.Faces[0]
			s.Family = previewFamily(o.Value)
			faces = append(faces, s)
		}
		sh, err := theme.NewShaperWith(nil, faces...)
		if err != nil {
			log.Printf("ui: font previews: %v", err)
			return
		}
		a.Post(func() {
			a.font.preview = sh
			a.Invalidate()
		})
	}()
}

func previewFamily(value string) string { return "Clack Font " + value }

// fontItems are the select's options: Slack's order and labels, each
// drawn in its font once the previews are in.
func (a *App) fontItems(cur uifont.Option) []MenuItem {
	var items []MenuItem
	for _, o := range uifont.Options(runtime.GOOS) {
		o := o
		it := MenuItem{Key: "font." + o.Value, Label: o.Label, Checked: o.Value == cur.Value, Do: func() {
			if o.Value != a.currentFont().Value || a.font.loading != "" {
				a.setClientTypeface(o)
			}
		}}
		if uifont.UsesThemeFont(o) {
			it.LabelText = func(gtx layout.Context, c color.NRGBA) layout.Dimensions {
				sh := a.font.preview
				if sh == nil {
					return a.th.Label(gtx, 15, theme.Regular, c, o.Label)
				}
				gtx.Constraints.Min = image.Point{}
				return theme.DrawText(gtx, sh, theme.LabelOpts{Font: font.Font{Typeface: font.Typeface(previewFamily(o.Value)) + ", " + theme.Typeface},
					Size: unit.Sp(15), MaxLines: 1, Color: c}, o.Label)
			}
		}
		items = append(items, it)
	}
	return items
}
