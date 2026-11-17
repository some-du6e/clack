package popups

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log"
	"math/rand/v2"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/exif"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// The profile photo through the Edit profile modal as Slack's client takes
// it (eNqz EditProfileUploadPane and EditProfileCropPane, web client build
// 2c0d02ca130f), against fakeslack's users.preparePhoto and
// users.setPhoto, which check the request and answer with a prepared image
// of their own (kept as uploaded when it fits in 1024×1024, fitted into it
// as a PNG otherwise; fakeslack/photo.go), on Slack's avatar CDN when the
// test routes it to the fake. Synthetic only: nothing here talks to Slack.

// epNoisePNG is a w×h PNG that doesn't compress well (one channel is
// noise), like a photo: about a byte a pixel.
func epNoisePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	r := rand.New(rand.NewPCG(uint64(w), uint64(h)))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 255 / w), G: uint8(r.UintN(256)), B: uint8(y * 255 / h), A: 0xff})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// epJPEG is a w×h baseline JPEG.
func epJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x ^ y), A: 0xff})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// epHalvesJPEG is a w×h JPEG whose left half is red and right half blue.
func epHalvesJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{R: 0xff, A: 0xff}
			if x >= w/2 {
				c = color.RGBA{B: 0xff, A: 0xff}
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// epGIF is a GIF with a sw×sh logical screen and one orange frame covering
// only fr.
func epGIF(t *testing.T, sw, sh int, fr image.Rectangle) []byte {
	t.Helper()
	pal := color.Palette{color.RGBA{0, 0, 0, 0xff}, color.RGBA{0xff, 0x80, 0, 0xff}}
	frame := image.NewPaletted(fr, pal)
	for i := range frame.Pix {
		frame.Pix[i] = 1
	}
	var b bytes.Buffer
	g := &gif.GIF{Image: []*image.Paletted{frame}, Delay: []int{0}, Config: image.Config{ColorModel: pal, Width: sw, Height: sh}}
	if err := gif.EncodeAll(&b, g); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// epSquare is the crop pane's first square on an image of w×h, as Save
// sends it.
func epSquare(w, h int) core.PhotoCrop {
	dw, dh := core.PhotoDisplaySize(float64(w), float64(h), core.PhotoCropMaxSize)
	x, y, side := core.DefaultPhotoCrop(dw, dh)
	return core.CropFromDisplay(x, y, side, w, h)
}

// epPhotoEnv is epEnv with the prepared images on Slack's avatar CDN, the
// client's and the credential-less fetches routed to the fake.
func epPhotoEnv(t *testing.T) (*tzHost, *fakeslack.Server) {
	t.Helper()
	h, fs := epEnv(t, false)
	fs.SetPhotoBase(fakeslack.PhotoBaseCDN)
	t.Cleanup(fs.RouteSlackHosts(h.a.C))
	return h, fs
}

// epToCrop uploads f and waits for the crop pane.
func epToCrop(t *testing.T, m *EditProfileModal, f core.UploadFile) {
	t.Helper()
	m.UploadPhoto(f)
	epUntil(t, m, "the crop pane", func() bool { return m.Pane() == "cropPane" || m.upl.Status() == "error" })
	if m.Pane() != "cropPane" {
		t.Fatalf("the upload stopped at %q: %q", m.upl.title(), m.upl.ErrorText())
	}
}

// epSave presses Save and waits for the form (or the alert).
func epSave(t *testing.T, m *EditProfileModal) {
	t.Helper()
	m.crop.Save()
	epUntil(t, m, "back on the form", func() bool { return m.Pane() == "formPane" || m.crop.Failed() })
	if m.crop.Failed() {
		t.Fatal("Save showed Sorry! Something went wrong. Please try again.")
	}
}

// Upload Photo with a 1.5 MB PNG of 1500×1000 (the report's was a 1.3 MB
// PNG) and a 1600×1200 JPEG: the crop pane shows the image
// users.preparePhoto answered with (the fake fits big ones into 1024 px;
// served from Slack's avatar CDN), its first square is that image's
// largest centred one, and Save sets the photo and goes back to the form.
func TestEditProfilePhotoLargeLikeSlack(t *testing.T) {
	big := epNoisePNG(t, 1500, 1000)
	if len(big) < 1<<20 {
		t.Fatalf("the big PNG is only %d bytes", len(big))
	}
	for _, c := range []struct {
		name string
		data []byte
		w, h int
	}{
		{"big photo.png", big, 1500, 1000},
		{"camera.jpg", epJPEG(t, 1600, 1200), 1600, 1200},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, fs := epPhotoEnv(t)
			m := NewEditProfileModal(h, EditProfileOpts{})
			f := m.Form()
			epToCrop(t, m, core.UploadFile{Name: c.name, Data: c.data})
			pw, ph := fakeslack.PreparedPhotoSize(c.w, c.h)
			if m.crop.up.Width != pw || m.crop.up.Height != ph {
				t.Errorf("crop pane image %d×%d, want the prepared image's %d×%d (not the file's %d×%d)", m.crop.up.Width, m.crop.up.Height, pw, ph, c.w, c.h)
			}
			if dw, dh := core.PhotoDisplaySize(float64(pw), float64(ph), core.PhotoCropMaxSize); m.crop.dw != dw || m.crop.dh != dh {
				t.Errorf("crop pane shows %v×%v, want the prepared image fitted into 460: %v×%v", m.crop.dw, m.crop.dh, dw, dh)
			}
			crop := m.crop.Crop()
			if crop.Size != min(pw, ph) || crop.X+crop.Size > pw || crop.Y+crop.Size > ph {
				t.Errorf("first square %+v isn't the prepared %d×%d image's largest", crop, pw, ph)
			}
			if loads := fs.PhotoLoads(); len(loads) != 1 || loads[0].Host != "avatars.slack-edge.com" || loads[0].Cookie {
				t.Errorf("prepared image loads %+v", loads)
			}
			epSave(t, m)
			if !f.hasPhoto() {
				t.Fatal("no custom photo after Save")
			}
		})
	}
}

