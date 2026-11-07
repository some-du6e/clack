package slack

import (
	"context"
	"errors"
)

// ErrLoginUnavailable is returned by Login: the old flow (loading the
// workspace's /ssb/redirect page and scraping tokens from it) was removed
// because it can end the browser session it was copied from.
var ErrLoginUnavailable = errors.New("cookie sign-in has been removed because it could sign you out of Slack; a safer sign-in is coming")

// Login is kept for callers but no longer contacts Slack.
func Login(ctx context.Context, workspace, cookie string) (Creds, error) {
	return Creds{}, ErrLoginUnavailable
}
