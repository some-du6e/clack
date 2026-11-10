// Command huddle-demo shows the huddle UI (internal/ui/huddleui) against
// the fake Slack + Chime (huddle.NewFake): a window with the sidebar's
// huddle bar and the channel header's huddle pill, plus the huddle window
// with a second person talking, reacting and sharing a test-pattern screen,
// and a test-pattern camera of your own (-camera turns it on at start).
// Nothing connects to Slack or AWS. A developer tool; not shipped.
//
//	huddle-demo -clip internal/huddle/testdata/testsrc-320x180.h264 -for 60s
package main

import (
	"errors"
	"flag"
	"image"
	"image/color"
	"log"
	"os"
	"time"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/huddle"
	"github.com/some-du6e/clack/internal/huddle/avperm"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/huddleui"
	"github.com/some-du6e/clack/internal/ui/theme"
)

type host struct {
	app *core.App
	th  *theme.Theme
	win *app.Window
}

func (h *host) Core() *core.App          { return h.app }
func (h *host) Theme() *theme.Theme      { return h.th }
func (h *host) Images() *imgcache.Cache  { return nil }
func (h *host) Navigate(route string)    { log.Printf("demo: navigate %s", route) }
func (h *host) OpenThread(ch, ts string) { log.Printf("demo: open thread %s/%s", ch, ts) }
func (h *host) Toast(text string)        { log.Printf("demo: toast %q", text) }
func (h *host) Invalidate() {
	if h.win != nil {
		h.win.Invalidate()
	}
}

const demoChannel = "C0DEMOCHAN"

