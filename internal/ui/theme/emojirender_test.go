package theme

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/emoji"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Standard emoji against Chrome (branch emoji-sharp). Karim, Windows 11 at
// 125%: three :grin: in a message were blurry. Clack drew Slack's 22px
// "medium" image stretched by the GPU's linear filter (no mipmaps) to the
// emoji's device pixels: 27.5 for a 22px emoji at 125%, 40 for a 32px
// jumbomoji. Slack's client loads <size>@2x whenever devicePixelRatio > 1
// (44px for medium) and Chromium scales it down.
//
// TestEmojiRenderShots draws :grin: (and a few others) at 22px (message
// text) and 32px (jumbomoji), three in a row like Karim's message, in both
// sets and both modes, at 1, 1.25, 1.5 and 2x, into $CLACK_EMOJI_RENDER_DIR:
//
//	cases.json              the cells (CSS px) and what Slack loads for each
//	page-<s>x.html          the same cells for Chrome, each an <img> of the
//	                        URL Slack's client builds at that scale
//	                        (constructStandardEmojiUrl), saved next to it
//	gio-clack-<s>x.png      Clack: the image cache's covering size, scaled
//	                        down on the CPU, drawn 1:1
//	gio-old-<s>x.png        before: the 22px medium image stretched by the GPU
//
// .github/scripts/emoji_render_compare.py measures edge sharpness against
// Chrome's screenshots and writes 400% side-by-sides. The images come from
// Slack's public CDN (a.slack-edge.com, no cookie) with CLACK_EMOJI_CDN=1.

type erCell struct {
	Mode  string `json:"mode"` // light, dark
	Style string `json:"style"`
	Name  string `json:"name"`
	Stem  string `json:"stem"`
	Size  int    `json:"size"` // CSS px: 22 (c-emoji__medium) or 32 (c-emoji__large, jumbomoji)
	X     int    `json:"x"`    // CSS px, multiples of 4 so every scale lands on whole device pixels
	Y     int    `json:"y"`
	Class string `json:"class"`
}

var (
	erScales = []float32{1, 1.25, 1.5, 2}
	erEmoji  = []struct{ name, stem string }{{"grin", "1f601"}, {"tada", "1f389"}, {"+1", "1f44d"}, {"palm_tree", "1f334"}}
	erBG     = map[string][3]uint8{"light": {255, 255, 255}, "dark": {26, 29, 33}} // Slack's message pane
)

const erW, erRow = 640, 48 // page width, row height (CSS px)

func erCells() []erCell {
	var cs []erCell
	y := 8
	for _, mode := range []string{"light", "dark"} {
		for _, style := range []string{"google", "apple"} {
			for _, e := range erEmoji {
				x := 8
				for _, size := range []int{22, 32} {
					class := "medium"
					if size == 32 {
						class = "large"
					}
					for range 3 { // Karim's message: three in a row
						cs = append(cs, erCell{Mode: mode, Style: style, Name: e.name, Stem: e.stem, Size: size, X: x, Y: y, Class: class})
						x += size + 4
					}
					x += 24
				}
				y += erRow
			}
		}
	}
	return cs
}

// erSlackURL is what Slack's client loads for c at device scale s: its
// class's size, @2x when devicePixelRatio > 1.
func erSlackURL(c erCell, s float32) string {
	x2 := ""
	if s > 1 {
		x2 = "@2x"
	}
	return emoji.CDN + c.Style + "-" + c.Class + "/" + c.Stem + x2 + ".png"
}

// erFetcher fetches standard emoji from Slack's public CDN, once per URL.
type erFetcher struct {
	mu sync.Mutex
	b  map[string][]byte
}

