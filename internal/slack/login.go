package slack

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrLoginUnavailable is returned by Login: the old flow (loading the
// workspace's /ssb/redirect page and scraping tokens from it) was removed
// because it can end the browser session it was copied from.
var ErrLoginUnavailable = errors.New("cookie-only sign-in was removed because it could sign you out of Slack; sign in with your token and d cookie instead")

// Login is kept for old callers and never contacts Slack.
func Login(ctx context.Context, workspace, cookie string) (Creds, error) {
	return Creds{}, ErrLoginUnavailable
}

// SignInInput is what the user pastes from their own signed-in Slack session.
type SignInInput struct {
	Workspace string // "hackclub", "hackclub.slack.com" or a URL
	Token     string // xoxc-… workspace token
	OrgToken  string // optional xoxc-… org (Enterprise Grid) token
	Cookie    string // value of the d cookie (xoxd-…)
	CookieDS  string // optional value of the d-s cookie
}

// SignIn validates pasted credentials with auth.test only. It never loads
// web pages and never calls any sign-out method, so the browser session the
// values came from is left alone.
func SignIn(ctx context.Context, in SignInInput) (Creds, error) {
	ws := strings.TrimSpace(in.Workspace)
	ws = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(ws, "https://"), "http://"), "/")
	if ws == "" {
		return Creds{}, errors.New("enter your workspace, e.g. hackclub")
	}
	if i := strings.IndexAny(ws, "/?#"); i >= 0 {
		ws = ws[:i] // a pasted https://hackclub.slack.com/client/… URL
	}
	if !strings.Contains(ws, ".") {
		ws += ".slack.com"
	}
	// The token and cookie are sent to this host: Slack's own only.
	if h, err := url.Parse("https://" + ws + "/"); err != nil || h.Host != ws || !IsSlackHost(ws) {
		return Creds{}, errors.New("the workspace should be a slack.com address, e.g. hackclub or hackclub.slack.com")
	}
	tok := strings.TrimSpace(in.Token)
	if !strings.HasPrefix(tok, "xoxc-") {
		return Creds{}, errors.New("the token should start with xoxc-")
	}
	ck := cleanCookie(in.Cookie, "d")
	if !strings.HasPrefix(ck, "xoxd-") && !strings.HasPrefix(ck, "xoxd%") {
		return Creds{}, errors.New("the d cookie should start with xoxd-")
	}
	c := Creds{Token: tok, Cookie: ck, CookieDS: cleanCookie(in.CookieDS, "d-s"), TeamURL: "https://" + ws + "/"}
	a, err := New(c).AuthTest(ctx)
	if err != nil {
		return Creds{}, fmt.Errorf("Slack rejected the token and cookie: %w", err)
	}
	if strings.HasPrefix(a.TeamID, "E") {
		return Creds{}, errors.New("that is the org token; paste the workspace token in Token and this one in Org token")
	}
	c.Team, c.UserID, c.EnterpriseID = a.TeamID, a.UserID, a.EnterpriseID
	if slackURL(a.URL, "https") {
		c.TeamURL = a.URL
	}
	if org := strings.TrimSpace(in.OrgToken); org != "" {
		if !strings.HasPrefix(org, "xoxc-") {
			return Creds{}, errors.New("the org token should start with xoxc-")
		}
		oc := c
		oc.Token = org
		if a.EnterpriseID != "" {
			oc.TeamURL = c.TeamURL // auth.test answers on any host for the org token
		}
		oa, err := New(oc).AuthTest(ctx)
		if err != nil {
			return Creds{}, fmt.Errorf("Slack rejected the org token: %w", err)
		}
		c.EnterpriseToken = org
		c.EnterpriseID = oa.TeamID
		if slackURL(oa.URL, "https") {
			c.EnterpriseURL = oa.URL
		}
	}
	return c, nil
}

// cleanCookie accepts a bare value, "name=value", or a pasted cookie row, and
// returns the URL-encoded value as Slack expects it in the Cookie header.
func cleanCookie(v, name string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, name+"=")
	if i := strings.IndexAny(v, " \t;"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return ""
	}
	if !strings.Contains(v, "%") {
		v = url.QueryEscape(v)
	}
	return v
}
