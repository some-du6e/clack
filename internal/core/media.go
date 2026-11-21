package core

import (
	"context"
	"fmt"
	"io"

	"github.com/some-du6e/clack/internal/slack"
)

// OpenRange streams a file from off for a video player (internal/media's
// Source.Open): Range: bytes=off- with the session's cookie through
// slack.Client.DownloadRange, so the connection replays the recorded TLS
// hello and keeps Download's guards (no Slack page, no redirect off
// Slack, no page answered for a file). The bytes go only to the player's
// memory (media.RangeSource), never to disk. Only a URL Download would
// fetch with credentials is opened: Slack's file hosts and paths.
func (a *App) OpenRange(ctx context.Context, rawURL string, off int64) (io.ReadCloser, slack.RangeInfo, error) {
	if !a.C.CredentialsOK(rawURL) {
		return nil, slack.RangeInfo{Total: -1}, fmt.Errorf("core: OpenRange refuses %q", slack.Redact(rawURL))
	}
	resp, info, err := a.C.DownloadRange(ctx, rawURL, off, -1)
	if err != nil {
		return nil, info, err
	}
	return resp.Body, info, nil
}
