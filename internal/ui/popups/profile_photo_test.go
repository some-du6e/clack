package popups

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

// The profile photo through the Edit profile modal as Slack's client takes
// it (eNqz EditProfileUploadPane and EditProfileCropPane, web client build
// 2c0d02ca130f), against fakeslack's users.preparePhoto and
// users.setPhoto, which check the request and, like the real method,
// answer with a prepared image of their own size: an upload bigger than
// 1024 px comes back fitted into 1024×1024 (fakeslack/profile.go
// photoWrite). Synthetic only: nothing here talks to Slack.

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

// Upload Photo with a 1.5 MB PNG of 1500×1000 (the report's was a 1.3 MB
// PNG) and a 1600×1200 JPEG: the crop pane shows the image
// users.preparePhoto answered with (1024 px on its long side), its first
// square is that image's largest centred one, and Save sets the photo and
// goes back to the form.
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
			h, _ := epEnv(t, false)
			m := NewEditProfileModal(h, EditProfileOpts{})
			f := m.Form()
			m.UploadPhoto(core.UploadFile{Name: c.name, Data: c.data})
			epUntil(t, m, "the crop pane", func() bool { return m.Pane() == "cropPane" || m.upl.Status() == "error" })
			if m.Pane() != "cropPane" {
				t.Fatalf("the upload stopped at %q: %q", m.upl.title(), m.upl.ErrorText())
			}
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
			m.crop.Save()
			epUntil(t, m, "back on the form", func() bool { return m.Pane() == "formPane" || m.crop.Failed() })
			if m.crop.Failed() {
				t.Fatal("Save showed Sorry! Something went wrong. Please try again.")
			}
			if !f.hasPhoto() {
				t.Fatal("no custom photo after Save")
			}
		})
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
// sends every press) and, the fault gone, sets the photo. An upload
// failing shows the Eek pane and is logged too. Neither line carries the
// file's name.
func TestEditProfilePhotoErrorsLogged(t *testing.T) {
	const name = "private-name.png"
	logs := &epLog{}
	prev := log.Writer()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(prev) })

	h, fs := epEnv(t, false)
	m := NewEditProfileModal(h, EditProfileOpts{})
	m.UploadPhoto(core.UploadFile{Name: name, Data: epPNG(t, 600, 400)})
	epUntil(t, m, "the crop pane", func() bool { return m.Pane() == "cropPane" })
	fs.SetFault("users.setPhoto", fakeslack.Fault{Error: "bad_image"})
	m.crop.Save()
	epUntil(t, m, "the error alert", func() bool { return m.crop.Failed() })
	if m.Pane() != "cropPane" {
		t.Fatalf("pane %q after a failed Save", m.Pane())
	}
	if want := "profile photo: users.setPhoto failed: error=bad_image http=200\n"; !strings.Contains(logs.String(), want) {
		t.Fatalf("log has no %q:\n%s", want, logs.String())
	}
	fs.SetFault("users.setPhoto", fakeslack.Fault{})
	m.crop.Save()
	epUntil(t, m, "back on the form", func() bool { return m.Pane() == "formPane" })

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
