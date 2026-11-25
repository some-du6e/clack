package core

import (
	"context"
	"testing"
	"time"
)

// scaleConnWaits makes the connectivity waits' seconds f of a second (the
// red tree's shim leaves them alone).
func scaleConnWaits(t *testing.T, f float64) {
	prev := connRetryScale
	connRetryScale = f
	t.Cleanup(func() { connRetryScale = prev })
}

// stubNetworkProbe answers the network wait's pixel loads at once
// (online), so a test over an unrouted fake never fetches the real CDN.
func stubNetworkProbe(t *testing.T) {
	prev := cdnProbe
	cdnProbe = func(context.Context) (time.Duration, error) { return 0, nil }
	t.Cleanup(func() { cdnProbe = prev })
}
