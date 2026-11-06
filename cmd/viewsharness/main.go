// Command viewsharness opens internal/ui/views in a bare Gio window against
// the real core, for fidelity checks. Temporary: deleted before ui-views
// merges.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/views"
)

type host struct {
	w     *app.Window
	core  *core.App
	th    *theme.Theme
	img   *imgcache.Cache
	route string

	mu    sync.Mutex
	toast string
	toAt  time.Time
}

func (h *host) Core() *core.App         { return h.core }
func (h *host) Theme() *theme.Theme     { return h.th }
func (h *host) Images() *imgcache.Cache { return h.img }
func (h *host) Invalidate()             { h.w.Invalidate() }
func (h *host) Navigate(route string) {
	log.Printf("navigate %s", route)
	h.mu.Lock()
	h.route = route
	h.mu.Unlock()
	h.w.Invalidate()
}
func (h *host) OpenThread(ch, ts string) { log.Printf("open thread %s %s", ch, ts) }
func (h *host) CloseSecondary()          { log.Printf("close secondary") }
func (h *host) OpenProfile(at image.Point, id string) {
	log.Printf("open profile %s at %v", id, at)
}
func (h *host) OpenMessageMenu(at image.Point, m core.Message) {
	log.Printf("message menu %s/%s", m.Channel, m.TS)
}
func (h *host) OpenLightbox(img richtext.Image, m core.Message) {
	log.Printf("lightbox %s", img.URL)
}
func (h *host) Toast(t string) {
	log.Printf("toast %q", t)
	h.mu.Lock()
	h.toast, h.toAt = t, time.Now()
	h.mu.Unlock()
	h.w.Invalidate()
}

type entry struct {
	name string
	w    func(gtx layout.Context) layout.Dimensions
	btn  widget.Clickable
}

func main() {
	credsPath := flag.String("creds", "/root/.config/yoloslack/creds.json", "credentials")
	view := flag.String("view", "activity", "initial view")
	channel := flag.String("channel", "C0B4DEFKFCJ", "channel for details")
	dark := flag.Bool("dark", false, "dark theme")
	flag.Parse()

	b, err := os.ReadFile(*credsPath)
	if err != nil {
		log.Fatal(err)
	}
	var creds slack.Creds
	if err := json.Unmarshal(b, &creds); err != nil {
		log.Fatal(err)
	}
	if creds.TeamURL == "" {
		creds.TeamURL = "https://slack.com/"
	}
	c := slack.New(creds)
	home, _ := os.UserCacheDir()
	cacheDir := filepath.Join(home, "yoloslack-viewsharness")
	a := core.New(c, store.New(filepath.Join(cacheDir, "store")))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := a.Boot(ctx); err != nil {
			log.Printf("boot: %v", err)
		}
	}()

	w := new(app.Window)
	w.Option(app.Title("viewsharness"), app.Size(unit.Dp(1440), unit.Dp(900)))
	h := &host{w: w, core: a, th: theme.New(*dark), route: *view}
	h.img = imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
		b, _, err := a.Fetch(ctx, u)
		return b, err
	}, filepath.Join(cacheDir, "img"), w.Invalidate)

	go func() {
		if err := run(w, h, *channel); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(w *app.Window, h *host, channel string) error {
	act := views.NewActivity(h)
	dms := views.NewDMs(h)
	later := views.NewLater(h)
	saved := views.NewSaved(h)
	files := views.NewFiles(h)
	threads := views.NewThreads(h)
	huddles := views.NewHuddles(h)
	drafts := views.NewDrafts(h)
	dirs := views.NewDirectories(h)
	var details *views.ChannelDetails
	entries := []*entry{
		{name: "activity", w: act.Layout},
		{name: "dms", w: dms.Layout},
		{name: "later", w: later.Layout},
		{name: "saved", w: saved.Layout},
		{name: "files", w: files.Layout},
		{name: "threads", w: threads.Layout},
		{name: "huddles", w: huddles.Layout},
		{name: "drafts", w: drafts.Layout},
		{name: "directories", w: dirs.Layout},
		{name: "details", w: func(gtx layout.Context) layout.Dimensions {
			if details == nil || details.Closed() {
				details = views.NewChannelDetails(h, channel)
			}
			return details.Layout(gtx)
		}},
	}
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			h.mu.Lock()
			route := h.route
			h.mu.Unlock()
			for _, en := range entries {
				if en.btn.Clicked(gtx) {
					h.mu.Lock()
					h.route = en.name
					route = en.name
					h.mu.Unlock()
				}
			}
			if strings.HasPrefix(route, "details/") {
				channel = strings.TrimPrefix(route, "details/")
				details = nil
				route = "details"
				h.mu.Lock()
				h.route = route
				h.mu.Unlock()
			}
			layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return rail(gtx, h, entries, route)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = gtx.Constraints.Max
					theme.Fill(gtx, image.Rectangle{Max: gtx.Constraints.Max}, bg(h))
					for _, en := range entries {
						if en.name == route {
							return en.w(gtx)
						}
					}
					return h.th.Label(gtx, 15, theme.Regular, fg(h), "route: "+route)
				}),
			)
			drawToast(gtx, h)
			e.Frame(gtx.Ops)
		}
	}
}

func bg(h *host) color.NRGBA {
	if h.th.Dark {
		return color.NRGBA{R: 0x1a, G: 0x1d, B: 0x21, A: 0xff}
	}
	return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
}

func fg(h *host) color.NRGBA {
	if h.th.Dark {
		return color.NRGBA{R: 0xd1, G: 0xd2, B: 0xd3, A: 0xff}
	}
	return color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}
}

// rail is a plain aubergine column of view names (the real rail belongs to
// the ui-shell).
func rail(gtx layout.Context, h *host, entries []*entry, route string) layout.Dimensions {
	width := gtx.Dp(110)
	size := image.Pt(width, gtx.Constraints.Max.Y)
	theme.Fill(gtx, image.Rectangle{Max: size}, color.NRGBA{R: 0x3f, G: 0x0e, B: 0x40, A: 0xff})
	gtx.Constraints = layout.Exact(size)
	children := make([]layout.FlexChild, 0, len(entries))
	for _, en := range entries {
		en := en
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return en.btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				d := layout.Inset{Top: 8, Bottom: 8, Left: 12, Right: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					w := theme.Regular
					if en.name == route {
						w = theme.Bold
					}
					return h.th.Label(gtx, 13, w, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, en.name)
				})
				return d
			})
		}))
	}
	layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	return layout.Dimensions{Size: size}
}

func drawToast(gtx layout.Context, h *host) {
	h.mu.Lock()
	t, at := h.toast, h.toAt
	h.mu.Unlock()
	if t == "" || gtx.Now.Sub(at) > 4*time.Second {
		return
	}
	gtx.Execute(op.InvalidateCmd{At: at.Add(4 * time.Second)})
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	d := layout.UniformInset(12).Layout(g, func(gtx layout.Context) layout.Dimensions {
		return h.th.Label(gtx, 15, theme.Bold, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, t)
	})
	call := m.Stop()
	x := (gtx.Constraints.Max.X - d.Size.X) / 2
	y := gtx.Constraints.Max.Y - d.Size.Y - gtx.Dp(32)
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	theme.RRect(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(8), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xf0})
	call.Add(gtx.Ops)
}
