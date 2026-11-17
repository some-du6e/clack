package messages

// Karim, α.102 (Windows, 1x, light custom theme): "broken": in the thread
// pane of a private channel an app mention pill wrapped after its first
// word, and its picture was drawn at the left edge between the two lines,
// over the first line's text, while the pill kept an empty gap after "("
// where the picture belongs. His plugins patch Blocks, MessageText,
// MessageWrapper and ThreadReplyGeneric; the picture is MentionProfiles'
// (Slick: MemberSlug wrapped in a span whose rule puts a 1em inline-block
// in the slug's ::before, margin 0 3px .2em 1px, vertical-align: middle).
// Slack's own MemberSlug (boot-shared ld4x) draws no picture in the
// pill; Chromium lays the ::before out inline at the pill's start, on its
// first line, and slices the pill's background per line.
//
// Here, in a channel row and a thread reply, with the rule installed
// alone (css) and from a MentionProfiles-shaped plugin that also patches
// Karim's four components (plugin), at the widths the mention wraps at:
// the picture is drawn once, on the pill's first line just before the
// name, over none of the mention's words. Synthetic data; the picture is
// solid green. Only what ui had before the fix is used, so the
// messages-audit-shots workflow runs this on ui too (where it fails) and
// saves both shots (MESSAGES_AUDIT_SHOTS_DIR, PILL_WRAP_SHOTS_TAG).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

const (
	pwChan = "C0INTAKE01"
	pwApp  = "U0RELAYAPP" // an app user, "relay connector"
	pwRoot = "1700000950.000100"
	pwMsg  = "1700000951.000100" // in the channel
	pwRep  = "1700000952.000100" // in the thread
)

// pwRule is MentionProfiles' ::before, in solid green (double quotes: the
// plugin's setStyle string is single-quoted).
const pwRule = `content: ""; display: inline-block; width: 1em; height: 1em; margin: 0 3px 0.2em 1px; border-radius: 50%; background: #00ff00; vertical-align: middle`

// pwPlugin is MentionProfiles' shape (its MemberSlug patch: the original
// slug in a span its rule selects) with pass-through patches of the
// components Karim's plugins patch.
var pwPlugin = pluginJS(`
class PillPicture extends TautPlugin {
  static id = 'PillPicture'
  static pluginName = 'Pill picture (test)'
  static description = 'test: a picture in mention pills, as MentionProfiles draws it'
  static authors = ['clack']
  static category = 'messages'
  static defaultConfig = { enabled: true }
  start() {
    const R = React
    this.api.setStyle('.x-pill-picture .c-member_slug::before { ` + pwRule + ` }')
    this.api.patchComponent('MemberSlug', (O) => (p) => R.createElement('span', { className: 'x-pill-picture' }, R.createElement(O, p)))
    for (const n of ['Blocks', 'MessageText', 'MessageWrapper', 'ThreadReplyGeneric'])
      this.api.patchComponent(n, (O) => (p) => R.createElement(O, p))
  }
}
return PillPicture`)

// pwSeed adds the app user (pic: its picture's URL prefix, or none) and
// #intake: a thread root, a channel message and a reply reading "Fields /
// relay connector (@relay connector) / Contact email".
func pwSeed(s *store.Store, pic string) {
	p := slack.Profile{DisplayName: "relay connector", RealName: "relay connector"}
	if pic != "" {
		p.Image24, p.Image48 = pic+"24.png", pic+"48.png"
	}
	s.PutUsers([]slack.User{{ID: pwApp, Name: "relay_connector", RealName: "relay connector", IsBot: true, Profile: p}})
	s.PutConv(slack.Conversation{ID: pwChan, Name: "intake", IsChannel: true, IsPrivate: true, IsMember: true, NumMembers: 4})
	body := shotRich(map[string]any{"type": "rich_text_section", "elements": []any{
		map[string]any{"type": "text", "text": "Fields\nrelay connector ("},
		map[string]any{"type": "user", "user_id": pwApp},
		map[string]any{"type": "text", "text": ")\nContact email"},
	}})
	text := "Fields\nrelay connector (<@" + pwApp + ">)\nContact email"
	root := slack.Message{Type: "message", TS: pwRoot, User: "UBOB", Text: "New intake form", Blocks: shotRich(shotSection("New intake form")),
		ThreadTS: pwRoot, ReplyCount: 1, ReplyUsers: []string{"UALICE"}, LatestReply: pwRep}
	s.SetLatest(pwChan, []slack.Message{root, {Type: "message", TS: pwMsg, User: "UALICE", Text: text, Blocks: body}}, false)
	s.SetThread(pwChan, pwRoot, []slack.Message{root, {Type: "message", TS: pwRep, ThreadTS: pwRoot, User: "UALICE", Text: text, Blocks: body}})
}

