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

// HarnessLoopback reports whether this is a perfharness build, which
// treats the loopback fake as Slack (the image cache's disk tier keeps its
// images, so the perf workflow's warm starts measure it).
func HarnessLoopback() bool { return harnessLoopback }

// IsHarnessURL reports whether raw is a loopback URL a perfharness build
// accepts as Slack's (false in every other build).
func IsHarnessURL(raw string) bool {
	if !harnessLoopback {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && harnessURL(u, "https")
}
