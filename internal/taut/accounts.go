package taut

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/slack"
)

// api.accounts (Taut's AccountSwitcher) over Clack's sign-in: saved
// accounts are Clack creds (team token, org token, `d` cookie) kept in the
// secret store; switching writes creds.json and asks the UI to restart the
// session (UIHooks.SwitchAccount). The JS side sees Taut's StoredAccount
// shape: {userId, teamId, team: {id, name, domain, user_id, token, url},
// xoxd, updatedAt}.

const accountsSecret = "accounts"

type storedAccount struct {
	UserID    string          `json:"userId"`
	TeamID    string          `json:"teamId"`
	Team      json.RawMessage `json:"team"`
	Xoxd      string          `json:"xoxd"`
	UpdatedAt int64           `json:"updatedAt"`
	Creds     slack.Creds     `json:"clackCreds"`
}

func (t *Taut) loadAccounts() (map[string]storedAccount, error) {
	raw, _ := t.data.readSecret(accountsSecret)
	out := map[string]storedAccount{}
	if raw == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, errors.New("saved accounts are unreadable")
	}
	return out, nil
}

func (t *Taut) saveAccounts(m map[string]storedAccount) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	t.data.writeSecret(accountsSecret, string(b))
	return t.data.flush()
}

func (t *Taut) currentAccount() storedAccount {
	c := t.app.C.SessionCreds() // a desktop session's cookies as rotated
	t.app.S.RLock()
	name, domain := t.app.S.TeamName, t.app.S.TeamDomain
	t.app.S.RUnlock()
	teamID := c.Team
	if c.EnterpriseID != "" {
		teamID = c.EnterpriseID
	}
	team, _ := json.Marshal(map[string]any{"id": teamID, "name": name, "domain": domain, "user_id": c.UserID, "token": c.Token,
		"url": firstNonEmpty(c.EnterpriseURL, c.TeamURL), "enterprise_id": c.EnterpriseID})
	xoxd := c.Cookie
	if d, err := url.QueryUnescape(xoxd); err == nil {
		xoxd = d
	}
	return storedAccount{UserID: c.UserID, TeamID: teamID, Team: team, Xoxd: xoxd, UpdatedAt: time.Now().UnixMilli(), Creds: c}
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func (t *Taut) accounts(ctx context.Context, op, arg string) (any, error) {
	switch op {
	case "list":
		m, err := t.loadAccounts()
		if err != nil {
			return nil, err
		}
		out := make([]storedAccount, 0, len(m))
		for _, a := range m {
			out = append(out, a)
		}
		return out, nil
	case "forget":
		m, err := t.loadAccounts()
		if err != nil {
			return nil, err
		}
		delete(m, arg)
		return nil, t.saveAccounts(m)
	case "captureCurrent":
		a := t.currentAccount()
		if a.UserID == "" || a.Creds.Token == "" {
			return nil, nil
		}
		m, err := t.loadAccounts()
		if err != nil {
			return nil, err
		}
		m[a.UserID] = a
		return a, t.saveAccounts(m)
	case "validate":
		m, err := t.loadAccounts()
		if err != nil {
			return nil, err
		}
		kept, dropped := []string{}, []string{}
		for id, a := range m {
			switch t.checkAuth(ctx, a) {
			case "valid":
				kept = append(kept, id)
			case "invalid":
				dropped = append(dropped, id)
				delete(m, id)
			}
		}
		if len(dropped) > 0 {
			if err := t.saveAccounts(m); err != nil {
				return nil, err
			}
		}
		return map[string]any{"kept": kept, "dropped": dropped}, nil
	case "switchTo":
		if t.opts.UI.SwitchAccount == nil {
			return nil, errors.New("account switching is not supported here")
		}
		m, err := t.loadAccounts()
		if err != nil {
			return nil, err
		}
		a, ok := m[arg]
		if !ok || a.Creds.Token == "" {
			return nil, fmt.Errorf("no usable saved account for %s", arg)
		}
		switch t.checkAuth(ctx, a) {
		case "invalid":
			return nil, errors.New("that account is signed out, add it again")
		case "unknown":
			return nil, errors.New("couldn't reach Slack to check that account")
		}
		if _, err := t.accounts(ctx, "captureCurrent", ""); err != nil {
			return nil, err
		}
		if err := writeCreds(t.opts.CredsPath, a.Creds); err != nil {
			return nil, err
		}
		t.opts.UI.SwitchAccount(a.Creds)
		return nil, nil
	case "addAccount":
		if t.opts.UI.SwitchAccount == nil {
			return nil, errors.New("account switching is not supported here")
		}
		if _, err := t.accounts(ctx, "captureCurrent", ""); err != nil {
			return nil, err
		}
		t.opts.UI.SwitchAccount(slack.Creds{}) // empty: show the sign-in screen
		return nil, nil
	}
	return nil, fmt.Errorf("accounts: unknown %q", op)
}

// checkAuth runs auth.test with a saved account's own token and cookie.
func (t *Taut) checkAuth(ctx context.Context, a storedAccount) string {
	c := a.Creds
	if c.Token == "" || c.Cookie == "" {
		return "invalid"
	}
	if c.Desktop() {
		// A desktop session's cookies rotate with every answer and only
		// its own Client follows them (and its DPoP binding): a one-off
		// check from here would send stale ones with the wrong identity.
		return "unknown"
	}
	base := strings.TrimSuffix(firstNonEmpty(c.TeamURL, "https://slack.com/"), "/") + "/api/auth.test"
	// A saved account's cookie only goes to slack.com (or the API origin
	// this session already talks to, as in tests).
	if !slack.CookieURL(base) && (t.app == nil || t.app.C == nil || !t.app.C.CredentialsOK(base)) {
		return "invalid"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base, strings.NewReader("token="+url.QueryEscape(c.Token)))
	if err != nil {
		return "unknown"
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	cookie := c.Cookie
	if !strings.Contains(cookie, "%") {
		cookie = url.QueryEscape(cookie)
	}
	// d only, never d-s: see slack.Client.cookieHeader.
	req.Header.Set("Cookie", "d="+cookie)
	slack.HonestHeaders(req.Header) // Clack's own User-Agent, as on every Slack request
	res, err := t.opts.Fetch(ctx, req)
	if err != nil {
		return "unknown"
	}
	defer res.Body.Close()
	var j struct {
		OK bool `json:"ok"`
	}
	if json.NewDecoder(res.Body).Decode(&j) != nil {
		return "unknown"
	}
	if j.OK {
		return "valid"
	}
	return "invalid"
}

// writeCreds saves the account Clack switches to: owner-only like
// cmd/clack's own creds.json (0600/0700, an owner-only ACL on Windows), and
// only for https slack.com hosts.
func writeCreds(path string, c slack.Creds) error {
	if err := slack.CheckCredsHosts(c); err != nil {
		return err
	}
	return slack.WriteCreds(path, c)
}
