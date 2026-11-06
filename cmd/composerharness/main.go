// Command composerharness opens internal/ui/composer in a bare Gio window
// against the real core, for fidelity checks. Temporary: deleted before
// ui-composer merges.
//
// The window mimics Slack's geometry at 1440x900: the message pane is
// x 398..1436, bottom 844; the composer sits at its bottom. -thread TS adds
// a 415px thread pane on the right. Up in the empty channel composer opens
// an inline edit composer for your last message in the channel.
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
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/x/explorer"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/composer"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

type host struct {
	w    *app.Window
	core *core.App
	th   *theme.Theme
	img  *imgcache.Cache

	mu       sync.Mutex
	toast    string
	toAt     time.Time
	overlays []popups.Overlay
	pick     func(string)
}

func (h *host) Core() *core.App         { return h.core }
func (h *host) Theme() *theme.Theme     { return h.th }
func (h *host) Images() *imgcache.Cache { return h.img }
func (h *host) Invalidate()             { h.w.Invalidate() }
func (h *host) Navigate(route string)   { log.Printf("navigate %s", route) }
func (h *host) OpenThread(ch, ts string) {
	log.Printf("open thread %s %s", ch, ts)
}
func (h *host) CloseSecondary() {}
func (h *host) ShowOverlay(o popups.Overlay) {
	h.mu.Lock()
	h.overlays = append(h.overlays, o)
	h.mu.Unlock()
	h.w.Invalidate()
}

// OpenEmojiPicker has no picker here (ui-popups owns it); it picks
// :thumbsup: so the insertion path can be exercised.
func (h *host) OpenEmojiPicker(at image.Point, onPick func(string)) {
	log.Printf("emoji picker at %v (harness picks thumbsup)", at)
	onPick("thumbsup")
}
func (h *host) OpenProfile(at image.Point, id string)           {}
func (h *host) OpenMessageMenu(at image.Point, m core.Message)  {}
func (h *host) OpenLightbox(img richtext.Image, m core.Message) {}
func (h *host) Toast(t string) {
	log.Printf("toast %q", t)
	h.mu.Lock()
	h.toast, h.toAt = t, time.Now()
	h.mu.Unlock()
	h.w.Invalidate()
}

