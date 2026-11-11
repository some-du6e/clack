package slack

import (
	"strings"
	"testing"
)

// Synthetic credentials only: the right shape, nobody's session.
const (
	testXoxc    = "xoxc-1111111111-2222222222-3333333333-aaaabbbbccccdddd"
	testXoxcOrg = "xoxc-9999999999-8888888888-7777777777-eeeeffff00001111"
	testXoxd    = "xoxd-AbC%2Fdef%2BGhI%3D%3D"
	testTeams   = `{"teams":{"T0AAA":{"id":"T0AAA","name":"Other","url":"https://other.slack.com/","token":"` + testXoxcOrg + `"},` +
		`"T0BBB":{"id":"T0BBB","name":"Hack Club","url":"https://hackclub.slack.com/","token":"` + testXoxc + `"},` +
		`"E0CCC":{"id":"E0CCC","name":"Hack Club Org","url":"https://hackclub.enterprise.slack.com/","token":"` + testXoxcOrg + `"}}}`
)

func TestClassifyPaste(t *testing.T) {
	cases := []struct {
		name, in string
		want     Paste
	}{
		{"link", " slack://T08PSQ7BQ/magic-login/abc123\n", Paste{Kind: PasteLink, Link: "slack://T08PSQ7BQ/magic-login/abc123"}},
		{"login-v2", "slack://login-v2/?0.host=slack.com&0.tokens=aaa_bbb", Paste{Kind: PasteLink, Link: "slack://login-v2/?0.host=slack.com&0.tokens=aaa_bbb"}},
		{"teams JSON picks the matching team and the org", testTeams, Paste{Kind: PasteTeams, Workspace: "https://hackclub.slack.com/", Token: testXoxc, OrgToken: testXoxcOrg, OrgURL: "https://hackclub.enterprise.slack.com/"}},
		{"teams JSON, pretty-printed", strings.ReplaceAll(testTeams, ",", ",\n  "), Paste{Kind: PasteTeams, Workspace: "https://hackclub.slack.com/", Token: testXoxc, OrgToken: testXoxcOrg, OrgURL: "https://hackclub.enterprise.slack.com/"}},
		{"bare token", "  " + testXoxc + " ", Paste{Kind: PasteToken, Token: testXoxc}},
		{"bare d", testXoxd, Paste{Kind: PasteCookie, Cookie: testXoxd}},
		{"d=", "d=" + testXoxd, Paste{Kind: PasteCookie, Cookie: testXoxd}},
		{"Cookie header", "b=abc; d=" + testXoxd + "; d-s=1794000000; lc=1", Paste{Kind: PasteCookie, Cookie: testXoxd}},
		{"DevTools row", "d\t" + testXoxd + "\t.slack.com\t/\t2027-01-01T00:00:00.000Z\t84\t✓\t✓\tLax", Paste{Kind: PasteCookie, Cookie: testXoxd}},
		// junk: left alone
		{"empty", "   ", Paste{}},
		{"a message", "a message I copied, https://example.com", Paste{}},
		{"not slack JSON", `{"hello":"world"}`, Paste{}},
		{"broken JSON", `{"teams":{`, Paste{}},
		{"teams without a workspace token", `{"teams":{"E0CCC":{"id":"E0CCC","url":"https://x.enterprise.slack.com/","token":"` + testXoxcOrg + `"}}}`, Paste{}},
		{"short token", "xoxc-123", Paste{}},
		{"token with junk", testXoxc + "<script>", Paste{}},
		{"bot token", "xox" + "b-1111111111-2222222222-aaaabbbbccccdddd", Paste{}}, // split: push protection reads it as a real one
		{"d-s only", "d-s=1794000000", Paste{}},
		{"d with junk", "d=xoxd-abc\"def<ghijklmnopqrstu", Paste{}},
		{"another site's link", "https://slack.com/magic-login/abc", Paste{}},
		{"too long", strings.Repeat("x", MaxPaste+1), Paste{}},
	}
	for _, c := range cases {
		if got := ClassifyPaste(c.in, "hackclub"); got != c.want {
			t.Errorf("%s: ClassifyPaste = %+v, want %+v", c.name, got, c.want)
		}
	}
	// the workspace picks the team
	if got := ClassifyPaste(testTeams, "other"); got.Token != testXoxcOrg || got.Workspace != "https://other.slack.com/" {
		t.Errorf("workspace other: %+v", got)
	}
}
