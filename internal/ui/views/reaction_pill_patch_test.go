package views

// The Threads, Unreads and Activity views' reaction pills under plugins,
// as messages/reaction_pill_patch_test.go checks a channel's. Their bars
// are Slack's ReactionBar too (reactions_slack.go): a patched ReactionBar
// hands each pill's children (the Emoji and ReactionCount slots) back
// through slackc, which laid them out as one inline run in a block root,
// the emoji off its native place and the count against it, and the bar
// dropped the className a plugin passed on, so an OwlMode-shaped rule
// for its pills never matched.
//
// Each view's bars (custom and unicode emoji, counts of 1, 2 and 3
// digits, one of each the viewer's, as the views draw a message's
// reactions: threads.go, unreads.go, activity.go's pane and an Activity
// row's bar without its add button) are drawn natively, then under a
// pass-through ReactionBar patch with no CSS, under an OwlMode-shaped
// plugin (a class on one bar, rules for its pills) and under a plugin
// that adds its own class to every pill's UnstyledButton with a ring
// rule for it, light and dark. Every pill's emoji, bounds and count ink
// must be the native's and, without a rule for them, the bars the same
// pixels; the plugins' rules must reach the pills they name, round.
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

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/taut"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

const (
	vpCustom = "1700003100.000100" // custom emoji pills: 1, 13 (yours), 137
	vpUni    = "1700003101.000100" // unicode emoji pills: 1, 13 (yours), 137
	vpOwlTS  = "1700003102.000100" // the owl-shaped plugin's message: its rules reach these
	vpW, vpH = 700, 360
	vpX      = 24 // the bars' left edge; the background left of it
)

var (
	vpGreen = color.NRGBA{G: 0xff, A: 0xff} // the custom emoji
	vpBlue  = color.NRGBA{B: 0xff, A: 0xff} // the unicode emoji
)

func vpPluginJS(src string) []byte {
	return []byte("(() => {\nconst TautPlugin = globalThis.TautPlugin, opt = globalThis.TautOpt;\n" + src + "\n})()")
}

// vpPass patches ReactionBar and passes it on: <Original {...props}/>.
var vpPass = vpPluginJS(`
class ViewPillPass extends TautPlugin {
  static id = 'ViewPillPass'
  static pluginName = 'Reaction bar pass-through (views test)'
  static description = 'test: patches ReactionBar and renders Original with its props'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    this.api.patchComponent('ReactionBar', (O) => (p) => React.createElement(O, p))
  }
}
return ViewPillPass`)

// vpOwlCSS is rules of the kind OwlMode installs for the pills of the
// bars it marks, in this test's own names and colours (Taut is GPLv3;
// its source isn't copied): a colour on <body> with a dark theme's own
// value, a 1px ring round every pill of a bar with the plugin's class,
// and your pill filled with the colour at 15%, its count content-pry.
const vpOwlCSS = `body { --v-hoot: 30, 120, 70 }
body.sk-client-theme--dark { --v-hoot: 120, 230, 160 }
.c-reaction_bar.v-hoot-bar .c-reaction { box-shadow: 0 0 0 1px rgba(var(--v-hoot), 1) }
.c-reaction_bar.v-hoot-bar .c-reaction--reacted { background-color: rgba(var(--v-hoot), 0.15); color: var(--dt_color-content-pry) }`

// vpOwl patches ReactionBar as OwlMode does: one message's bar gets the
// plugin's class in its className, every other bar is passed on as is.
var vpOwl = vpPluginJS(`
class ViewHootPills extends TautPlugin {
  static id = 'ViewHootPills'
  static pluginName = 'Owl-shaped reaction bar (views test)'
  static description = 'test: a class on one message\'s ReactionBar, and rules for its pills'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    this.api.setStyle(` + strconv.Quote(vpOwlCSS) + `)
    this.api.patchComponent('ReactionBar', (O) => (p) => {
      if (p.reactionKey !== 'message-` + vpOwlTS + `-` + sGen + `') return React.createElement(O, p)
      return React.createElement(O, { ...p, className: (p.className ?? '') + ' v-hoot-bar' })
    })
  }
}
return ViewHootPills`)

