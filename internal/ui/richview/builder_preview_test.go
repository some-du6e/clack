package richview

// Block Kit Builder's template posted as a person (Karim, #clack,
// 2026-11-21: "doesn't work"). The message itself was never the fault:
// Clack draws a person's Block Kit blocks as it draws an app's (richtext
// marks every layout block Kit whoever sent it), and the photos, on a
// third-party https host, load the way Slack's client loads them,
// through its image proxy (getImgProxyUrl, client-boot d0up). What broke
// was the buttons (blockkit's TestBuilderPreviewButtons). This pins the
// first two: every block draws as Block Kit, and every photo loads.
// Synthetic fixtures (the template is Slack's public example).

import (
	"context"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// TestBuilderPreviewDraws: the gallery's builder/preview (the viewer's own
// message: user, text, ts, type and blocks only) is the template's seven
// blocks, all Block Kit: the intro, a divider, three restaurants with
// their photo on the right, a divider and the three buttons. Its photos
// load with no session credentials through slack-imgs.com (the photos'
// own host answers 404), each an 88×88 cover box at the right of the
// 600px a section with an accessory takes, radius 8.
func TestBuilderPreviewDraws(t *testing.T) {
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
	ts := fs.GalleryTS(fakeslack.GalleryBuilderPreview)
	if ts == "" {
		t.Fatalf("no gallery message %s", fakeslack.GalleryBuilderPreview)
	}
	pg, err := c.History(ctx, fakeslack.GalleryChannelID, slack.HistoryOpts{Oldest: ts, Inclusive: true, Limit: 1})
	if err != nil || len(pg.Messages) != 1 || pg.Messages[0].TS != ts {
		t.Fatalf("conversations.history from %s: %v (%+v)", ts, err, pg)
	}
	m := pg.Messages[0]
	if m.User != fakeslack.SelfID || m.BotID != "" || m.BotProfile != nil || m.Subtype != "" || m.ClientMsgID != "" ||
		len(m.Attachments) != 0 || len(m.Files) != 0 {
		t.Fatalf("builder/preview isn't a person's plain message: user %q bot %q profile %+v subtype %q client_msg_id %q",
			m.User, m.BotID, m.BotProfile, m.Subtype, m.ClientMsgID)
	}

	d := rt.FromMessage(codeRes{}, &m)
	want := []rt.BlockKind{rt.Section, rt.Divider, rt.Section, rt.Section, rt.Section, rt.Divider, rt.Actions}
	if len(d.Blocks) != len(want) {
		t.Fatalf("%d blocks, want the template's %d: %+v", len(d.Blocks), len(want), d.Blocks)
	}
	photos := fakeslack.GalleryBuilderPhotos
	names := []string{"Farmhouse Thai Cuisine", "Kin Khao", "Ler Ros"}
	for i, b := range d.Blocks {
		if b.Kind != want[i] || !b.Kit {
			t.Errorf("block %d: kind %v kit %v, want %v as Block Kit", i, b.Kind, b.Kit, want[i])
		}
		if i >= 2 && i <= 4 {
			text := rt.PlainText(rt.Doc{Blocks: []rt.Block{b}})
			if !strings.HasPrefix(text, names[i-2]+"\n") || !strings.Contains(text, "reviews") {
				t.Errorf("section %d text %q, want %s's", i, text, names[i-2])
			}
			if b.Image == nil || b.Image.URL != photos[i-2] || b.Image.Alt != "alt text for image" {
				t.Errorf("section %d accessory %+v, want %s", i, b.Image, photos[i-2])
			}
		}
	}
	if s := rt.PlainText(rt.Doc{Blocks: d.Blocks[4:5]}); !strings.Contains(s, "chili & rice powder") {
		t.Errorf("Ler Ros: %q, want &amp; read as &", s)
	}
	if bs := d.Blocks[6].Buttons; len(bs) != 3 || bs[0].Text != "Farmhouse" || bs[0].URL != "" ||
		bs[1].Text != "Kin Khao" || bs[1].URL != "https://google.com" || bs[2].Text != "Ler Ros" || bs[2].URL != "https://google.com" {
		t.Errorf("buttons %+v", bs)
	}

	// cmd/clack's fetcher: the session's credentials only for Slack's
	// hosts, imgcache.PublicFetch (Slack's image proxy) for the rest
	imgs := imgcache.New(func(ctx context.Context, raw string) ([]byte, error) {
		if c.CredentialsOK(raw) {
			t.Errorf("the session's credentials asked for %s", raw)
		}
		return imgcache.PublicFetch(ctx, raw)
	}, "", nil)
	st := newTestStyle()
	st.Images = imgs
	v := New(d, Options{})
	draw := func() {
		var ops op.Ops
		v.Layout(testGtx(&ops, 800), st, &recHandler{})
	}
	draw()
	for i, u := range photos {
		ims := imgsWithURL(&v.d, u)
		if len(ims) != 1 {
			t.Fatalf("photo %d drawn %d times (images %+v)", i, len(ims), v.d.imgs)
		}
		im := ims[0]
		if im.r.Size() != image.Pt(88, 88) || im.r.Max.X != 600 || im.fit || im.radii != [4]int{8, 8, 8, 8} {
			t.Errorf("photo %d: %v fit %v radii %v, want 88×88 cover at x 512-600, radius 8", i, im.r, im.fit, im.radii)
		}
	}
	dl := time.Now().Add(20 * time.Second)
	for {
		loaded := 0
		for _, u := range photos {
			if ims := imgsWithURL(&v.d, u); len(ims) == 1 {
				if _, ok := imgs.Get(u, ims[0].px); ok {
					loaded++
				}
			}
		}
		if loaded == len(photos) {
			break
		}
		if time.Now().After(dl) {
			t.Fatalf("%d of %d photos loaded through the image proxy", loaded, len(photos))
		}
		time.Sleep(20 * time.Millisecond)
		draw()
	}
}
