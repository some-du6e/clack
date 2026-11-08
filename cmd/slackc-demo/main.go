//go:build slackcdemo

// Command slackc-demo lays out a few hand-written host trees with the
// slackc layout engine, for visual checks. Build with -tags slackcdemo
// (CI builds it as an artifact in bench-slackc.yml, merged with the CSS
// engine and SVG branches).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/slackc"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"

	"github.com/some-du6e/yoloslackgoclient/internal/slack/tlsfp"
)

type section struct {
	title string
	tree  string
	width int
	view  *slackc.View
	nodes []*slackc.Node
}

var sections = []*section{
	{title: "Patched MessageSender: name, pronoun badge with svg icon, <Original>, timestamp link, APP pill", width: 700, tree: `
{"t":"span","k":1,"style":{"display":"inline-flex","alignItems":"baseline","gap":6},"c":[
 {"t":"span","k":2,"style":{"fontWeight":900,"fontSize":15},"c":["Karim Example"]},
 {"t":"span","k":3,"style":{"display":"inline-flex","alignItems":"center","gap":3,"background":"#e8f5fa","color":"#1264a3","borderRadius":4,"padding":"0 5px","fontSize":12,"lineHeight":"18px"},"title":"Pronouns from UserPronouns","c":[
   {"t":"svg","k":4,"viewBox":"0 0 24 24","width":12,"height":12,"fill":"currentColor","c":[{"t":"path","k":5,"d":"M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z"}]},
   {"t":"span","k":6,"c":["he/him"]}]},
 {"t":"@","n":"Original","k":7,"p":{"label":"<Original/>"}},
 {"t":"a","k":8,"href":"https://hackclub.slack.com/archives/C0B4DEFKFCJ","style":{"fontSize":12,"color":"#616061"},"c":["12:30 PM"]},
 {"t":"span","k":9,"style":{"fontSize":10,"fontWeight":700,"background":"rgba(29,28,29,0.13)","color":"#616061","padding":"1px 3px","borderRadius":3},"c":["APP"]}
]}`},
	{title: "Flex wrap, gap, justify-content: center; align-items; flex-grow", width: 520, tree: `
{"t":"div","k":1,"style":{"display":"flex","flexWrap":"wrap","gap":8,"justifyContent":"center","padding":8,"border":"1px solid #ddd","borderRadius":8},"c":[
 {"t":"span","k":2,"style":{"padding":"4px 10px","borderRadius":12,"background":"#ecb22e","color":"white","fontWeight":700},"c":["design"]},
 {"t":"span","k":3,"style":{"padding":"4px 10px","borderRadius":12,"background":"#2eb67d","color":"white"},"c":["hack club"]},
 {"t":"span","k":4,"style":{"padding":"4px 10px","borderRadius":12,"background":"#e01e5a","color":"white"},"c":["plugins ✨"]},
 {"t":"span","k":5,"style":{"padding":"4px 10px","borderRadius":12,"background":"#36c5f0","color":"#1d1c1d"},"c":["native layout"]},
 {"t":"span","k":6,"style":{"padding":"4px 10px","borderRadius":12,"background":"#4a154b","color":"white"},"c":["flexbox"]},
 {"t":"div","k":7,"style":{"flexGrow":1,"minWidth":120,"height":28,"background":"linear-gradient(90deg, #4a154b, #36c5f0)","borderRadius":6}}
]}`},
	{title: "Positioning: relative card, absolute badge, box-shadow, gradient, z-index, transform", width: 520, tree: `
{"t":"div","k":1,"style":{"position":"relative","height":90,"padding":12,"borderRadius":10,"background":"white","boxShadow":"0 4px 12px rgba(0,0,0,0.25)","border":"1px solid #e2e2e2"},"c":[
 {"t":"div","k":2,"style":{"fontWeight":700,"fontSize":16},"c":["Huddle in #lounge"]},
 {"t":"div","k":3,"style":{"color":"#616061","marginTop":4},"c":["3 people are talking"]},
 {"t":"span","k":4,"style":{"position":"absolute","top":-8,"right":-8,"background":"#e01e5a","color":"white","borderRadius":10,"padding":"2px 7px","fontSize":12,"fontWeight":700,"zIndex":2},"c":["12"]},
 {"t":"div","k":5,"style":{"position":"absolute","right":14,"bottom":12,"width":36,"height":36,"borderRadius":"50%","background":"radial-gradient(circle, #ecb22e, #e01e5a)","transform":"rotate(20deg) scale(1.1)"}}
]}`},
	{title: "Inline formatting: wrapping, bold/italic/underline/strike, emoji, ellipsis, line-clamp", width: 380, tree: `
[{"t":"p","k":1,"style":{"margin":0,"lineHeight":"22px"},"c":["Plain text that wraps across lines with ",{"t":"b","k":2,"c":["bold"]},", ",{"t":"i","k":3,"c":["italic"]},", ",{"t":"u","k":4,"c":["underlined"]},", ",{"t":"s","k":5,"c":["struck"]}," and ",{"t":"code","k":6,"style":{"background":"#f4f4f4","border":"1px solid #ddd","borderRadius":3,"padding":"0 3px","color":"#e01e5a"},"c":["inline code"]}," plus emoji 🎉🚀 inline."]},
 {"t":"div","k":7,"style":{"whiteSpace":"nowrap","overflow":"hidden","textOverflow":"ellipsis","fontWeight":700,"marginTop":6},"c":["This single line is far too long to fit and ends with an ellipsis"]},
 {"t":"div","k":8,"style":{"WebkitLineClamp":2,"display":"-webkit-box","WebkitBoxOrient":"vertical","overflow":"hidden","marginTop":6,"color":"#454245"},"c":["Line clamp keeps two lines of this long paragraph and cuts the rest with an ellipsis, so the message preview doesn't take over the sidebar when it's huge."]}
]`},
	{title: "StreamerMode: filter: blur(6px) on names and message text (unreadable), opacity, grayscale", width: 520, tree: `
{"t":"div","k":1,"style":{"display":"flex","gap":16,"alignItems":"center"},"c":[
 {"t":"span","k":2,"style":{"filter":"blur(6px)","fontWeight":700},"c":["Secret Person"]},
 {"t":"span","k":3,"style":{"filter":"blur(6px)"},"c":["the message body nobody should read"]},
 {"t":"span","k":4,"style":{"opacity":0.4},"c":["40% opacity"]},
 {"t":"span","k":5,"style":{"filter":"grayscale(1)","color":"#e01e5a","fontWeight":700},"c":["grayscale"]}
]}`},
	{title: "Form controls: input with placeholder, checkbox, radio, range, select, progress, button", width: 620, tree: `
{"t":"div","k":1,"style":{"display":"flex","flexWrap":"wrap","gap":10,"alignItems":"center"},"c":[
 {"t":"input","k":2,"placeholder":"Search plugins…","style":{"width":160,"padding":"4px 8px","border":"1px solid #bbb","borderRadius":6}},
 {"t":"label","k":3,"style":{"display":"inline-flex","alignItems":"center","gap":4},"c":[{"t":"input","k":4,"type":"checkbox","defaultChecked":true},"Enabled"]},
 {"t":"label","k":5,"style":{"display":"inline-flex","alignItems":"center","gap":4},"c":[{"t":"input","k":6,"type":"radio","name":"r","defaultChecked":true},"One"]},
 {"t":"input","k":7,"type":"range","min":0,"max":10,"defaultValue":7},
 {"t":"select","k":8,"defaultValue":"b","style":{"padding":"2px 6px","border":"1px solid #bbb","borderRadius":4},"c":[{"t":"option","k":9,"value":"a","c":["Apple"]},{"t":"option","k":10,"value":"b","c":["Banana"]},{"t":"option","k":11,"value":"c","c":["Cherry"]}]},
 {"t":"progress","k":12,"value":0.6,"max":1},
 {"t":"button","k":13,"style":{"background":"#007a5a","color":"white","border":"none","borderRadius":4,"padding":"4px 12px","fontWeight":700},"c":["Save"]}
]}`},
	{title: "Scroll container (overflow: auto, max-height) with a list and :hover via CSS", width: 300, tree: `
{"t":"div","k":1,"style":{"maxHeight":90,"overflow":"auto","border":"1px solid #ddd","borderRadius":6},"c":[
 {"t":"div","k":2,"className":"row","c":["#general"]},{"t":"div","k":3,"className":"row","c":["#random"]},{"t":"div","k":4,"className":"row","c":["#lounge"]},
 {"t":"div","k":5,"className":"row","c":["#ship"]},{"t":"div","k":6,"className":"row","c":["#scrapbook"]},{"t":"div","k":7,"className":"row","c":["#meta"]},
 {"t":"div","k":8,"className":"row","c":["#code"]},{"t":"div","k":9,"className":"row","c":["#music"]}
]}`},
}