// vpClsCSS rings a pill that carries the plugin's own class.
const vpClsCSS = `body { --v-ring: 200, 60, 20 }
body.sk-client-theme--dark { --v-ring: 255, 160, 90 }
.c-reaction.v-own-pill { box-shadow: 0 0 0 1px rgba(var(--v-ring), 1) }`

// vpClsJS adds its own class to every reaction pill's UnstyledButton;
// with bar it also passes ReactionBar on, so the buttons are slots in the
// bars' trees (patched alone, each button is a tree of its own, its root
// the pill).
func vpClsJS(bar bool) []byte {
	pass := ""
	if bar {
		pass = "    this.api.patchComponent('ReactionBar', (O) => (p) => React.createElement(O, p))\n"
	}
	return vpPluginJS(`
class ViewOwnPill extends TautPlugin {
  static id = 'ViewOwnPill'
  static pluginName = 'A class on the pill (views test)'
  static description = 'test: adds a class to the reaction pill button, and a rule for it'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    this.api.setStyle(` + strconv.Quote(vpClsCSS) + `)
` + pass + `    this.api.patchComponent('UnstyledButton', (O) => (p) => p['data-qa'] !== 'reactji' ? React.createElement(O, p) :
      React.createElement(O, { ...p, className: (p.className ?? '') + ' v-own-pill' }))
  }
}
return ViewOwnPill`)
}

// vpHost is slickHost with the image cache the pills' emoji load from.
type vpHost struct {
	*slickHost
	imgs *imgcache.Cache
}

func (h vpHost) Images() *imgcache.Cache { return h.imgs }

