package fakeslack

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // photoUploadForm's DecodeConfig
	_ "image/jpeg" // photoUploadForm's DecodeConfig
	"image/png"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/some-du6e/clack/internal/exif"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/slack/tlsfp"
)

// The profile photo's methods as the fake's Slack answers them, and the
// prepared images it serves. What Slack's client sends (web client build
// 2c0d02ca130f: eNqz processUpload and EditProfileCropPane, generated
// fetchers through apiCall):
//
//   - users.preparePhoto: a FormData body with the selected workspace's
//     token (computeToken, no enterprise_token: TeamToken here) and the
//     picked File as the "image" part, typed as Chromium types the file
//     by its extension (processUpload lets only image/gif, image/png and
//     image/jpeg through, 36 to 4000 px a side). Slack's body also carries
//     apiCall's metadata (_x_reason, _x_mode, _x_sonic, _x_app_name),
//     which Clack sends for no call. The fake checks the token, that
//     there's an image part that decodes as a GIF, PNG or JPEG, and its
//     size; it records the part's filename and Content-Type (PhotoFilenameKey,
//     PhotoTypeKey) for tests to compare with what a browser sends,
//     without refusing other types, as what Slack's server does with them
//     isn't known. It answers {id, url}: id a number's digits (Slack's
//     client parseInts the ids it gets back elsewhere), url the prepared
//     image. An upload that fits in PreparedPhotoMax as Chromium would
//     show it is kept as uploaded, its EXIF Orientation and a GIF's frames
//     included; a bigger one is fitted into it as a PNG, so the tests can
//     tell the prepared image from the picked file. Slack's own server
//     doesn't seem to shrink them: public avatars' _original images run
//     past 1024 px (1584×1584 and 1081×1080 from 2023), re-encoded (JPEGs
//     with no EXIF since then, PNGs with ImageMagick's tIME and tEXt).
//     What its prepared image is exactly isn't known (Clack never calls the
//     method against Slack); the crop pane measures whatever it gets.
//   - users.setPhoto {id, crop_x, crop_y, crop_w}: the same token; an id
//     preparePhoto answered (not_found otherwise); the crop, when sent,
//     whole numbers making a square inside the prepared image as Chromium
//     loads it (getLoadedImage(e.url)'s width and height: turned by EXIF,
//     a GIF's logical screen), which is what the crop pane picks it in. A
//     crop outside it answers invalid_arguments: Slack's own answer to one
//     has never been seen (Clack never calls the photo methods against
//     Slack), so that's the fake's stand-in, one of the method's
//     documented errors. No crop is the whole image.
//   - users.deletePhoto: the same token.
//
// A wrong token answers like Slack does for one: the org's
// enterprise_is_restricted (the method can't be called on the org),
// anything else invalid_auth.
//
// The prepared image's url is on the fake's own origin (/photos/) unless
// SetPhotoBase names one of Slack's hosts: then the fake answers as that host
// through HostsTransport (files.slack.com wants the session cookie and
// redirects to the workspace's sign-in page without it, as Slack does;
// avatars.slack-edge.com is a CDN; slack-imgs.com is Slack's image proxy,
// serving the image its url parameter names).

// PreparedPhotoMax is the longest side of the fake's prepared photos
// (users.setPhoto's docs ask for "a maximum size of 1024 by 1024 pixels"
// when the API is sent the image itself).
const PreparedPhotoMax = 1024

// The prepared images' bases for SetPhotoBase: Slack's avatar CDN, its file
// host (a thumbnail path), and a host of nobody's that Clack loads through
// Slack's image proxy.
const (
	PhotoBaseCDN   = "https://avatars.slack-edge.com/2026-11-17/"
	PhotoBaseFiles = "https://files.slack.com/files-tmb/T0FAKE000-F0PHOTO/"
	PhotoBaseOther = "https://photos.example.net/"
)

