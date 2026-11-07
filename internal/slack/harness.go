package slack

import (
	"net"
	"net/url"
)

// harnessLoopback lets the Slack-host checks (credentials, gateway URLs)
// accept loopback addresses, for internal/fakeslack. Only perfharness
// builds set it (perfharness.go, build tag perfharness: the perf
// workflow's benchmarks and its process test); release builds never do.
var harnessLoopback bool

// harnessURL: a loopback host with the scheme or its plain counterpart
// (http for https, ws for wss).
func harnessURL(u *url.URL, scheme string) bool {
	plain := map[string]string{"https": "http", "wss": "ws"}[scheme]
	if u.Scheme != scheme && u.Scheme != plain {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