// vpImages serves the custom emoji (emoji.slack-edge.com) solid green and
// the standard ones (Slack's emoji CDN) solid blue; nothing else loads.
func vpImages() *imgcache.Cache {
	solid := func(c color.NRGBA) []byte {
		im := image.NewNRGBA(image.Rect(0, 0, 64, 64))
		for i := 0; i < len(im.Pix); i += 4 {
			im.Pix[i], im.Pix[i+1], im.Pix[i+2], im.Pix[i+3] = c.R, c.G, c.B, c.A
		}
		var b bytes.Buffer
		_ = png.Encode(&b, im)
		return b.Bytes()
	}
	green, blue := solid(vpGreen), solid(vpBlue)
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

// vpMessages are the three messages whose bars are drawn.
func vpMessages() []*core.Message {
	custom := func(n string) string { return "https://emoji.slack-edge.com/T0266FRGM/" + n + "/0a1b2c3d4e5f6071.png" }
	uni := func(n string) string {
		return "https://a.slack-edge.com/production-standard-emoji-assets/15.0/apple-medium/" + n + ".png"
	}
	return []*core.Message{
		{Channel: sGen, TS: vpCustom, User: sAlice, Text: "synthetic custom emoji pills", Reactions: []core.Reaction{
			{Name: "vp-parrot", Count: 1, Users: []string{sBob}, URL: custom("vp-parrot")},
			{Name: "vp-cat", Count: 13, Users: []string{sBob, sSelf}, Self: true, URL: custom("vp-cat")},
			{Name: "vp-owl", Count: 137, Users: []string{sBob, sAlice}, URL: custom("vp-owl")},
		}},
		{Channel: sGen, TS: vpUni, User: sBob, Text: "synthetic unicode emoji pills", Reactions: []core.Reaction{
			{Name: "tada", Count: 1, Users: []string{sAlice}, URL: uni("1f389")},
			{Name: "eyes", Count: 13, Users: []string{sAlice, sSelf}, Self: true, URL: uni("1f440")},
			{Name: "rocket", Count: 137, Users: []string{sAlice, sBob}, URL: uni("1f680")},
		}},
		{Channel: sGen, TS: vpOwlTS, User: sAlice, Text: "synthetic owl thread pills", Reactions: []core.Reaction{
			{Name: "vp-parrot", Count: 2, Users: []string{sBob, sAlice}, URL: custom("vp-parrot")},
			{Name: "tada", Count: 3, Users: []string{sBob, sSelf}, Self: true, URL: uni("1f389")},
		}},
	}
}

// vpEnv is the synthetic workspace (its Web API a local fake), Taut with
// the ui root's hooks (the document's <html> and <body> included: the
// plugins' colours are on <body>) and slackc with the emoji pictures.
func vpEnv(t *testing.T, dark bool) (*taut.Taut, *slickUI, vpHost) {
	t.Helper()
	api := newSlickAPI(t)
	c := slack.New(slack.Creds{Token: "xoxc-test", Cookie: "xoxd-test", UserID: sSelf, Team: "T0266FRGM", EnterpriseID: "E09V59WQY1E",
		TeamURL: api.srv.URL + "/", EnterpriseURL: api.srv.URL + "/"})
	c.EdgeURL = api.srv.URL + "/edge/"
	st := store.New("")
	seedSlick(st)
	app := core.New(c, st)
	if u, ok := st.User(sSelf); ok {
		app.S.Self = *u
	}
	app.ServeOffline()
	ui := &slickUI{trees: map[uint64][]byte{}}
	hooks := ui.hooks()
	hooks.OnDocumentProps = func(html, body []byte) { _ = slackc.SetDocumentProps(html, body) }
	dir := t.TempDir()
	tt, err := taut.New(taut.Options{App: app, Dir: filepath.Join(dir, "taut"), CredsPath: filepath.Join(dir, "creds.json"), UI: hooks,
		NoAutoStart: true, CacheDir: filepath.Join(dir, "cache"), Log: func(level, msg string) {
			if level == "error" {
				t.Logf("js %s: %s", level, msg)
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	slackc.SetBridge(tt.Bridge())
	th := theme.New(dark)
	imgs := vpImages()
	slackc.Init(slackc.Env{Theme: th, Core: app, Images: imgs, Invalidate: func() {}})
	t.Cleanup(func() {
		slackc.Init(slackc.Env{})
		_ = tt.Close()
		slackc.Reset()
		slackc.SetBridge(nil)
		slackc.SetPatched(nil)
	})
	return tt, ui, vpHost{slickHost: &slickHost{app: app, th: th, tt: tt}, imgs: imgs}
}

func (u *slickUI) treeHas(sub string) bool { return u.countIn(sub) > 0 }

// countIn counts sub in the delivered trees.
func (u *slickUI) countIn(sub string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	n := 0
	for _, tr := range u.trees {
		n += strings.Count(string(tr), sub)
	}
	return n
}

// vpFrame draws the messages' bars as view b lays a message's reactions
// out (actReactionsBar; noAdd as an Activity row's), one under another on
// the view's background.
func vpFrame(b *base, msgs []*core.Message, noAdd bool, ops *op.Ops) {
	ops.Reset()
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(vpW, vpH)), Now: time.Now()}
	slackc.Frame(gtx)
	b.frame()
	paint.FillShape(ops, b.p.Bg, clip.Rect{Max: image.Pt(vpW, vpH)}.Op())
	y := 40
	for _, m := range msgs {
		g := gtx
		g.Constraints = layout.Constraints{Max: image.Pt(vpW-2*vpX, 200)}
		off := op.Offset(image.Pt(vpX, y)).Push(ops)
		d := b.actReactionsBar(g, m, noAdd)
		off.Pop()
		y += d.Size.Y + 40
	}
}

// vpPill is one pill as drawn: its emoji's pixels, its fill along its
// middle (the pill's bounds), and its count's ink: the pixels at least
// half the text's colour over the fill, their ink summed and its centre.
type vpPill struct {
	emoji, box, ink image.Rectangle
	mass, cx, cy    float64
	fill, text      color.RGBA
}

func (p vpPill) String() string {
	return fmt.Sprintf("emoji %v pill %v count ink %v (%.1f px, centre %.2f,%.2f)", p.emoji, p.box, p.ink, p.mass, p.cx, p.cy)
}

func vpAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func vpDist(a, b color.RGBA) int {
	return vpAbs(int(a.R)-int(b.R)) + vpAbs(int(a.G)-int(b.G)) + vpAbs(int(a.B)-int(b.B))
}

// vpPills finds every pill in img by its emoji (one per pill, solid green
// or blue), in reading order.
func vpPills(img *image.RGBA) []vpPill {
	b := img.Bounds()
	near := func(c color.RGBA, w color.NRGBA) bool {
		return vpAbs(int(c.R)-int(w.R)) <= 24 && vpAbs(int(c.G)-int(w.G)) <= 24 && vpAbs(int(c.B)-int(w.B)) <= 24
	}
	isEmoji := func(x, y int) bool {
		c := img.RGBAAt(x, y)
		return near(c, vpGreen) || near(c, vpBlue)
	}
	seen := make([]bool, b.Dx()*b.Dy())
	var out []vpPill
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if seen[y*b.Dx()+x] || !isEmoji(x, y) {
				continue
			}
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
				out = append(out, vpMeasure(img, r))
			}
		}
	}
	return out
}

