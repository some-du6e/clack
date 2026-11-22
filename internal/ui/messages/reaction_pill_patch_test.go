package messages

// Karim, α.113 (Windows, 1x, light with a custom theme): "misaligned ?" on
// a reaction pill (a custom emoji, 13) under a public channel's message,
// his plugins patching ReactionBar. OwlMode's patch renders <Original
// {...props}/> outside its owl threads, so the bar hands the pill's
// children (the Emoji and ReactionCount slots) back through slackc, which
// laid them out as one inline run: touching, the emoji's bottom on the
// baseline of a line box with the row's 15px strut. Slack's .c-reaction
// is inline-flex (client-boot-styles: gap 4px, align-items center,
// padding-inline 8px, 24px tall, 12px text with line-height 1).
//
// Here a channel's pills (custom and unicode emoji, counts of 1, 2 and 3
// digits, one of each the viewer's) are drawn natively, then under a
// pass-through ReactionBar patch with no CSS and under an OwlMode-shaped
// plugin (rules for its own class installed, the messages outside its
// thread), light and dark: every pill's emoji box, bounds and count ink
// must be the native's, and the bar the same pixels. In its thread its
// rules must reach the pills: the ring in its colour (a <body> variable
// with a dark value), the reacted pill's fill and its content-pry count.
// Synthetic data; the emoji are solid green (custom) and solid blue
// (unicode) pictures.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

const (
	rpChan   = "C0PILLROW1"
	rpCustom = "1700001100.000100" // custom emoji pills: 1, 13 (yours), 137
	rpUni    = "1700001101.000100" // unicode emoji pills: 1, 13 (yours), 137
	rpOwlTS  = "1700001102.000100" // the owl-shaped plugin's thread: its rules reach these
	rpW, rpH = 900, 700
)

var (
	rpGreen = color.NRGBA{G: 0xff, A: 0xff} // the custom emoji
	rpBlue  = color.NRGBA{B: 0xff, A: 0xff} // the unicode emoji
)

// rpPass patches ReactionBar and passes it on: <Original {...props}/>.
var rpPass = pluginJS(`
class PillPass extends TautPlugin {
  static id = 'PillPass'
  static pluginName = 'Reaction bar pass-through (test)'
  static description = 'test: patches ReactionBar and renders Original with its props'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    this.api.patchComponent('ReactionBar', (O) => (p) => React.createElement(O, p))
  }
}
return PillPass`)

// rpOwlCSS is rules of the kind OwlMode installs for the pills in its
// threads, in this test's own names and colours (Taut is GPLv3; its
// source isn't copied): a colour on <body> with a dark theme's own value,
// a 1px ring round every pill of a bar that carries the plugin's class,
// and your pill filled with the colour at 15%, its count content-pry.
const rpOwlCSS = `body { --x-owl: 150, 40, 190 }
body.sk-client-theme--dark { --x-owl: 210, 140, 255 }
.c-reaction_bar.x-owl-pills .c-reaction { box-shadow: 0 0 0 1px rgba(var(--x-owl), 1) }
.c-reaction_bar.x-owl-pills .c-reaction--reacted { background-color: rgba(var(--x-owl), 0.15); color: var(--dt_color-content-pry) }`

// rpOwl patches ReactionBar the way OwlMode does: the bar of a message in
// its thread gets the plugin's class in its className, every other bar
// is passed on as it is.
var rpOwl = pluginJS(`
class OwlShapedPills extends TautPlugin {
  static id = 'OwlShapedPills'
  static pluginName = 'Owl-shaped reaction bar (test)'
  static description = 'test: a class on one message\'s ReactionBar, and rules for its pills'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    this.api.setStyle(` + strconv.Quote(rpOwlCSS) + `)
    this.api.patchComponent('ReactionBar', (O) => (p) => {
      if (p.reactionKey !== 'message-` + rpOwlTS + `-` + rpChan + `') return React.createElement(O, p)
      return React.createElement(O, { ...p, className: (p.className ?? '') + ' x-owl-pills' })
    })
  }
}
return OwlShapedPills`)

// rpClsCSS rings a pill that carries the plugin's own class.
const rpClsCSS = `body { --x-ring: 200, 60, 20 }
body.sk-client-theme--dark { --x-ring: 255, 160, 90 }
.c-reaction.x-own-pill { box-shadow: 0 0 0 1px rgba(var(--x-ring), 1) }`