// The crop pane measures and shows the prepared image as Chromium's <img>
// has it: a JPEG with EXIF Orientation 6 stored 1024×768 is 768×1024, a
// GIF is its logical screen; Save's square is in that size, and the
// photo is set.
func TestEditProfilePhotoSizeLikeChromium(t *testing.T) {
	for _, c := range []struct {
		name string
		data []byte
		w, h int
	}{
		{"turned.jpg", exif.WithOrientation(epJPEG(t, 1024, 768), 6), 768, 1024},
		{"partial.gif", epGIF(t, 400, 300, image.Rect(100, 75, 300, 225)), 400, 300},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, _ := epPhotoEnv(t)
			m := NewEditProfileModal(h, EditProfileOpts{})
			epToCrop(t, m, core.UploadFile{Name: c.name, Data: c.data})
			if m.crop.up.Width != c.w || m.crop.up.Height != c.h {
				t.Errorf("crop pane image %d×%d, want %d×%d", m.crop.up.Width, m.crop.up.Height, c.w, c.h)
			}
			if got, want := m.crop.Crop(), epSquare(c.w, c.h); got != want {
				t.Errorf("first square %+v, want %+v", got, want)
			}
			epSave(t, m)
		})
	}
}

// cropImage turns a JPEG by its EXIF Orientation and draws a GIF's first
// frame on its logical screen, at its offset: what the crop pane shows.
func TestCropImageLikeChromium(t *testing.T) {
	ctx := context.Background()
	// stored 64×32, left half red: a quarter turn clockwise shows it 32×64
	// with the red half on top
	disp, size, err := cropImage(ctx, exif.WithOrientation(epHalvesJPEG(t, 64, 32), 6))
	if err != nil {
		t.Fatal(err)
	}
	if size != image.Pt(32, 64) || disp.Bounds().Size() != image.Pt(32, 64) {
		t.Fatalf("turned JPEG: size %v, shown %v", size, disp.Bounds().Size())
	}
	if c := disp.RGBAAt(16, 8); c.R < 0xc0 || c.B > 0x40 {
		t.Errorf("turned JPEG's top is %v, want red", c)
	}
	if c := disp.RGBAAt(16, 56); c.B < 0xc0 || c.R > 0x40 {
		t.Errorf("turned JPEG's bottom is %v, want blue", c)
	}
	disp, size, err = cropImage(ctx, epGIF(t, 40, 30, image.Rect(10, 10, 30, 20)))
	if err != nil {
		t.Fatal(err)
	}
	if size != image.Pt(40, 30) || disp.Bounds().Size() != image.Pt(40, 30) {
		t.Fatalf("GIF: size %v, shown %v", size, disp.Bounds().Size())
	}
	if c := disp.RGBAAt(4, 4); c.A != 0 {
		t.Errorf("GIF screen outside the frame is %v, want transparent", c)
	}
	if c := disp.RGBAAt(20, 15); c.R != 0xff || c.G != 0x80 || c.A != 0xff {
		t.Errorf("GIF frame at its offset is %v, want orange", c)
	}
}