// vpMeasure measures the pill around the emoji at e: the background is
// left of the bars (x 2), the fill just left of the emoji (the pill's
// 6px padding); the pill ends where the background starts again, along
// the emoji's middle row and its middle column. Its count is the ink
// right of the emoji, 3px off the pill's top and bottom and 7px off its
// end (inside your pill's border and a plugin's ring).
func vpMeasure(img *image.RGBA, e image.Rectangle) vpPill {
	p := vpPill{emoji: e}
	yc, xc := (e.Min.Y+e.Max.Y)/2, (e.Min.X+e.Max.X)/2
	bg := img.RGBAAt(2, yc)
	p.fill = img.RGBAAt(e.Min.X-3, yc)
	half := vpDist(bg, p.fill) / 2
	isBg := func(x, y int) bool { return vpDist(img.RGBAAt(x, y), bg) <= half }
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
	cr := image.Rect(e.Max.X, y0+3, x1-7, y1-3)
	far := -1
	for y := cr.Min.Y; y < cr.Max.Y; y++ {
		for x := cr.Min.X; x < cr.Max.X; x++ {
			if c := img.RGBAAt(x, y); vpDist(c, p.fill) > far {
				far, p.text = vpDist(c, p.fill), c
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

// vpDiff counts r's pixels whose channels differ by more than 48 between
// a and b (a moved edge differs by far more; atlas placement noise by far
// less), and the first.
func vpDiff(a, b *image.RGBA, r image.Rectangle) (int, image.Point) {
	n := 0
	var first image.Point
	r = r.Intersect(a.Bounds()).Intersect(b.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ca, cb := a.RGBAAt(x, y), b.RGBAAt(x, y)
			if vpAbs(int(ca.R)-int(cb.R)) > 48 || vpAbs(int(ca.G)-int(cb.G)) > 48 || vpAbs(int(ca.B)-int(cb.B)) > 48 {
				if n == 0 {
					first = image.Pt(x, y)
				}
				n++
			}
		}
	}
	return n, first
}

// vpShot saves img for the CI artifact (MESSAGES_AUDIT_SHOTS_DIR).
func vpShot(t *testing.T, name string, img *image.RGBA) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		return
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "views-reaction-pill-"+name+".png"), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// vpView is one view whose bars are drawn: its base, and whether it
// draws them without the add button (an Activity row).
type vpView struct {
	name  string
	base  func(h Host) *base
	noAdd bool
}

var vpViews = []vpView{
	{"threads", func(h Host) *base { return &NewThreads(h).base }, false},
	{"unreads", func(h Host) *base { return &NewUnreads(h).base }, false},
	{"activity", func(h Host) *base { return &NewActivity(h).base }, false},
	{"activity-row", func(h Host) *base { return &NewActivity(h).base }, true},
}

func TestViewsPatchedReactionPillLikeNative(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, plug := range []string{"pass", "owl", "cls", "cls-bar"} {
			for _, vw := range vpViews {
				t.Run(mode+"/"+plug+"/"+vw.name, func(t *testing.T) { vpCompare(t, dark, plug, vw) })
			}
		}
	}
}

func vpCompare(t *testing.T, dark bool, plug string, vw vpView) {
	tt, u, h := vpEnv(t, dark)
	name := strings.ReplaceAll(t.Name()[strings.Index(t.Name(), "/")+1:], "/", "-")
	b := vw.base(h)
	msgs := vpMessages()
	ops := new(op.Ops)
	frame := func() { vpFrame(b, msgs, vw.noAdd, ops) }
	render := func() *image.RGBA {
		for i := 0; i < 3; i++ {
			frame()
		}
		return pixeltest.Render(t, vpW, vpH, ops)
	}
	// 8 pills: 3 custom, 3 unicode and the owl message's 2, each emoji in
	// (the pictures load through the image cache)
	settled := func(what string) (*image.RGBA, []vpPill) {
		t.Helper()
		var img *image.RGBA
		var pills []vpPill
		deadline := time.Now().Add(20 * time.Second)
		for {
			frame()
			img = render()
			if pills = vpPills(img); len(pills) == 8 {
				return img, pills
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: %s: %d pills drawn, want 8", name, what, len(pills))
			}
			time.Sleep(15 * time.Millisecond)
		}
	}
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); !cond(); time.Sleep(15 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: timed out waiting for %s", name, what)
			}
			frame()
		}
	}
	nimg, native := settled("the native pills")
	vpShot(t, name+"-native", nimg)

	js, patch := vpPass, "ReactionBar"
	switch plug {
	case "owl":
		js = vpOwl
	case "cls", "cls-bar":
		js, patch = vpClsJS(plug == "cls-bar"), "UnstyledButton"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	id, err := tt.Import(ctx, js, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatalf("enable %s: %v", id, err)
	}
	cls := strings.HasPrefix(plug, "cls")
	waitFor("the "+patch+" patch", func() bool {
		return slackc.Patched(patch) && (plug != "cls-bar" || slackc.Patched("ReactionBar"))
	})
	// every pill drawn through its tree (the plugin's class in it)
	waitFor("the trees", func() bool {
		switch plug {
		case "cls", "cls-bar":
			return u.countIn("v-own-pill") >= 8
		case "owl":
			if !u.treeHas("v-hoot-bar") {
				return false
			}
		}
		for _, m := range msgs {
			if !u.treeHas(`"reactionKey":"message-` + m.TS + `-` + sGen + `"`) {
				return false
			}
		}
		return true
	})
	for i := 0; i < 10; i++ {
		frame()
		time.Sleep(10 * time.Millisecond)
	}
	pimg, patched := settled("the patched pills")
	vpShot(t, name+"-patched", pimg)

	// the pills a plugin's rule doesn't reach are the native's, box and
	// all; a ring round a pill (the owl message's, every one under cls)
	// leaves its emoji where it was, and under cls (no colours of its
	// own) its count
	ringed := func(i int) bool { return cls || plug == "owl" && i >= 6 }
	var bar image.Rectangle
	for i := range native {
		n, p := native[i], patched[i]
		t.Logf("%s pill %d native:  %v", name, i, n)
		t.Logf("%s pill %d patched: %v", name, i, p)
		if p.emoji != n.emoji {
			t.Errorf("%s pill %d: emoji at %v, native %v", name, i, p.emoji, n.emoji)
		}
		if plug == "owl" && ringed(i) {
			continue // the plugin's own colours
		}
		if !ringed(i) && p.box != n.box {
			t.Errorf("%s pill %d: pill %v, native %v", name, i, p.box, n.box)
		}
		if p.ink != n.ink {
			t.Errorf("%s pill %d: count ink in %v, native %v", name, i, p.ink, n.ink)
		}
		if math.Abs(p.mass-n.mass) > 0.04*n.mass || math.Abs(p.cx-n.cx) > 0.3 || math.Abs(p.cy-n.cy) > 0.3 {
			t.Errorf("%s pill %d: count ink %.1f px centred at %.2f,%.2f; native %.1f at %.2f,%.2f",
				name, i, p.mass, p.cx, p.cy, n.mass, n.cx, n.cy)
		}
		if !ringed(i) {
			bar = bar.Union(n.box.Inset(-3))
		}
	}
	if !cls {
		// the bars, the add buttons after the pills included, pixel for
		// pixel
		bar.Max.X += 48
		n, at := vpDiff(nimg, pimg, bar)
		t.Logf("%s: %d px of the bars in %v differ from the native's", name, n, bar)
		if n > 0 {
			t.Errorf("%s: %d px of the bars in %v differ from the native's (the first at %v)", name, n, bar, at)
		}
	}
	if plug == "pass" {
		return
	}

	// the plugin's rules reach the pills it names, in its <body> colour
	// for the theme: a ring round each, round like the pill (the
	// UnstyledButton "@" box carries the pill's className too; painted as
	// a box of its own, its ring was square)
	ring := color.RGBA{R: 30, G: 120, B: 70, A: 0xff}
	if dark {
		ring = color.RGBA{R: 120, G: 230, B: 160, A: 0xff}
	}
	if cls {
		ring = color.RGBA{R: 200, G: 60, B: 20, A: 0xff}
		if dark {
			ring = color.RGBA{R: 255, G: 160, B: 90, A: 0xff}
		}
	}
	for i, p := range patched {
		if !ringed(i) {
			continue
		}
		r := p.box.Inset(-2)
		px := 0
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				if vpDist(pimg.RGBAAt(x, y), ring) <= 60 {
					px++
				}
			}
		}
		t.Logf("%s: %d px of the plugin's ring around pill %d %v", name, px, i, r)
		if px < 30 {
			t.Errorf("%s: %d px in the plugin's colour %v around pill %d, want its 1px ring round it (the plugin's box-shadow)", name, px, ring, i)
		}
		// the pill's box's corners, outside its round ends, are the
		// row's, and so are the pixels diagonally out from them (a
		// square ring's)
		x0, y0, y1 := p.emoji.Min.X-6, p.emoji.Min.Y-4, p.emoji.Max.Y+4
		row := pimg.RGBAAt(2, (y0+y1)/2)
		for _, c := range []image.Point{{x0 + 1, y0 + 1}, {x0 + 1, y1 - 2}, {x0 - 1, y0 - 1}, {x0 - 1, y1}} {
			if got := pimg.RGBAAt(c.X, c.Y); vpDist(got, row) > 16 {
				t.Errorf("%s: pill %d: %v at its box's corner %v, outside its round end, where the row is %v", name, i, got, c, row)
			}
		}
	}
	if plug != "owl" {
		return
	}
	owl := ring
	re := patched[7] // tada, yours
	if dark {
		// its fill rgba(owl, .15) over the row (blended in sRGB as Chrome
		// does, or in linear light), not your pill's own tint
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
		if vpDist(re.fill, ws) > 24 && vpDist(re.fill, wl) > 24 {
			t.Errorf("%s: your pill's fill %v, want the plugin's rgba(owl, .15) over the row %v: %v (sRGB) or %v (linear)", name, re.fill, bg, ws, wl)
		}
		return
	}
	// its count in var(--dt_color-content-pry), not your pill's link blue
	pry := color.RGBA{R: 0x1d, G: 0x1c, B: 0x1d, A: 0xff}
	if vpDist(re.text, pry) > 60 {
		t.Errorf("%s: your pill's count in %v, want the plugin's content-pry %v", name, re.text, pry)
	}
}
