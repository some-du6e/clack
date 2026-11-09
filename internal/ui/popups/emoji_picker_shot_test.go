package popups

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/fakeslack"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// epShotEnv is a picker host over a core booted on fakeslack. With
// CLACK_EMOJI_CDN set the image cache fetches the emoji from Slack's
// public static CDN (a.slack-edge.com, no cookie); otherwise no network.
func epShotEnv(t *testing.T, dark bool) *tzHost {
	t.Helper()
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	a := core.New(slack.New(fs.Creds()), store.New(t.TempDir()))
	t.Cleanup(a.Shutdown)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.Boot(ctx); err != nil {
		t.Fatal(err)
	}
	fetch := func(context.Context, string) ([]byte, error) { return nil, errors.New("no network in tests") }
	if os.Getenv("CLACK_EMOJI_CDN") != "" {
		fetch = func(ctx context.Context, u string) ([]byte, error) {
			if !strings.HasPrefix(u, "https://a.slack-edge.com/production-standard-emoji-assets/") {
				return nil, errors.New("not a standard emoji image")
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if err != nil {
				return nil, err
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return nil, err
			}
			defer res.Body.Close()
			if res.StatusCode != 200 {
				return nil, fmt.Errorf("%s: %s", u, res.Status)
			}
			return io.ReadAll(res.Body)
		}
	}
	th := theme.New(dark)
	sh := theme.NewShaper()
	th.SetShapers(sh, sh)
	return &tzHost{a: a, th: th, im: imgcache.New(fetch, "", nil)}
}

// The picker's Frequently Used is core's getFrequentlyUsedEmoji over
// fakeslack's frecency and emoji_use prefs, and a pick records the search
// text into the frecency pref.
func TestEmojiPickerFrequentlyUsed(t *testing.T) {
	h := epShotEnv(t, false)
	p := NewEmojiPicker(h, nil)
	var got []string
	for _, id := range p.lists[epGroupFrequent] {
		got = append(got, p.idx.names[id])
	}
	if want := []string{"+1", "tada", "fire", "smile", "eyes"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Frequently Used = %v, want %v", got, want)
	}
	p.query = "roc"
	id, _ := p.idx.lookup("rocket")
	p.lists[epGroupSearch] = []int32{id}
	p.searching = true
	p.rows = []epRow{{list: epGroupSearch}, {list: epGroupSearch, n: 1}}
	p.pickCell(epCell{1, 0}, false)
	c := h.a.FrecencyCache()
	if r := c.Item("E1f680"); r == nil || r.Count != 1 {
		t.Fatalf("pick not recorded: %+v", r)
	}
	if hits := c.Hits("roc", time.Now()); hits["E1f680"] == nil || !hits["E1f680"].Exact {
		t.Errorf("search text not recorded: %+v", hits)
	}
}

// TestEmojiPickerShots draws the whole picker, light and dark, idle and
// with the pointer on grinning (its tint and the footer preview), through
// the GPU renderer at 2x, into EMOJI_SHOTS_DIR. Needs a GPU (CI:
// CLACK_PIXEL_TESTS=1 under xvfb) and, for the emoji images,
// CLACK_EMOJI_CDN=1.
func TestEmojiPickerShots(t *testing.T) {
	dir := os.Getenv("EMOJI_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		for _, hover := range []bool{false, true} {
			name := fmt.Sprintf("emoji-picker-%s-%s", map[bool]string{false: "light", true: "dark"}[dark], map[bool]string{false: "idle", true: "hover"}[hover])
			t.Run(name, func(t *testing.T) {
				h := epShotEnv(t, dark)
				img := epPickerShot(t, h, hover)
				epSaveShot(t, dir, name, img)
			})
		}
	}
}

func epPickerShot(t *testing.T, h *tzHost, hover bool) *image.RGBA {
	t.Helper()
	const scale = 2
	win := image.Pt(420*scale, 560*scale)
	p := NewEmojiPicker(h, nil)
	p.SetAnchorRect(image.Rect(20*scale, 548*scale, 60*scale, 556*scale))
	var ops op.Ops
	frame := func() {
		ops.Reset()
		paint.FillShape(&ops, colorsOf(h.th).EmojiFoot, clip.Rect{Max: win}.Op())
		gtx := layout.Context{Ops: &ops, Now: time.Now(), Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Constraints: layout.Exact(win)}
		p.Layout(gtx)
	}
	frame()
	if hover {
		// grinning: the first cell of Smileys & People
		for i, r := range p.rows {
			if r.list == epGroupSmileys && r.n > 0 {
				p.ptr = image.Pt((epCellX+epCellW/2)*scale, (int(r.y)+(epCellH/2)*scale)-p.scrollTop())
				p.ptrIn = true
				_ = i
				break
			}
		}
	}
	// let the images land (the cache loads off the UI goroutine)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		frame()
		Pump()
		if os.Getenv("CLACK_EMOJI_CDN") == "" || epImagesReady(h, p, scale) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	frame()
	if hover {
		if id, ok := p.cellID(p.hover); !ok || p.idx.names[id] != "grinning" {
			t.Errorf("hover = %+v (%v), want grinning", p.hover, ok)
		}
	}
	img := pixeltest.Render(t, win.X, win.Y, &ops)
	r := p.rect.Inset(-12 * scale).Intersect(image.Rectangle{Max: win})
	out := image.NewRGBA(image.Rectangle{Max: r.Size()})
	draw.Draw(out, out.Bounds(), img, r.Min, draw.Src)
	return out
}

// epImagesReady reports whether the visible rows' images are decoded.
func epImagesReady(h *tzHost, p *EmojiPicker, scale int) bool {
	px := epImgSz * scale
	for _, r := range p.rows {
		if int(r.y) > 400*scale {
			break
		}
		if r.n == 0 {
			continue
		}
		for _, id := range p.rowIDs(r) {
			if _, ok := h.im.Get(p.idx.url(id, p.tone), px); !ok && !h.im.Failed(p.idx.url(id, p.tone), px) {
				return false
			}
		}
	}
	return true
}
