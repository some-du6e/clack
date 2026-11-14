package richview

// GIFs (animated.go). Synthetic documents only.

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/op/paint"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// gifImages is an imgcache stand-in: every image ready, the URLs in
// anim animated GIFs (playing while animations are on), and the calls
// counted.
type gifImages struct {
	anim  map[string]bool
	gets  map[string]int
	plays map[string]int
	img   paint.ImageOp
}

func newGIFImages(anim ...string) *gifImages {
	a := &gifImages{anim: map[string]bool{}, gets: map[string]int{}, plays: map[string]int{},
		img: paint.NewImageOp(image.NewRGBA(image.Rect(0, 0, 480, 270)))}
	for _, u := range anim {
		a.anim[u] = true
	}
	return a
}

func (a *gifImages) Get(url string, px int) (paint.ImageOp, bool) { a.gets[url]++; return a.img, true }
func (a *gifImages) Animated(url string, px int) bool {
	return a.anim[url] && !uipref.Get().NoAnimations
}
func (a *gifImages) IsAnimation(url string, px int) bool { return a.anim[url] }
func (a *gifImages) GetPlaying(url string, px int) (paint.ImageOp, bool) {
	a.plays[url]++
	return a.img, true
}

const (
	gifThumb = "https://files.slack.com/files-tmb/T1-F1-x/party_480.gif"
	gifStill = "https://files.slack.com/files-tmb/T1-F1-x/party_deanimate.png"
	pngThumb = "https://files.slack.com/files-tmb/T1-F2-x/shot_720.png"
)

func gifDoc() (rt.Doc, *rt.File) {
	f := &rt.File{ID: "F1", Name: "party.gif", Mime: "image/gif", Type: "gif", IsImage: true, OrigW: 498, OrigH: 280,
		Thumb: &rt.Image{URL: gifThumb, W: 480, H: 270}, Still: &rt.Image{URL: gifStill, W: 498, H: 280}, Deanimate: true}
	g := &rt.File{ID: "F2", Name: "shot.png", Mime: "image/png", Type: "png", IsImage: true, OrigW: 1200, OrigH: 700,
		Thumb: &rt.Image{URL: pngThumb, W: 720, H: 420}}
	return doc(rt.Block{Kind: rt.FileBlock, File: f}, para(txt("and")), rt.Block{Kind: rt.FileBlock, File: g}), f
}

// TestAnimatedImageDrawnEveryFrame: a GIF tile far larger than 160px is
// drawn from the cache at every frame (its frame due now), not frozen in
// the recording; a still image stays recorded.
func TestAnimatedImageDrawnEveryFrame(t *testing.T) {
	uipref.Reset()
	d, _ := gifDoc()
	ai := newGIFImages(gifThumb)
	hs := newHarness(d, &recHandler{})
	hs.w = 700
	hs.st.Images = ai
	hs.frame()
	hs.frame()
	if len(hs.v.liveImgs) != 1 || hs.v.d.imgs[hs.v.liveImgs[0]].url != gifThumb {
		t.Fatalf("live images %v, want the GIF's tile", hs.v.liveImgs)
	}
	if im := hs.v.d.imgs[hs.v.liveImgs[0]]; max(im.r.Dx(), im.r.Dy()) <= 160 || im.px <= 160 {
		t.Fatalf("the GIF tile is %v at %dpx", im.r, im.px)
	}
	g, s := ai.gets[gifThumb], ai.gets[pngThumb]
	for i := 0; i < 5; i++ {
		hs.frame()
	}
	if got := ai.gets[gifThumb] - g; got != 5 {
		t.Errorf("5 frames drew the GIF %d times", got)
	}
	if got := ai.gets[pngThumb] - s; got != 0 {
		t.Errorf("5 frames drew the recorded PNG %d times", got)
	}
}

