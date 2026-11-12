package popups

// Work objects on the results page (work_objects_message_search_xp_v2)
// against internal/fakeslack's #work-objects (synthetic data, nothing
// talks to Slack): a messages search also asks search.modules.workObjects,
// each app with matches becomes a source in the [Messages ▾] menu with its
// count, and picking it shows the app's cards (detailed, or compact with
// "View as dense"). With SEARCH_SHOTS_DIR set (the search workflow) both
// layouts are saved, light and dark.

import (
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/gpu/headless"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func srWOEnv(t *testing.T, dark bool) *srEnv {
	t.Helper()
	sz := fakeslack.Small
	sz.WorkObjects = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	noNet := func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	th := theme.New(dark)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	srResetRecents()
	t.Cleanup(srResetRecents)
	prev := RichStyle
	t.Cleanup(func() { RichStyle = prev })
	st := richview.NewStyle(sh, nil, richview.LightPalette())
	if dark {
		st.SetPalette(richview.DarkPalette())
	}
	st.Face = theme.Typeface
	RichStyle = func(Host) *richview.Style {
		f := a.WorkObjectFlags()
		st.SetWorkObjects(richview.WorkObjectFlags{Rearrange: f.Rearrange, RefreshTimestamp: f.RefreshTimestamp, Locale: "en-US"})
		return st
	}
	return &srEnv{h: &tzHost{a: a, th: th, im: imgcache.New(noNet, "", nil)}, fs: fs}
}

// searchWorkObjects: the items grouped into one source per app, in the
// order the apps first appear, with Slack's params.
func TestSearchWorkObjectsGrouping(t *testing.T) {
	e := srWOEnv(t, false)
	srcs, err := e.h.a.SearchWorkObjects(context.Background(), "example", "timestamp", core.NewSearchSession())
	if err != nil {
		t.Fatal(err)
	}
	if len(srcs) != 2 || srcs[0].AppID != fakeslack.WOTrackerAppID || len(srcs[0].Hits) != 1 ||
		srcs[1].AppID != fakeslack.WODocsAppID || len(srcs[1].Hits) != 2 || !srcs[0].Expanded {
		t.Fatalf("sources %+v", srcs)
	}
	forms := e.fs.SearchForms("search.modules.workObjects")
	if len(forms) != 1 {
		t.Fatalf("%d calls", len(forms))
	}
	f := forms[0]
	if f.Get("reason") != "search-work-objects" || f.Get("sort") != "timestamp" || f.Get("sort_dir") != "desc" ||
		f.Get("search_context") != "desktop_work_object_tab" || f.Get("query") != "example" {
		t.Errorf("params %v", f)
	}
}

// The results page's work object sources and results.
func TestSearchWorkObjectSources(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := srWOEnv(t, dark)
			v, d := srView(t, e, "cards", dark)
			d.until(t, "the work object sources", func() bool { return len(v.wo.srcs) == 1 })
			if s := v.wo.srcs[0]; s.AppName != "Example Tracker" || len(s.Hits) != 1 {
				t.Fatalf("source %+v", s)
			}
			// the source menu lists the app after the standard sources
			v.typeMenu(layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(d.size)})
			m := srMenu(e.h)
			if m == nil {
				t.Fatal("no source menu")
			}
			labels := srLabels(m.items)
			if len(labels) == 0 || labels[len(labels)-1] != "Example Tracker" {
				t.Fatalf("source menu %q, want Example Tracker last", labels)
			}
			m.items[len(m.items)-1].Do()
			d.frame()
			if v.woSource() == nil {
				t.Fatal("the app's source isn't selected")
			}
			d.until(t, "the result card", func() bool {
				rv := v.wo.views[fakeslack.WOTaskID]
				return rv != nil && len(rv.WorkObjectRects()) == 1
			})
			srWOShot(t, d, "search-work-objects-detailed-"+mode)
			v.wo.dense = true
			d.frame()
			d.frame()
			srWOShot(t, d, "search-work-objects-dense-"+mode)
			// a standard source again
			v.setTab(srTabMessages)
			if v.woSource() != nil {
				t.Error("still on the work object source")
			}
		})
	}
}

func srWOShot(t *testing.T, d *srDrv, name string) {
	t.Helper()
	dir := os.Getenv("SEARCH_SHOTS_DIR")
	if dir == "" {
		return
	}
	w, err := headless.NewWindow(d.size.X, d.size.Y)
	if err != nil {
		t.Fatalf("headless window: %v", err)
	}
	defer w.Release()
	d.frame()
	d.frame()
	if err := w.Frame(&d.ops); err != nil {
		t.Fatalf("gpu frame: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, d.size.X, d.size.Y))
	if err := w.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
