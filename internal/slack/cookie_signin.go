package slack

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrCookieRejected: Slack didn't take the pasted d cookie (its token page
// had no tokens for it, or the token it gave didn't pass auth.test).
var ErrCookieRejected = errors.New("Slack didn't accept that d cookie")

// signInCookie signs in with the browser's d cookie alone (Advanced, and
// a pasted xoxd-… or d=xoxd-…). It reuses the desktop sign-in's own token
// step: one GET of Slack's token page, authPage, with only "d=<cookie>"
// (no jar: d-s, lc and whatever else Slack sets are neither kept nor sent,
// as for every pasted session), then one auth.test with the workspace
// token on the workspace host. The org (E) token from the page is kept
// but never sent to auth.test. Both requests go through magicClient: the
// API transport (tlsfp's recorded OpenSSL ClientHello, HTTP/1.1, never Go's
// crypto/tls hello) and magicRedirect, which keeps redirects on https
// slack.com and refuses /ssb/redirect.
//
// ws ("hackclub.slack.com") picks the workspace when the session has
// several; if none matches, the page's first workspace is used. The session
// is still the browser's own (docs/SECURITY.md C-16): Slack can end it
// everywhere, as with a pasted token.
func signInCookie(ctx context.Context, ws, ck string) (Creds, error) {
	sess := &magicSession{hc: magicClient(nil), cookie: ck}
	pt, err := sess.bootTokensFor(ctx, "", strings.ToLower(ws))
	if err != nil {
		if errors.Is(err, errNoPageTokens) {
			err = &kindError{fmt.Errorf("Slack didn't accept that d cookie: %w. Copy d again from a tab where you're signed in", err), ErrCookieRejected}
		}
		return Creds{}, err
	}
	teamURL := pt.TeamURL
	if teamURL == "" {
		teamURL = "https://" + ws + "/"
	}
	if !CookieURL(teamURL) {
		return Creds{}, fmt.Errorf("refusing to sign in to %q: not a slack.com address", Redact(teamURL))
	}
	a, err := sess.authTest(ctx, teamURL, pt.Team[0])
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return Creds{}, &kindError{fmt.Errorf("couldn't reach Slack to check the token: %w", err), ErrSlackUnreachable}
		}
		return Creds{}, &kindError{fmt.Errorf("Slack didn't accept the token its page gave for that d cookie: %w", err), ErrCookieRejected}
	}
	if strings.HasPrefix(a.TeamID, "E") {
		return Creds{}, errors.New("Slack's token page gave the org token where the workspace token should be; sign in to a workspace in the browser and copy d again")
	}
	c := Creds{Token: pt.Team[0], Cookie: ck, TeamURL: teamURL, Team: a.TeamID, UserID: a.UserID, EnterpriseID: a.EnterpriseID}
	if slackURL(a.URL, "https") {
		c.TeamURL = a.URL
	}
	if pt.Org != "" && a.EnterpriseID != "" && pt.OrgURL != "" {
		c.EnterpriseToken, c.EnterpriseURL = pt.Org, pt.OrgURL
	}
	return c, nil
}
