package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gioui.org/layout"

	"github.com/some-du6e/yoloslackgoclient/internal/pluginlib"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// The plugin Library: Preferences > Taut > Library and Preferences > Slick >
// Library list the official plugins from the Clack repo's `plugins`
// release (internal/pluginlib), each built unmodified by its project's own
// build, with one-click install and update. An installed Library plugin is
// an ordinary user plugin in that system's plugins/ dir.

var (
	pluginLibMu sync.Mutex
	pluginLibs  = map[string]*pluginlib.Library{} // by index URL
)

// pluginLibURL is the index the Library reads: CLACK_PLUGIN_LIBRARY (a URL,
// or "off"), else the published one.
func pluginLibURL() string {
	if u := strings.TrimSpace(os.Getenv("CLACK_PLUGIN_LIBRARY")); u != "" {
		if u == "off" {
			return ""
		}
		return u
	}
	return pluginlib.DefaultIndexURL
}

// pluginLib is the shared Library (no I/O until a Library screen opens);
// nil when the Library is turned off.
func (a *App) pluginLib() *pluginlib.Library {
	u := pluginLibURL()
	if u == "" {
		return nil
	}
	pluginLibMu.Lock()
	defer pluginLibMu.Unlock()
	if l := pluginLibs[u]; l != nil {
		return l
	}
	dir := ""
	if a.opts.ConfigDir != "" {
		dir = filepath.Join(a.opts.ConfigDir, "cache", "plugin-library")
	}
	l := pluginlib.New(pluginlib.Options{IndexURL: u, CacheDir: dir})
	pluginLibs[u] = l
	return l
}

// libraryHost is what a plugin system gives the Library view.
type libraryHost struct {
	// installed reports the hex sha256 of an installed plugin's files ("" = none)
	installed func(id string) (code, main string)
	// install imports a downloaded entry (update: replace the installed one)
	install func(ctx context.Context, e pluginlib.Entry, code, main []byte, update bool) error
	// open shows an installed plugin's settings (nil: no settings link)
	open func(id string)
}

// libraryView is one system's Library screen.
type libraryView struct {
	sys     string // pluginlib.Taut | pluginlib.Slick
	k       *tautPage
	ix      *pluginlib.Index
	loading bool
	loadErr string
	busy    map[string]bool   // entry ids installing
	status  map[string]string // entry id → last result
	failed  map[string]bool   // status is an error
}

func newLibraryView(sys string, k *tautPage) *libraryView {
	return &libraryView{sys: sys, k: k, busy: map[string]bool{}, status: map[string]string{}, failed: map[string]bool{}}
}

func (v *libraryView) load(refresh bool) {
	if v.loading {
		return
	}
	a := v.k.a
	lib := a.pluginLib()
	if lib == nil {
		v.loadErr = "The plugin library is turned off (CLACK_PLUGIN_LIBRARY=off)."
		return
	}
	if v.ix == nil {
		v.ix = lib.Cached()
	}
	v.loading, v.loadErr = true, ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ix, err := lib.Index(ctx, refresh)
		a.Post(func() {
			v.loading = false
			if err != nil {
				v.loadErr = err.Error()
			} else {
				v.ix = ix
			}
			a.Invalidate()
		})
	}()
}

func (v *libraryView) installEntry(h libraryHost, e pluginlib.Entry, update bool) {
	if v.busy[e.ID] {
		return
	}
	a := v.k.a
	v.busy[e.ID], v.status[e.ID], v.failed[e.ID] = true, "", false
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		var code, main []byte
		err := errors.New("the plugin library is turned off")
		if lib := a.pluginLib(); lib != nil {
			code, main, err = lib.Download(ctx, e)
		}
		if err == nil {
			err = h.install(ctx, e, code, main, update)
		}
		a.Post(func() {
			delete(v.busy, e.ID)
			switch {
			case err != nil:
				v.status[e.ID], v.failed[e.ID] = err.Error(), true
			case update:
				v.status[e.ID] = "Updated."
			default:
				v.status[e.ID] = "Installed."
			}
			a.Invalidate()
		})
	}()
}

