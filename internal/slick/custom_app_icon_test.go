package slick

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCustomAppIconLikeSlack: Slick's CustomAppIcon, from Slick's build with
// its main half, changes Clack's own app icon as it changes Slick's (here
// on Linux: the window's icon, Electron's BrowserWindow.setIcon).
//
//   - Enabled with its defaults (Slick's design, the system appearance) it
//     puts the app's own icon up, which Clack's build makes Clack's
//     (clack-512.png, fitted to 256 px as Slick fits a window icon).
//   - Its Icon setting is one its panel edits (hidden): Preferences draws
//     no row for it, and the Appearance row stays; it has a panel.
//   - The panel's previews come from the main half (rpc previews): each
//     built-in design at 128 px at most; Slack's own only where Slack is
//     installed.
//   - Choosing the Pride design puts that up (its light version: Linux has
//     no dark app icons).
//   - An uploaded image (dialog.openFile, the picked PNG decoded through
//     nativeImage) is kept in its storage and goes up as the custom icon;
//     removing it puts the app's own back.
//   - Stopped, the app's own icon is back.
func TestCustomAppIconLikeSlack(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS: the plugin's own icon is the Dock's (app.dock.setIcon(null)), which this test's icons don't follow")
	}
	dir := pluginsDir(t)
	h := newHarness(t)
	clack, err := os.ReadFile(filepath.Join("..", "..", "assets", "icon", "clack-512.png"))
	if err != nil {
		t.Fatal(err)
	}
	own := fitted(t, clack, 256)
	// the icon up last: each step below changes which, so waiting for the
	// next one is waiting for the last to be it
	icon := func(what string, want func(m *image.NRGBA) bool) *image.NRGBA {
		t.Helper()
		var got *image.NRGBA
		eventually(t, 10*time.Second, what, func() bool {
			ic := h.appIcons()
			if len(ic) == 0 || ic[len(ic)-1] == nil {
				return false
			}
			m, err := png.Decode(bytes.NewReader(ic[len(ic)-1]))
			if err != nil {
				return false
			}
			got = toNRGBA(m)
			return want(got)
		})
		return got
	}

	h.importFile(filepath.Join(dir, "CustomAppIcon.js"))
	h.importFile(filepath.Join(dir, "CustomAppIcon.main.js"))
	if err := h.s.SetEnabled(h.ctx(), "CustomAppIcon", true); err != nil {
		t.Fatal(err)
	}
	if info := h.plugin("CustomAppIcon"); !info.Running || info.StartError != "" || info.MainError != "" {
		t.Fatalf("CustomAppIcon: %+v", info)
	}
	icon("Clack's own icon up first", func(m *image.NRGBA) bool { return samePixels(m, own) })

	// Preferences: no Icon row, its Appearance row, its panel
	info := h.plugin("CustomAppIcon")
	rows := map[string]bool{}
	for _, o := range info.Options {
		rows[o.Key] = !o.Hidden
	}
	if v, ok := rows["icon"]; !ok || v {
		t.Errorf("its Icon setting: present %v, drawn %v; want present and hidden (its panel edits it)", ok, v)
	}
	if !rows["appearance"] {
		t.Error("its Appearance setting isn't drawn")
	}
	eventually(t, 5*time.Second, "its panel", func() bool { return h.plugin("CustomAppIcon").Panel })

	// the panel's previews, from the main half
	var previews map[string]string
	h.jsInto(`return await S.callMain('CustomAppIcon', 'previews', ['system'])`, &previews)
	for _, id := range []string{"slick", "inverted", "pride", "pride-inverted"} {
		b, ok := dataURLBytes(previews[id])
		m, err := png.Decode(bytes.NewReader(b))
		if !ok || err != nil {
			t.Errorf("no preview of %s: %.60q", id, previews[id])
			continue
		}
		if s := m.Bounds().Size(); max(s.X, s.Y) > 128 {
			t.Errorf("the %s preview is %v, want 128 px at most", id, s)
		}
	}
	if _, ok := previews["slack"]; ok && h.s.slackResources() == "" {
		t.Error("a preview of Slack's own icon with no Slack installed")
	}

	// the Pride design
	if err := h.s.SetOption(h.ctx(), "CustomAppIcon", "icon", "pride"); err != nil {
		t.Fatal(err)
	}
	pride := icon("the Pride icon", func(m *image.NRGBA) bool { return !samePixels(m, own) })
	if s := pride.Bounds().Size(); max(s.X, s.Y) > 256 {
		t.Errorf("the Pride icon is %v, want 256 px at most (a window icon)", s)
	}
	if src := os.Getenv("SLICK_SRC"); src != "" {
		b, err := os.ReadFile(filepath.Join(src, "src", "plugins", "CustomAppIcon", "icons", "pride-light.png"))
		if err != nil {
			t.Fatal(err)
		}
		if want := fitted(t, b, 256); !samePixels(pride, want) {
			t.Error("the icon up isn't Pride's light design fitted to 256 px")
		}
	}

	// an uploaded image: the picked file, decoded, kept and put up
	pic := filepath.Join(t.TempDir(), "my-icon.png")
	writeTestPNG(t, pic, 300, 200)
	h.iconMu.Lock()
	h.pick = pic
	h.iconMu.Unlock()
	var result string
	h.jsInto(`return await S.callMain('CustomAppIcon', 'importCustom', [])`, &result)
	if result != "saved" {
		t.Fatalf("importCustom: %q, want saved", result)
	}
	if err := h.s.SetOption(h.ctx(), "CustomAppIcon", "icon", "custom"); err != nil {
		t.Fatal(err)
	}
	// the next icon up (Pride's is 256 px wide too)
	custom := icon("the uploaded icon", func(m *image.NRGBA) bool { return !samePixels(m, pride) })
	if s := custom.Bounds().Size(); s != image.Pt(256, 170) {
		t.Errorf("the uploaded 300 × 200 image went up as %v, want 256 × 170 (fitted, its aspect kept)", s)
	}
	var stored string
	h.jsInto(`return await S.callMain('CustomAppIcon', 'customPreview', [])`, &stored)
	if b, ok := dataURLBytes(stored); !ok || len(b) == 0 {
		t.Errorf("no custom preview after the upload: %.60q", stored)
	}
	var removed bool
	h.jsInto(`return await S.callMain('CustomAppIcon', 'removeCustom', [])`, &removed)
	if !removed {
		t.Error("removeCustom didn't remove it")
	}
	icon("Clack's own icon once the custom one went", func(m *image.NRGBA) bool { return samePixels(m, own) })

	// stopped: Clack's own icon
	if err := h.s.SetOption(h.ctx(), "CustomAppIcon", "icon", "pride"); err != nil {
		t.Fatal(err)
	}
	icon("the Pride icon again", func(m *image.NRGBA) bool { return !samePixels(m, own) })
	if err := h.s.SetEnabled(h.ctx(), "CustomAppIcon", false); err != nil {
		t.Fatal(err)
	}
	icon("Clack's own icon once it stopped", func(m *image.NRGBA) bool { return samePixels(m, own) })
}