func (f *erFetcher) fetch(ctx context.Context, u string) ([]byte, error) {
	if !strings.HasPrefix(u, emoji.CDN) {
		return nil, errors.New("not a standard emoji image")
	}
	f.mu.Lock()
	b, ok := f.b[u]
	f.mu.Unlock()
	if ok {
		return b, nil
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
	b, err = io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.b[u] = b
	f.mu.Unlock()
	return b, nil
}

func erFile(u string) string {
	return strings.ReplaceAll(strings.TrimPrefix(u, emoji.CDN), "/", "_")
}

func TestEmojiRenderShots(t *testing.T) {
	dir := os.Getenv("CLACK_EMOJI_RENDER_DIR")
	if dir == "" {
		t.Skip("CLACK_EMOJI_RENDER_DIR not set (emoji-picker's emoji-sharp jobs set it)")
	}
	if os.Getenv("CLACK_EMOJI_CDN") == "" {
		t.Skip("CLACK_EMOJI_CDN not set: the images come from Slack's public CDN")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cs := erCells()
	f := &erFetcher{b: map[string][]byte{}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	slack := map[string][]string{}
	for _, s := range erScales {
		for _, c := range cs {
			u := erSlackURL(c, s)
			b, err := f.fetch(ctx, u)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, erFile(u)), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "page-"+trTag(s)+".html"), []byte(erPage(cs, s)), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, size := range []int{22, 32} {
			class := map[int]string{22: "medium", 32: "large"}[size]
			c := erCell{Style: "google", Stem: "1f601", Class: class}
			px := int(math.Round(float64(float32(size) * s)))
			slack[trTag(s)] = append(slack[trTag(s)], fmt.Sprintf("%dpx: Slack %s, Clack %s", size,
				strings.TrimPrefix(erSlackURL(c, s), emoji.CDN), strings.TrimPrefix(emoji.ForPixels(emoji.URL(c.Stem, "google"), px), emoji.CDN)))
		}
	}
	trWriteJSON(t, filepath.Join(dir, "cases.json"), map[string]any{
		"width": erW, "height": erHeight(cs), "scales": erScales, "cells": cs, "bg": erBG, "urls": slack, "goos": runtime.GOOS,
	})
	for _, s := range erScales {
		for _, v := range []string{"clack", "old"} {
			img := erRender(t, f, cs, s, v)
			out, err := os.Create(filepath.Join(dir, "gio-"+v+"-"+trTag(s)+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(out, img); err != nil {
				t.Fatal(err)
			}
			out.Close()
		}
	}
}

func erHeight(cs []erCell) int { return cs[len(cs)-1].Y + erRow }

// erPage is cs as Chrome draws them at scale s: Slack's c-emoji <img> of
// the URL Slack builds, sized by its class.
func erPage(cs []erCell, s float32) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!doctype html><html><head><meta charset=utf-8><style>\n")
	fmt.Fprintf(&b, "html,body{margin:0;padding:0;width:%dpx;height:%dpx;overflow:hidden;background:#fff}\n", erW, erHeight(cs))
	fmt.Fprintf(&b, "div,img{position:absolute;margin:0;padding:0;border:0}\n</style></head><body>\n")
	y := -1
	for _, c := range cs {
		if c.Y != y {
			y = c.Y
			bg := erBG[c.Mode]
			fmt.Fprintf(&b, "<div style=\"left:0;top:%dpx;width:%dpx;height:%dpx;background:rgb(%d,%d,%d)\"></div>\n", c.Y-8, erW, erRow, bg[0], bg[1], bg[2])
		}
		fmt.Fprintf(&b, "<img src=\"%s\" style=\"left:%dpx;top:%dpx;width:%dpx;height:%dpx\">\n", erFile(erSlackURL(c, s)), c.X, c.Y, c.Size, c.Size)
	}
	b.WriteString("</body></html>\n")
	return b.String()
}

// erRender draws cs through Gio at scale s: "clack" the way the theme's
// emoji paths do now (Images.Get at the device size, Image 1:1), "old" the
// 22px medium image stretched by Image, as before.
func erRender(t *testing.T, f *erFetcher, cs []erCell, s float32, variant string) *image.RGBA {
	t.Helper()
	cache := imgcache.New(f.fetch, "", func() {})
	w, h := trPx(erW, s), trPx(erHeight(cs), s)
	var ops op.Ops
	draw := func() bool {
		ops.Reset()
		ready := true
		gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: s, PxPerSp: s}, Constraints: layout.Exact(image.Pt(w, h))}
		y := -1
		for _, c := range cs {
			if c.Y != y {
				y = c.Y
				bg := erBG[c.Mode]
				paint.FillShape(&ops, trNRGBA([4]uint8{bg[0], bg[1], bg[2], 0xff}), clip.Rect(image.Rect(0, trPx(c.Y-8, s), w, trPx(c.Y-8+erRow, s))).Op())
			}
			es := gtx.Dp(unit.Dp(c.Size))
			var img paint.ImageOp
			ok := false
			switch variant {
			case "clack": // the app's URL is medium for jumbomoji too (store.Emoji)
				img, ok = cache.Get(emoji.URL(c.Stem, c.Style), es)
			case "old":
				img, ok = erOld(t, f, emoji.URL(c.Stem, c.Style))
			}
			if !ok {
				ready = false
				continue
			}
			st := op.Offset(image.Pt(trPx(c.X, s), trPx(c.Y, s))).Push(&ops)
			Image(gtx, img, image.Pt(es, es), 0)
			st.Pop()
		}
		return ready
	}
	deadline := time.Now().Add(30 * time.Second)
	for !draw() {
		if time.Now().After(deadline) {
			t.Fatalf("%s %gx: images not loaded", variant, s)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return pixeltest.Render(t, w, h, &ops)
}

// erOld is what Clack drew before: the medium (22px) image as decoded,
// for Image to stretch to the emoji's size.
func erOld(t *testing.T, f *erFetcher, u string) (paint.ImageOp, bool) {
	b, err := f.fetch(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return paint.NewImageOp(imgcache.Scale(img, 22*4)), true
}