// TestStillPlayButtonAndExpand is Accessibility > "Allow animations" off:
// the GIF file draws its deanimated still (recorded), as
// PlayPauseAnimations with Slack's Play button; a click plays the
// animated thumb through GetPlaying, another stops it; hovering the tile
// shows FileActions' Expand at top 8, right 8, which opens the file.
func TestStillPlayButtonAndExpand(t *testing.T) {
	uipref.Set(store.Prefs{NoAnimations: true})
	defer uipref.Reset()
	d, f := gifDoc()
	ai := newGIFImages(gifThumb)
	h := &recHandler{}
	hs := newHarness(d, h)
	hs.w = 700
	hs.st.Images = ai
	hs.frame()
	tile := hs.v.d.imgs[0]
	if tile.url != gifStill || tile.play || len(hs.v.liveImgs) != 0 {
		t.Fatalf("animations off: the tile draws %q (play %v, live %v), want the still", tile.url, tile.play, hs.v.liveImgs)
	}
	if len(hs.v.d.plays) != 1 || len(hs.v.d.expands) != 1 {
		t.Fatalf("%d play buttons, %d Expand buttons; want one each (the PNG has neither)", len(hs.v.d.plays), len(hs.v.d.expands))
	}
	mid := tile.r.Min.Add(tile.r.Max).Div(2)
	p := f32.Pt(float32(mid.X), float32(mid.Y))
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: p})
	if hs.v.playAt(hs.st, p) != 0 {
		t.Fatal("no PlayPauseAnimations under the tile's middle")
	}

	// Expand: top 8, right 8, 32px, while the tile is hovered
	bar, btn := hs.v.expandBar(0)
	if btn.Dx() != 32 || bar.Max.X != tile.r.Max.X-8 && bar.Max.X != hs.v.d.expands[0].card.Max.X-8 || bar.Min.Y != hs.v.d.expands[0].card.Min.Y+8 {
		t.Errorf("Expand %v in tile %v", btn, hs.v.d.expands[0].card)
	}
	bp := f32.Pt(float32(btn.Min.X+btn.Dx()/2), float32(btn.Min.Y+btn.Dy()/2))
	hs.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: bp})
	if hs.v.expandAt(hs.st, bp) != 0 {
		t.Fatal("no Expand under the pointer")
	}
	hs.send(press(bp, time.Second), release(bp, time.Second+20*time.Millisecond))
	if len(h.files) != 1 || h.files[0] != f || hs.v.playing[gifThumb] {
		t.Fatalf("Expand opened %d files (playing %v)", len(h.files), hs.v.playing[gifThumb])
	}

	// a click on the still plays it
	hs.send(press(p, 2*time.Second), release(p, 2*time.Second+20*time.Millisecond))
	hs.frame()
	tile = hs.v.d.imgs[0]
	if !hs.v.playing[gifThumb] || tile.url != gifThumb || !tile.play || len(hs.v.liveImgs) != 1 {
		t.Fatalf("after a click: playing %v, the tile draws %q (play %v, live %v)", hs.v.playing[gifThumb], tile.url, tile.play, hs.v.liveImgs)
	}
	n := ai.plays[gifThumb]
	hs.frame()
	hs.frame()
	if ai.plays[gifThumb]-n != 2 {
		t.Errorf("playing: 2 frames called GetPlaying %d times", ai.plays[gifThumb]-n)
	}
	if len(h.files) != 1 {
		t.Errorf("the click opened the file too (%d)", len(h.files))
	}
	// and another stops it
	hs.send(press(p, 3*time.Second), release(p, 3*time.Second+20*time.Millisecond))
	hs.frame()
	if hs.v.playing[gifThumb] || hs.v.d.imgs[0].url != gifStill {
		t.Errorf("after a second click: playing %v, drawing %q", hs.v.playing[gifThumb], hs.v.d.imgs[0].url)
	}
}

// TestAttachmentStillPlays: an attachment's GIF has the Play button once
// its decoded image says it animates (Slack asks its proxy for
// x-slack-animated), and none for a still image.
func TestAttachmentStillPlays(t *testing.T) {
	uipref.Set(store.Prefs{NoAnimations: true})
	defer uipref.Reset()
	const gifURL, pngURL = "https://media.example.test/cat.gif", "https://media.example.test/cat.png"
	for _, c := range []struct {
		url  string
		anim bool
	}{{gifURL, true}, {pngURL, false}} {
		ai := newGIFImages(gifURL)
		a := &rt.Attachment{Image: &rt.Image{URL: c.url, W: 480, H: 270}}
		hs := newHarness(doc(rt.Block{Kind: rt.AttachmentBlock, Att: a}), &recHandler{})
		hs.w = 700
		hs.st.Images = ai
		hs.frame()
		if len(hs.v.d.plays) != 1 {
			t.Fatalf("%s: %d play controls", c.url, len(hs.v.d.plays))
		}
		im := hs.v.d.imgs[hs.v.d.plays[0].img]
		mid := im.r.Min.Add(im.r.Max).Div(2)
		p := f32.Pt(float32(mid.X), float32(mid.Y))
		if on := hs.v.playAt(hs.st, p) == 0; on != c.anim {
			t.Errorf("%s: play button %v, want %v", c.url, on, c.anim)
		}
	}
}
