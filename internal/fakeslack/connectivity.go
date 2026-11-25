package fakeslack

import (
	"net/http"
	"time"
)

// The two requests Slack's client makes to learn it's back online
// (internal/slack/connectivity.go), for clients routed with
// RouteSlackHosts: the CDN's test pixel on a. and b.slack-edge.com, and
// api.test on www.slack.com. SetFault("test_cdn_pixel", …) and
// SetFault("api.test", …) fail them; CallCount counts them.

// cdnPixelPath is getCDNConnectivityTests' path.
const cdnPixelPath = "/f70ec/img/test_cdn_pixel.gif"

// pixelGIF is a 1×1 transparent GIF.
var pixelGIF = []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")

// serveConnectivity answers the pixel and api.test; false for anything
// else.
func (s *Server) serveConnectivity(w http.ResponseWriter, r *http.Request, host string) bool {
	var method string
	switch {
	case (host == "a.slack-edge.com" || host == "b.slack-edge.com") && r.URL.Path == cdnPixelPath && r.Method == http.MethodGet:
		method = "test_cdn_pixel"
	case host == "www.slack.com" && r.URL.Path == "/api/api.test" && r.Method == http.MethodPost:
		method = "api.test"
	default:
		return false
	}
	s.count(method)
	if f, ok := s.takeFault(method); ok {
		if f.Delay > 0 {
			select {
			case <-time.After(f.Delay):
			case <-r.Context().Done():
				return true
			}
		}
		if s.applyFault(w, f) {
			return true
		}
	}
	if method == "api.test" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
		return true
	}
	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(pixelGIF)
	return true
}