// sourceLine is "from Taut · GPLv3 · upstream commit 6fe5123".
func (v *libraryView) sourceLine() string {
	src := v.ix.Sources[v.sys]
	name := src.Name
	if name == "" {
		name = strings.ToUpper(v.sys[:1]) + v.sys[1:]
	}
	lic := strings.TrimSuffix(strings.ReplaceAll(src.License, "GPL-", "GPLv"), ".0")
	if lic == "" {
		lic = "GPLv3"
	}
	c := src.Commit
	if len(c) > 7 {
		c = c[:7]
	}
	return fmt.Sprintf("from %s · %s · upstream commit %s", name, lic, c)
}

// layout draws the Library into f; title is drawn by the caller.
func (v *libraryView) layout(f *flow, h libraryHost) {
	k := v.k
	cl := k.colors()
	if v.ix == nil && !v.loading && v.loadErr == "" {
		v.load(false)
	}
	sysName := "Taut"
	if v.sys == pluginlib.Slick {
		sysName = "Slick"
	}
	intro := "Official " + sysName + " plugins, built unmodified by " + sysName + "'s own build from its source. Installing one adds it as a user plugin; turn it on in the plugin list."
	if v.ix != nil {
		src := v.ix.Sources[v.sys]
		intro = fmt.Sprintf("Official %s plugins, built unmodified by %s's own build from %s at commit %s. They're third-party code under %s by their authors. Installing one adds it as a user plugin; turn it on in the plugin list.",
			sysName, sysName, strings.TrimPrefix(src.Repo, "https://"), shortSHA(src.Commit), orStr(src.License, "GPL-3.0"))
	}
	f.put(0, func(gtx layout.Context) layout.Dimensions { return k.para(gtx, 15, 22, cl.fg2, intro) })
	f.gap(12)

	var entries []pluginlib.Entry
	if v.ix != nil {
		entries = v.ix.For(v.sys)
	}
	updates := 0
	for _, e := range entries {
		c, m := h.installed(e.ID)
		if pluginlib.StateOf(e, c, m) == pluginlib.UpdateAvailable {
			updates++
		}
	}
	// toolbar: Refresh | Update all (n) | status
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		label := "Refresh"
		kind := popups.BtnOutline
		if v.loading {
			label, kind = "Loading...", popups.BtnDisabled
		}
		b := k.btn("lib." + v.sys + ".refresh")
		hit := b.Clicked(gtx)
		d := popups.Button(gtx, k.a.th, b, kind, popups.BtnSmall, label, "", 0)
		if hit && !v.loading {
			v.load(true)
		}
		x := d.Size.X + gtx.Dp(8)
		if updates > 0 {
			ub := k.btn("lib." + v.sys + ".updateall")
			uhit := ub.Clicked(gtx)
			ud := at(gtx, image.Pt(x, 0), image.Pt(1<<20, d.Size.Y), func(gtx layout.Context) layout.Dimensions {
				return popups.Button(gtx, k.a.th, ub, popups.BtnPrimary, popups.BtnSmall, fmt.Sprintf("Update all (%d)", updates), "", 0)
			})
			if uhit {
				for _, e := range entries {
					c, m := h.installed(e.ID)
					if pluginlib.StateOf(e, c, m) == pluginlib.UpdateAvailable {
						v.installEntry(h, e, true)
					}
				}
			}
			x += ud.Size.X + gtx.Dp(8)
		}
		if v.ix != nil {
			at(gtx, image.Pt(x, 0), image.Pt(1<<20, d.Size.Y), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, d.Size.Y, func(gtx layout.Context) layout.Dimensions {
					return k.label(gtx, 13, theme.Regular, cl.fg3, fmt.Sprintf("%d plugins · %s", len(entries), v.sourceLine()))
				})
			})
		}
		return d
	})
	if v.loadErr != "" {
		f.gap(8)
		f.put(0, func(gtx layout.Context) layout.Dimensions { return k.alert(gtx, v.loadErr) })
	}
	f.gap(16)
	if v.ix == nil {
		if v.loading {
			k.muted(f, "Loading the plugin library...")
		}
		return
	}
	q := k.ed("lib."+v.sys+".q", true)
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		return k.input(gtx, q, f.w, gtx.Dp(36), "Search the library", false)
	})
	f.gap(8)
	words := strings.Fields(strings.ToLower(q.Text()))
	cat := ""
	shown := 0
	for _, e := range entries {
		if !libraryMatches(e, words) {
			continue
		}
		if e.CategoryLabel != cat {
			cat = e.CategoryLabel
			f.gap(12)
			k.legend(f, orStr(cat, "Other"))
		}
		v.row(f, h, e)
		shown++
	}
	if shown == 0 {
		f.gap(8)
		k.muted(f, "No plugins match.")
	}
}