// processUpload checks the File's type, which Chromium takes from its
// name's extension, before it loads it; and that type is the part's
// Content-Type. A PNG named photo.webp or a JPEG named x.heic is "Try
// another photo" with nothing sent; a PNG named photo.jpg goes up as
// image/jpeg; a WebP named saved.png goes up as image/png (the fake, like a
// server that takes GIF, PNG and JPEG only, answers it bad_image: Eek).
func TestEditProfilePhotoTypeLikeSlack(t *testing.T) {
	webp, err := os.ReadFile("../../imgcache/testdata/blue-purple-pink.lossy.webp")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name  string
		data  []byte
		sent  string // the part's Content-Type, "" for nothing sent
		title string // the upload pane's, "" for the crop pane
	}{
		{"photo.webp", epPNG(t, 600, 400), "", "Try another photo"},
		{"x.heic", epJPEG(t, 600, 400), "", "Try another photo"},
		{"photo.jpg", epPNG(t, 600, 400), "image/jpeg", ""},
		{"saved.png", webp, "image/png", "Eek"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, fs := epEnv(t, false)
			var mu sync.Mutex
			var sent []string
			fs.SetHook(func(method string, f url.Values) (any, bool) {
				if method == "users.preparePhoto" {
					mu.Lock()
					sent = append(sent, f.Get(fakeslack.PhotoTypeKey))
					mu.Unlock()
				}
				return nil, false
			})
			m := NewEditProfileModal(h, EditProfileOpts{})
			m.UploadPhoto(core.UploadFile{Name: c.name, Data: c.data})
			epUntil(t, m, "the upload's end", func() bool { return m.Pane() == "cropPane" || m.upl.Status() == "error" })
			if c.title == "" && m.Pane() != "cropPane" || c.title != "" && (m.Pane() == "cropPane" || m.upl.title() != c.title) {
				t.Errorf("pane %q, title %q, want %q", m.Pane(), m.upl.title(), c.title)
			}
			mu.Lock()
			defer mu.Unlock()
			if c.sent == "" && len(sent) != 0 || c.sent != "" && (len(sent) != 1 || sent[0] != c.sent) {
				t.Errorf("sent %q, want %q", sent, c.sent)
			}
		})
	}
}