// PreparedPhotoSize is the size the fake's users.preparePhoto answers an
// upload shown at w×h with: fitted into PreparedPhotoMax, never scaled up.
func PreparedPhotoSize(w, h int) (int, int) {
	if w <= PreparedPhotoMax && h <= PreparedPhotoMax {
		return w, h
	}
	if w >= h {
		return PreparedPhotoMax, max(1, int(math.Round(float64(h)*PreparedPhotoMax/float64(w))))
	}
	return max(1, int(math.Round(float64(w)*PreparedPhotoMax/float64(h)))), PreparedPhotoMax
}

// preparedPhoto is one of users.preparePhoto's images: what its url
// serves, and the size Chromium loads it at, which setPhoto's crop counts
// in.
type preparedPhoto struct {
	name  string // the url's last path element
	data  []byte
	ctype string
	size  image.Point
}

// photoStore is the fake's prepared photos and what loaded them.
type photoStore struct {
	mu     sync.Mutex
	byID   map[string]*preparedPhoto
	byName map[string]*preparedPhoto
	nextID int64
	loads  []PhotoLoad
	base   string // SetPhotoBase's
}

var photoStores sync.Map // *Server -> *photoStore

func (s *Server) photos() *photoStore {
	v, _ := photoStores.LoadOrStore(s, &photoStore{byID: map[string]*preparedPhoto{}, byName: map[string]*preparedPhoto{}})
	return v.(*photoStore)
}

// PhotoLoad is a request the fake answered as one of Slack's hosts
// (HostsTransport): the URL it was for, its host, and whether it carried
// the session cookie.
type PhotoLoad struct {
	URL, Host string
	Cookie    bool
}

// PhotoLoads lists the requests for prepared images on Slack's hosts.
func (s *Server) PhotoLoads() []PhotoLoad {
	ps := s.photos()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return append([]PhotoLoad(nil), ps.loads...)
}

// SetPhotoBase makes users.preparePhoto's urls start with base (one of
// the PhotoBase consts, for a client routed with RouteSlackHosts); ""
// puts them back on the fake's own origin.
func (s *Server) SetPhotoBase(base string) {
	ps := s.photos()
	ps.mu.Lock()
	ps.base = base
	ps.mu.Unlock()
}

// photoBase is where prepared images' urls point.
func (s *Server) photoBase() string {
	ps := s.photos()
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if ps.base != "" {
		return ps.base
	}
	return s.URL + "photos/"
}

func (s *Server) photoWrite(method string, f url.Values) (any, bool) {
	fail := func(code string) (any, bool) { return map[string]any{"ok": false, "error": code}, true }
	switch method {
	case "users.preparePhoto", "users.setPhoto", "users.deletePhoto":
		switch f.Get("token") {
		case TeamToken:
		case OrgToken:
			return fail("enterprise_is_restricted")
		default:
			return fail("invalid_auth")
		}
	default:
		return nil, false
	}
	switch method {
	case "users.preparePhoto":
		data := []byte(f.Get(photoDataKey))
		if len(data) == 0 {
			return fail("no_image")
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || format != "gif" && format != "png" && format != "jpeg" {
			return fail("bad_image")
		}
		w, h := cfg.Width, cfg.Height
		if format == "jpeg" && exif.Swaps(exif.Orientation(data)) {
			w, h = h, w
		}
		if w > 4000 || h > 4000 {
			return fail("too_large")
		}
		p := &preparedPhoto{data: data, ctype: "image/" + format, size: image.Pt(w, h)}
		ext := map[string]string{"gif": "gif", "png": "png", "jpeg": "jpg"}[format]
		if pw, ph := PreparedPhotoSize(w, h); pw != w || ph != h {
			p.data, p.ctype, p.size, ext = genPNG(pw, ph, 7), "image/png", image.Pt(pw, ph), "png"
		}
		ps := s.photos()
		ps.mu.Lock()
		ps.nextID++
		id := strconv.FormatInt(1000000000000+ps.nextID, 10)
		p.name = fmt.Sprintf("%s_%016x_original.%s", id, uint64(ps.nextID)*0x9e3779b97f4a7c15, ext)
		ps.byID[id], ps.byName[p.name] = p, p
		ps.mu.Unlock()
		return ok("id", id, "url", s.photoBase()+p.name), true
	case "users.setPhoto":
		ps := s.photos()
		ps.mu.Lock()
		p := ps.byID[f.Get("id")]
		ps.mu.Unlock()
		if p == nil {
			return fail("not_found")
		}
		if f.Has("crop_x") || f.Has("crop_y") || f.Has("crop_w") {
			x, ex := strconv.Atoi(f.Get("crop_x"))
			y, ey := strconv.Atoi(f.Get("crop_y"))
			cw, ew := strconv.Atoi(f.Get("crop_w"))
			if ex != nil || ey != nil || ew != nil || x < 0 || y < 0 || cw < 1 || x+cw > p.size.X || y+cw > p.size.Y {
				return fail("invalid_arguments")
			}
		}
		prof := s.profile()
		prof.mu.Lock()
		defer prof.mu.Unlock()
		s.setSelfPhoto(true)
		return ok("profile", s.users[0].Profile), true
	default: // users.deletePhoto
		prof := s.profile()
		prof.mu.Lock()
		s.setSelfPhoto(false)
		prof.mu.Unlock()
		return ok(), true
	}
}

// genPNG is a w×h gradient PNG (the /img/ server's pictures).
func genPNG(w, h, seed int) []byte {
	im := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			im.SetNRGBA(x, y, color.NRGBA{uint8(x*255/w + seed), uint8(y*255/h + seed>>3), uint8(seed >> 6), 255})
		}
	}
	var buf bytes.Buffer
	_ = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, im)
	return buf.Bytes()
}

