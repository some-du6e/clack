package demo

import (
	"context"
	"net"
	"net/http"
	"os"

	"github.com/some-du6e/yoloslackgoclient/internal/slack/tlsfp"
)

// LockNetwork makes the whole process loopback-only, for good: package
// tlsfp's dialers and transports (every Slack client, the plugin and
// image fetchers) and Go's default transport refuse any host that isn't
// localhost or a loopback address, and no proxy is used. `clack demo`
// calls it before anything else, so the demo cannot reach Slack (or
// anything else) whatever a bug elsewhere asks for.
func LockNetwork() {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		os.Unsetenv(k)
	}
	tlsfp.LoopbackOnly()
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		t.Proxy = nil
		dial := t.DialContext
		if dial == nil {
			dial = (&net.Dialer{}).DialContext
		}
		t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if err := tlsfp.CheckAddr(addr); err != nil {
				return nil, err
			}
			return dial(ctx, network, addr)
		}
		t.DialTLSContext = nil
	}
}

func loopbackHost(h string) bool { return tlsfp.LoopbackHost(h) }