func TestWrappedMentionPictureInRows(t *testing.T) {
	for _, mode := range []string{"css", "plugin"} {
		t.Run(mode, func(t *testing.T) {
			var app *core.App
			var h coreHost
			if mode == "plugin" {
				a, _, tt, _, ph := pluginEnv(t)
				app, h = a, ph
				install(t, tt, pwPlugin)
				until(t, 5*time.Second, "the patches", func() {}, func() bool {
					return slackc.Patched("MemberSlug") && slackc.Patched("ThreadReplyGeneric") && slackc.Patched("Blocks")
				})
			} else {
				a, _ := newTestApp(t)
				app, h = a, coreHost{app: a, th: theme.New(false)}
				slackc.SetStyle("test:pill-picture", `.c-member_slug::before { `+pwRule+` }`)
				t.Cleanup(func() { slackc.RemoveStyle("test:pill-picture") })
			}
			pwSeed(app.S, "")
			pwPane(t, h, mode, "channel")
			pwPane(t, h, mode, "thread")
		})
	}
}

// The same with Slick's MentionProfiles itself (the ui-slick workflow
// builds it): the app user's image_48 in the pill, from a real-shaped
// avatars.slack-edge.com URL through the image cache.
func TestSlickViewMentionProfilesWrap(t *testing.T) {
	e := newSlickEnv(t, func(s *store.Store) {
		pwSeed(s, "https://avatars.slack-edge.com/2025-06-01/9001234567890_0a1b2c3d4e5f60718293_")
	})
	im := image.NewNRGBA(image.Rect(0, 0, 48, 48))
	for i := range im.Pix {
		if i%4 == 1 || i%4 == 3 {
			im.Pix[i] = 0xff // opaque green
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, im)
	imgs := imgcache.New(func(ctx context.Context, url string) ([]byte, error) {
		if strings.HasPrefix(url, "https://avatars.slack-edge.com/") {
			return b.Bytes(), nil
		}
		return nil, errors.New("offline")
	}, "", nil)
	slackc.Init(slackc.Env{Theme: e.h.th, Core: e.app, Images: imgs, Invalidate: func() {}})
	t.Cleanup(func() { slackc.Init(slackc.Env{}) })
	e.install(t, "MentionProfiles", nil)
	patched(t, "MemberSlug")
	pwPane(t, e.h, "slick", "channel")
	pwPane(t, e.h, "slick", "thread")
}

// pwGreen counts the picture's (green) pixels in img and their bounds.
func pwGreen(img *image.RGBA) (int, image.Rectangle) {
	n, bb := 0, image.Rectangle{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if c := img.RGBAAt(x, y); c.G > 0xc0 && c.R < 0x60 && c.B < 0x60 {
				n++
				bb = bb.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return n, bb
}

// pwInk counts img's dark pixels in r (text: the message's and the pill's
// glyphs; luma under 150) and where the first is.
func pwInk(img *image.RGBA, r image.Rectangle) (int, image.Point) {
	n := 0
	var at image.Point
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if 299*int(c.R)+587*int(c.G)+114*int(c.B) < 150*1000 {
				if n == 0 {
					at = image.Pt(x, y)
				}
				n++
			}
		}
	}
	return n, at
}

// pwPane sweeps the pane's width and checks where the mention's picture
// is drawn at each layout the mention takes there (a new one whenever its
// words move): one circle, over none of its words, in the pill's first
// line box right before "@relay", or ending the line above when the line
// broke after it (then "@relay" starts its line). Chromium breaks the pill
// at the space in it, after the picture, and after "(" before it; the
// sweep has to see the first and one of the others.
func pwPane(t *testing.T, h coreHost, mode, pane string) {
	t.Helper()
	var v interface {
		Layout(gtx layout.Context) layout.Dimensions
		LinkRects() []LinkRect
	}
	var ls *listState
	ts := pwMsg
	if pane == "thread" {
		tv := NewThreadView(h, pwChan, pwRoot)
		v, ts, ls = tv, pwRep, tv.s
	} else {
		cv := NewChannelView(h, pwChan)
		cv.Preview = true
		v, ls = cv, cv.s
	}
	name := mode + "/" + pane
	const hgt = 560
	ops := new(op.Ops)
	frame := func(w int) { shotFrame(v, h.th, w, hgt, ops) }
	words := func() []LinkRect { // the mention's words, in layout order
		var out []LinkRect
		for _, r := range v.LinkRects() {
			if r.TS == ts && r.Kind == richview.HitUser && r.ID == pwApp {
				out = append(out, r)
			}
		}
		return out
	}
	until(t, 10*time.Second, name+": the message with the mention", func() { frame(700) }, func() bool { return len(words()) >= 2 })
	// the picture is in before the sweep (Slick's loads through the image
	// cache)
	until(t, 15*time.Second, name+": the picture drawn", func() { frame(700) }, func() bool {
		n, _ := pwGreen(pixeltest.Render(t, 700, hgt, ops))
		return n >= 80
	})
	seen := map[string]int{}
	var sig string
	for w := 200; w <= 600; w++ {
		for i := 0; i < 3; i++ {
			frame(w)
		}
		ws := words()
		r := ls.rows[ts]
		if len(ws) < 2 || r == nil {
			continue
		}
		left := r.bodyRect.Min.X // the body's left edge (its lines start there)
		first := ws[0].R
		s := fmt.Sprint(ws[0].R.Sub(image.Pt(left, 0)), ws[1].R.Sub(image.Pt(left, 0)))
		if s == sig {
			continue // the same layout as the last width
		}
		sig = s
		kind := ""
		switch {
		case first.Min.Y != ws[1].R.Min.Y:
			kind = "space" // the break at the space in the pill
		case first.Min.X <= left+1:
			kind = "after" // @relay starts its line: the picture ended the one above
		case first.Min.X <= left+24:
			kind = "paren" // the pill (padding, picture) opens its line: "(" ended the one above
		default:
			continue // on one line, after other words
		}
		seen[kind]++
		var rs []image.Rectangle
		for _, r := range ws {
			rs = append(rs, r.R)
		}
		t.Logf("%s, w=%d (%s): the row's body from x=%d, the mention's words %v", name, w, kind, left, rs)
		img := pixeltest.Render(t, w, hgt, ops)
		if dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR"); dir != "" && seen[kind] == 1 {
			tag := os.Getenv("PILL_WRAP_SHOTS_TAG")
			if tag == "" {
				tag = "after"
			}
			y0, y1 := max(first.Min.Y-70, 0), min(ws[len(ws)-1].R.Max.Y+50, hgt)
			savePNG(t, filepath.Join(dir, fmt.Sprintf("pill-wrap-%s-%s-%s-%s.png", mode, pane, kind, tag)), crop(img, y0, y1))
		}
		n, bb := pwGreen(img)
		if n < 80 || bb.Dx() > 18 || bb.Dy() > 18 {
			t.Errorf("%s, w=%d (%s): %d picture px in %v, want one ~15px circle", name, w, kind, n, bb)
			continue
		}
		for _, r := range ws {
			if r.R.Overlaps(bb) {
				t.Errorf("%s, w=%d (%s): the picture at %v is drawn over the mention's word at %v", name, w, kind, bb, r.R)
			}
		}
		lineH := first.Dy()
		switch cy := (bb.Min.Y + bb.Max.Y) / 2; {
		case cy >= first.Min.Y && cy < first.Max.Y:
			if kind == "after" || bb.Max.X > first.Min.X+1 || bb.Min.X < first.Min.X-22 {
				t.Errorf("%s, w=%d (%s): the picture at %v isn't just before @relay at %v", name, w, kind, bb, first)
			}
		case cy >= first.Min.Y-lineH && cy < first.Min.Y:
			if kind != "after" {
				t.Errorf("%s, w=%d (%s): the picture at %v is on the line above @relay at %v, which doesn't start its line (x=%d)", name, w, kind, bb, first, left)
			}
			// it ends that line: nothing is drawn after it there (its 3px
			// margin and the pill's 2px padding, then the line's end), so
			// it isn't drawn over the line's words (at its start, as Karim
			// saw it)
			if n, at := pwInk(img, image.Rect(bb.Max.X+6, bb.Min.Y, w-16, bb.Max.Y)); n > 0 {
				t.Errorf("%s, w=%d (%s): %d px of text after the picture at %v on its line (the first at %v): the picture doesn't end its line", name, w, kind, n, bb, at)
			}
		default:
			t.Errorf("%s, w=%d (%s): the picture at %v is on neither @relay's line %v nor the one above", name, w, kind, bb, first)
		}
	}
	t.Logf("%s: layouts seen %v", name, seen)
	if seen["space"] == 0 || seen["after"]+seen["paren"] == 0 {
		t.Errorf("%s: the mention broke at the space in %d layouts, after the picture or \"(\" in %d: want both (Chromium breaks around the picture)",
			name, seen["space"], seen["after"]+seen["paren"])
	}
}