// rpClsJS adds its own class to every reaction pill's UnstyledButton: the
// pill must take it (Slack's DOM has it on the button), so its rule rings
// every pill, once and round. With bar it also passes ReactionBar on, so
// the buttons are slots in the bars' trees (patched alone, each button is
// a tree of its own, its root the pill).
func rpClsJS(bar bool) []byte {
	pass := ""
	if bar {
		pass = "    this.api.patchComponent('ReactionBar', (O) => (p) => React.createElement(O, p))\n"
	}
	return pluginJS(`
class OwnPillClass extends TautPlugin {
  static id = 'OwnPillClass'
  static pluginName = 'A class on the pill (test)'
  static description = 'test: adds a class to the reaction pill button, and a rule for it'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    this.api.setStyle(` + strconv.Quote(rpClsCSS) + `)
` + pass + `    this.api.patchComponent('UnstyledButton', (O) => (p) => p['data-qa'] !== 'reactji' ? React.createElement(O, p) :
      React.createElement(O, { ...p, className: (p.className ?? '') + ' x-own-pill' }))
  }
}
return OwnPillClass`)
}

// rpHost is coreHost with the image cache the pills' emoji load from.
type rpHost struct {
	coreHost
	imgs *imgcache.Cache
}

func (h rpHost) Images() *imgcache.Cache { return h.imgs }

// rpImages serves the custom emoji (emoji.slack-edge.com) solid green and
// the standard ones (Slack's emoji CDN) solid blue; nothing else loads.
func rpImages() *imgcache.Cache {
	solid := func(c color.NRGBA) []byte {
		im := image.NewNRGBA(image.Rect(0, 0, 64, 64))
		for i := 0; i < len(im.Pix); i += 4 {
			im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = c.R, c.G, c.B, c.A
		}
		var b bytes.Buffer
		_ = png.Encode(&b, im)
		return b.Bytes()
	}
	green, blue := solid(rpGreen), solid(rpBlue)
	return imgcache.New(func(ctx context.Context, url string) ([]byte, error) {
		switch {
		case strings.HasPrefix(url, "https://emoji.slack-edge.com/"):
			return green, nil
		case strings.HasPrefix(url, "https://a.slack-edge.com/production-standard-emoji-assets/"):
			return blue, nil
		}
		return nil, errors.New("offline")
	}, "", nil)
}

func rpSeed(s *store.Store) {
	s.PutConv(slack.Conversation{ID: rpChan, Name: "pills", IsChannel: true, IsMember: true, NumMembers: 3})
	s.CustomEmoji.Set(map[string]string{
		"rp-parrot": "https://emoji.slack-edge.com/T0266FRGM/rp-parrot/0a1b2c3d4e5f6071.png",
		"rp-cat":    "https://emoji.slack-edge.com/T0266FRGM/rp-cat/1a2b3c4d5e6f7081.png",
		"rp-owl":    "https://emoji.slack-edge.com/T0266FRGM/rp-owl/2a3b4c5d6e7f8091.png",
	})
	s.SetLatest(rpChan, []slack.Message{
		{Type: "message", TS: rpCustom, User: "UALICE", Text: "synthetic custom emoji pills", Reactions: []slack.Reaction{
			{Name: "rp-parrot", Count: 1, Users: []string{"UBOB"}},
			{Name: "rp-cat", Count: 13, Users: []string{"UBOB", tSelf}},
			{Name: "rp-owl", Count: 137, Users: []string{"UBOB", "UALICE"}},
		}},
		{Type: "message", TS: rpUni, User: "UBOB", Text: "synthetic unicode emoji pills", Reactions: []slack.Reaction{
			{Name: "tada", Count: 1, Users: []string{"UALICE"}},
			{Name: "eyes", Count: 13, Users: []string{"UALICE", tSelf}},
			{Name: "rocket", Count: 137, Users: []string{"UALICE", "UBOB"}},
		}},
		{Type: "message", TS: rpOwlTS, User: "UALICE", Text: "synthetic owl thread pills", Reactions: []slack.Reaction{
			{Name: "rp-parrot", Count: 2, Users: []string{"UBOB", "UALICE"}},
			{Name: "tada", Count: 3, Users: []string{"UBOB", tSelf}},
		}},
	}, false)
}

