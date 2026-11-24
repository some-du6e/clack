//go:build linux && cgo && !nox11

package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"slices"
	"testing"

	"gioui.org/app"
)

// testIconPNG is a size × size PNG of one colour.
func testIconPNG(t *testing.T, size int, c color.NRGBA) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := 0; i < len(m.Pix); i += 4 {
		m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// iconSides is each icon's width in a _NET_WM_ICON list, as listed.
func iconSides(t *testing.T, words []uint64) []int {
	t.Helper()
	var out []int
	for i := 0; i+1 < len(words); {
		w, h := int(words[i]), int(words[i+1])
		if w <= 0 || h <= 0 || i+2+w*h > len(words) {
			t.Fatalf("a malformed _NET_WM_ICON list at %d: %d × %d in %d words", i, w, h, len(words))
		}
		out = append(out, w)
		i += 2 + w*h
	}
	return out
}

// TestSetAppIconX11: a plugin's app icon on X11 (Slick's window setIcon:
// CustomAppIcon) is set at the window's next event, on the goroutine that
// reads them, never from the caller's, and within the server's request
// size: an icon of 4096 px (nativeImage's limits allow 8192) goes as the
// set from 256 px down to 16, and the property fits one request. nil puts
// Clack's own back; once the window is going, nothing is set on it.
func TestSetAppIconX11(t *testing.T) {
	dpy, win, done, ok := x11TestWindow()
	if !ok {
		t.Skip("no X server (DISPLAY): ci's linux-gio job runs this under Xvfb")
	}
	defer done()
	defaults := [][]byte{testIconPNG(t, 32, color.NRGBA{B: 0xff, A: 0xff}), testIconPNG(t, 64, color.NRGBA{B: 0xff, A: 0xff}),
		testIconPNG(t, 512, color.NRGBA{B: 0xff, A: 0xff})}
	ev := app.X11ViewEvent{Display: dpy, Window: win}
	SetWindowIcon(ev, defaults...)
	own := x11IconWords(dpy, win)
	if got := iconSides(t, own); !slices.Equal(got, []int{32, 64, 512}) {
		t.Fatalf("Clack's own icons: %v, want 32, 64 and 512 px", got)
	}

	SetAppIcon(testIconPNG(t, 4096, color.NRGBA{R: 0xff, A: 0xff}), defaults...)
	if !slices.Equal(x11IconWords(dpy, win), own) {
		t.Fatal("SetAppIcon set the icon from its caller's goroutine, before the window's next event")
	}
	SetWindowIcon(app.FrameEvent{}, defaults...)
	words := x11IconWords(dpy, win)
	if got := iconSides(t, words); !slices.Equal(got, []int{16, 32, 48, 64, 128, 256}) {
		t.Errorf("a 4096 px icon went up as %v px, want 16 to 256", got)
	}
	if max := x11MaxRequest(dpy); len(words)+7 > max {
		t.Errorf("the icon is %d words, past the server's %d-word requests", len(words), max)
	}
	if len(words) > 2 && words[len(words)-1]>>16&0xff != 0xff {
		t.Errorf("the 256 px icon isn't the plugin's red: %#x", words[len(words)-1])
	}

	SetAppIcon(nil, defaults...)
	SetWindowIcon(app.FrameEvent{}, defaults...)
	if !slices.Equal(x11IconWords(dpy, win), own) {
		t.Error("nil didn't put Clack's own icons back")
	}

	// the window going (Gio's shutdown): an icon set after it waits
	SetWindowIcon(app.X11ViewEvent{}, defaults...)
	SetAppIcon(testIconPNG(t, 64, color.NRGBA{G: 0xff, A: 0xff}), defaults...)
	SetWindowIcon(app.FrameEvent{}, defaults...)
	if !slices.Equal(x11IconWords(dpy, win), own) {
		t.Error("an icon was set on a window that was going")
	}
	SetAppIcon(nil, defaults...)
}
