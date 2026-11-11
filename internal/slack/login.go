package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
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
	OrgURL    string // the org's https://….enterprise.slack.com/ (from the pasted JSON)
	Cookie    string // value of the d cookie (xoxd-…)
	CookieDS  string // ignored: d-s is never kept or sent (Client.cookieHeader)
	// Link, when set, is a slack:// magic-login link: callers sign in with
	// LoginMagic instead of SignIn (no token or cookie is pasted).
	Link string
}

// SignIn validates pasted credentials with auth.test only, and never calls
// any sign-out method. With a token it loads no web page. With only the d
// cookie it first loads Slack's token page for that cookie (signInCookie),
// the one page Clack loads with a cookie, as the desktop sign-in does.
func SignIn(ctx context.Context, in SignInInput) (Creds, error) {
	if err := expandPastedTeams(&in); err != nil {
		return Creds{}, err
	}
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
	// The token and cookie are about to be sent here: only slack.com hosts.
	if h, err := url.Parse("https://" + ws + "/"); err != nil || h.Host != ws || !CookieURL("https://"+ws+"/") {
		return Creds{}, errors.New("the workspace should be a slack.com address, e.g. hackclub or hackclub.slack.com")
	}
	tok := strings.TrimSpace(in.Token)
	ck := cleanCookie(in.Cookie, "d")
	if !strings.HasPrefix(ck, "xoxd-") && !strings.HasPrefix(ck, "xoxd%") {
		return Creds{}, errors.New("the d cookie should start with xoxd-")
	}
	if tok == "" && strings.TrimSpace(in.OrgToken) == "" {
		// the d cookie alone: Clack asks Slack for the tokens (cookie_signin.go)
		return signInCookie(ctx, ws, ck)
	}
	if !strings.HasPrefix(tok, "xoxc-") {
		return Creds{}, errors.New("the token should start with xoxc-")
	}
	c := Creds{Token: tok, Cookie: ck, TeamURL: "https://" + ws + "/"} // d-s is never kept or sent (Client.cookieHeader)
	a, err := New(c).AuthTest(ctx)
	if err != nil {
		// Slack's answer (invalid_auth, …) or no answer at all
		var kind error
		var se *Error
		var ue *url.Error
		switch {
		case errors.As(err, &se):
			kind = ErrSessionRejected
		case errors.As(err, &ue):
			kind = ErrSlackUnreachable
		}
		err = fmt.Errorf("Slack rejected the token and cookie: %w", err)
		if kind != nil {
			err = &kindError{err, kind}
		}
		return Creds{}, err
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
		// The org token is never checked with auth.test (or sent anywhere
		// but the org's own host): every Grid sign-out so far followed an
		// auth.test with the org token on the workspace host, and Slack's
		// own client never makes that call (docs/SIGNOUT-AUDIT.md). The
		// workspace's auth.test already names the org.
		if a.EnterpriseID == "" {
			return Creds{}, errors.New("that workspace isn't part of an Enterprise Grid org; leave Org token empty")
		}
		ou := strings.TrimSpace(in.OrgURL)
		if !slackURL(ou, "https") || !strings.Contains(strings.ToLower(ou), ".enterprise.slack.com") {
			return Creds{}, errors.New("Clack needs the org's address along with the org token: paste the JSON from the console snippet into Token instead of typing the tokens")
		}
		c.EnterpriseToken = org
		c.EnterpriseURL = strings.TrimSuffix(ou, "/") + "/"
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

// pastedTeam is one entry of the JSON the DevTools snippet copies from the
// user's own signed-in Slack tab (localStorage.localConfig_v2.teams).
type pastedTeam struct {
	Name  string `json:"name"`
	ID    string `json:"id"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

// expandPastedTeams lets the Token (or Org token) field take the snippet's
// JSON instead of a bare xoxc token: it picks the workspace token (T…), the
// org token (E…) and the workspace URL from it.
func expandPastedTeams(in *SignInInput) error {
	raw := strings.TrimSpace(in.Token)
	if !strings.HasPrefix(raw, "[") && !strings.HasPrefix(raw, "{") {
		raw = strings.TrimSpace(in.OrgToken)
		if !strings.HasPrefix(raw, "[") && !strings.HasPrefix(raw, "{") {
			return nil
		}
	}
	var teams []pastedTeam
	if strings.HasPrefix(raw, "{") {
		// {"teams": [...] or {...}} (the README snippet), or a single team
		// object. Older snippets also copied "ua" (navigator.userAgent): it
		// is accepted and ignored, since Clack only sends its own (ua.go).
		var wrap struct {
			Teams json.RawMessage `json:"teams"`
		}
		if err := json.Unmarshal([]byte(raw), &wrap); err != nil {
			return errors.New("couldn't read the pasted JSON; copy it again with the console snippet")
		}
		switch t := strings.TrimSpace(string(wrap.Teams)); {
		case strings.HasPrefix(t, "["):
			if json.Unmarshal(wrap.Teams, &teams) != nil {
				return errors.New("couldn't read the pasted JSON; copy it again with the console snippet")
			}
		case strings.HasPrefix(t, "{"): // localConfig_v2.teams is a map by id
			var m map[string]pastedTeam
			if json.Unmarshal(wrap.Teams, &m) != nil {
				return errors.New("couldn't read the pasted JSON; copy it again with the console snippet")
			}
			for _, v := range m {
				teams = append(teams, v)
			}
			sort.Slice(teams, func(i, j int) bool { return teams[i].ID < teams[j].ID })
		default:
			var one pastedTeam
			if err := json.Unmarshal([]byte(raw), &one); err != nil {
				return errors.New("couldn't read the pasted JSON; copy it again with the console snippet")
			}
			teams = []pastedTeam{one}
		}
	} else if err := json.Unmarshal([]byte(raw), &teams); err != nil {
		return errors.New("couldn't read the pasted JSON; copy it again with the console snippet")
	}
	want := strings.ToLower(strings.TrimSpace(in.Workspace))
	var team, org *pastedTeam
	for i := range teams {
		t := &teams[i]
		switch {
		case strings.HasPrefix(t.ID, "E"):
			if org == nil {
				org = t
			}
		case strings.HasPrefix(t.ID, "T"):
			host := strings.ToLower(t.URL)
			if team == nil || (want != "" && strings.Contains(host, want)) {
				team = t
			}
		}
	}
	if team == nil || team.Token == "" {
		return errors.New("the pasted JSON has no workspace (T…) token; make sure you're signed in to the workspace in that tab")
	}
	in.Token = team.Token
	if team.URL != "" {
		in.Workspace = team.URL
	}
	if org != nil && org.Token != "" {
		in.OrgToken = org.Token
		in.OrgURL = org.URL
	} else if strings.HasPrefix(strings.TrimSpace(in.OrgToken), "[") || strings.HasPrefix(strings.TrimSpace(in.OrgToken), "{") {
		in.OrgToken = ""
	}
	return nil
}
