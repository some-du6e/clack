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
	"github.com/some-du6e/yoloslackgoclient/internal/ui/popups"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

type host struct {
	w        *app.Window
	core     *core.App
	th       *theme.Theme
	images   *imgcache.Cache
	mu       sync.Mutex
	thread   *messages.ThreadView
	route    string
	overlays []popups.Overlay
}

func (h *host) ShowOverlay(o popups.Overlay) { h.overlays = append(h.overlays, o) }

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
	scale := flag.Float64("scale", 0, "force a UI scale (e.g. 2 for a 2x comparison)")
	highlight := flag.String("highlight", "", "ts to jump to and flash in the first channel (route c/<id>/m/<ts>)")
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
		// read-state probe (ids/ts only, no contents)
		for i := 0; i < 6; i++ {
			cnt := svc.Count(chans[0])
			log.Printf("probe %s last_read=%s latest=%s unread=%v", chans[0], cnt.LastRead, cnt.Latest, cnt.Unread)
			time.Sleep(4 * time.Second)
		}
	}()
	go func() {
		<-svc.Ready()
		if *highlight == "auto-old" {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			p, err := svc.History(ctx, chans[0], "")
			if err == nil && len(p.Messages) > 0 {
				p2, err2 := svc.History(ctx, chans[0], p.Messages[0].TS)
				if err2 == nil && len(p2.Messages) > 10 {
					*highlight = p2.Messages[10].TS
				}
			}
			cancel()
			log.Printf("highlight target %s", *highlight)
		}
	}()
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
				if *scale > 0 {
					gtx.Metric.PxPerDp, gtx.Metric.PxPerSp = float32(*scale), float32(*scale)
				}
				for {
					ev, ok := gtx.Event(key.Filter{Name: key.NameTab}, key.Filter{Name: "D", Required: key.ModShortcut},
						key.Filter{Name: "E", Required: key.ModShortcut}, key.Filter{Name: "J", Required: key.ModShortcut},
						key.Filter{Name: "U", Required: key.ModShortcut},
						key.Filter{Name: "S", Required: key.ModShortcut}, key.Filter{Name: "F", Required: key.ModShortcut},
						key.Filter{Name: "B", Required: key.ModShortcut})
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
						if ke.Name == "E" {
							log.Printf("edit last: %v", view.EditLast())
						}
						if ke.Name == "U" {
							// Mark unread from the 4th-newest message (test channel only), then reopen.
							go func() {
								ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
								defer cancel()
								p, err := svc.History(ctx, "C0B4DEFKFCJ", "")
								if err == nil && len(p.Messages) > 4 {
									_, err = svc.MarkUnread(ctx, "C0B4DEFKFCJ", p.Messages[len(p.Messages)-4].TS)
								}
								log.Printf("mark unread: %v", err)
							}()
						}
						if ke.Name == "S" || ke.Name == "F" {
							// Optimistic send test in the test channel only. S: a real
							// send, deleted again after 10 s. F: a send Slack rejects
							// (invalid blocks), so nothing is posted.
							bad := ke.Name == "F"
							go func() {
								ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
								defer cancel()
								o := core.SendOpts{Channel: "C0B4DEFKFCJ", Text: "[yoloslack messages test] optimistic send (will be deleted)"}
								if bad {
									o.Text = "[yoloslack messages test] failing send"
									o.Blocks = json.RawMessage(`[{"type":"not_a_block"}]`)
								}
								m, err := svc.Send(ctx, o)
								log.Printf("send bad=%v err=%v ts=%s", bad, err, m.TS)
								if err == nil && !bad {
									time.Sleep(10 * time.Second)
									log.Printf("delete test send: %v", svc.Delete(context.Background(), "C0B4DEFKFCJ", m.TS))
								}
							}()
						}
						if ke.Name == "B" {
							// Block Kit buttons with emoji labels (test channel; deleted after 60 s).
							go func() {
								ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
								defer cancel()
								blocks := json.RawMessage(`[{"type":"section","text":{"type":"mrkdwn","text":"[yoloslack messages test] emoji in button labels (will be deleted)"}},` +
									`{"type":"actions","elements":[{"type":"button","text":{"type":"plain_text","text":"Launch 🚀","emoji":true},"action_id":"a1"},` +
									`{"type":"button","style":"primary","text":{"type":"plain_text","text":"Ship it ✅","emoji":true},"action_id":"a2"}]}]`)
								m, err := svc.Send(ctx, core.SendOpts{Channel: "C0B4DEFKFCJ", Text: "[yoloslack messages test] emoji buttons", Blocks: blocks})
								log.Printf("button test send err=%v", err)
								if err == nil {
									time.Sleep(60 * time.Second)
									log.Printf("delete button test: %v", svc.Delete(context.Background(), "C0B4DEFKFCJ", m.TS))
								}
							}()
						}
						if ke.Name == "J" && *highlight != "" {
							view.Highlight(*highlight)
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
				// popups overlays on top
				live := h.overlays[:0]
				for _, o := range h.overlays {
					if !o.Dismissed() {
						g2 := gtx
						g2.Constraints = layout.Exact(size)
						o.Layout(g2)
						live = append(live, o)
					}
				}
				h.overlays = live
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
