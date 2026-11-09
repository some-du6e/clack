package composer

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// TestAutocompleteShots draws the four lists ranked by Slack's rules over
// the fake API's synthetic workspace (autocomplete_slack_test.go), light
// and dark at 2x: "@al", "#gen", ":smi" and "/rem". It saves
// autocomplete-<list>-<mode>.png to AUTOCOMPLETE_SHOTS_DIR (the
// autocomplete workflow) and is skipped without it.
func TestAutocompleteShots(t *testing.T) {
	dir := os.Getenv("AUTOCOMPLETE_SHOTS_DIR")
	if dir == "" {
		t.Skip("AUTOCOMPLETE_SHOTS_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	imgs := &shotImages{}
	lists := []struct {
		name, text string
		ready      func([]string) bool
	}{
		{"members", "@al", func(n []string) bool { return hasName(n, "alina") }},
		{"channels", "#gen", func(n []string) bool { return hasName(n, "genetics") }},
		{"emoji", ":smi", func(n []string) bool { return len(n) > 3 }},
		{"commands", "/rem", func(n []string) bool { return len(n) > 0 }},
	}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		for _, l := range lists {
			t.Run(l.name+"-"+mode, func(t *testing.T) {
				app, _ := acSlackApp(t)
				th := theme.Bare(dark)
				th.SetShaper(theme.NewShaper())
				th.Images = imgs
				ac := newAutocomplete(&shotHost{slickHost: slickHost{app: app, th: th}, imgs: shotCache()}, tChGen)
				t.Cleanup(ac.Close)
				ac.commands = []core.Command{{Name: "/remind", Desc: "Set a reminder"}, {Name: "/rename", Desc: "Rename a channel"},
					{Name: "/feed", Desc: "Manage RSS feeds"}, {Name: "/away", Desc: "Toggle your away status"}}
				names := acSettle(t, ac, l.text, l.ready)
				t.Logf("%s: %v", l.text, names)

				const scale = 2
				w, h := 520*scale, 420*scale
				var ops op.Ops
				paint.FillShape(&ops, th.C.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
				gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
					Constraints: layout.Constraints{Max: image.Pt(w-32*scale, h-32*scale)}}
				if l.name == "emoji" {
					warmImages(ac, gtx)
				}
				st := op.Offset(image.Pt(16*scale, 16*scale)).Push(gtx.Ops)
				ac.Layout(gtx, 0)
				st.Pop()
				img := pixeltest.Render(t, w, h, &ops)
				path := filepath.Join(dir, fmt.Sprintf("autocomplete-%s-%s.png", l.name, mode))
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := png.Encode(f, img); err != nil {
					t.Fatal(err)
				}
				t.Logf("wrote %s", path)
			})
		}
	}
}

// shotHost serves images from Slack's public emoji CDN (static files, no
// cookie) so the emoji rows draw their pictures.
type shotHost struct {
	slickHost
	imgs *imgcache.Cache
}

func (h *shotHost) Images() *imgcache.Cache { return h.imgs }

func shotCache() *imgcache.Cache {
	cl := &http.Client{Timeout: 5 * time.Second}
	return imgcache.New(func(ctx context.Context, url string) ([]byte, error) {
		if !strings.HasPrefix(url, "https://a.slack-edge.com/") {
			return nil, fmt.Errorf("not a public CDN url: %s", url)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := cl.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	}, "", nil)
}

// warmImages lays the popup out once so it asks for its images at their
// real sizes, then gives the fetches time to land.
func warmImages(ac *Autocomplete, gtx layout.Context) {
	var scratch op.Ops
	g := gtx
	g.Ops = &scratch
	ac.Layout(g, 0)
	time.Sleep(3 * time.Second)
	scratch.Reset()
	ac.Layout(g, 0) // decoded at size now
	time.Sleep(500 * time.Millisecond)
}
