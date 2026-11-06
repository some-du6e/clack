// Command msgharness is a temporary dev harness for internal/ui/messages:
// a Gio window showing a ChannelView (and a thread pane) over a real
// core.App, with frame-time and channel-switch measurements. It is deleted
// once the ui root mounts the views.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/messages"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

type host struct {
	w      *app.Window
	core   *core.App
	th     *theme.Theme
	images *imgcache.Cache
	mu     sync.Mutex
	thread *messages.ThreadView
	route  string
}

func (h *host) Core() *core.App          { return h.core }
func (h *host) Theme() *theme.Theme      { return h.th }
func (h *host) Images() *imgcache.Cache  { return h.images }
func (h *host) Invalidate()              { h.w.Invalidate() }
func (h *host) Navigate(route string)    { log.Printf("navigate %s", route); h.route = route }
func (h *host) CloseSecondary()          { h.thread = nil }
func (h *host) Toast(text string)        { log.Printf("toast: %s", text) }
func (h *host) OpenThread(ch, ts string) { h.thread = messages.NewThreadView(h, ch, ts) }
func (h *host) OpenEmojiPicker(a image.Point, f func(string)) {
	log.Printf("emoji picker at %v", a)
}
func (h *host) OpenProfile(a image.Point, id string) { log.Printf("profile %s at %v", id, a) }
func (h *host) OpenMessageMenu(a image.Point, m core.Message) {
	log.Printf("message menu %s at %v", m.TS, a)
}
func (h *host) OpenLightbox(img richtext.Image, m core.Message) {
	log.Printf("lightbox %dx%d", img.W, img.H)
}

var httpClient = &http.Client{Timeout: 45 * time.Second}

func httpFetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

type stats struct {
	d []time.Duration
}

func (s *stats) add(d time.Duration) { s.d = append(s.d, d) }
func (s *stats) report(name string) {
	if len(s.d) == 0 {
		return
	}
	x := append([]time.Duration(nil), s.d...)
	sort.Slice(x, func(i, j int) bool { return x[i] < x[j] })
	var sum time.Duration
	for _, v := range x {
		sum += v
	}
	log.Printf("%s: n=%d avg=%s p50=%s p95=%s max=%s", name, len(x), sum/time.Duration(len(x)), x[len(x)/2], x[len(x)*95/100], x[len(x)-1])
	s.d = s.d[:0]
}