// rpEnv is pluginEnv with the pills' channel, the emoji pictures and the
// document's <html> and <body> (the owl-shaped plugin's colour is on
// <body>, which slackc.Frame puts at the bottom of the element stack, as
// the ui root does every frame).
func rpEnv(t *testing.T, dark bool) (*taut.Taut, *pluginUI, rpHost) {
	app, _ := newTestApp(t)
	rpSeed(app.S)
	u := &pluginUI{trees: map[uint64][]byte{}}
	hooks := u.hooks()
	hooks.OnDocumentProps = func(html, body []byte) { _ = slackc.SetDocumentProps(html, body) }
	tt := newTaut(t, app, hooks)
	slackc.SetBridge(tt.Bridge())
	SetTaut(tt)
	th := theme.New(dark)
	h := rpHost{coreHost: coreHost{app: app, th: th}, imgs: rpImages()}
	slackc.Init(slackc.Env{Theme: th, Core: app, Images: h.imgs, Invalidate: func() {}})
	t.Cleanup(func() {
		SetTaut(nil)
		slackc.SetBridge(nil)
		slackc.Reset()
		slackc.SetPatched(nil)
		slackc.Init(slackc.Env{})
	})
	return tt, u, h
}

func rpFrame(v *ChannelView, th *theme.Theme, ops *op.Ops) {
	ops.Reset()
	paint.FillShape(ops, th.C.Bg, clip.Rect{Max: image.Pt(rpW, rpH)}.Op())
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(rpW, rpH)), Now: time.Now()}
	slackc.Frame(gtx)
	v.Layout(gtx)
}

// rpPill is one pill as drawn: its emoji's pixels, its fill along its
// middle (the pill's bounds), and its count's ink: the pixels at least
// half the text's colour over the fill, their ink summed and its centre.
type rpPill struct {
	emoji, box, ink image.Rectangle
	mass, cx, cy    float64
	fill, text      color.RGBA
}

func (p rpPill) String() string {
	return fmt.Sprintf("emoji %v pill %v count ink %v (%.1f px, centre %.2f,%.2f)", p.emoji, p.box, p.ink, p.mass, p.cx, p.cy)
}

// rpPills finds every pill in img by its emoji (one per pill, solid green
// or blue), in reading order.
func rpPills(img *image.RGBA) []rpPill {
	b := img.Bounds()
	near := func(c color.RGBA, w color.NRGBA) bool {
		d := func(a, b uint8) int { return int(a) - int(b) }
		return rpAbs(d(c.R, w.R)) <= 24 && rpAbs(d(c.G, w.G)) <= 24 && rpAbs(d(c.B, w.B)) <= 24
	}
	isEmoji := func(x, y int) bool {
		c := img.RGBAAt(x, y)
		return near(c, rpGreen) || near(c, rpBlue)
	}
	seen := make([]bool, b.Dx()*b.Dy())
	var out []rpPill
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if seen[y*b.Dx()+x] || !isEmoji(x, y) {
				continue
			}
			// the emoji: its connected pixels
			r := image.Rect(x, y, x+1, y+1)
			q := []image.Point{{x, y}}
			seen[y*b.Dx()+x] = true
			for len(q) > 0 {
				p := q[len(q)-1]
				q = q[:len(q)-1]
				r = r.Union(image.Rect(p.X, p.Y, p.X+1, p.Y+1))
				for _, d := range []image.Point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					n := p.Add(d)
					if n.In(b) && !seen[n.Y*b.Dx()+n.X] && isEmoji(n.X, n.Y) {
						seen[n.Y*b.Dx()+n.X] = true
						q = append(q, n)
					}
				}
			}
			if r.Dx() >= 4 && r.Dy() >= 4 {
				out = append(out, rpMeasure(img, r))
			}
		}
	}
	return out
}

func rpAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func rpDist(a, b color.RGBA) int {
	return rpAbs(int(a.R)-int(b.R)) + rpAbs(int(a.G)-int(b.G)) + rpAbs(int(a.B)-int(b.B))
}

