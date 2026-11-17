package core

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log"
	"math/rand/v2"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// The profile photo as Slack's client sends it (eNqz processUpload and
// EditProfileCropPane, web client build 2c0d02ca130f), against
// fakeslack's users.preparePhoto and users.setPhoto, which check the
// request (fakeslack/profile.go photoWrite) and, like the real method,
// answer with a prepared image of their own size: an upload bigger than
// 1024 px comes back fitted into 1024×1024. Synthetic only: nothing here
// talks to Slack.

// photoNoisePNG is a w×h PNG that doesn't compress well (one channel is
// noise), like a photo: about a byte a pixel.
func photoNoisePNG(t *testing.T, w, h int) []byte {
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

// photoJPEG is a w×h baseline JPEG.
func photoJPEG(t *testing.T, w, h int) []byte {
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

// cropPaneSquare is the crop pane's first square, Save as is: BaseImageCrop's
// default (the largest centred square) on an image of w×h shown fitted into
// 460 px, mapped back to its pixels the way handleUpload maps it.
func cropPaneSquare(w, h int) PhotoCrop {
	dw, dh := PhotoDisplaySize(float64(w), float64(h), PhotoCropMaxSize)
	x, y, side := DefaultPhotoCrop(dw, dh)
	return CropFromDisplay(x, y, side, w, h)
}

// A 1.5 MB PNG of 1500×1000 (the report's was a 1.3 MB PNG), a 1600×1200 JPEG
// and a 640×480 JPEG: users.preparePhoto gets the workspace token and the
// file as an "image" part typed as the file is; the prepared image (1024
// px on its long side for the big ones) is what the crop counts in, so
// Save's users.setPhoto crop is the largest centred square of the
// prepared image and the photo is set.
func TestProfilePhotoRequestLikeSlack(t *testing.T) {
	big := photoNoisePNG(t, 1500, 1000)
	if len(big) < 1<<20 {
		t.Fatalf("the big PNG is only %d bytes", len(big))
	}
	for _, c := range []struct {
		name, typ string
		data      []byte
		w, h      int
	}{
		{"big photo.png", "image/png", big, 1500, 1000},
		{"camera.jpg", "image/jpeg", photoJPEG(t, 1600, 1200), 1600, 1200},
		{"small.jpeg", "image/jpeg", photoJPEG(t, 640, 480), 640, 480},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, _, calls := pwEnv(t)
			ctx := context.Background()
			up, err := a.PreparePhoto(ctx, c.name, c.data)
			if err != nil {
				t.Fatalf("PreparePhoto: %v", err)
			}
			pp := calls.get("users.preparePhoto")
			if len(pp) != 1 {
				t.Fatalf("users.preparePhoto calls: %d", len(pp))
			}
			f := pp[0]
			if f.Get("token") != fakeslack.TeamToken {
				t.Errorf("users.preparePhoto token %q, want the workspace's", f.Get("token"))
			}
			if got := f.Get(fakeslack.PhotoTypeKey); got != c.typ {
				t.Errorf("image part Content-Type %q, want %q (a browser's FormData types a File as it is)", got, c.typ)
			}
			if got := f.Get(fakeslack.PhotoFilenameKey); got != c.name {
				t.Errorf("image part filename %q, want %q", got, c.name)
			}
			if got := f.Get(fakeslack.PhotoBytesKey); got != strconv.Itoa(len(c.data)) {
				t.Errorf("image part %s bytes, want %d", got, len(c.data))
			}
			for k := range f {
				if k != "token" && !strings.HasPrefix(k, "_fake_") {
					t.Errorf("users.preparePhoto sent a %q field Slack's client doesn't", k)
				}
			}
			pw, ph := fakeslack.PreparedPhotoSize(c.w, c.h)
			if up.Width != pw || up.Height != ph {
				t.Errorf("crop pane image %d×%d, want the prepared image's %d×%d (not the file's %d×%d)", up.Width, up.Height, pw, ph, c.w, c.h)
			}
			crop := cropPaneSquare(up.Width, up.Height)
			if side := min(pw, ph); crop.Size != side || crop.X+crop.Size > pw || crop.Y+crop.Size > ph {
				t.Errorf("first square %+v isn't the prepared %d×%d image's largest", crop, pw, ph)
			}
			if err := a.SetPhoto(ctx, up.ID, crop); err != nil {
				t.Fatalf("SetPhoto %+v: %v", crop, err)
			}
			sp := calls.get("users.setPhoto")
			want := url.Values{"token": {fakeslack.TeamToken}, "id": {up.ID}, "crop_x": {strconv.Itoa(crop.X)},
				"crop_y": {strconv.Itoa(crop.Y)}, "crop_w": {strconv.Itoa(crop.Size)}}
			if len(sp) != 1 || sp[0].Encode() != want.Encode() {
				t.Fatalf("users.setPhoto %v, want %v", sp, want)
			}
			if !a.HasCustomPhoto(fakeslack.SelfID) {
				t.Error("no custom photo after users.setPhoto")
			}
		})
	}
}

