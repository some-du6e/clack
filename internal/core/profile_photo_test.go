package core

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/exif"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// The profile photo as Slack's client sends it (eNqz processUpload and
// EditProfileCropPane, web client build 2c0d02ca130f), against
// fakeslack's users.preparePhoto and users.setPhoto, which check the
// request (fakeslack/photo.go) and answer with a prepared image of their
// own: kept as uploaded when it fits in 1024×1024, fitted into it as a PNG
// otherwise, served on Slack's own hosts (files.slack.com,
// avatars.slack-edge.com, through slack-imgs.com) when the test routes
// them to the fake. Synthetic only: nothing here talks to Slack.

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

// photoGIF is a GIF whose logical screen is sw×sh and whose one frame
// covers only fr of it.
func photoGIF(t *testing.T, sw, sh int, fr image.Rectangle) []byte {
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

// cropPaneSquare is the crop pane's first square, Save as is: BaseImageCrop's
// default (the largest centred square) on an image of w×h shown fitted into
// 460 px, mapped back to its pixels the way handleUpload maps it.
func cropPaneSquare(w, h int) PhotoCrop {
	dw, dh := PhotoDisplaySize(float64(w), float64(h), PhotoCropMaxSize)
	x, y, side := DefaultPhotoCrop(dw, dh)
	return CropFromDisplay(x, y, side, w, h)
}

// photoEnvOn is pwEnv with the prepared images on base (one of Slack's
// hosts), the client's and the credential-less fetches routed to the fake.
func photoEnvOn(t *testing.T, base string) (*App, *fakeslack.Server, *pwCalls) {
	t.Helper()
	a, fs, calls := pwEnv(t)
	fs.SetPhotoBase(base)
	t.Cleanup(fs.RouteSlackHosts(a.C))
	return a, fs, calls
}

// A 1.5 MB PNG of 1500×1000 (the report's was a 1.3 MB PNG), a 1600×1200
// JPEG and a 640×480 JPEG: users.preparePhoto gets the workspace token and
// the file as an "image" part typed as Chromium types the file by its
// name; the prepared image, on Slack's avatar CDN, loads with no cookie,
// and its size (the fake fits the big ones into 1024 px) is what the crop
// counts in, so Save's users.setPhoto crop is the largest centred square
// of the prepared image and the photo is set.
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
			a, fs, calls := photoEnvOn(t, fakeslack.PhotoBaseCDN)
			ctx := context.Background()
			up, err := a.PreparePhoto(ctx, c.name, PhotoFileType(c.name), c.data)
			if err != nil {
				t.Fatalf("PreparePhoto: %v", err)
			}
			pp := calls.get("users.preparePhoto")
			if len(pp) != 1 || fs.CallCount("users.preparePhoto") != 1 {
				t.Fatalf("users.preparePhoto calls: %d recorded, %d made", len(pp), fs.CallCount("users.preparePhoto"))
			}
			f := pp[0]
			if f.Get("token") != fakeslack.TeamToken {
				t.Errorf("users.preparePhoto token %q, want the workspace's", f.Get("token"))
			}
			if got := f.Get(fakeslack.PhotoTypeKey); got != c.typ {
				t.Errorf("image part Content-Type %q, want %q (FormData types a File as Chromium typed it)", got, c.typ)
			}
			if got := f.Get(fakeslack.PhotoFilenameKey); got != c.name {
				t.Errorf("image part filename %q, want %q", got, c.name)
			}
			if got := f.Get(fakeslack.PhotoBytesKey); got != strconv.Itoa(len(c.data)) {
				t.Errorf("image part %s bytes, want %d", got, len(c.data))
			}
			for k := range f {
				if k != "token" && !strings.HasPrefix(k, "_fake_") {
					// Slack's body is the token, the image and apiCall's
					// _x_reason, _x_mode, _x_sonic and _x_app_name, which
					// Clack sends for no call
					t.Errorf("users.preparePhoto sent a %q field Clack doesn't send", k)
				}
			}
			loads := fs.PhotoLoads()
			if len(loads) != 1 || loads[0].Host != "avatars.slack-edge.com" || loads[0].Cookie || loads[0].URL != up.URL {
				t.Errorf("prepared image loads %+v, want %s once, with no cookie", loads, up.URL)
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
			// Slack's also carries _x_reason "edit-profile-set-photo" and
			// _x_mode, which Clack sends for no call
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

// The prepared image loads as an <img> of it would, under Clack's rules
// for credentials: the session cookie to Slack's file host only, none to
// its avatar CDN, and a host of nobody's through Slack's image proxy,
// never directly.
func TestProfilePhotoImageRoutingLikeSlack(t *testing.T) {
	for _, c := range []struct {
		base, host string
		cookie     bool
		proxied    bool
	}{
		{fakeslack.PhotoBaseCDN, "avatars.slack-edge.com", false, false},
		{fakeslack.PhotoBaseFiles, "files.slack.com", true, false},
		{fakeslack.PhotoBaseOther, "slack-imgs.com", false, true},
	} {
		t.Run(c.host, func(t *testing.T) {
			a, fs, _ := photoEnvOn(t, c.base)
			up, err := a.PreparePhoto(context.Background(), "me.png", "image/png", pwPNG(t, 600, 400))
			if err != nil {
				t.Fatalf("PreparePhoto: %v", err)
			}
			if !strings.HasPrefix(up.URL, c.base) || up.Width != 600 || up.Height != 400 || len(up.Image) == 0 {
				t.Fatalf("prepared %q %d×%d, %d bytes", up.URL, up.Width, up.Height, len(up.Image))
			}
			loads := fs.PhotoLoads()
			if len(loads) != 1 || loads[0].Host != c.host || loads[0].Cookie != c.cookie {
				t.Fatalf("loads %+v, want one on %s, cookie %v", loads, c.host, c.cookie)
			}
			if c.proxied && loads[0].URL != slack.ImageProxyBase+url.QueryEscape(up.URL) {
				t.Errorf("proxied as %q", loads[0].URL)
			}
		})
	}
}

// The prepared image's size is the one Chromium gives it, which the crop
// counts in: a JPEG kept with its EXIF Orientation 6 (a quarter turn) is
// 768×1024, not its stored 1024×768; a GIF is its logical screen, not its
// first frame. Save's square is in that size, and Slack's server (here the
// fake, which measures the same way) takes it.
func TestProfilePhotoSizeLikeChromium(t *testing.T) {
	for _, c := range []struct {
		name string
		data []byte
		w, h int
	}{
		{"turned.jpg", exif.WithOrientation(photoJPEG(t, 1024, 768), 6), 768, 1024},
		{"partial.gif", photoGIF(t, 400, 300, image.Rect(100, 75, 300, 225)), 400, 300},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, _, calls := photoEnvOn(t, fakeslack.PhotoBaseCDN)
			ctx := context.Background()
			up, err := a.PreparePhoto(ctx, c.name, PhotoFileType(c.name), c.data)
			if err != nil {
				t.Fatalf("PreparePhoto: %v", err)
			}
			if up.Width != c.w || up.Height != c.h {
				t.Errorf("prepared image %d×%d, want %d×%d", up.Width, up.Height, c.w, c.h)
			}
			crop := cropPaneSquare(up.Width, up.Height)
			if err := a.SetPhoto(ctx, up.ID, crop); err != nil {
				t.Fatalf("SetPhoto %+v: %v", crop, err)
			}
			if want := cropPaneSquare(c.w, c.h); crop != want {
				t.Errorf("square %+v, want %+v", crop, want)
			}
			if len(calls.get("users.setPhoto")) != 1 {
				t.Error("users.setPhoto wasn't sent once")
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
	up, err := a.PreparePhoto(ctx, "me.png", "image/png", pwPNG(t, 600, 400))
	if err != nil || up.ID != "1234567890123" {
		t.Fatalf("PreparePhoto %q %v", up.ID, err)
	}
	if err := a.SetPhoto(ctx, up.ID, cropPaneSquare(up.Width, up.Height)); err != nil || setID != "1234567890123" {
		t.Fatalf("SetPhoto id %q %v", setID, err)
	}
}

// Slack's API queue (P8AB, 3GCR) tries a photo call up to 3 times: again
// after a 503, a 504, a dropped connection or internal_error, and not after
// a 500, another error code or ok:false ratelimited (the queue's dummy).
// Each retry is logged with what failed; the last failure as before.
func TestProfilePhotoRetriesLikeSlack(t *testing.T) {
	old := slack.RetryUnit
	slack.RetryUnit = time.Millisecond // the schedule's 60 s are 60 ms here
	t.Cleanup(func() { slack.RetryUnit = old })
	for _, c := range []struct {
		what, method string
		fault        fakeslack.Fault
		calls        int
		ok           bool
	}{
		{"503 once", "users.setPhoto", fakeslack.Fault{Status: 503, Times: 1}, 2, true},
		{"504 once", "users.deletePhoto", fakeslack.Fault{Status: 504, Times: 1}, 2, true},
		{"internal_error once", "users.setPhoto", fakeslack.Fault{Error: "internal_error", Times: 1}, 2, true},
		{"hang-up once", "users.preparePhoto", fakeslack.Fault{HangUp: true, Times: 1}, 2, true},
		{"503 always", "users.setPhoto", fakeslack.Fault{Status: 503}, 3, false},
		{"500", "users.setPhoto", fakeslack.Fault{Status: 500}, 1, false},
		{"bad_image", "users.setPhoto", fakeslack.Fault{Error: "bad_image"}, 1, false},
		{"ratelimited", "users.setPhoto", fakeslack.Fault{Error: "ratelimited"}, 1, false},
	} {
		t.Run(c.what, func(t *testing.T) {
			a, fs, _ := pwEnv(t)
			logs := capturePhotoLog(t)
			ctx := context.Background()
			var id string
			if c.method != "users.preparePhoto" {
				up, err := a.PreparePhoto(ctx, "me.png", "image/png", pwPNG(t, 600, 400))
				if err != nil {
					t.Fatalf("PreparePhoto: %v", err)
				}
				id = up.ID
			}
			fs.SetFault(c.method, c.fault)
			before := fs.CallCount(c.method)
			var err error
			switch c.method {
			case "users.preparePhoto":
				_, err = a.PreparePhoto(ctx, "me.png", "image/png", pwPNG(t, 600, 400))
			case "users.setPhoto":
				err = a.SetPhoto(ctx, id, cropPaneSquare(600, 400))
			default:
				err = a.DeletePhoto(ctx)
			}
			if (err == nil) != c.ok {
				t.Fatalf("error %v, want ok %v\nlog:\n%s", err, c.ok, logs.String())
			}
			if n := int(fs.CallCount(c.method) - before); n != c.calls {
				t.Errorf("%s sent %d times, want %d\nlog:\n%s", c.method, n, c.calls, logs.String())
			}
			retries := strings.Count(logs.String(), "slack: "+c.method+": ")
			if c.method == "users.preparePhoto" {
				retries = c.calls - 1 // a dead pooled connection is sent again at once, without a wait or a line
			}
			if retries != c.calls-1 {
				t.Errorf("%d retry lines for %d calls:\n%s", retries, c.calls, logs.String())
			}
			if !c.ok && !strings.Contains(logs.String(), "profile photo: "+c.method+" failed: ") {
				t.Errorf("no failure line:\n%s", logs.String())
			}
		})
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

// redirectTo answers requests for paths ending in from with a 302 to to,
// and sends the rest on to next.
type redirectTo struct {
	from, to string
	next     http.RoundTripper
}

func (r redirectTo) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(req.URL.Path, r.from) {
		return r.next.RoundTrip(req)
	}
	return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {r.to}},
		Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

// A failed photo call leaves a line in clack.log with the method, Slack's
// error code and the HTTP status, or what failed before an answer, and
// nothing it sent or got back: not the file's name, the prepared id or
// url, or a token.
func TestProfilePhotoFailuresLogged(t *testing.T) {
	const name = "secret-selfie.png"
	ctx := context.Background()
	for _, c := range []struct {
		what   string
		method string
		fault  fakeslack.Fault
		setup  func(t *testing.T, a *App, fs *fakeslack.Server)
		want   string
	}{
		{what: "prepare error", method: "users.preparePhoto", fault: fakeslack.Fault{Error: "too_large"},
			want: "profile photo: users.preparePhoto failed: error=too_large http=200"},
		{what: "prepare 500", method: "users.preparePhoto", fault: fakeslack.Fault{Status: 500},
			want: "profile photo: users.preparePhoto failed: http=500"},
		{what: "prepare not JSON", setup: func(t *testing.T, a *App, fs *fakeslack.Server) {
			fs.SetHook(func(method string, f url.Values) (any, bool) {
				if method == "users.preparePhoto" {
					return []byte("<html>be right back</html>"), true
				}
				return nil, false
			})
		}, want: "profile photo: users.preparePhoto failed: unexpected answer"},
		{what: "prepared image 404", setup: func(t *testing.T, a *App, fs *fakeslack.Server) {
			fs.SetHook(func(method string, f url.Values) (any, bool) {
				if method == "users.preparePhoto" {
					return map[string]any{"ok": true, "id": "1000000000042", "url": fs.URL + "missing/prepared-1000000000042.png"}, true
				}
				return nil, false
			})
		}, want: "profile photo: users.preparePhoto's image failed: http=404"},
		{what: "prepared image is a Slack page", setup: func(t *testing.T, a *App, fs *fakeslack.Server) {
			fs.SetPhotoBase(fakeslack.PhotoBaseFiles)
			t.Cleanup(fs.RouteSlackHosts(a.C))
			a.C.Creds.Cookie = "xoxd-expired" // files.slack.com sends it to sign-in
		}, want: "profile photo: users.preparePhoto's image failed: answered with a Slack page, not a file"},
		{what: "prepared image redirects off Slack", setup: func(t *testing.T, a *App, fs *fakeslack.Server) {
			fs.SetPhotoBase(fakeslack.PhotoBaseFiles)
			t.Cleanup(fs.RouteSlackHosts(a.C))
			a.C.HTTP.Transport = redirectTo{from: ".png", to: "https://bucket.example.net/prepared.png", next: a.C.HTTP.Transport}
		}, want: "profile photo: users.preparePhoto's image failed: redirect off Slack refused"},
		{what: "set error", method: "users.setPhoto", fault: fakeslack.Fault{Error: "bad_image"},
			want: "profile photo: users.setPhoto failed: error=bad_image http=200"},
		{what: "set 500", method: "users.setPhoto", fault: fakeslack.Fault{Status: 500},
			want: "profile photo: users.setPhoto failed: http=500"},
		{what: "delete error", method: "users.deletePhoto", fault: fakeslack.Fault{Error: "read_only_profile_field"},
			want: "profile photo: users.deletePhoto failed: error=read_only_profile_field http=200"},
	} {
		t.Run(c.what, func(t *testing.T) {
			a, fs, _ := pwEnv(t)
			logs := capturePhotoLog(t)
			var id, purl string
			if c.method == "users.setPhoto" || c.method == "users.deletePhoto" {
				up, err := a.PreparePhoto(ctx, name, "image/png", pwPNG(t, 600, 400))
				if err != nil {
					t.Fatalf("PreparePhoto: %v", err)
				}
				id, purl = up.ID, up.URL
			}
			if c.setup != nil {
				c.setup(t, a, fs)
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
				_, err = a.PreparePhoto(ctx, name, "image/png", pwPNG(t, 600, 400))
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
			for _, leak := range []string{name, "secret-selfie", fakeslack.TeamToken, fakeslack.OrgToken, "prepared-", "missing/",
				"xoxd", "files-tmb", "bucket.example"} {
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