// servePhoto answers a prepared image by its name ("" when it's no prepared
// image's: false).
func (s *Server) servePhoto(w http.ResponseWriter, name string) bool {
	ps := s.photos()
	ps.mu.Lock()
	p := ps.byName[name]
	ps.mu.Unlock()
	if p == nil {
		return false
	}
	w.Header().Set("Content-Type", p.ctype)
	w.Write(p.data)
	return true
}

// photosPage serves /photos/<name>: prepared images on the fake's origin.
func (s *Server) photosPage(w http.ResponseWriter, r *http.Request) {
	if !s.servePhoto(w, path.Base(r.URL.Path)) {
		http.NotFound(w, r)
	}
}

// hostHeader carries the host a HostsTransport request was for.
const hostHeader = "X-Fakeslack-Host"

// asSlackHost answers a request HostsTransport sent for one of Slack's
// hosts; false for a request that's the fake's own.
func (s *Server) asSlackHost(w http.ResponseWriter, r *http.Request) bool {
	host := r.Header.Get(hostHeader)
	if host == "" {
		return false
	}
	cookie := strings.Contains(r.Header.Get("Cookie"), "d=xoxd-fake")
	ps := s.photos()
	ps.mu.Lock()
	ps.loads = append(ps.loads, PhotoLoad{URL: "https://" + host + r.URL.RequestURI(), Host: host, Cookie: cookie})
	ps.mu.Unlock()
	if s.serveSystemDMAsset(w, r, host) { // system_dm.go
		return true
	}
	name := path.Base(r.URL.Path)
	switch {
	case host == "files.slack.com":
		if !cookie { // Slack sends a file request without the session to sign-in
			http.Redirect(w, r, "https://fakeclub.slack.com/?redir="+url.QueryEscape(r.URL.Path), http.StatusFound)
			return true
		}
	case host == "slack-imgs.com":
		u, err := url.Parse(r.URL.Query().Get("url"))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			http.NotFound(w, r)
			return true
		}
		// the proxy fetches the original, http or https: one of the
		// fake's /img/ pictures (ProxiedImageURL) on whatever host it
		// names
		if strings.HasPrefix(u.Path, "/img/") {
			r2 := r.Clone(r.Context())
			r2.URL.Path, r2.URL.RawQuery = u.Path, ""
			s.img(w, r2)
			return true
		}
		name = path.Base(u.Path)
	case strings.HasSuffix(host, ".slack-edge.com"):
	default: // nobody's host: Clack must not load it directly
		http.NotFound(w, r)
		return true
	}
	if !s.servePhoto(w, name) {
		http.NotFound(w, r)
	}
	return true
}

