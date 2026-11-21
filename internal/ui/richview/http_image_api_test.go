package richview

import (
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/slack"
)

// sessionFetch is the image fetcher cmd/clack gives a signed-in session's
// image cache (imgcache.SessionFetch).
func sessionFetch(c *slack.Client, authed imgcache.Fetcher) imgcache.Fetcher {
	return imgcache.SessionFetch(c.CredentialsOK, authed)
}

// imgBare reports whether nothing draws under im while it loads or once
// it failed.
func imgBare(im imgPrim) bool { return im.bare }