func main() {
	chFlag := flag.String("ch", "C0B4DEFKFCJ", "channel to show")
	switchTo := flag.String("switch", "", "comma-separated channels to cycle with Tab (preloaded, read-only previews)")
	find := flag.String("find", "", "comma-separated public channel names to add to the Tab cycle (read-only previews)")
	dark := flag.Bool("dark", false, "dark mode")
	w := flag.Int("w", 1100, "window width (dp)")
	hgt := flag.Int("h", 800, "window height (dp)")
	flag.Parse()

	home, _ := os.UserConfigDir()
	cfg := filepath.Join(home, "yoloslack")
	var creds slack.Creds
	b, err := os.ReadFile(filepath.Join(cfg, "creds.json"))
	if err != nil {
		log.Fatal("no creds")
	}
	if err := json.Unmarshal(b, &creds); err != nil {
		log.Fatal("bad creds")
	}
	if creds.TeamURL == "" {
		creds.TeamURL = "https://slack.com/"
	}
	c := slack.New(creds)
	st := store.New(filepath.Join(os.TempDir(), "msgharness-cache"))
	svc := core.New(c, st)

	win := new(app.Window)
	win.Option(app.Title("msgharness"), app.Size(unit.Dp(*w), unit.Dp(*hgt)))
	h := &host{w: win, core: svc, th: theme.New(*dark)}
	h.images = imgcache.New(func(ctx context.Context, url string) ([]byte, error) {
		if imgcache.DiskOK(url) || !strings.Contains(url, "slack") {
			return httpFetch(ctx, url)
		}
		b, _, err := svc.Fetch(ctx, url)
		return b, err
	}, filepath.Join(os.TempDir(), "msgharness-img"), win.Invalidate)

	go func() {
		t0 := time.Now()
		if err := svc.Boot(context.Background()); err != nil {
			log.Printf("boot: %v", err)
		}
		log.Printf("boot %s", time.Since(t0).Round(time.Millisecond))
		win.Invalidate()
	}()

	chans := []string{*chFlag}
	if *switchTo != "" && *switchTo != "auto" {
		chans = append(chans, strings.Split(*switchTo, ",")...)
	}
	var chMu sync.Mutex
	go func() {
		<-svc.Ready()
		if *switchTo == "auto" {
			snap := svc.Snapshot()
			auto := []string{chans[0]}
			for _, c := range snap.Convs {
				if c.Kind == "channel" && c.ID != chans[0] && len(auto) < 7 {
					auto = append(auto, c.ID)
				}
			}
			chMu.Lock()
			chans = auto
			chMu.Unlock()
			log.Printf("auto channels: %d", len(auto)-1)
		}
		if *find != "" {
			for _, name := range strings.Split(*find, ",") {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				hits, err := svc.SearchChannels(ctx, name, 5)
				cancel()
				for _, h := range hits {
					if h.Name == name {
						chMu.Lock()
						chans = append(chans, h.ID)
						chMu.Unlock()
						log.Printf("found #%s = %s", name, h.ID)
						break
					}
				}
				if err != nil {
					log.Printf("find %s: %v", name, err)
				}
			}
		}
		chMu.Lock()
		pre := append([]string(nil), chans[1:]...)
		chMu.Unlock()
		for _, ch := range pre {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, err := svc.History(ctx, ch, "")
			cancel()
			log.Printf("preload %s err=%v", ch, err)
		}
	}()

	go func() {
		var ops op.Ops
		cur := 0
		views := map[string]*messages.ChannelView{}
		mk := func(ch string) *messages.ChannelView {
			v := messages.NewChannelView(h, ch)
			v.Preview = ch != "C0B4DEFKFCJ"
			return v
		}
		<-svc.Ready()
		view := mk(chans[0])
		views[chans[0]] = view
		var frames, switches stats
		switchStart := time.Time{}
		lastReport := time.Now()
		for {
			switch e := win.Event().(type) {
			case app.DestroyEvent:
				frames.report("frame layout")
				switches.report("switch first layout")
				os.Exit(0)
			case app.FrameEvent:
				gtx := app.NewContext(&ops, e)
				for {
					ev, ok := gtx.Event(key.Filter{Name: key.NameTab}, key.Filter{Name: "D", Required: key.ModShortcut})
					if !ok {
						break
					}
					if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
						chMu.Lock()
						if ke.Name == key.NameTab && len(chans) > 1 {
							cur = (cur + 1) % len(chans)
							switchStart = time.Now()
							// A fresh view each time, like the ui root does.
							view = mk(chans[cur])
							log.Printf("now showing channel index %d (%s)", cur, chans[cur])
						}
						chMu.Unlock()
						if ke.Name == "D" {
							h.th.SetDark(!h.th.Dark)
						}
					}
				}
				t0 := time.Now()
				size := gtx.Constraints.Max
				paneW := 0
				if h.thread != nil {
					paneW = gtx.Dp(400)
				}
				g := gtx
				g.Constraints = layout.Exact(image.Pt(size.X-paneW, size.Y))
				view.Layout(g)
				if h.thread != nil {
					t := op.Offset(image.Pt(size.X-paneW, 0)).Push(gtx.Ops)
					g.Constraints = layout.Exact(image.Pt(paneW, size.Y))
					h.thread.Layout(g)
					t.Pop()
				}
				d := time.Since(t0)
				if !switchStart.IsZero() {
					switches.add(d)
					log.Printf("switch: first layout %s", d)
					switchStart = time.Time{}
				} else {
					frames.add(d)
				}
				if time.Since(lastReport) > 5*time.Second {
					frames.report("frame layout")
					lastReport = time.Now()
				}
				e.Frame(gtx.Ops)
			}
		}
	}()
	app.Main()
}
