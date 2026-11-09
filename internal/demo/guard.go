package demo

import (
	"os"

	"github.com/some-du6e/yoloslackgoclient/internal/slack/tlsfp"
)

// LockNetwork makes the whole process loopback-only, for good
// (tlsfp.LoopbackOnly): package tlsfp's dialers and transports (every
// Slack client, the realtime socket, the plugin and image fetchers) and
// Go's default transport refuse any host that isn't localhost or a
// loopback address, and no proxy is used. `clack demo` calls it before
// anything else, so the demo cannot reach Slack (or anything else)
// whatever a bug elsewhere asks for.
func LockNetwork() {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		os.Unsetenv(k)
	}
	tlsfp.LoopbackOnly()
}

func loopbackHost(h string) bool { return tlsfp.LoopbackHost(h) }