// The photo calls aren't held to run's limit: with it shortened to 200 ms,
// an upload and a Save that each take 600 ms still reach the crop pane and
// the form.
func TestEditProfilePhotoWaitsPastRunsLimit(t *testing.T) {
	h, fs := epEnv(t, false)
	m := NewEditProfileModal(h, EditProfileOpts{})
	f := m.Form()
	epUntil(t, m, "the form's loads", func() bool { return !f.grantsLoading && !f.secsLoading })
	old := asyncTimeout
	asyncTimeout = 200 * time.Millisecond
	t.Cleanup(func() { asyncTimeout = old })
	fs.SetFault("users.preparePhoto", fakeslack.Fault{Delay: 600 * time.Millisecond})
	epToCrop(t, m, core.UploadFile{Name: "me.png", Data: epPNG(t, 600, 400)})
	fs.SetFault("users.preparePhoto", fakeslack.Fault{})
	fs.SetFault("users.setPhoto", fakeslack.Fault{Delay: 600 * time.Millisecond})
	epSave(t, m)
	if !f.hasPhoto() {
		t.Error("no custom photo after Save")
	}
}

// While its call runs, Save, and then Yes, Remove Photo, is Slack's
// LoadingButton loading: not pressable, and its label out of the flow, so
// the button is its 80px min-width around the spinner.
func TestEditProfilePhotoLoadingLikeSlack(t *testing.T) {
	h, fs := epEnv(t, false)
	m := NewEditProfileModal(h, EditProfileOpts{})
	epToCrop(t, m, core.UploadFile{Name: "me.png", Data: epPNG(t, 600, 400)})
	save := m.crop.m.o.Buttons[1]
	epFrame(m)
	if save.loading() || !save.enabled() {
		t.Fatal("Save loading before it's pressed")
	}
	fs.SetFault("users.setPhoto", fakeslack.Fault{Delay: time.Second})
	m.crop.Save()
	epFrame(m)
	if !save.loading() || save.enabled() {
		t.Error("Save isn't loading while users.setPhoto runs")
	}
	if w := save.Rect().Dx(); w != 80 {
		t.Errorf("loading Save is %dpx wide, want 80", w)
	}
	epUntil(t, m, "back on the form", func() bool { return m.Pane() == "formPane" })

	m.switchPane("removePane")
	yes := m.rm.m.o.Buttons[1]
	epFrame(m)
	w0 := yes.Rect().Dx()
	fs.SetFault("users.deletePhoto", fakeslack.Fault{Delay: time.Second})
	m.rm.Remove()
	epFrame(m)
	if !yes.loading() || yes.enabled() {
		t.Error("Yes, Remove Photo isn't loading while users.deletePhoto runs")
	}
	if w := yes.Rect().Dx(); w != 80 || w0 <= 80 {
		t.Errorf("Yes, Remove Photo is %dpx wide, %dpx loading; want wider than 80, then 80", w0, w)
	}
	epUntil(t, m, "back on the form", func() bool { return m.Pane() == "formPane" })
	epFrame(m)
	if yes.loading() {
		t.Error("still loading after the call")
	}
}

// Slack's API queue tries the photo calls again: an upload whose
// connection is dropped and a Save answered 503 once both go through.
func TestEditProfilePhotoRetriesLikeSlack(t *testing.T) {
	old := slack.RetryUnit
	slack.RetryUnit = time.Millisecond
	t.Cleanup(func() { slack.RetryUnit = old })
	h, fs := epEnv(t, false)
	m := NewEditProfileModal(h, EditProfileOpts{})
	fs.SetFault("users.preparePhoto", fakeslack.Fault{HangUp: true, Times: 1})
	p0 := fs.CallCount("users.preparePhoto")
	epToCrop(t, m, core.UploadFile{Name: "me.png", Data: epPNG(t, 600, 400)})
	if n := fs.CallCount("users.preparePhoto") - p0; n != 2 {
		t.Errorf("users.preparePhoto sent %d times, want 2", n)
	}
	fs.SetFault("users.setPhoto", fakeslack.Fault{Status: 503, Times: 1})
	s0 := fs.CallCount("users.setPhoto")
	epSave(t, m)
	if n := fs.CallCount("users.setPhoto") - s0; n != 2 {
		t.Errorf("users.setPhoto sent %d times, want 2", n)
	}
}