func libraryMatches(e pluginlib.Entry, words []string) bool {
	hay := strings.ToLower(strings.Join(append([]string{e.ID, e.Name, e.Description, e.CategoryLabel}, e.Credit()...), " "))
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// row: name (15/700) + tags, description, credit line, and the action button.
func (v *libraryView) row(f *flow, h libraryHost, e pluginlib.Entry) {
	k := v.k
	cl := k.colors()
	code, main := h.installed(e.ID)
	state := pluginlib.StateOf(e, code, main)
	f.gap(6)
	f.put(0, func(gtx layout.Context) layout.Dimensions {
		bw := gtx.Dp(104)
		tw := f.w - bw - gtx.Dp(12)
		y := 0
		name := e.Name
		var tags []string
		if e.HackClubOnly {
			tags = append(tags, "Hack Club only")
		}
		if e.UpstreamDisabled {
			tags = append(tags, "disabled upstream")
		}
		nd := at(gtx, image.Pt(0, y), image.Pt(tw, gtx.Dp(22)), func(gtx layout.Context) layout.Dimensions {
			return k.label(gtx, 15, theme.Bold, cl.fg, name)
		})
		if len(tags) > 0 {
			at(gtx, image.Pt(nd.Size.X+gtx.Dp(8), y+gtx.Dp(2)), image.Pt(max(0, tw-nd.Size.X-gtx.Dp(8)), gtx.Dp(20)), func(gtx layout.Context) layout.Dimensions {
				return k.label(gtx, 12, theme.Regular, cl.fg3, strings.Join(tags, " · "))
			})
		}
		y += gtx.Dp(22)
		if e.Description != "" {
			d := at(gtx, image.Pt(0, y), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Point{}
				return k.para(gtx, 13, 18, cl.fg2, plainMrkdwn(e.Description))
			})
			y += d.Size.Y
		}
		credit := v.sourceLine()
		if c := e.Credit(); len(c) > 0 {
			credit = "by " + strings.Join(c, ", ") + " · " + credit
		}
		d := at(gtx, image.Pt(0, y+gtx.Dp(2)), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = image.Point{}
			return k.para(gtx, 12, 16, cl.fg3, credit)
		})
		y += d.Size.Y + gtx.Dp(2)
		if st := v.status[e.ID]; st != "" {
			col := cl.fg3
			if v.failed[e.ID] {
				col = cl.err
			}
			d := at(gtx, image.Pt(0, y+gtx.Dp(2)), image.Pt(tw, 1<<20), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min = image.Point{}
				return k.para(gtx, 12, 16, col, st)
			})
			y += d.Size.Y + gtx.Dp(2)
		}

		// action
		label, kind := "Install", popups.BtnPrimary
		switch {
		case v.busy[e.ID]:
			label, kind = "Installing...", popups.BtnDisabled
			if state != pluginlib.NotInstalled {
				label = "Updating..."
			}
		case state == pluginlib.UpdateAvailable:
			label, kind = "Update", popups.BtnPrimary
		case state == pluginlib.Installed:
			label, kind = "Installed", popups.BtnOutline
			if h.open != nil {
				label = "Settings"
			}
		}
		b := k.btn("lib." + v.sys + ".act." + e.ID)
		hit := b.Clicked(gtx)
		at(gtx, image.Pt(f.w-bw, 0), image.Pt(bw, gtx.Dp(28)), func(gtx layout.Context) layout.Dimensions {
			return popups.Button(gtx, k.a.th, b, kind, popups.BtnSmall, label, "", 104)
		})
		if hit && !v.busy[e.ID] {
			switch state {
			case pluginlib.NotInstalled:
				v.installEntry(h, e, false)
			case pluginlib.UpdateAvailable:
				v.installEntry(h, e, true)
			default:
				if h.open != nil {
					h.open(e.ID)
				}
			}
		}
		return layout.Dimensions{Size: image.Pt(f.w, max(y, gtx.Dp(28)))}
	})
	f.gap(6)
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