// users.preparePhoto's id may come back as a JSON number: Slack's client
// hands it to FormData either way, so setPhoto gets its digits.
func TestProfilePhotoNumericIDLikeSlack(t *testing.T) {
	a, fs, _ := pwEnv(t)
	var setID string
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		switch method {
		case "users.preparePhoto":
			return map[string]any{"ok": true, "id": 1234567890123, "url": fs.URL + "img/600x400/prepared-number.png"}, true
		case "users.setPhoto":
			setID = f.Get("id")
			return map[string]any{"ok": true}, true
		}
		return nil, false
	})
	ctx := context.Background()
	up, err := a.PreparePhoto(ctx, "me.png", pwPNG(t, 600, 400))
	if err != nil || up.ID != "1234567890123" {
		t.Fatalf("PreparePhoto %q %v", up.ID, err)
	}
	if err := a.SetPhoto(ctx, up.ID, cropPaneSquare(up.Width, up.Height)); err != nil || setID != "1234567890123" {
		t.Fatalf("SetPhoto id %q %v", setID, err)
	}
}

// photoLog is the standard logger's output (clack.log) while a test runs.
type photoLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *photoLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *photoLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func capturePhotoLog(t *testing.T) *photoLog {
	t.Helper()
	l := &photoLog{}
	prev := log.Writer()
	log.SetOutput(l)
	t.Cleanup(func() { log.SetOutput(prev) })
	return l
}

// A failed photo call leaves a line in clack.log with the method, Slack's
// error code and the HTTP status, and nothing it sent or got back: not the
// file's name, the prepared id or url, or a token.
func TestProfilePhotoFailuresLogged(t *testing.T) {
	const name = "secret-selfie.png"
	ctx := context.Background()
	for _, c := range []struct {
		what   string
		method string
		fault  fakeslack.Fault
		hook   bool // users.preparePhoto answers a url that isn't there
		want   string
	}{
		{"prepare error", "users.preparePhoto", fakeslack.Fault{Error: "too_large"}, false, "profile photo: users.preparePhoto failed: error=too_large http=200"},
		{"prepare 500", "users.preparePhoto", fakeslack.Fault{Status: 500}, false, "profile photo: users.preparePhoto failed: http=500"},
		{"prepared image 404", "", fakeslack.Fault{}, true, "profile photo: users.preparePhoto's image failed: http=404"},
		{"set error", "users.setPhoto", fakeslack.Fault{Error: "bad_image"}, false, "profile photo: users.setPhoto failed: error=bad_image http=200"},
		{"set 503", "users.setPhoto", fakeslack.Fault{Status: 503}, false, "profile photo: users.setPhoto failed: http=503"},
		{"delete error", "users.deletePhoto", fakeslack.Fault{Error: "read_only_profile_field"}, false, "profile photo: users.deletePhoto failed: error=read_only_profile_field http=200"},
	} {
		t.Run(c.what, func(t *testing.T) {
			a, fs, _ := pwEnv(t)
			logs := capturePhotoLog(t)
			if c.hook {
				fs.SetHook(func(method string, f url.Values) (any, bool) {
					if method == "users.preparePhoto" {
						return map[string]any{"ok": true, "id": "1000000000042", "url": fs.URL + "missing/prepared-1000000000042.png"}, true
					}
					return nil, false
				})
			}
			var id, purl string
			if c.method == "users.setPhoto" || c.method == "users.deletePhoto" {
				up, err := a.PreparePhoto(ctx, name, pwPNG(t, 600, 400))
				if err != nil {
					t.Fatalf("PreparePhoto: %v", err)
				}
				id, purl = up.ID, up.URL
			}
			if c.method != "" {
				fs.SetFault(c.method, c.fault)
			}
			var err error
			switch c.method {
			case "users.setPhoto":
				err = a.SetPhoto(ctx, id, cropPaneSquare(600, 400))
			case "users.deletePhoto":
				err = a.DeletePhoto(ctx)
			default:
				_, err = a.PreparePhoto(ctx, name, pwPNG(t, 600, 400))
				if err != ErrPhotoAPI {
					t.Errorf("PreparePhoto: %v, want ErrPhotoAPI (the Eek pane)", err)
				}
			}
			if err == nil {
				t.Fatal("no error")
			}
			got := logs.String()
			if !strings.Contains(got, c.want+"\n") {
				t.Fatalf("log has no %q:\n%s", c.want, got)
			}
			for _, leak := range []string{name, "secret-selfie", fakeslack.TeamToken, fakeslack.OrgToken, "prepared-", "missing/"} {
				if strings.Contains(got, leak) {
					t.Errorf("the log carries %q:\n%s", leak, got)
				}
			}
			if id != "" && strings.Contains(got, id) {
				t.Errorf("the log carries the prepared id:\n%s", got)
			}
			if purl != "" && strings.Contains(got, purl) {
				t.Errorf("the log carries the prepared url:\n%s", got)
			}
		})
	}
}