// ProxiedImageURL is a w×h picture of the fake's (its /img/ server's) on
// a third-party origin ("http://icons.example.net"): Clack must load it
// the way Slack's client does, through the image proxy, which serves it
// under HostsTransport (RouteSlackHosts); the origin itself answers 404.
func ProxiedImageURL(origin string, w, h int, seed string) string {
	return fmt.Sprintf("%s/img/%dx%d/%s.png", strings.TrimSuffix(origin, "/"), w, h, seed)
}

// HostsTransport sends every request to the fake: one for the fake's own
// address as it is, any other over plain http to the fake's address with
// the host it was for in a header, for the fake to answer as that host
// (asSlackHost). next sends them (nil: tlsfp.Mixed). Tests use it
// to load Slack's production URLs (files.slack.com, avatars.slack-edge.com,
// slack-imgs.com) from the fake, with every rule the client applies to
// those URLs before they go out (RouteSlackHosts).
func (s *Server) HostsTransport(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = tlsfp.Mixed
	}
	u, _ := url.Parse(s.URL)
	return &hostsTransport{fake: u, next: next}
}

type hostsTransport struct {
	fake *url.URL
	next http.RoundTripper
}

func (t *hostsTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == t.fake.Host {
		return t.next.RoundTrip(r)
	}
	r2 := r.Clone(r.Context())
	r2.Header.Set(hostHeader, r.URL.Hostname())
	r2.URL.Scheme, r2.URL.Host, r2.Host = t.fake.Scheme, t.fake.Host, t.fake.Host
	return t.next.RoundTrip(r2)
}

// RouteSlackHosts sends c's requests, and the fetches without credentials
// (slack.PublicHTTP), to the fake through HostsTransport, and returns the
// undo: t.Cleanup(fs.RouteSlackHosts(a.C)). PublicHTTP's own transport
// dials public addresses only, so its requests go out on tlsfp.Mixed while
// routed.
func (s *Server) RouteSlackHosts(c *slack.Client) func() {
	prevC, prevPub := c.HTTP.Transport, slack.PublicHTTP.Transport
	c.HTTP.Transport = s.HostsTransport(prevC)
	slack.PublicHTTP.Transport = s.HostsTransport(nil)
	return func() {
		c.HTTP.Transport = prevC
		slack.PublicHTTP.Transport = prevPub
	}
}

// What the fake reads from a users.preparePhoto upload's image part (the
// multipart body isn't a form): its bytes, their count, its filename and
// its Content-Type.
const (
	photoDataKey  = "_fake_image_data"
	photoBytesKey = "_fake_image_bytes"
	photoNameKey  = "_fake_image_filename"
	photoTypeKey  = "_fake_image_type"
)

// The fake's form keys for a users.preparePhoto image part, for tests.
const (
	PhotoFilenameKey = photoNameKey
	PhotoTypeKey     = photoTypeKey
	PhotoBytesKey    = photoBytesKey
)

// photoUploadForm reads a users.preparePhoto multipart body: its text
// fields (token first, as Slack's client sends it) and the image part.
func photoUploadForm(contentType string, body []byte) url.Values {
	form := url.Values{}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return form
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		p, err := mr.NextPart()
		if err != nil {
			return form
		}
		b, _ := io.ReadAll(p)
		if p.FormName() != "image" {
			form.Add(p.FormName(), string(b))
			continue
		}
		if len(b) == 0 {
			continue
		}
		form.Set(photoDataKey, string(b))
		form.Set(photoBytesKey, strconv.Itoa(len(b)))
		form.Set(photoNameKey, p.FileName())
		form.Set(photoTypeKey, p.Header.Get("Content-Type"))
	}
}
