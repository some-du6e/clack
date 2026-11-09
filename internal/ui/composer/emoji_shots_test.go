package composer

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme/pixeltest"
)

// shotImages serves emoji images for the screenshots: the real PNG from
// Slack's public emoji CDN (static files, no cookie) when it can be
// fetched, else a yellow disc.
type shotImages struct {
	mu  sync.Mutex
	got map[string]paint.ImageOp
}

func (s *shotImages) Get(url string, px int) (paint.ImageOp, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.got == nil {
		s.got = map[string]paint.ImageOp{}
	}
	if op, ok := s.got[url]; ok {
		return op, true
	}
	var img image.Image
	cl := http.Client{Timeout: 5 * time.Second}
	if resp, err := cl.Get(url); err == nil {
		if resp.StatusCode == 200 {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			img, _ = png.Decode(bytesReader(b))
		}
		resp.Body.Close()
	}
	if img == nil {
		const n = 32
		m := image.NewRGBA(image.Rect(0, 0, n, n))
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				if dx, dy := x-n/2, y-n/2; dx*dx+dy*dy < n*n/4 {
					m.Set(x, y, color.NRGBA{R: 0xf5, G: 0xc0, B: 0x2a, A: 0xff})
				}
			}
		}
		img = m
	}
	op := paint.NewImageOp(img)
	s.got[url] = op
	return op, true
}

type byteReader struct {
	b []byte
	i int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func bytesReader(b []byte) io.Reader { return &byteReader{b: b} }

// TestComposerEmojiShots draws, light and dark at 2x: the message
// composer with typed, picked and pasted emoji (atoms) plus one in inline
// code (raw, slot + image); and a theme.Editor field (search box,
// plain_text_input) with emoji, tones and a ZWJ sequence. It saves
// composer-emoji-<mode>.png to EMOJI_SHOTS_DIR (the composer-emoji
// workflow) and is skipped without it.
func TestComposerEmojiShots(t *testing.T) {
	dir := os.Getenv("EMOJI_SHOTS_DIR")
	if dir == "" {
		t.Skip("EMOJI_SHOTS_DIR not set")
	}
	imgs := &shotImages{}
	for _, dark := range []bool{false, true} {
		mode := "light"
		if dark {
			mode = "dark"
		}
		t.Run(mode, func(t *testing.T) {
			const scale = 2
			w, h := 640*scale, 150*scale
			th := theme.Bare(dark)
			th.SetShaper(theme.NewShaper())
			th.Images = imgs
			p := &th.C
			var ops op.Ops
			paint.FillShape(&ops, p.Bg, clip.Rect{Max: image.Pt(w, h)}.Op())
			gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale}, Constraints: layout.Exact(image.Pt(w, h))}
			at := func(x, y, cw, ch int, f func(gtx layout.Context)) {
				g := gtx
				g.Constraints = layout.Exact(image.Pt(cw*scale, ch*scale))
				st := op.Offset(image.Pt(x*scale, y*scale)).Push(gtx.Ops)
				f(g)
				st.Pop()
			}
			label := func(y int, s string) {
				at(16, y, 600, 18, func(g layout.Context) { th.Label(g, 12, theme.Regular, p.FgMuted, s) })
			}

			// 1. the message composer
			label(8, "Composer: typed 😀 👍🏽 👨‍👩‍👧‍👦 🇺🇸 ❤️, :tada: by hand, `🚀` in inline code")
			e := emojiEditor(nil)
			edType(e, "Ship it ")
			edInput(e, "😀 👍🏽 👨‍👩‍👧‍👦 🇺🇸 ❤️")
			edType(e, " :tada: then ")
			e.ToggleStyle(Code)
			edInput(e, "🚀 code")
			e.ToggleStyle(Code)
			edType(e, " done")
			st := DefaultEditorStyle(th.Shaper)
			st.Color, st.CodeColor, st.CodeBg, st.CodeBorder = p.Fg, p.CodeFg, p.CodeBg, p.CodeBorder
			st.Images = imgs.Get
			at(16, 30, 608, 40, func(g layout.Context) { e.Layout(g, st) })

			// 2. a theme.Editor field, new and old
			label(84, "Field (theme.Editor: search box, plain_text_input): typed emoji as Slack's images")
			txt := "find 🎉 party 👋🏾 in #general 🧑🏽‍💻"
			var ed widget.Editor
			ed.SingleLine = true
			ed.SetText(txt)
			at(16, 106, 608, 30, func(g layout.Context) {
				th.Editor(g, &ed, 15, p.Fg, theme.WithAlpha(p.Accent, 0x55))
			})
			img := pixeltest.Render(t, w, h, &ops)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, fmt.Sprintf("composer-emoji-%s.png", mode))
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
