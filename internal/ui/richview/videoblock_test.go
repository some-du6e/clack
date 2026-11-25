package richview

import (
	"image"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/expandable"
)

const videoBlk = `[{"type":"video","block_id":"vid","title":{"type":"plain_text","text":"Demo"},"title_url":"https://v.test/w",` +
	`"video_url":"https://v.test/embed","thumbnail_url":"https://v.test/t.jpg","alt_text":"demo","author_name":"Someone",` +
	`"provider_name":"VidSite","provider_icon_url":"https://v.test/i.png","description":{"type":"plain_text","text":"A short description"}}]`

// TestVideoBlockLikeSlack: Slack's VideoBlock (u8h8) draws the provider
// row (its icon, name and the author), the description, the title linked
// to title_url with the trigger's caret after it, then the 360x283 player
// (450x354 in a modal or App Home), letterboxed, with its "Video" pill.
// The caret collapses the player under block_video_<ts>_<block_id>, and a
// new View of the message reads it collapsed (the store); a video block
// without a block_id keys "block_video_<ts>_" (processProps' ?? "").
func TestVideoBlockLikeSlack(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, videoBlk)
	o := Options{Channel: "C1", TS: "1700000000.000100", ViewContext: expandable.ViewChannel}
	key := expandable.VideoBlockKey(o.TS, "vid")
	if key != "block_video_1700000000.000100_vid" {
		t.Fatalf("key %q", key)
	}
	t.Cleanup(func() { forgetExpandable(key) })
	v := layoutAt(st, d, o, 700)
	thumbs := imgsWithURL(&v.d, "https://v.test/t.jpg")
	if len(thumbs) != 1 || thumbs[0].r.Dx() != 360 || thumbs[0].r.Dy() != 283 || !thumbs[0].fit {
		t.Fatalf("the player %+v", thumbs)
	}
	if icons := imgsWithURL(&v.d, "https://v.test/i.png"); len(icons) != 1 || icons[0].r.Dx() != 16 {
		t.Errorf("the provider's icon %+v", icons)
	}
	plain := string(v.d.plain)
	order := []string{"VidSite", "Someone", "A short description", "Demo", "Video"}
	at := -1
	for _, s := range order {
		i := strings.Index(plain, s)
		if i < at {
			t.Fatalf("plain %q: %q out of order (provider, author, description, title, the pill)", plain, s)
		}
		at = i
	}
	title := fragWith(t, &v.d, "Demo")
	if tg := v.d.targets[title.target]; tg.kind != tLink || tg.url != "https://v.test/w" {
		t.Errorf("the title's target %+v", tg)
	}
	if title.y > thumbs[0].r.Min.Y {
		t.Errorf("the title (baseline %d) isn't above the player (%v)", title.y, thumbs[0].r)
	}

	// the caret: collapsed, here and in a new View of the message
	tg := expTarget(v, key)
	if tg < 0 || v.d.targets[tg].on {
		t.Fatalf("no collapse caret for %s", key)
	}
	v.activate(tg, image.Point{}, nil)
	v.relayout(st, 700, 1, 1)
	if len(imgsWithURL(&v.d, "https://v.test/t.jpg")) != 0 || len(boxesOf(&v.d, bCaretRight)) != 1 {
		t.Error("the caret didn't collapse the player")
	}
	if w := layoutAt(st, d, o, 700); len(imgsWithURL(&w.d, "https://v.test/t.jpg")) != 0 {
		t.Error("a new View of the message drew the collapsed player")
	}
	forgetExpandable(key)

	// a modal's (FillBlocks): 450x354
	if w := layoutAt(st, d, Options{FillBlocks: true}, 700); len(imgsWithURL(&w.d, "https://v.test/t.jpg")) != 1 ||
		imgsWithURL(&w.d, "https://v.test/t.jpg")[0].r.Dx() != 450 || imgsWithURL(&w.d, "https://v.test/t.jpg")[0].r.Dy() != 354 {
		t.Errorf("a modal's player %+v", imgsWithURL(&w.d, "https://v.test/t.jpg"))
	}
	// no block_id
	nd := kitDoc(t, strings.Replace(videoBlk, `"block_id":"vid",`, "", 1))
	nk := expandable.VideoBlockKey("1700000000.000200", "")
	t.Cleanup(func() { forgetExpandable(nk) })
	if w := layoutAt(st, nd, Options{Channel: "C1", TS: "1700000000.000200"}, 700); expTarget(w, "block_video_1700000000.000200_") < 0 {
		t.Error("a video block without a block_id: no block_video_<ts>_ caret")
	}
}
