package richview

// Karim's weather icon (#clack, α.112): a message unfurl whose image block
// is a 64×64 icon on a plain-http third-party host drew an empty box (its
// title and collapse caret were fine). Slack's client loads a block's
// image through AttachmentImage (boot-shared UvHM), whose source is
// getImgProxyUrl(url, {height, sizeForRetina, rotate}) (client-boot
// d0up): the URL itself only when it's https on one of Slack's hosts,
// anything else (plain http included) the team's image proxy
// (getTeamImgProxyUrlRoot, rUVB: image_proxy_url, else slack-imgs.com)
// as ?c=1&o1=ro&url=<the URL> (no wi/he: AttachmentImage passes the
// height alone and the size options need both). Clack's fetcher refused
// any URL that wasn't https before it got to the proxy. The box is the
// API's image_width × image_height (Image, 8plt), and the image is the
// box's CSS background (.c-message_attachment__image, an inset 1px
// otl-ter ring, radius 8), so the box stays empty while the image loads
// or if it fails. Fixtures are synthetic.

import (
	"context"
	"errors"
	"image"
	"net/url"
	"testing"
	"time"

	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// TestHTTPImageBlockLoads: the gallery's coverage/http image (a message
// unfurl, its image block a 64×64 icon on http://icons.example.net) draws
// a 64×64 box whose image loads with the session's fetcher
// (imgcache.SessionFetch, as cmd/clack builds it) through Slack's image
// proxy, https://slack-imgs.com/?c=1&o1=ro&url=…, without the session's
// cookie, and never from the icon's own host (which answers 404).
func TestHTTPImageBlockLoads(t *testing.T) {
	sz := fakeslack.Small
	sz.Gallery = true
	fs, err := fakeslack.Start(sz)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	c := slack.New(fs.Creds())
	t.Cleanup(fs.RouteSlackHosts(c))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ts := fs.GalleryTS("coverage/http image")
	if ts == "" {
		t.Fatal("no gallery message coverage/http image")
	}
	pg, err := c.History(ctx, fakeslack.GalleryChannelID, slack.HistoryOpts{Oldest: ts, Inclusive: true, Limit: 1})
	if err != nil || len(pg.Messages) != 1 || pg.Messages[0].TS != ts {
		t.Fatalf("conversations.history from %s: %v (%+v)", ts, err, pg)
	}
	m := pg.Messages[0]
	d := rt.FromMessage(codeRes{}, &m)
	imgs := imgcache.New(sessionFetch(c, func(_ context.Context, raw string) ([]byte, error) {
		t.Errorf("the session's credentials asked for %s", raw)
		return nil, errors.New("not the session's")
	}), "", nil)
	st := newTestStyle()
	st.Images = imgs
	v := New(d, Options{})
	draw := func() {
		var ops op.Ops
		v.Layout(testGtx(&ops, 600), st, &recHandler{})
	}
	draw()
	ims := imgsWithURL(&v.d, fakeslack.GalleryHTTPImage)
	if len(ims) != 1 {
		t.Fatalf("%d images drawn from %s, want the image block's one (images %+v)", len(ims), fakeslack.GalleryHTTPImage, v.d.imgs)
	}
	im := ims[0]
	if got := im.r.Size(); got != image.Pt(64, 64) {
		t.Errorf("image block box %v, want its image_width × image_height, 64×64", got)
	}
	for end := time.Now().Add(20 * time.Second); imgs.Pending() > 0 && time.Now().Before(end); {
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := imgs.Get(im.url, im.px); !ok {
		t.Fatalf("%s didn't load (failed: %v; loads %+v)", im.url, imgs.Failed(im.url, im.px), fs.PhotoLoads())
	}
	draw()
	want := "https://slack-imgs.com/?c=1&o1=ro&url=" + url.QueryEscape(fakeslack.GalleryHTTPImage)
	proxied := 0
	for _, l := range fs.PhotoLoads() {
		switch l.Host {
		case "icons.example.net":
			t.Errorf("loaded from the icon's own host: %+v", l)
		case "slack-imgs.com":
			proxied++
			if l.URL != want || l.Cookie {
				t.Errorf("proxy load %+v, want %s without the session's cookie", l, want)
			}
		}
	}
	if proxied != 1 {
		t.Errorf("%d loads through the image proxy, want 1 (loads %+v)", proxied, fs.PhotoLoads())
	}
}

// TestImageBlockEmptyUntilLoaded: while an image block's image loads, and
// if it fails, its box is Slack's empty c-message_attachment__image (the
// image is a CSS background; only the 1px ring shows), not the
// placeholder tile Clack draws under other images.
func TestImageBlockEmptyUntilLoaded(t *testing.T) {
	const u = "http://icons.example.net/img/120x80/pending.png"
	d := kitDoc(t, `[{"type":"image","image_url":"`+u+`","alt_text":"pending","image_width":120,"image_height":80,"image_bytes":2048}]`)
	pal := LightPalette()
	st := NewStyle(themeShaper(), noImages{}, pal)
	const w, h = 400, 200
	v := layoutAt(st, d, Options{}, w)
	ims := imgsWithURL(&v.d, u)
	if len(ims) != 1 || !imgBare(ims[0]) {
		t.Fatalf("image block images %+v: want one bare box (nothing under it)", ims)
	}
	r := ims[0].r
	if r.Size() != image.Pt(120, 80) {
		t.Errorf("box %v, want 120×80", r)
	}
	var ops op.Ops
	paint.FillShape(&ops, pal.Panel, clip.Rect{Max: image.Pt(w, h)}.Op())
	v.Layout(testGtx(&ops, w), st, &recHandler{})
	img := pixeltest.Render(t, w, h, &ops)
	in := r.Inset(4)
	if n := pixeltest.Count(img, in, pal.Placeholder, 3); n > 0 {
		t.Errorf("%d placeholder pixels inside the pending image block's box", n)
	}
	if n, all := pixeltest.Count(img, in, pal.Panel, 3), in.Dx()*in.Dy(); n < all*95/100 {
		t.Errorf("%d of %d px inside the pending box are the background", n, all)
	}
}