// epLog is the standard logger's output (clack.log) while a test runs.
type epLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *epLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *epLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// Save failing keeps the crop pane with Slack's alert ("Sorry! Something
// went wrong. Please try again.") and logs the method, Slack's error code
// and the HTTP status; pressing Save again sends it again (Slack's client
// sends every press: here past a backoff of a minute) and, the fault
// gone, sets the photo. Yes, Remove Photo does the same. An upload
// failing shows the Eek pane and is logged too. No line carries the
// file's name.
func TestEditProfilePhotoErrorsLogged(t *testing.T) {
	const name = "private-name.png"
	logs := &epLog{}
	prev := log.Writer()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(prev) })
	oldBase, oldMax := slack.BackoffBase, slack.BackoffMax
	slack.BackoffBase, slack.BackoffMax = time.Minute, time.Minute
	t.Cleanup(func() { slack.BackoffBase, slack.BackoffMax = oldBase, oldMax })

	h, fs := epEnv(t, false)
	m := NewEditProfileModal(h, EditProfileOpts{})
	epToCrop(t, m, core.UploadFile{Name: name, Data: epPNG(t, 600, 400)})
	fs.SetFault("users.setPhoto", fakeslack.Fault{Error: "bad_image"})
	s0 := fs.CallCount("users.setPhoto")
	m.crop.Save()
	epUntil(t, m, "the error alert", func() bool { return m.crop.Failed() })
	if m.Pane() != "cropPane" {
		t.Fatalf("pane %q after a failed Save", m.Pane())
	}
	if want := "profile photo: users.setPhoto failed: error=bad_image http=200\n"; !strings.Contains(logs.String(), want) {
		t.Fatalf("log has no %q:\n%s", want, logs.String())
	}
	fs.SetFault("users.setPhoto", fakeslack.Fault{})
	epSave(t, m)
	if n := fs.CallCount("users.setPhoto") - s0; n != 2 {
		t.Errorf("users.setPhoto sent %d times for two Saves, want 2", n)
	}

	m.switchPane("removePane")
	fs.SetFault("users.deletePhoto", fakeslack.Fault{Error: "read_only_profile_field"})
	d0 := fs.CallCount("users.deletePhoto")
	m.rm.Remove()
	epUntil(t, m, "the remove pane's alert", func() bool { return m.rm.failed })
	if want := "profile photo: users.deletePhoto failed: error=read_only_profile_field http=200\n"; !strings.Contains(logs.String(), want) {
		t.Fatalf("log has no %q:\n%s", want, logs.String())
	}
	fs.SetFault("users.deletePhoto", fakeslack.Fault{})
	m.rm.Remove()
	epUntil(t, m, "back on the form", func() bool { return m.Pane() == "formPane" })
	if n := fs.CallCount("users.deletePhoto") - d0; n != 2 {
		t.Errorf("users.deletePhoto sent %d times for two presses, want 2", n)
	}

	fs.SetFault("users.preparePhoto", fakeslack.Fault{Status: 500})
	m.UploadPhoto(core.UploadFile{Name: name, Data: epPNG(t, 600, 400)})
	epUntil(t, m, "the upload error", func() bool { return m.upl.Status() == "error" })
	if m.upl.title() != "Eek" || m.upl.ErrorText() != "Sorry! Something went wrong. Please try again." {
		t.Errorf("upload error pane %q %q", m.upl.title(), m.upl.ErrorText())
	}
	if want := "profile photo: users.preparePhoto failed: http=500\n"; !strings.Contains(logs.String(), want) {
		t.Fatalf("log has no %q:\n%s", want, logs.String())
	}
	if got := logs.String(); strings.Contains(got, "private-name") || strings.Contains(got, fakeslack.TeamToken) {
		t.Errorf("the log carries the file's name or the token:\n%s", got)
	}
}
