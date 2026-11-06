// Command popupharness opens internal/ui/popups in a bare Gio window against
// the real core, for fidelity checks. Temporary: deleted before ui-popups
// merges.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

type host struct {
	w    *app.Window
	core *core.App
	th   *theme.Theme
	img  *imgcache.Cache

	overlays []popups.Overlay
	route    string
	profile  string
	view     *popups.SearchView
	pview    *popups.ProfileView

	namedOpen map[string]func(at image.Point)

	mu      sync.Mutex
	posted  []func()
	toast   string
	toastAt time.Time
}

func (h *host) Core() *core.App         { return h.core }
func (h *host) Theme() *theme.Theme     { return h.th }
func (h *host) Images() *imgcache.Cache { return h.img }
func (h *host) Invalidate()             { h.w.Invalidate() }
func (h *host) Navigate(route string) {
	log.Printf("navigate %s", route)
	h.route = route
	if strings.HasPrefix(route, "search?q=") {
		q, _ := url.QueryUnescape(strings.TrimPrefix(route, "search?q="))
		h.view = popups.NewSearchView(h, q)
	}
	h.w.Invalidate()
}
func (h *host) OpenThread(ch, ts string) { log.Printf("open thread %s %s", ch, ts) }
func (h *host) CloseSecondary()          { h.profile, h.pview = "", nil }
func (h *host) ShowOverlay(o popups.Overlay) {
	h.overlays = append(h.overlays, o)
	h.w.Invalidate()
}
func (h *host) OpenEmojiPicker(at image.Point, onPick func(string)) {
	h.ShowOverlay(popups.NewEmojiPickerAt(h, at, onPick))
}
func (h *host) OpenProfile(at image.Point, id string) {
	h.ShowOverlay(popups.NewProfileCard(h, at, id))
}
func (h *host) OpenMessageMenu(at image.Point, m core.Message) {
	h.ShowOverlay(popups.NewMessageMenu(h, at, m))
}
func (h *host) OpenLightbox(img richtext.Image, m core.Message) {
	h.ShowOverlay(popups.NewLightbox(h, img, m))
}
func (h *host) Toast(t string) {
	log.Printf("toast %q", t)
	h.toast, h.toastAt = t, time.Now()
	h.w.Invalidate()
}

type entry struct {
	name string
	open func(at image.Point)
	btn  widget.Clickable
	tip  popups.TipState
}

