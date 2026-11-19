package slack

import (
	"net/http"
	"testing"
)

// The sign-in client's redirects carry the session Slack just vended, so a
// hop to /ssb/redirect is refused however the path is written: Go's
// ResolveReference removes only literal dot segments, so a Location of
// /x/%2e%2e/ssb/redirect arrives as /x/../ssb/redirect, and a doubled
// slash or a ";…" parameter hides the prefix from a plain comparison, as
// do encodings a server decodes twice and look-alikes NFKC folds.
func TestBannedPageNormalises(t *testing.T) {
	hop := func(target string) error {
		return magicRedirect(&http.Request{URL: mustURL(t, target), Header: http.Header{}}, nil)
	}
	for _, u := range []string{
		"https://hackclub.slack.com/ssb/redirect",
		"https://hackclub.slack.com/ssb/redirect/",
		"https://hackclub.slack.com/SSB/Redirect?x=1",
		"https://hackclub.slack.com/x/../ssb/redirect",
		"https://hackclub.slack.com/x/%2e%2e/ssb/redirect",
		"https://hackclub.slack.com/ssb/%2e/redirect",
		"https://hackclub.slack.com/ssb/%2fredirect",
		"https://hackclub.slack.com/ssb//redirect",
		"https://hackclub.slack.com//ssb/redirect",
		"https://hackclub.slack.com/ssb;x/redirect",
		"https://hackclub.slack.com/ssb/redirect;jsessionid=1",
		// the page once a server decodes twice or three times (a slash, an
		// "r", an "s"), or applies NFKC (a fullwidth slash or backslash,
		// fullwidth letters)
		"https://hackclub.slack.com/ssb/%252fredirect",
		"https://hackclub.slack.com/ssb%252fredirect",
		"https://hackclub.slack.com/ssb/%2572edirect",
		"https://hackclub.slack.com/%2573sb/redirect",
		"https://hackclub.slack.com/%252573sb/redirect",
		"https://hackclub.slack.com/ssb%EF%BC%8Fredirect",
		"https://hackclub.slack.com/ssb%EF%BC%BCredirect",
		"https://hackclub.slack.com/%EF%BD%93%EF%BD%93%EF%BD%82/redirect",
	} {
		if !bannedPage(mustURL(t, u)) {
			t.Errorf("bannedPage(%s) = false, want true", u)
		}
		if hop(u) == nil {
			t.Errorf("sign-in redirect to %s followed", u)
		}
	}
	for _, u := range []string{
		"https://hackclub.slack.com/",
		"https://hackclub.slack.com/checkcookie?redir=%2Fssb%2Fredirect",
		"https://app.slack.com/client/T1/C1",
		"https://slack.com/signin/find",
		"https://hackclub.slack.com/ssb/signin_redirect",
		"https://hackclub.slack.com/ssb/signin",
	} {
		if bannedPage(mustURL(t, u)) {
			t.Errorf("bannedPage(%s) = true, want false", u)
		}
		if err := hop(u); err != nil {
			t.Errorf("sign-in redirect to %s: %v (want followed)", u, err)
		}
	}
}
