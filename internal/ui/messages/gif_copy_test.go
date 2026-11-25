package messages

import (
	"context"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// The GIF picker's message (fakeslack.GIFPickerMessage, the shape of
// Karim's #emojibot GIF, 2026-11-24) right-clicked as Slack answers it
// (UvHM AttachmentImage, build e277bbacc0bc):
//
//   - animations on, the GIF is the CSS background of its <a href>: the
//     desktop shell's link menu, Copy Link and Open Link, no Copy Image;
//   - animations off, it is PlayPauseAnimations' <img src={source}
//     alt={title}>: the image menu, Copy Image and Copy Image URL, for
//     the proxy's still (getImgProxyUrl rotate + gu) with the block's
//     alt_text.

const (
	tsGIFPick  = "1700000030.000100"
	gifPickURL = "https://media3.giphy.com/media/v1.Y2lkPTc5MGI3NjExYWJj/l0HlNQ03J5JxX6lva/giphy.webp?cid=790b7611abc&rid=giphy.webp&ct=g"
	gifPickQ   = "https%3A%2F%2Fmedia3.giphy.com%2Fmedia%2Fv1.Y2lkPTc5MGI3NjExYWJj%2Fl0HlNQ03J5JxX6lva%2Fgiphy.webp%3Fcid%3D790b7611abc%26rid%3Dgiphy.webp%26ct%3Dg"
	gifPickAlt = "A test GIF"
)

// gifPickEnv shows the GIF picker's message alone, its image served as
// fakeslack's animated WebP.
func gifPickEnv(t *testing.T) *ctxEnv {
	t.Helper()
	webp := fakeslack.GIFPickerWebP(220, 260)
	im := imgcache.New(func(context.Context, string) ([]byte, error) { return webp, nil }, "", nil)
	m := fakeslack.GIFPickerMessage(tsGIFPick, "UALICE", gifPickURL, gifPickAlt, 220, 260, len(webp))
	return newCtxEnvImages(t, tGen, []slack.Message{m}, nil, im)
}

// gifPickAt is the GIF's middle once ok holds for its hit (the image
// decoded: richview knows it animates).
func (e *ctxEnv) gifPickAt(what string, ok func(richview.Hit) bool) f32.Point {
	e.t.Helper()
	var p f32.Point
	until(e.t, 10*time.Second, what, e.frame, func() bool {
		r := e.row(tsGIFPick)
		if r == nil {
			return false
		}
		rs := r.rv.ImageRects()
		if len(rs) != 1 {
			return false
		}
		c := rs[0].R.Min.Add(rs[0].R.Size().Div(2))
		local := f32.Pt(float32(c.X)+.5, float32(c.Y)+.5)
		if !ok(r.rv.HitAt(local)) {
			return false
		}
		top := e.v.s.pos[e.v.s.sel.keyIdx[tsGIFPick]]
		p = local.Add(f32.Pt(float32(r.bodyRect.Min.X), float32(top+r.bodyRect.Min.Y)))
		return true
	})
	return p
}

func TestGIFPickerImageAnimationsOn(t *testing.T) {
	uipref.Reset()
	e := gifPickEnv(t)
	var opened []string
	openURL = func(u string) { opened = append(opened, u) }
	p := e.gifPickAt("the GIF's link", func(h richview.Hit) bool { return h.Kind == richview.HitImage && h.Link != "" })
	e.rightClick(p)
	linkMenuDoes(t, e.menu(0, "right-click on the GIF"), gifPickURL, "the GIF, animations on", &opened)
}

func TestGIFPickerImageAnimationsOff(t *testing.T) {
	uipref.Set(store.Prefs{NoAnimations: true})
	t.Cleanup(uipref.Reset)
	e := gifPickEnv(t)
	m := e.gifPickStillMenu()
	if got := strings.Join(labels(m), "|"); got != "Copy Image|Copy Image URL" {
		t.Fatalf("animations off: menu %q, want the image menu", got)
	}
	if got := clipOf(t, m.Items()[1]); got != gifPickStill {
		t.Errorf("Copy Image URL copied %q, want the <img>'s src %q", got, gifPickStill)
	}
}

// gifPickStill is the GIF's <img> src with animations off: the proxy's
// still (getImgProxyUrl rotate + gu).
const gifPickStill = "https://slack-imgs.com/?c=1&o1=ro.gu&url=" + gifPickQ

// gifPickStillMenu right-clicks the GIF's PlayPauseAnimations (animations
// off: the image, no link) and returns the menu.
func (e *ctxEnv) gifPickStillMenu() *popups.Menu {
	e.t.Helper()
	p := e.gifPickAt("the GIF's PlayPauseAnimations", func(h richview.Hit) bool { return h.Kind == richview.HitImage && h.Link == "" })
	e.rightClick(p)
	return e.menu(0, "right-click on the GIF's still")
}