// rpMeasure measures the pill around the emoji at e: the row's background
// is left of the list (x 2), the fill just left of the emoji (the pill's
// 8px padding); the pill ends where the background starts again, along
// the emoji's middle row and its middle column. Its count is the ink
// right of the emoji.
func rpMeasure(img *image.RGBA, e image.Rectangle) rpPill {
	p := rpPill{emoji: e}
	yc, xc := (e.Min.Y+e.Max.Y)/2, (e.Min.X+e.Max.X)/2
	bg := img.RGBAAt(2, yc)
	p.fill = img.RGBAAt(e.Min.X-3, yc)
	half := rpDist(bg, p.fill) / 2
	isBg := func(x, y int) bool { return rpDist(img.RGBAAt(x, y), bg) <= half }
	x0 := e.Min.X - 1
	for x0 > 0 && !isBg(x0-1, yc) {
		x0--
	}
	x1 := e.Max.X
	for x1 < img.Bounds().Max.X && !isBg(x1, yc) {
		x1++
	}
	y0 := e.Min.Y
	for y0 > 0 && !isBg(xc, y0-1) {
		y0--
	}
	y1 := e.Max.Y
	for y1 < img.Bounds().Max.Y && !isBg(xc, y1) {
		y1++
	}
	p.box = image.Rect(x0, y0, x1, y1)
	// the count: the pixel furthest from the fill is the text's colour.
	// Its window keeps 3px off the pill's top and bottom and 7px off its
	// end, inside the round end's border (your pill's ring, in the count's
	// colour) and a plugin's ring round the pill.
	cr := image.Rect(e.Max.X, y0+3, x1-7, y1-3)
	far := -1
	for y := cr.Min.Y; y < cr.Max.Y; y++ {
		for x := cr.Min.X; x < cr.Max.X; x++ {
			if c := img.RGBAAt(x, y); rpDist(c, p.fill) > far {
				far, p.text = rpDist(c, p.fill), c
			}
		}
	}
	d := [3]float64{float64(p.text.R) - float64(p.fill.R), float64(p.text.G) - float64(p.fill.G), float64(p.text.B) - float64(p.fill.B)}
	dd := d[0]*d[0] + d[1]*d[1] + d[2]*d[2]
	if dd == 0 {
		return p
	}
	var sx, sy float64
	for y := cr.Min.Y; y < cr.Max.Y; y++ {
		for x := cr.Min.X; x < cr.Max.X; x++ {
			c := img.RGBAAt(x, y)
			w := ((float64(c.R)-float64(p.fill.R))*d[0] + (float64(c.G)-float64(p.fill.G))*d[1] + (float64(c.B)-float64(p.fill.B))*d[2]) / dd
			if w <= 0.02 {
				continue
			}
			w = min(w, 1)
			p.mass += w
			sx += w * (float64(x) + 0.5)
			sy += w * (float64(y) + 0.5)
			if w >= 0.5 {
				p.ink = p.ink.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if p.mass > 0 {
		p.cx, p.cy = sx/p.mass, sy/p.mass
	}
	return p
}

// rpDiff counts r's pixels whose channels differ by more than 48 between
// a and b (a moved edge differs by far more; atlas placement noise by far
// less), and the first.
func rpDiff(a, b *image.RGBA, r image.Rectangle) (int, image.Point) {
	n := 0
	var first image.Point
	r = r.Intersect(a.Bounds()).Intersect(b.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ca, cb := a.RGBAAt(x, y), b.RGBAAt(x, y)
			if rpAbs(int(ca.R)-int(cb.R)) > 48 || rpAbs(int(ca.G)-int(cb.G)) > 48 || rpAbs(int(ca.B)-int(cb.B)) > 48 {
				if n == 0 {
					first = image.Pt(x, y)
				}
				n++
			}
		}
	}
	return n, first
}

// rpShot saves the bars of img for the CI artifact (MESSAGES_AUDIT_SHOTS_DIR).
func rpShot(t *testing.T, name string, img *image.RGBA, pills []rpPill) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" || len(pills) == 0 {
		return
	}
	y0, y1 := pills[0].box.Min.Y, pills[len(pills)-1].box.Max.Y
	savePNG(t, filepath.Join(dir, "reaction-pill-"+name+".png"), crop(img, max(y0-40, 0), min(y1+16, img.Bounds().Max.Y)))
}

func TestPatchedReactionPillLikeNative(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, plug := range []string{"pass", "owl", "cls", "cls-bar"} {
			t.Run(mode+"/"+plug, func(t *testing.T) { rpCompare(t, dark, mode, plug) })
		}
	}
}

