package slack

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	tokenRe  = regexp.MustCompile(`"api_token":"(xoxc-[0-9A-Za-z-]+)"`)
	anyTokRe = regexp.MustCompile(`xoxc-[0-9]+-[0-9]+-[0-9]+-[0-9a-f]{20,}`)
)

// Login derives tokens from a d cookie: it loads the workspace's
// /ssb/redirect page (which embeds both team and org tokens), then
// identifies them with auth.test.
func Login(ctx context.Context, workspace, cookie string) (Creds, error) {
	workspace = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(workspace, "https://"), "http://"), "/")
	if !strings.Contains(workspace, ".") {
		workspace += ".slack.com"
	}
	if !strings.Contains(cookie, "%") {
		cookie = url.QueryEscape(cookie)
	}
	c := Creds{Cookie: cookie, TeamURL: "https://" + workspace + "/"}
	cl := New(c)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.TeamURL+"ssb/redirect", nil)
	req.Header.Set("Cookie", "d="+cookie)
	req.Header.Set("User-Agent", UserAgent)
	resp, err := cl.HTTP.Do(req)
	if err != nil {
		return c, err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	seen := map[string]bool{}
	var toks []string
	for _, m := range tokenRe.FindAllSubmatch(body, -1) {
		if t := string(m[1]); !seen[t] {
			seen[t] = true
			toks = append(toks, t)
		}
	}
	for _, m := range anyTokRe.FindAll(body, -1) {
		if t := string(m); !seen[t] {
			seen[t] = true
			toks = append(toks, t)
		}
	}
	if len(toks) == 0 {
		return c, fmt.Errorf("no token found: is the d cookie valid and are you signed in to %s?", workspace)
	}
	for _, t := range toks {
		probe := New(Creds{Token: t, Cookie: cookie, TeamURL: c.TeamURL})
		a, err := probe.AuthTest(ctx)
		if err != nil {
			continue
		}
		if strings.HasPrefix(a.TeamID, "E") {
			c.EnterpriseToken = t
			c.EnterpriseID = a.TeamID
			c.EnterpriseURL = a.URL
		} else {
			c.Token = t
			c.Team = a.TeamID
			c.UserID = a.UserID
			c.TeamURL = a.URL
			if a.EnterpriseID != "" {
				c.EnterpriseID = a.EnterpriseID
			}
		}
	}
	if c.Token == "" {
		return c, fmt.Errorf("found %d token(s) but none authenticated for the workspace", len(toks))
	}
	return c, nil
}