// fitted is PNG b fitted within size px as Slick's fitIcon does (the
// longer side at size, Electron's 'best' resize).
func fitted(t *testing.T, b []byte, size int) *image.NRGBA {
	t.Helper()
	m, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	n := toNRGBA(m) // as a main half's image is decoded (decodeNative)
	s := n.Bounds().Size()
	if max(s.X, s.Y) <= size {
		return n
	}
	w, hh := size, s.Y*size/s.X
	if s.Y > s.X {
		w, hh = s.X*size/s.Y, size
	}
	return scaled(n, w, hh, "best")
}

func samePixels(a, b *image.NRGBA) bool {
	return a.Bounds().Size() == b.Bounds().Size() && bytes.Equal(a.Pix, b.Pix)
}

func dataURLBytes(u string) ([]byte, bool) {
	head, data, ok := strings.Cut(u, ",")
	if !ok || !strings.HasSuffix(head, ";base64") {
		return nil, false
	}
	b, err := base64.StdEncoding.DecodeString(data)
	return b, err == nil
}

// writeTestPNG writes a w × h PNG (a gradient) to path.
func writeTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := m.PixOffset(x, y)
			m.Pix[i], m.Pix[i+1], m.Pix[i+2], m.Pix[i+3] = byte(x), byte(y), 200, 255
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