func rpCompare(t *testing.T, dark bool, mode, plug string) {
	tt, u, h := rpEnv(t, dark)
	th := h.th
	v := NewChannelView(h, rpChan)
	v.Preview = true
	ops := new(op.Ops)
	frame := func() { rpFrame(v, th, ops) }
	render := func() *image.RGBA {
		for i := 0; i < 3; i++ {
			frame()
		}
		return pixeltest.Render(t, rpW, rpH, ops)
	}
	// 8 pills: 3 custom, 3 unicode and the owl thread's 2, each emoji in
	// (the pictures load through the image cache)
	settled := func(what string) (*image.RGBA, []rpPill) {
		t.Helper()
		var img *image.RGBA
		var pills []rpPill
		until(t, 20*time.Second, mode+"/"+plug+": "+what, frame, func() bool {
			img = render()
			pills = rpPills(img)
			return len(pills) == 8
		})
		return img, pills
	}
	nimg, native := settled("the native pills")
	rpShot(t, mode+"-"+plug+"-native", nimg, native)

	js, patch := rpPass, "ReactionBar"
	switch plug {
	case "owl":
		js = rpOwl
	case "cls", "cls-bar":
		js, patch = rpClsJS(plug == "cls-bar"), "UnstyledButton"
	}
	install(t, tt, js)
	until(t, 10*time.Second, "the "+patch+" patch", frame, func() bool {
		return slackc.Patched(patch) && (plug != "cls-bar" || slackc.Patched("ReactionBar"))
	})
	// every bar drawn through its tree (the plugin's className in the owl
	// thread's), or under cls every pill's button with the plugin's class
	until(t, 15*time.Second, "the bars' trees", frame, func() bool {
		if plug == "cls" || plug == "cls-bar" {
			return u.countIn("x-own-pill") >= 8
		}
		for _, ts := range []string{rpCustom, rpUni, rpOwlTS} {
			if !u.anyTree(`"reactionKey":"message-` + ts + `-` + rpChan + `"`) {
				return false
			}
		}
		return plug != "owl" || u.anyTree("x-owl-pills")
	})
	for i := 0; i < 10; i++ {
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	pimg, patched := settled("the patched pills")
	rpShot(t, mode+"-"+plug+"-patched", pimg, patched)
	t.Logf("%s/%s: CSS installed %v", mode, plug, !slackc.Styles.Empty())

	// the custom and unicode bars (the owl thread's last two pills are
	// the plugin's own look in the owl variant); under cls every pill is
	// ringed, its emoji and count where they were
	cmp := 6
	if plug != "owl" {
		cmp = 8
	}
	var bar image.Rectangle
	for i := 0; i < cmp; i++ {
		n, p := native[i], patched[i]
		t.Logf("%s/%s pill %d native:  %v", mode, plug, i, n)
		t.Logf("%s/%s pill %d patched: %v", mode, plug, i, p)
		if p.emoji != n.emoji {
			t.Errorf("%s/%s pill %d: emoji at %v, native %v", mode, plug, i, p.emoji, n.emoji)
		}
		if !strings.HasPrefix(plug, "cls") && p.box != n.box {
			t.Errorf("%s/%s pill %d: pill %v, native %v", mode, plug, i, p.box, n.box)
		}
		if p.ink != n.ink {
			t.Errorf("%s/%s pill %d: count ink in %v, native %v", mode, plug, i, p.ink, n.ink)
		}
		if math.Abs(p.mass-n.mass) > 0.04*n.mass || math.Abs(p.cx-n.cx) > 0.3 || math.Abs(p.cy-n.cy) > 0.3 {
			t.Errorf("%s/%s pill %d: count ink %.1f px centred at %.2f,%.2f; native %.1f at %.2f,%.2f",
				mode, plug, i, p.mass, p.cx, p.cy, n.mass, n.cx, n.cy)
		}
		bar = bar.Union(n.box.Inset(-3))
	}
	if strings.HasPrefix(plug, "cls") {
		rpRinged(t, mode+"/"+plug, pimg, patched, rpRingColour(dark, true))
		return
	}
	// the bars, the add buttons after the pills included, pixel for pixel
	bar.Max.X += 48
	n, at := rpDiff(nimg, pimg, bar)
	t.Logf("%s/%s: %d px of the bars in %v differ from the native's", mode, plug, n, bar)
	if n > 0 {
		t.Errorf("%s/%s: %d px of the bars in %v differ from the native's (the first at %v)", mode, plug, n, bar, at)
	}
	if plug != "owl" {
		return
	}

	// the plugin's thread: its rules reach the pills inside the patched
	// bar, in its <body> colour for the theme
	owl := rpRingColour(dark, false)
	rpRinged(t, mode+"/owl", pimg, patched[6:], owl)
	re := patched[7] // tada, yours
	if dark {
		// its fill rgba(owl, .15) over the row (blended in sRGB as Chrome
		// does, or in linear light), not your pill's solid #0a77a7
		bg := pimg.RGBAAt(2, (re.emoji.Min.Y+re.emoji.Max.Y)/2)
		lin := func(c uint8) float64 {
			v := float64(c) / 255
			if v <= 0.04045 {
				return v / 12.92
			}
			return math.Pow((v+0.055)/1.055, 2.4)
		}
		enc := func(v float64) uint8 {
			if v <= 0.0031308 {
				return uint8(math.Round(255 * 12.92 * v))
			}
			return uint8(math.Round(255 * (1.055*math.Pow(v, 1/2.4) - 0.055)))
		}
		srgb := func(o, b uint8) uint8 { return uint8(math.Round(float64(b)*0.85 + float64(o)*0.15)) }
		linear := func(o, b uint8) uint8 { return enc(lin(b)*0.85 + lin(o)*0.15) }
		ws := color.RGBA{R: srgb(owl.R, bg.R), G: srgb(owl.G, bg.G), B: srgb(owl.B, bg.B), A: 0xff}
		wl := color.RGBA{R: linear(owl.R, bg.R), G: linear(owl.G, bg.G), B: linear(owl.B, bg.B), A: 0xff}
		if rpDist(re.fill, ws) > 24 && rpDist(re.fill, wl) > 24 {
			t.Errorf("%s/owl: your pill's fill %v, want the plugin's rgba(owl, .15) over the row %v: %v (sRGB) or %v (linear)", mode, re.fill, bg, ws, wl)
		}
		return
	}
	// its count in var(--dt_color-content-pry), not your pill's #1264a3
	pry := color.RGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}
	if rpDist(re.text, pry) > 60 {
		t.Errorf("%s/owl: your pill's count in %v, want the plugin's content-pry %v", mode, re.text, pry)
	}
}