func main() {
	clipPath := flag.String("clip", "internal/huddle/testdata/testsrc-320x180.h264", "Annex B H.264 for the fake screen share (\"\" = none)")
	dur := flag.Duration("for", 0, "quit after this long (0 = when the window closes)")
	muted := flag.Bool("muted", false, "join muted")
	captions := flag.Bool("captions", true, "show live captions")
	mic := flag.String("mic", "ok", "the microphone: ok (a tone), denied, restricted, failed or silent")
	camera := flag.Bool("camera", false, "turn your (test-pattern) camera on at start")
	window := flag.Bool("window", false, "open the huddle window at start")
	flag.Parse()
	if *mic == "ok" && os.Getenv("CLACK_HUDDLE_TONE") == "" {
		os.Setenv("CLACK_HUDDLE_TONE", "440") // a synthetic microphone
	}
	if os.Getenv("CLACK_HUDDLE_TESTCAM") == "" {
		os.Setenv("CLACK_HUDDLE_TESTCAM", "640x360") // a synthetic camera
	}
	var clip []byte
	if *clipPath != "" {
		b, err := os.ReadFile(*clipPath)
		if err != nil {
			log.Fatal(err)
		}
		clip = b
	}
	fake := huddle.NewFake(clip, log.Printf)

	dir, _ := os.MkdirTemp("", "huddle-demo")
	defer os.RemoveAll(dir)
	st := store.New(dir)
	st.Self = slack.User{ID: huddle.FakeSelfUser, Name: "you"}
	user := func(id, name string) slack.User {
		u := slack.User{ID: id, Name: name, RealName: name}
		u.Profile.DisplayName = name
		return u
	}
	st.PutUsers([]slack.User{user(huddle.FakeSelfUser, "Demo You"), user(huddle.FakeRemoteUser, "Orpheus Example")})
	st.PutConv(slack.Conversation{ID: demoChannel, Name: "huddle-demo", IsChannel: true})
	h := &host{app: core.New(slack.New(slack.Creds{}), st), th: theme.New(false)}

	ctl := huddleui.New(h)
	ctl.Client, ctl.SelfID = fake.Caller(), huddle.FakeSelfUser
	if *mic != "ok" {
		ctl.Tweak = func(o *huddle.Options) { fakeMic(o, *mic) }
	}

	go func() {
		w := new(app.Window)
		w.Option(app.Title("Huddle demo - Clack"), app.Size(unit.Dp(900), unit.Dp(560)))
		h.win = w
		ctl.Start(demoChannel)
		go func() {
			for ctl.Session() == nil {
				time.Sleep(50 * time.Millisecond)
			}
			s := ctl.Session()
			if *captions { // ⋯ › Show captions › Closed captions
				ctl.ToggleClosedCaptions(s)
			}
			if *muted {
				s.SetMuted(true)
			}
			if *camera {
				s.SetCamera(true)
			}
			if *window {
				ctl.OpenWindow()
			}
		}()
		if *dur > 0 {
			go func() {
				time.Sleep(*dur)
				ctl.Close()
				fake.Close()
				os.Exit(0)
			}()
		}
		tick := time.NewTicker(200 * time.Millisecond)
		go func() {
			for range tick.C {
				w.Invalidate()
			}
		}()
		var ops op.Ops
		for {
			switch e := w.Event().(type) {
			case app.DestroyEvent:
				ctl.Close()
				fake.Close()
				os.Exit(0)
			case app.FrameEvent:
				gtx := app.NewContext(&ops, e)
				sz := gtx.Constraints.Max
				// Slack's aubergine sidebar and a white channel header
				sbW := gtx.Dp(260)
				theme.Fill(gtx, image.Rect(0, 0, sbW, sz.Y), color.NRGBA{R: 0x4a, G: 0x15, B: 0x4b, A: 0xff})
				theme.Fill(gtx, image.Rect(sbW, 0, sz.X, sz.Y), color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
				theme.Fill(gtx, image.Rect(sbW, gtx.Dp(49), sz.X, gtx.Dp(50)), color.NRGBA{R: 0xdd, G: 0xdd, B: 0xdd, A: 0xff})
				func() {
					off := op.Offset(image.Pt(sbW+gtx.Dp(16), gtx.Dp(12))).Push(gtx.Ops)
					gtx := gtx
					gtx.Constraints.Min = image.Point{}
					h.th.Label(gtx, 18, theme.Black, color.NRGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}, "# huddle-demo")
					off.Pop()
				}()
				if ctl.HeaderLive(demoChannel) {
					pw := ctl.HeaderPillWidth(gtx, h.th, demoChannel)
					func() {
						off := op.Offset(image.Pt(sz.X-pw-gtx.Dp(60), gtx.Dp(11))).Push(gtx.Ops)
						g := gtx
						g.Constraints.Max = image.Pt(pw, gtx.Dp(28))
						ctl.HeaderPill(g, h.th, demoChannel, gtx.Dp(28))
						off.Pop()
					}()
				}
				if bh := ctl.BarHeight(gtx); bh > 0 {
					func() {
						off := op.Offset(image.Pt(0, sz.Y-bh)).Push(gtx.Ops)
						g := gtx
						g.Constraints.Max = image.Pt(sbW, bh)
						ctl.LayoutBar(g, h.th)
						off.Pop()
					}()
				}
				e.Frame(gtx.Ops)
			}
		}
	}()
	app.Main()
}

// fakeMic answers the microphone permission and opens a fake sound card,
// to show the huddle UI's microphone notices.
func fakeMic(o *huddle.Options, mode string) {
	answer := avperm.Authorized
	switch mode {
	case "denied":
		answer = avperm.Denied
	case "restricted":
		answer = avperm.Restricted
	}
	o.MicAccess = func() <-chan avperm.Status {
		ch := make(chan avperm.Status, 1)
		ch <- answer
		return ch
	}
	n := 0
	o.OpenAudio = func(func(string, ...any)) (huddle.Device, error) {
		n++
		if mode == "failed" && n > 1 { // the speakers open, the microphone doesn't
			return nil, errors.New("no capture device")
		}
		return huddle.NewToneDevice(0), nil // silence
	}
}
