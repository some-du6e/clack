package slack

import (
	"net/http"
	"testing"
)

// The sign-in client's redirects carry the session Slack just vended, so a
// hop to /ssb/redirect is refused however the path is written: Go's
// ResolveReference removes only literal dot segments, so a Location of
// /x/%2e%2e/ssb/redirect arrives as /x/../ssb/redirect, and a doubled
// slash or a ";…" parameter hides the prefix from a plain comparison.
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
	} {
		if bannedPage(mustURL(t, u)) {
			t.Errorf("bannedPage(%s) = true, want false", u)
		}
		if err := hop(u); err != nil {
			t.Errorf("sign-in redirect to %s: %v (want followed)", u, err)
		}
	}
}