// rpRingColour is the owl-shaped plugin's ring colour (cls: the class
// plugin's) for the theme.
func rpRingColour(dark, cls bool) color.RGBA {
	switch {
	case cls && dark:
		return color.RGBA{R: 255, G: 160, B: 90, A: 0xff}
	case cls:
		return color.RGBA{R: 200, G: 60, B: 20, A: 0xff}
	case dark:
		return color.RGBA{R: 210, G: 140, B: 255, A: 0xff}
	}
	return color.RGBA{R: 150, G: 40, B: 190, A: 0xff}
}

// rpRinged checks a plugin's ring round each of pills, in ring: its flat
// top and bottom alone are about 2 x 19 px of the 43px pills (light: ~50
// px each, dark ~78); and that the ring and the fill follow the pill's
// round ends: its box's corners, outside them, are the row's, and so are
// the pixels diagonally out from them (the UnstyledButton "@" box carries
// the plugin's className too; painted as a box of its own, its fill and
// ring were square).
func rpRinged(t *testing.T, what string, img *image.RGBA, pills []rpPill, ring color.RGBA) {
	t.Helper()
	for i, p := range pills {
		r := p.box.Inset(-2)
		px := 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if rpDist(img.RGBAAt(x, y), ring) <= 60 {
					px++
				}
			}
		}
		t.Logf("%s: %d px of the plugin's ring around pill %d %v", what, px, i, r)
		if px < 30 {
			t.Errorf("%s: %d px in the plugin's colour %v around pill %d, want its 1px ring round it (the plugin's box-shadow)", what, px, ring, i)
		}
		x0, y0, y1 := p.emoji.Min.X-8, p.emoji.Min.Y-4, p.emoji.Max.Y+4
		row := img.RGBAAt(2, (y0+y1)/2)
		for _, c := range []image.Point{{x0 + 1, y0 + 1}, {x0 + 1, y1 - 2}, {x0 - 1, y0 - 1}, {x0 - 1, y1}} {
			if got := img.RGBAAt(c.X, c.Y); rpDist(got, row) > 16 {
				t.Errorf("%s: pill %d: %v at its box's corner %v, outside its round end, where the row is %v", what, i, got, c, row)
			}
		}
	}
}

// countIn counts sub in the delivered trees.
func (u *pluginUI) countIn(sub string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := 0
	for _, tr := range u.trees {
		n += strings.Count(string(tr), sub)
	}
	return n
}