func main() {
	credsPath := flag.String("creds", "/root/.config/yoloslack/creds.json", "credentials")
	open := flag.String("open", "", "popup to open after boot")
	channel := flag.String("channel", "C0B4DEFKFCJ", "test channel")
	userFlag := flag.String("user", "", "user for the profile card (default: self)")
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
	cacheDir := filepath.Join(home, "yoloslack-popupharness")
	a := core.New(c, store.New(filepath.Join(cacheDir, "store")))
	booted := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := a.Boot(ctx); err != nil {
			log.Printf("boot: %v", err)
		}
		close(booted)
	}()

	w := new(app.Window)
	w.Option(app.Title("popupharness"), app.Size(unit.Dp(1440), unit.Dp(900)))
	h := &host{w: w, core: a, th: theme.New(*dark)}
	h.img = imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
		if imgcache.DiskOK(u) || !strings.Contains(u, "slack") {
			return httpGet(ctx, u)
		}
		b, _, err := a.Fetch(ctx, u) // files.slack.com: authenticated, memory only
		return b, err
	}, filepath.Join(cacheDir, "img"), w.Invalidate)
	popups.OpenFullProfile = func(id string) { h.profile, h.pview = id, popups.NewProfileView(h, id) }
	popups.OpenOwnProfile = func() { popups.OpenFullProfile(a.Self().ID) }

	// a message with an image and an own message, for the lightbox/menus.
	var msgs []core.Message
	go func() {
		<-booted
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if p, err := a.History(ctx, *channel, ""); err == nil {
			h.mu.Lock()
			msgs = p.Messages
			h.mu.Unlock()
		} else {
			log.Printf("history: %v", err)
		}
		if *open != "" {
			h.mu.Lock()
			h.posted = append(h.posted, func() { h.openNamed(*open) })
			h.mu.Unlock()
		}
		w.Invalidate()
	}()
	h.namedOpen = map[string]func(at image.Point){}
	ownMsg := func() (core.Message, bool) {
		h.mu.Lock()
		defer h.mu.Unlock()
		self := a.Self().ID
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].User == self {
				return msgs[i], true
			}
		}
		return core.Message{}, false
	}
	otherMsg := func() (core.Message, bool) {
		h.mu.Lock()
		defer h.mu.Unlock()
		self := a.Self().ID
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].User != self && msgs[i].User != "" {
				return msgs[i], true
			}
		}
		return core.Message{}, false
	}
	imageMsg := func() (richtext.Image, core.Message, bool) {
		h.mu.Lock()
		defer h.mu.Unlock()
		for i := len(msgs) - 1; i >= 0; i-- {
			for _, bl := range msgs[i].Doc.Blocks {
				if bl.File != nil && bl.File.IsImage && bl.File.Thumb != nil {
					im := *bl.File.Thumb
					if bl.File.URL != "" {
						im.URL = bl.File.URL
					}
					im.Proxy = true
					return im, msgs[i], true
				}
				if bl.Kind == richtext.ImageBlock && bl.Image != nil {
					return *bl.Image, msgs[i], true
				}
			}
		}
		return richtext.Image{}, core.Message{}, false
	}
	profileUser := func() string {
		if *userFlag != "" {
			return *userFlag
		}
		if m, ok := otherMsg(); ok {
			return m.User
		}
		return a.Self().ID
	}
	entries := []*entry{
		{name: "emoji", open: func(at image.Point) {
			h.OpenEmojiPicker(image.Pt(420, 700), func(n string) { h.Toast("picked :" + n + ":") })
		}},
		{name: "switcher", open: func(image.Point) { h.ShowOverlay(popups.NewQuickSwitcher(h)) }},
		{name: "search", open: func(image.Point) {
			h.ShowOverlay(popups.NewSearchPopover(h, "", image.Rect(398, 6, 1031, 34)))
		}},
		{name: "results", open: func(image.Point) { h.Navigate("search?q=" + url.QueryEscape("yoloslack")) }},
		{name: "card", open: func(at image.Point) { h.OpenProfile(image.Pt(414, 271), profileUser()) }},
		{name: "cardself", open: func(at image.Point) { h.OpenProfile(image.Pt(414, 271), a.Self().ID) }},
		{name: "profile", open: func(image.Point) { popups.OpenFullProfile(profileUser()) }},
		{name: "lightbox", open: func(image.Point) {
			if im, m, ok := imageMsg(); ok {
				h.OpenLightbox(im, m)
			} else {
				h.Toast("no image in channel history")
			}
		}},
		{name: "msgmenu", open: func(image.Point) {
			if m, ok := otherMsg(); ok {
				h.OpenMessageMenu(image.Pt(1390, 187), m)
			}
		}},
		{name: "ownmenu", open: func(image.Point) {
			if m, ok := ownMsg(); ok {
				h.OpenMessageMenu(image.Pt(1390, 135), m)
			}
		}},
		{name: "delete", open: func(image.Point) {
			if m, ok := ownMsg(); ok {
				popups.ConfirmDelete(h, m) // the harness never presses Delete
			}
		}},
		{name: "forward", open: func(image.Point) {
			if m, ok := otherMsg(); ok {
				h.ShowOverlay(popups.NewForwardModal(h, m))
			}
		}},
		{name: "chanmenu", open: func(image.Point) {
			h.ShowOverlay(popups.NewChannelMenu(h, image.Pt(1424, 74), *channel))
		}},
		{name: "teammenu", open: func(image.Point) { h.ShowOverlay(popups.NewTeamMenu(h, image.Pt(58, 91))) }},
		{name: "usermenu", open: func(image.Point) { h.ShowOverlay(popups.NewUserMenu(h, image.Pt(65, 875))) }},
		{name: "plusmenu", open: func(image.Point) {
			h.ShowOverlay(popups.NewComposerPlusMenu(h, image.Pt(405, 780), nil))
		}},
		{name: "status", open: func(image.Point) { h.ShowOverlay(popups.NewSetStatusModal(h)) }},
		{name: "toast", open: func(image.Point) { h.ShowOverlay(popups.NewToast(h, "Link copied")) }},
		{name: "pill", open: func(image.Point) {
			h.ShowOverlay(popups.NewHintPill(h, "Tip: Open this faster with Ctrl + Shift + Y"))
		}},
	}
	for _, e := range entries {
		h.namedOpen[e.name] = e.open
	}
	go func() {
		if err := h.run(entries); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func httpGet(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

func (h *host) openNamed(name string) {
	if f, ok := h.namedOpen[name]; ok {
		f(image.Pt(600, 400))
	} else {
		log.Printf("unknown popup %q", name)
	}
}

func (h *host) run(entries []*entry) error {
	var ops op.Ops
	for {
		switch e := h.w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			h.mu.Lock()
			posted := h.posted
			h.posted = nil
			h.mu.Unlock()
			for _, f := range posted {
				f()
			}
			popups.Pump()
			// Escape closes the topmost overlay (the ui root's routing).
			for {
				ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
				if !ok {
					break
				}
				if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
					h.dismissTop()
				}
			}
			for _, en := range entries {
				if en.btn.Clicked(gtx) {
					en.open(image.Pt(160, 100))
				}
			}
			win := gtx.Constraints.Max
			theme.Fill(gtx, image.Rectangle{Max: win}, color.NRGBA{R: 0x3f, G: 0x0e, B: 0x40, A: 0xff})
			h.shell(gtx, entries)
			live := h.overlays[:0]
			for _, o := range h.overlays {
				if !o.Dismissed() {
					live = append(live, o)
				}
			}
			h.overlays = live
			for _, o := range h.overlays {
				g := gtx
				g.Constraints = layout.Constraints{Max: win}
				o.Layout(g)
			}
			h.drawToast(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (h *host) dismissTop() {
	for i := len(h.overlays) - 1; i >= 0; i-- {
		o := h.overlays[i]
		if o.Dismissed() {
			continue
		}
		if d, ok := o.(interface{ Dismiss() }); ok {
			d.Dismiss()
		}
		h.overlays = append(h.overlays[:i], h.overlays[i+1:]...)
		return
	}
}

// shell is a stand-in for the real ui shell: a rail of popup buttons, a
// white primary pane (search results), and a profile pane.
func (h *host) shell(gtx layout.Context, entries []*entry) {
	win := gtx.Constraints.Max
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	// top-nav pill stand-in
	theme.RRect(gtx, image.Rect(gtx.Dp(398), gtx.Dp(6), gtx.Dp(1031), gtx.Dp(34)), gtx.Dp(6), color.NRGBA{R: 0xf9, G: 0xed, B: 0xff, A: 0x40})
	// rail buttons
	y := gtx.Dp(44)
	for _, en := range entries {
		func() {
			defer op.Offset(image.Pt(gtx.Dp(4), y)).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(gtx.Dp(62), gtx.Dp(36)))
			en.btn.Layout(g, func(gtx layout.Context) layout.Dimensions {
				if en.btn.Hovered() {
					theme.RRect(gtx, image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(6), color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x30})
				}
				defer op.Offset(image.Pt(gtx.Dp(4), gtx.Dp(9))).Push(gtx.Ops).Pop()
				g := gtx
				g.Constraints.Min = image.Point{}
				h.th.Label(g, 11, theme.Bold, white, en.name)
				return layout.Dimensions{Size: gtx.Constraints.Max}
			})
			popups.Tooltip(gtx, h.th, &en.tip, en.btn.Hovered(), image.Pt(gtx.Dp(62), gtx.Dp(36)), popups.Right, "Open "+en.name, "Ctrl+Shift+"+strings.ToUpper(en.name[:1]))
		}()
		y += gtx.Dp(38)
	}
	// primary pane
	pane := image.Rect(gtx.Dp(70), gtx.Dp(40), win.X-gtx.Dp(4), win.Y-gtx.Dp(4))
	right := pane.Max.X
	if h.pview != nil {
		right = pane.Max.X - gtx.Dp(683)
	}
	theme.RRect(gtx, image.Rect(pane.Min.X, pane.Min.Y, right, pane.Max.Y), gtx.Dp(6), white)
	if h.view != nil && strings.HasPrefix(h.route, "search") {
		func() {
			defer op.Offset(pane.Min).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(image.Pt(right-pane.Min.X, pane.Dy()))
			h.view.SetOrigin(pane.Min)
			h.view.Layout(g)
		}()
	}
	if h.pview != nil {
		func() {
			r := image.Rect(right+gtx.Dp(1), pane.Min.Y, pane.Max.X, pane.Max.Y)
			theme.Fill(gtx, r, white)
			defer op.Offset(r.Min).Push(gtx.Ops).Pop()
			g := gtx
			g.Constraints = layout.Exact(r.Size())
			h.pview.Layout(g)
		}()
	}
}

func (h *host) drawToast(gtx layout.Context) {
	if h.toast == "" || gtx.Now.Sub(h.toastAt) > 4*time.Second {
		return
	}
	gtx.Execute(op.InvalidateCmd{At: h.toastAt.Add(4 * time.Second)})
	m := op.Record(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	d := layout.UniformInset(12).Layout(g, func(gtx layout.Context) layout.Dimensions {
		return h.th.Label(gtx, 15, theme.Bold, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, h.toast)
	})
	call := m.Stop()
	x := (gtx.Constraints.Max.X - d.Size.X) / 2
	y := gtx.Constraints.Max.Y - d.Size.Y - gtx.Dp(32)
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	theme.RRect(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(8), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xf0})
	call.Add(gtx.Ops)
}
