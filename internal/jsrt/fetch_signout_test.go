package jsrt

import "testing"

// No plugin fetch may reach Slack's sign-out or revoke methods, however the
// URL is spelled.
func TestFetchRefusesSignOut(t *testing.T) {
	for _, u := range []string{
		"https://hackclub.slack.com/api/auth.signout",
		"https://hackclub.slack.com/api/AUTH.SIGNOUT",
		"https://hackclub.slack.com/api/auth.signout?x=1",
		"https://hackclub.slack.com/api/auth.signout/",
		"https://hackclub.slack.com/api/auth.signout;x",
		"https://hackclub.slack.com/x/../api/auth.signout",
		"https://hackclub.slack.com/api/auth.signout%3Fx",
		"https://slack.com/api/auth.revoke",
		"https://hackclub.enterprise.slack.com/api/auth.enterpriseSignout",
		"https://hackclub.enterprise.slack.com/api/enterpriseSignout",
	} {
		if err := fetchURLAllowed(u); err == nil {
			t.Errorf("fetchURLAllowed(%q) = nil, want refused", u)
		}
	}
	for _, u := range []string{
		"https://hackclub.slack.com/api/auth.test",
		"https://hackclub.slack.com/api/conversations.history",
		"https://example.com/signout",
		"https://example.com/",
	} {
		if err := fetchURLAllowed(u); err != nil {
			t.Errorf("fetchURLAllowed(%q) = %v, want allowed", u, err)
		}
	}
}
