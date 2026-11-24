package ui

import (
	"image"
	"sort"
	"strconv"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/pluginimport"
	"github.com/some-du6e/clack/internal/pluginlib"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The introduction's offer of the user's Slick or Taut plugins
// (plugin_import.go). On the first launch, when another app on this
// computer has plugins that can come, the plugins beat ("Your plugins,
// unmodified.") shows them in place of its illustration, each one's switch
// its pick, and waits there for an answer: Continue becomes "Bring them
// over", which keeps the picks until Clack has signed in and its plugins
// run (savePendingImport, resumePendingImport); Skip brings nothing.

// introImport is the offer's state.
type introImport struct {
	installs []pluginimport.Install
	items    []importItem
	ix       *pluginlib.Index // the index items were made with
	btns     map[string]*widget.Clickable
	asked    bool            // the Library's index asked for (the card's first draw)
	toggled  map[string]bool // the picks the user changed: kept when the index comes
}

// introImportRows is how many plugins the card lists; Preferences > Plugins
// lists every one.
const introImportRows = 4

// findImports looks for Slick and Taut on this computer, off the UI
// goroutine; the card asks for the Library's index (the plugins' names)
// when it first shows (askIndex), not before.
func (in *intro) findImports(a *App) {
	go func() {
		found := pluginimport.Find()
		if len(found) == 0 {
			return
		}
		a.Post(func() {
			in.imp.installs = found
			in.imp.items = nil
			a.Invalidate()
		})
	}()
}

// askIndex asks for the Library's index once, when the card first shows.
func (in *intro) askIndex(a *App) {
	if in.imp.asked {
		return
	}
	in.imp.asked = true
	if lib := a.pluginLib(); lib != nil {
		go lib.Refresh(libraryTTL, false, func(*pluginlib.Index, error) { a.Invalidate() })
	}
}

// importItems is the offer's items, made again once the Library's index
// comes (the plugins' names, what it hasn't got), its picks kept.
func (in *intro) importItems(a *App) []importItem {
	ii := &in.imp
	if len(ii.installs) == 0 {
		return nil
	}
	var ix *pluginlib.Index
	if lib := a.pluginLib(); lib != nil {
		ix = lib.Cached()
	}
	if ii.items == nil || ix != ii.ix {
		picks := map[string]bool{}
		for _, it := range ii.items {
			picks[it.key()] = it.pick
		}
		ii.items, ii.ix = importItems(ix, ii.installs, nil), ix
		for i := range ii.items {
			if k := ii.items[i].key(); ii.toggled[k] && ii.items[i].reason == "" {
				ii.items[i].pick = picks[k]
			}
		}
		// the card lists the picked first (and doesn't move a row its
		// switch turns)
		sort.SliceStable(ii.items, func(i, j int) bool { return ii.items[i].pick && !ii.items[j].pick })
	}
	return ii.items
}

// importing reports whether the plugins beat offers the import now.
func (in *intro) importing(a *App) bool {
	if in.mode != introFirstRun {
		return false
	}
	for _, it := range in.importItems(a) {
		if it.reason == "" {
			return true
		}
	}
	return false
}

// importPicks is how many items are picked.
func (in *intro) importPicks() int {
	n := 0
	for _, it := range in.imp.items {
		if it.pick && it.reason == "" {
			n++
		}
	}
	return n
}

// importApps names the apps the plugins are from: "Slick", "Slick and
// Taut".
func (in *intro) importApps() string {
	var apps []string
	for _, inst := range in.imp.installs {
		if len(inst.Plugins) > 0 {
			apps = append(apps, systemName(inst.System))
		}
	}
	return strings.Join(apps, " and ")
}

// importSub is the plugins beat's sentence while it offers the import.
func (in *intro) importSub() string {
	return AppName + " found " + in.importApps() + " on this computer. Pick the plugins to bring over, with their settings: they come once you've signed in."
}

// readImportClicks toggles the picks whose switch was clicked (update).
func (in *intro) readImportClicks(gtx layout.Context) {
	for i := range in.imp.items {
		it := &in.imp.items[i]
		if b := in.imp.btns[it.key()]; b != nil && b.Clicked(gtx) {
			it.pick = !it.pick
			if in.imp.toggled == nil {
				in.imp.toggled = map[string]bool{}
			}
			in.imp.toggled[it.key()] = true
		}
	}
}

func (in *intro) importBtn(key string) *widget.Clickable {
	if in.imp.btns == nil {
		in.imp.btns = map[string]*widget.Clickable{}
	}
	b := in.imp.btns[key]
	if b == nil {
		b = new(widget.Clickable)
		in.imp.btns[key] = b
	}
	return b
}

// drawImportStage is the plugins beat's stage while it offers the import:
// a card of the plugins that can come (picked first, the first
// introImportRows), each with its system and its switch, how many more
// there are, and which can't come and why.
func (in *intro) drawImportStage(f *introFrame, g introGeom, start time.Time, stage fx) {
	k := f.kit(g)
	var can, cannot []*importItem
	for i := range in.imp.items {
		it := &in.imp.items[i]
		if it.reason == "" {
			can = append(can, it)
		} else {
			cannot = append(cannot, it)
		}
	}
	cw := float32(460)
	cx := (k.w - cw) / 2
	card := k.rect(cx, 6, cw, 318)
	e := f.rise(start, f.df(24), 0.97).mul(stage)
	pop := f.transform(fx{dx: e.dx, dy: e.dy, s: e.s}, center(card))
	defer pop()
	A := e.a
	f.with("import-card", fx{a: A, s: 1}, f32.Point{}, func() {
		f.card(card, f.d(14), introWhite)
		f.label(k.pt(cx+20, 18), 16, theme.Black, miniFg, "Bring over your plugins")
		f.label(k.pt(cx+20, 41), 12, theme.Regular, miniSubtle, "From "+in.importApps()+" on this computer")
		theme.Fill(f.gtx, k.rect(cx, 66, cw, 0.7), miniBorder)
	})
	shown := can[:min(introImportRows, len(can))]
	for i, it := range shown {
		y := 80 + float32(i)*48
		re := f.enter(start.Add(msec(90+60*float64(i))), f.df(14), springBouncy, false).fade(A)
		f.with("import-row", re, f32.Point{}, func() {
			f.label(k.pt(cx+20, y), 14.5, theme.Black, miniFg, it.name)
			nw := f.measure(14.5, theme.Black, it.name)
			chip := systemName(it.found.System)
			cwid := f.measure(10, theme.Black, chip) + f.d(12)
			cr := image.Rectangle{Min: k.pt(cx+20, y+2.5)}
			cr.Min.X += nw + f.d(8)
			cr.Max = cr.Min.Add(image.Pt(cwid, f.d(16)))
			bg, fg := hexc(0xf1ecfe), hexc(0x5b3fd6)
			if it.found.System == pluginlib.Taut {
				bg, fg = hexc(0xfdf0e0), hexc(0xa85b00)
			}
			theme.RRect(f.gtx, cr, cr.Dy()/2, bg)
			f.label(image.Pt(cr.Min.X+f.d(6), cr.Min.Y+(cr.Dy()-f.d(12.5))/2), 10, theme.Black, fg, chip)
			sub := "On in " + it.app
			switch {
			case !it.known:
				sub = "As " + it.app + " has it"
			case !it.on:
				sub = "Off in " + it.app
			}
			if it.line != "" {
				sub = it.line
			}
			f.label(k.pt(cx+20, y+22), 12, theme.Regular, miniMuted, sub)
			// its switch: the pick
			sr := k.rect(cx+cw-20-34, y+6, 34, 20)
			b := in.importBtn(it.key())
			atExact(f.gtx, sr.Min, sr.Size(), func(gtx layout.Context) layout.Dimensions {
				return b.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					pointer.CursorPointer.Add(gtx.Ops)
					return layout.Dimensions{Size: gtx.Constraints.Max}
				})
			})
			in.drawToggle(f, sr, it.pick)
		})
	}
	y := 80 + float32(len(shown))*48
	var lines []string
	if n := len(can) - len(shown); n > 0 {
		lines = append(lines, "and "+strconv.Itoa(n)+" more, as they're set there (Preferences > Plugins lists every one)")
	}
	if len(cannot) > 0 {
		var names []string
		for _, it := range cannot[:min(2, len(cannot))] {
			names = append(names, it.name)
		}
		s := "Not coming: " + strings.Join(names, ", ")
		if n := len(cannot) - len(names); n > 0 {
			s += " and " + strconv.Itoa(n) + " more"
		}
		lines = append(lines, s+" (Clack can't run them yet)")
	}
	le := f.enter(start.Add(msec(90+60*float64(len(shown)))), f.df(10), springSmooth, false).fade(A)
	f.with("import-more", le, f32.Point{}, func() {
		for i, s := range lines {
			f.label(k.pt(cx+20, y+float32(i)*20), 12, theme.Regular, miniSubtle, s)
		}
	})
}

// drawToggle is a switch in the plugins card's style, still: on or off.
func (in *intro) drawToggle(f *introFrame, r image.Rectangle, on bool) {
	h := r.Dy()
	kd := h - f.d(4)
	x := r.Min.X + f.d(2)
	bg := hexc(0xc8c8c8)
	if on {
		x, bg = r.Max.X-f.d(2)-kd, miniSwitch
	}
	theme.RRect(f.gtx, r, h/2, bg)
	c := image.Pt(x+kd/2, r.Min.Y+h/2)
	theme.Circle(f.gtx, c.Add(image.Pt(0, max(1, f.d(0.7)))), kd, alphaMul(introBlack, 0.12))
	theme.Circle(f.gtx, c, kd, introWhite)
}