const demoCSS = `
.row { padding: 4px 10px; cursor: pointer; }
.row:hover { background: #1264a3; color: white; }
`

type logBridge struct{}

func (logBridge) Render(root uint64, name string, props []byte)        {}
func (logBridge) RenderFn(root uint64, fn int64, args []byte)          {}
func (logBridge) Unmount(root uint64)                                  {}
func (logBridge) Call(fn int64, args []byte, done func([]byte, error)) {}
func (logBridge) SetViewport(w, h, dpr float32, dark bool)             {}
func (logBridge) Dispatch(target int64, ev []byte) {
	s := string(ev)
	if strings.Contains(s, `"type":"mousemove"`) || strings.Contains(s, `"type":"pointermove"`) {
		return
	}
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	fmt.Fprintf(os.Stderr, "dispatch k=%d %s\n", target, s)
}
func (logBridge) SetRects(r map[int64][4]float32) {
	fmt.Fprintf(os.Stderr, "rects: %d nodes\n", len(r))
}

// normalize moves the props written inline on a node ({"t":"div","style":…})
// into "p", the wire format the JS renderer sends.
func normalize(src string) []byte {
	var v any
	if err := json.Unmarshal([]byte(src), &v); err != nil {
		log.Fatal(err)
	}
	var walk func(x any)
	walk = func(x any) {
		switch x := x.(type) {
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			p, _ := x["p"].(map[string]any)
			if p == nil {
				p = map[string]any{}
			}
			for k, val := range x {
				switch k {
				case "t", "n", "v", "k", "p", "on", "c":
					continue
				}
				p[k] = val
				delete(x, k)
			}
			if len(p) > 0 {
				x["p"] = p
			}
			walk(x["c"])
		}
	}
	walk(v)
	b, _ := json.Marshal(v)
	return b
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := tlsfp.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func main() {
	go run()
	app.Main()
}

func run() {
	w := new(app.Window)
	w.Option(app.Title("slackc demo"), app.Size(unit.Dp(900), unit.Dp(900)))
	th := theme.New(os.Getenv("SLACKC_DEMO_DARK") != "")
	images := imgcache.New(fetch, "", w.Invalidate)
	th.Images = images
	var ptr image.Point
	var ptrTag int
	slackc.Init(slackc.Env{Theme: th, Images: images, Invalidate: w.Invalidate,
		OpenURL:    func(u string) { log.Printf("open %s", u) },
		Navigate:   func(r string) { log.Printf("navigate %s", r) },
		PointerWin: func() image.Point { return ptr }})
	slackc.SetBridge(logBridge{})
	slackc.SetStyle("demo", demoCSS)
	slackc.RegisterHost(func(gtx layout.Context, h *slackc.Host) layout.Dimensions {
		// stands in for the native widget behind <Original>
		return th.Label(gtx, 15, theme.Bold, th.C.Link, h.Props.StrOr("label", "native"))
	}, "Original")
	for i, s := range sections {
		nodes, err := slackc.ParseTree(normalize(s.tree))
		if err != nil {
			log.Fatalf("section %d: %v", i, err)
		}
		s.nodes = nodes
		s.view = slackc.NewView(uint64(i + 1))
	}
	shot := os.Getenv("SLACKC_DEMO_FRAMES") != ""
	frames := 0
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			os.Exit(0)
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			for {
				ev, ok := gtx.Event(pointer.Filter{Target: &ptrTag, Kinds: pointer.Move | pointer.Press | pointer.Drag | pointer.Release | pointer.Enter})
				if !ok {
					break
				}
				if pe, ok := ev.(pointer.Event); ok {
					ptr = image.Pt(int(pe.Position.X+.5), int(pe.Position.Y+.5))
				}
			}
			slackc.Frame(gtx)
			paint.Fill(gtx.Ops, th.C.Bg)
			start := time.Now()
			layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				var kids []layout.FlexChild
				for _, s := range sections {
					s := s
					kids = append(kids,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return th.Label(gtx, 12, theme.Bold, th.C.FgMuted, s.title)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min = image.Point{}
							gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Dp(unit.Dp(s.width)))
							return s.view.Layout(gtx, s.nodes, "", nil, nil)
						}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, kids...)
			})
			slackc.LayoutOverlays(gtx)
			if frames%60 == 0 {
				log.Printf("frame %d: layout+paint %v", frames, time.Since(start))
			}
			func() {
				defer pointer.PassOp{}.Push(gtx.Ops).Pop()
				defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
				event.Op(gtx.Ops, &ptrTag)
			}()
			e.Frame(gtx.Ops)
			frames++
			if shot && frames < 5 {
				w.Invalidate()
			}
		}
	}
}