func main() {
	credsPath := flag.String("creds", "/root/.config/yoloslack/creds.json", "credentials")
	channel := flag.String("channel", "C0B4DEFKFCJ", "channel")
	thread := flag.String("thread", "", "thread ts: also show a thread composer")
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
	cacheDir := filepath.Join(home, "yoloslack-composerharness")
	a := core.New(c, store.New(filepath.Join(cacheDir, "store")))
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := a.Boot(ctx); err != nil {
			log.Printf("boot: %v", err)
		}
	}()

	w := new(app.Window)
	w.Option(app.Title("composerharness"), app.Size(unit.Dp(1440), unit.Dp(900)))
	h := &host{w: w, core: a, th: theme.New(*dark)}
	h.img = imgcache.New(func(ctx context.Context, u string) ([]byte, error) {
		b, _, err := a.Fetch(ctx, u)
		return b, err
	}, filepath.Join(cacheDir, "img"), w.Invalidate)

	go func() {
		if err := run(w, h, *channel, *thread); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(w *app.Window, h *host, channel, thread string) error {
	ex := explorer.NewExplorer(w)
	composer.SetExplorer(ex)
	main := composer.New(h, channel, "")
	var side *composer.Composer
	if thread != "" {
		side = composer.New(h, channel, thread)
	}
	var edit *composer.Composer
	var editMu sync.Mutex
	main.OnEditLast = func() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			p, err := h.core.History(ctx, channel, "")
			if err != nil {
				h.Toast("history: " + err.Error())
				return
			}
			self := h.core.Self().ID
			for i := len(p.Messages) - 1; i >= 0; i-- {
				m := p.Messages[i]
				if m.User == self && !m.System {
					editMu.Lock()
					edit = composer.NewEdit(h, m, func() { log.Printf("edit done") })
					editMu.Unlock()
					h.Invalidate()
					return
				}
			}
			h.Toast("no message of yours to edit")
		}()
	}
	mainH, sideH := 0, 0
	var ops op.Ops
	for {
		ev := w.Event()
		ex.ListenEvents(ev)
		switch e := ev.(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			sz := gtx.Constraints.Max
			theme.Fill(gtx, image.Rectangle{Max: sz}, color.NRGBA{R: 0x3f, G: 0x0e, B: 0x40, A: 0xff})
			paneL, paneR, paneB := gtx.Dp(398), sz.X-gtx.Dp(4), sz.Y-gtx.Dp(56)
			if side != nil {
				paneR = sz.X - gtx.Dp(4) - gtx.Dp(415) - gtx.Dp(1)
			}
			theme.Fill(gtx, image.Rect(paneL, gtx.Dp(40), paneR, paneB), h.th.C.Bg)
			label(gtx, h, image.Pt(paneL+gtx.Dp(20), gtx.Dp(60)), "composerharness · channel "+channel)

			editMu.Lock()
			ed := edit
			if ed != nil && ed.Done() {
				edit, ed = nil, nil
			}
			editMu.Unlock()
			if ed != nil {
				x0 := paneL + gtx.Dp(60)
				y0 := gtx.Dp(200)
				theme.Fill(gtx, image.Rect(paneL, y0-gtx.Dp(8), paneR, y0+gtx.Dp(140)), color.NRGBA{R: 0xfc, G: 0xf4, B: 0xda, A: 0xff})
				ed.Origin = image.Pt(x0, y0)
				place(gtx, image.Pt(x0, y0), image.Pt(paneR-gtx.Dp(16)-x0, gtx.Dp(400)), ed.Layout)
			}

			main.Origin = image.Pt(paneL, paneB-mainH)
			d := place(gtx, main.Origin, image.Pt(paneR-paneL, paneB-gtx.Dp(40)), main.Layout)
			if d.Size.Y != mainH {
				mainH = d.Size.Y
				h.Invalidate()
			}
			if side != nil {
				sl := paneR + gtx.Dp(1)
				theme.Fill(gtx, image.Rect(sl, gtx.Dp(40), sz.X-gtx.Dp(4), paneB), h.th.C.Bg)
				side.Origin = image.Pt(sl, paneB-sideH)
				d := place(gtx, side.Origin, image.Pt(sz.X-gtx.Dp(4)-sl, paneB-gtx.Dp(40)), side.Layout)
				if d.Size.Y != sideH {
					sideH = d.Size.Y
					h.Invalidate()
				}
			}
			popups.Pump()
			drawOverlays(gtx, h)
			drawToast(gtx, h)
			e.Frame(gtx.Ops)
		}
	}
}

// place lays w out at pt with max size max (bottom-aligned callers pass
// last frame's height in pt).
func place(gtx layout.Context, pt, max image.Point, w layout.Widget) layout.Dimensions {
	defer op.Offset(pt).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints = layout.Constraints{Min: image.Pt(max.X, 0), Max: max}
	return w(g)
}

func label(gtx layout.Context, h *host, pt image.Point, s string) {
	defer op.Offset(pt).Push(gtx.Ops).Pop()
	g := gtx
	g.Constraints.Min = image.Point{}
	h.th.Label(g, 13, theme.Regular, h.th.C.FgMuted, s)
}

func drawOverlays(gtx layout.Context, h *host) {
	h.mu.Lock()
	ovs := append([]popups.Overlay(nil), h.overlays...)
	h.mu.Unlock()
	live := ovs[:0]
	for _, o := range ovs {
		g := gtx
		g.Constraints = layout.Constraints{Max: gtx.Constraints.Max} // like the ui root
		o.Layout(g)
		if !o.Dismissed() {
			live = append(live, o)
		}
	}
	h.mu.Lock()
	h.overlays = append(live, h.overlays[len(ovs):]...)
	h.mu.Unlock()
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
	y := gtx.Dp(8)
	defer op.Offset(image.Pt(x, y)).Push(gtx.Ops).Pop()
	theme.RRect(gtx, image.Rectangle{Max: d.Size}, gtx.Dp(8), color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xf0})
	call.Add(gtx.Ops)
}
