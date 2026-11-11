//go:build windows

package composer

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

// The real Windows clipboard: PowerShell (Windows PowerShell 5.1, STA) puts
// data there with .NET's WinForms Clipboard, the way other apps do, and
// Clack's reader takes it back. It replaces the user's clipboard, so it
// runs only on CI (the windows-applog job sets CLACK_WINDOWS_CLIPBOARD_TESTS).

func winClipSet(t *testing.T, script string) {
	t.Helper()
	full := "$ErrorActionPreference = 'Stop'\n" +
		"Add-Type -AssemblyName System.Windows.Forms, System.Drawing\n" +
		// The 3x2 picture TestDIBToPNG* use, with one pixel at alpha 0x80.
		"$b = New-Object System.Drawing.Bitmap 3, 2, ([System.Drawing.Imaging.PixelFormat]::Format32bppArgb)\n" +
		"$b.SetPixel(0, 0, [System.Drawing.Color]::FromArgb(255, 255, 0, 0))\n" +
		"$b.SetPixel(1, 0, [System.Drawing.Color]::FromArgb(255, 0, 255, 0))\n" +
		"$b.SetPixel(2, 0, [System.Drawing.Color]::FromArgb(255, 0, 0, 255))\n" +
		"$b.SetPixel(0, 1, [System.Drawing.Color]::FromArgb(255, 0x12, 0x34, 0x56))\n" +
		"$b.SetPixel(1, 1, [System.Drawing.Color]::FromArgb(255, 255, 255, 255))\n" +
		"$b.SetPixel(2, 1, [System.Drawing.Color]::FromArgb(0x80, 255, 0, 0))\n" +
		script
	u := utf16.Encode([]rune(full))
	raw := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(raw[2*i:], c)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-STA",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(raw))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("powershell: %v\n%s", err, out)
	}
}

func winClipCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func near(a, b color.NRGBA) bool {
	d := func(x, y uint8) bool { return x-y <= 2 || y-x <= 2 }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && d(a.A, b.A)
}

func winClipImage(t *testing.T, explicit bool) (map[[2]int]color.NRGBA, error) {
	t.Helper()
	data, mime, err := attClipboardImage(winClipCtx(t), explicit)
	if err != nil {
		return nil, err
	}
	if mime != "image/png" {
		t.Fatalf("mime %q, want image/png", mime)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	if s := img.Bounds().Size(); s.X != 3 || s.Y != 2 {
		t.Fatalf("size %v, want 3x2", s)
	}
	px := map[[2]int]color.NRGBA{}
	for y := 0; y < 2; y++ {
		for x := 0; x < 3; x++ {
			px[[2]int{x, y}] = color.NRGBAModel.Convert(img.At(img.Bounds().Min.X+x, img.Bounds().Min.Y+y)).(color.NRGBA)
		}
	}
	return px, nil
}

func winClipEnabled(t *testing.T) {
	if os.Getenv("CLACK_WINDOWS_CLIPBOARD_TESTS") == "" {
		t.Skip("replaces the clipboard; set CLACK_WINDOWS_CLIPBOARD_TESTS=1 (CI's windows-applog job)")
	}
}

var winClipOpaque = map[[2]int]color.NRGBA{
	{0, 0}: {0xff, 0, 0, 0xff}, {1, 0}: {0, 0xff, 0, 0xff}, {2, 0}: {0, 0, 0xff, 0xff},
	{0, 1}: {0x12, 0x34, 0x56, 0xff}, {1, 1}: {0xff, 0xff, 0xff, 0xff},
}

// Clipboard.SetImage: a bitmap only (CF_BITMAP; Windows makes CF_DIB and
// CF_DIBV5 from it). It pastes as a PNG with the same pixels.
func TestWinClipboardBitmap(t *testing.T) {
	winClipEnabled(t)
	winClipSet(t, "[System.Windows.Forms.Clipboard]::SetImage($b)\n")
	if paths, err := attClipboardFiles(winClipCtx(t)); err != nil || len(paths) != 0 {
		t.Errorf("files on a bitmap clipboard: %q, %v", paths, err)
	}
	px, err := winClipImage(t, false)
	if err != nil {
		t.Fatalf("attClipboardImage: %v", err)
	}
	for at, want := range winClipOpaque {
		if !near(px[at], want) {
			t.Errorf("pixel %v = %v, want %v", at, px[at], want)
		}
	}
	t.Logf("the translucent pixel came back as %v (a CF_BITMAP has no alpha)", px[[2]int{2, 1}])
}

// A "PNG" next to the bitmap (what browsers, Snipping Tool and Office
// write) wins, and keeps the transparency.
func TestWinClipboardPNG(t *testing.T) {
	winClipEnabled(t)
	winClipSet(t, "$ms = New-Object System.IO.MemoryStream\n"+
		"$b.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)\n"+
		"$do = New-Object System.Windows.Forms.DataObject\n"+
		"$do.SetData('PNG', $false, $ms)\n"+
		"$do.SetImage($b)\n"+
		"[System.Windows.Forms.Clipboard]::SetDataObject($do, $true)\n")
	px, err := winClipImage(t, false)
	if err != nil {
		t.Fatalf("attClipboardImage: %v", err)
	}
	for at, want := range winClipOpaque {
		if px[at] != want {
			t.Errorf("pixel %v = %v, want %v", at, px[at], want)
		}
	}
	if got, want := px[[2]int{2, 1}], (color.NRGBA{0xff, 0, 0, 0x80}); got != want {
		t.Errorf("translucent pixel = %v, want %v (the PNG's alpha)", got, want)
	}
}

// Rich text with a picture (an office app's copy) pastes as text, unless
// the paste asks for the image.
func TestWinClipboardRichText(t *testing.T) {
	winClipEnabled(t)
	winClipSet(t, "$do = New-Object System.Windows.Forms.DataObject\n"+
		"$do.SetData([System.Windows.Forms.DataFormats]::Rtf, '{\\rtf1\\ansi hello}')\n"+
		"$do.SetText('hello')\n"+
		"$do.SetImage($b)\n"+
		"[System.Windows.Forms.Clipboard]::SetDataObject($do, $true)\n")
	if _, err := winClipImage(t, false); err != errAttNoClipImage {
		t.Errorf("rich text copy: err %v, want errAttNoClipImage", err)
	}
	if _, err := winClipImage(t, true); err != nil {
		t.Errorf("rich text copy, explicit: %v", err)
	}
}

// Files copied in Explorer (CF_HDROP) come back as their paths.
func TestWinClipboardFiles(t *testing.T) {
	winClipEnabled(t)
	dir := t.TempDir()
	want := []string{filepath.Join(dir, "a b.png"), filepath.Join(dir, "résumé 📎.txt")}
	for _, p := range want {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var add strings.Builder
	for _, p := range want {
		add.WriteString("[void]$sc.Add('" + strings.ReplaceAll(p, "'", "''") + "')\n")
	}
	winClipSet(t, "$sc = New-Object System.Collections.Specialized.StringCollection\n"+
		add.String()+
		"[System.Windows.Forms.Clipboard]::SetFileDropList($sc)\n")
	got, err := attClipboardFiles(winClipCtx(t))
	if err != nil {
		t.Fatalf("attClipboardFiles: %v", err)
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("files %q, want %q", got, want)
	}
	if _, _, err := attClipboardImage(winClipCtx(t), false); err != errAttNoClipImage {
		t.Errorf("image on a file-list clipboard: %v", err)
	}
}
