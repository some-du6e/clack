package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Desktop-style sign-in (docs/DESKTOP-SIGNIN.md). The user signs in in their
// own browser, and Slack's page hands the desktop app a one-time slack://
// magic-login link. Clack trades that link for a session of its own with
// auth.loginMagicBulk, the way Slack desktop 4.53.276 does: Slack answers
// with a fresh d cookie (and lc) in Set-Cookie, minted for this client. The
// browser's cookie is never copied, so this session is Clack's alone.

// MagicTokenSet is one base domain's worth of magic-login tokens from a
// slack:// link.
type MagicTokenSet struct {
	Tokens       []string // as sent in magic_tokens (z-app-<TEAM>-<tok> for the classic form)
	BaseDomain   string   // host the exchange goes to, e.g. slack.com
	RequiresDPoP bool     // the link said dpop=1 (see dpop.go)
}

// teamIDPattern is Slack desktop's team id pattern (module 66008 in
// 4.53.276: (E|T)[A-Z0-9]{8,50}). It is matched case-insensitively and
// upper-cased, as Slack does, because KDE's kde-open lowercases the host of
// slack://T…/magic-login/… links.
var classicMagic = regexp.MustCompile(`(?i)\b((?:E|T)[A-Z0-9]{8,50})/magic-login/([A-Za-z0-9_-]+)/?`)

// magicToken is what a token in a link may contain. Slack's own regex is
// [A-z0-9-] (which, as an ASCII range, also lets through [\]^_`); Clack
// allows letters, digits, '-' and '_' only, so a token can't smuggle
// anything into the query string.
var magicToken = regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`)

// IsMagicLink reports whether raw is a slack:// sign-in link (either form)
// rather than a navigation deep link. It doesn't validate it: ParseMagicLink
// does.
func IsMagicLink(raw string) bool {
	raw = strings.TrimSpace(raw)
	if len(raw) < 8 || !strings.EqualFold(raw[:8], "slack://") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, "login-v2") || strings.Contains(strings.ToLower(u.Path), "/magic-login/")
}

// ErrNotMagicLink is returned by ParseMagicLink for anything that isn't a
// slack:// sign-in link.
var ErrNotMagicLink = errors.New("that isn't a Slack sign-in link; it should start with slack:// and contain magic-login or login-v2")

// ParseMagicLink reads both sign-in link shapes Slack desktop accepts:
//
//	slack://<TEAM>/magic-login/<token>[?host=<basedomain>][&dpop=1]
//	slack://login-v2/?0.host=<basedomain>&0.tokens=<a>_<b>&0.dpop=1&1.host=…
//
// Every base domain must be slack.com or a subdomain of it (CookieHost):
// the exchange's answer is a session cookie, and a link is untrusted input
// (anyone can send you one).
func ParseMagicLink(raw string) ([]MagicTokenSet, error) {
	raw = strings.TrimSpace(raw)
	if !IsMagicLink(raw) {
		return nil, ErrNotMagicLink
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, ErrNotMagicLink
	}
	q := u.Query()
	if strings.EqualFold(u.Host, "login-v2") {
		var sets []MagicTokenSet
		for i := 0; i < 16; i++ {
			p := strconv.Itoa(i) + "."
			host, toks := q.Get(p+"host"), q.Get(p+"tokens")
			if host == "" && toks == "" {
				break
			}
			if host == "" || toks == "" {
				return nil, fmt.Errorf("the sign-in link's token set %d is incomplete; copy the whole link again", i)
			}
			base, err := magicBase(host)
			if err != nil {
				return nil, err
			}
			var list []string
			for _, t := range strings.Split(toks, "_") {
				if t == "" {
					continue
				}
				if !magicToken.MatchString(t) {
					return nil, errors.New("the sign-in link has a malformed token; copy the whole link again")
				}
				list = append(list, t)
			}
			if len(list) == 0 {
				return nil, fmt.Errorf("the sign-in link's token set %d has no tokens", i)
			}
			sets = append(sets, MagicTokenSet{Tokens: list, BaseDomain: base, RequiresDPoP: q.Get(p+"dpop") == "1"})
		}
		if len(sets) == 0 {
			return nil, errors.New("the sign-in link has no tokens; copy the whole link again")
		}
		return sets, nil
	}
	// Classic form. Slack matches the regex against the whole URL; the team
	// id is the URL's host here, which Go (like KDE) has lower-cased.
	m := classicMagic.FindAllStringSubmatch(u.Host+u.Path, -1)
	if len(m) == 0 {
		return nil, errors.New("couldn't read the team and token from the sign-in link; copy the whole link again")
	}
	base, err := magicBase(q.Get("host"))
	if err != nil {
		return nil, err
	}
	set := MagicTokenSet{BaseDomain: base, RequiresDPoP: q.Get("dpop") == "1"}
	for _, x := range m {
		if !magicToken.MatchString(x[2]) {
			return nil, errors.New("the sign-in link has a malformed token; copy the whole link again")
		}
		set.Tokens = append(set.Tokens, "z-app-"+strings.ToUpper(x[1])+"-"+x[2])
	}
	return []MagicTokenSet{set}, nil
}

// magicBase validates a link's host= value. Empty means slack.com, Slack
// desktop's default (commercial) environment.
func magicBase(h string) (string, error) {
	if h == "" {
		return "slack.com", nil
	}
	n, ok := normHost(h)
	if !ok || !CookieHost(n) {
		return "", fmt.Errorf("refusing a sign-in link for %q: Clack only signs in to slack.com", Redact(h))
	}
	return n, nil
}

// MagicConfig is optional context for LoginMagic.
type MagicConfig struct {
	// DPoPDir is where per-domain DPoP keys live (next to creds.json). A
	// link that asks for DPoP fails to sign in without one.
	DPoPDir string
}

// MagicRedirectError means a workspace wants the browser to finish signing in
// (SSO/SAML): Slack answered with auth_redir instead of a session.
type MagicRedirectError struct {
	URL string // https on a Slack host, or "" if Slack's URL wasn't
}

func (e *MagicRedirectError) Error() string {
	if e.URL == "" {
		return "that workspace needs you to finish signing in in your browser (single sign-on). Sign in there, then copy the new slack:// link"
	}
	return "that workspace needs you to finish signing in in your browser (single sign-on): open " + e.URL + " , sign in, then copy the new slack:// link"
}

// magicTransport is the transport LoginMagic's own client uses (nil: a
// fresh one). Tests point it at an httptest server.
var magicTransport http.RoundTripper

// magicTimeout bounds each request of the sign-in exchange.
const magicTimeout = 30 * time.Second

// magicResult is one entry of auth.loginMagicBulk's token_results, with the
// fields Slack desktop reads (main.bundle.cjs @952921 onwards).
type magicResult struct {
	Team *struct {
		ID   string `json:"id"`
		URL  string `json:"url"`
		Name string `json:"name"`
	} `json:"team"`
	AuthRedir string          `json:"auth_redir"`
	Redir     string          `json:"redir"`
	Reason    string          `json:"reason"`
	User      json.RawMessage `json:"user"`
	// Token/APIToken: not read by Slack desktop and not seen in the
	// evidence. If Slack ever includes one, it saves loading the boot page.
	Token    string `json:"token"`
	APIToken string `json:"api_token"`
}

// LoginMagic trades the link's tokens for Clack's own session. For each
// token set it GETs https://<base>/api/auth.loginMagicBulk?magic_tokens=…&ssb=1
// with a dedicated client and cookie jar (Slack desktop's request, its user
// agent, and a DPoP binding proof when the set asks for one), keeps the
// cookies Slack vends, then finds the session's xoxc token (bootToken) and
// checks it with auth.test. The returned Creds hold only what Slack issued
// to this client: no cookie or token from the browser.
func LoginMagic(ctx context.Context, sets []MagicTokenSet, cfg MagicConfig) (Creds, error) {
	if len(sets) == 0 {
		return Creds{}, errors.New("the sign-in link has no tokens")
	}
	jar, _ := cookiejar.New(nil)
	tr := magicTransport
	if tr == nil {
		tr = newTransport()
	}
	hc := &http.Client{Jar: jar, Transport: tr, Timeout: magicTimeout, CheckRedirect: magicRedirect}
	ua := DesktopUserAgent()
	var (
		res     *magicResult
		resBase string
		dpop    *dpopKey
	)
	for _, s := range sets {
		if _, err := magicBase(s.BaseDomain); err != nil || s.BaseDomain == "" {
			return Creds{}, fmt.Errorf("refusing a sign-in link for %q: Clack only signs in to slack.com", Redact(s.BaseDomain))
		}
		if len(s.Tokens) == 0 {
			return Creds{}, errors.New("the sign-in link has no tokens")
		}
		for _, t := range s.Tokens {
			// already checked by ParseMagicLink; they go into the URL unescaped,
			// as Slack desktop sends them
			if !magicToken.MatchString(t) {
				return Creds{}, errors.New("the sign-in link has a malformed token; copy the whole link again")
			}
		}
		var key *dpopKey
		if s.RequiresDPoP && cfg.DPoPDir == "" {
			return Creds{}, errors.New("this sign-in link asks for a DPoP key, and Clack has nowhere to keep one (no config directory)")
		}
		if s.RequiresDPoP {
			k, err := loadOrCreateDPoPKey(cfg.DPoPDir, s.BaseDomain)
			if err != nil {
				return Creds{}, fmt.Errorf("couldn't set up the DPoP key Slack asked for: %w", err)
			}
			key = k
		}
		r, err := magicExchange(ctx, hc, ua, s, key)
		if err != nil {
			return Creds{}, err
		}
		if res == nil && r != nil {
			res, resBase, dpop = r, s.BaseDomain, key
		}
	}
	if res == nil {
		return Creds{}, errors.New("Slack accepted the link but didn't say which workspace it signed in to; get a new link and try again")
	}
	cookies := jarCookies(jar, "https://"+resBase+"/", res.Team.URL)
	d := cookieValue(cookies, "d")
	if d == "" {
		return Creds{}, errors.New("Slack accepted the link but didn't give Clack a session cookie; get a new link and try again")
	}
	c := Creds{
		Session: SessionDesktop, Cookie: d, TeamURL: res.Team.URL, Team: res.Team.ID,
	}
	if dpop != nil {
		c.DPoP = resBase
	}
	c.Cookies = encodeCookies(withoutCookie(cookies, "d")) // d lives in Cookie
	sess := &magicSession{hc: hc, ua: ua, dpop: dpop}
	var toks pageTokens
	for _, t := range []string{res.Token, res.APIToken} {
		if strings.HasPrefix(t, "xoxc-") {
			toks.Team = append(toks.Team, t)
		}
	}
	if len(toks.Team) == 0 {
		var err error
		if toks, err = sess.bootTokens(ctx, res.Team.ID); err != nil {
			return Creds{}, err
		}
	}
	if err := sess.fillFromAuthTest(ctx, &c, toks); err != nil {
		return Creds{}, err
	}
	// The cookies again: the token page and auth.test may have rotated them.
	all := jarCookies(jar, "https://"+resBase+"/", c.TeamURL, c.EnterpriseURL)
	if v := cookieValue(all, "d"); v != "" {
		c.Cookie = v
	}
	c.Cookies = encodeCookies(withoutCookie(all, "d"))
	return c, nil
}

// magicRedirect follows redirects only within https slack.com. The
// exchange's own client carries the newly vended session, so a hop
// anywhere else is refused outright.
func magicRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("slack: stopped after 10 redirects")
	}
	if !cookieURL(req.URL) {
		return fmt.Errorf("slack: refusing redirect off Slack to %q", req.URL.Hostname())
	}
	if bannedPage(req.URL) {
		return errors.New("slack: Slack redirected sign-in to its /ssb/redirect page, which Clack never loads with a session cookie (docs/SIGNOUT-AUDIT.md)")
	}
	return nil
}

// bannedPage reports whether u is a team's /ssb/redirect page, which Clack
// never loads with a session cookie, even when a redirect points there.
func bannedPage(u *url.URL) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimRight(u.Path, "/")), "/ssb/redirect")
}

func magicExchange(ctx context.Context, hc *http.Client, ua string, s MagicTokenSet, key *dpopKey) (*magicResult, error) {
	u := "https://" + s.BaseDomain + "/api/auth.loginMagicBulk?magic_tokens=" + strings.Join(s.Tokens, ",") + "&ssb=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, scrubErr(err)
	}
	desktopNavHeaders(req.Header, ua)
	if key != nil {
		// The binding proof carries the public key (Slack desktop passes it
		// to loadURL as an extra header; main.bundle.cjs @952921).
		p, err := key.proof(http.MethodGet, req.URL, false)
		if err != nil {
			return nil, &DPoPError{Reason: "Clack couldn't sign the binding proof (" + err.Error() + ")"}
		}
		req.Header.Set("Dpop", p)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach Slack to finish signing in: %w", scrubErr(err))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if err != nil {
		return nil, scrubErr(err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Slack answered the sign-in link with HTTP %d; get a new link and try again", resp.StatusCode)
	}
	var j struct {
		OK           bool                   `json:"ok"`
		Error        string                 `json:"error"`
		TokenResults map[string]magicResult `json:"token_results"`
	}
	if err := json.Unmarshal(body, &j); err != nil {
		return nil, errors.New("Slack's answer to the sign-in link wasn't JSON; get a new link and try again")
	}
	if !j.OK {
		return nil, magicError(j.Error)
	}
	ids := make([]string, 0, len(j.TokenResults))
	for id := range j.TokenResults {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// Slack desktop: any auth_redir wins (the browser has to finish), else
	// the first result with a team URL.
	for _, id := range ids {
		if r := j.TokenResults[id]; r.AuthRedir != "" {
			e := &MagicRedirectError{}
			if p, err := url.Parse(r.AuthRedir); err == nil && p.Scheme == "https" && slackOwnedURL(p) {
				e.URL = p.String()
			}
			return nil, e
		}
	}
	for _, id := range ids {
		r := j.TokenResults[id]
		if r.Team != nil && r.Team.URL != "" {
			if !slackURL(r.Team.URL, "https") {
				return nil, fmt.Errorf("refusing to sign in to %q: not an https slack.com address", Redact(r.Team.URL))
			}
			if !strings.HasSuffix(r.Team.URL, "/") {
				r.Team.URL += "/"
			}
			return &r, nil
		}
	}
	for _, id := range ids {
		if r := j.TokenResults[id]; r.Reason != "" {
			return nil, magicError(r.Reason)
		}
	}
	return nil, nil
}

// magicError turns an exchange failure code into something to act on.
func magicError(code string) error {
	switch {
	case code == "":
		return errors.New("Slack didn't accept the sign-in link; get a new one and try again")
	case strings.Contains(code, "expired"), strings.Contains(code, "invalid"), strings.Contains(code, "used"), code == "not_found":
		return fmt.Errorf("that sign-in link has expired or was already used (%s). Links work once and only for a few minutes: get a new one", code)
	}
	return fmt.Errorf("Slack didn't accept the sign-in link (%s); get a new one and try again", code)
}

// desktopNavHeaders are the headers of Slack desktop's page loads (the
// exchange is a navigation in a hidden window, and the boot page is the main
// window's): its user agent and a browser's navigation Accept.
func desktopNavHeaders(h http.Header, ua string) {
	h.Set("User-Agent", ua)
	h.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	h.Set("Accept-Language", "en-US,en;q=0.9")
}

// magicSession is the sign-in exchange's client after the cookies landed.
type magicSession struct {
	hc   *http.Client
	ua   string
	dpop *dpopKey
}

func (s *magicSession) do(req *http.Request) (*http.Response, error) {
	if s.dpop != nil && dpopApplies(req) {
		p, err := s.dpop.proof(req.Method, req.URL, false)
		if err != nil {
			return nil, &DPoPError{Reason: "Clack couldn't sign a proof (" + err.Error() + ")"}
		}
		req.Header.Set("Dpop", p)
	}
	resp, err := s.hc.Do(req)
	if err == nil && s.dpop != nil && resp.StatusCode == http.StatusUnauthorized &&
		strings.Contains(strings.ToLower(resp.Header.Get("WWW-Authenticate")), "dpop") {
		resp.Body.Close()
		return nil, &DPoPError{Reason: "Slack rejected its proof while signing in (HTTP 401)"}
	}
	return resp, err
}

// authPage is the hidden page Slack's web client loads, with nothing but the
// session cookie, to learn the session's tokens ("auth_fetch" then
// "lc_ingest" in its beacons). Slack desktop's window runs that same web
// client, so after the exchange this is how its session gets a token.
// Observed live on 2026-11-08 with a freshly vended session in a clean
// browser: the page sets sessionStorage.incomingConfig to the same
// {"teams": {id: {id, url, domain, token, …}}} map Slack keeps in
// localStorage.localConfig_v2; Grid adds enterprise_api_token.
func authPage(teamID string) string {
	return "https://app.slack.com/auth?app=client&return_to=" + url.QueryEscape("/client/"+teamID) + "&teams=&iframe=1"
}

// incomingConfigRe finds the start of the config object on authPage.
var incomingConfigRe = regexp.MustCompile(`incomingConfig\s*=\s*JSON\.stringify\(`)

// bootTokens loads authPage with this session's own, freshly vended cookie
// and Slack desktop's user agent, and returns the xoxc tokens it hands out:
// teamID's workspace token first, then the org (E) token. It is the one
// Slack web page Clack loads with a session cookie, and never with a cookie
// copied from the browser. No other page is tried, in particular never
// <team>/ssb/redirect (docs/SIGNOUT-AUDIT.md).
func (s *magicSession) bootTokens(ctx context.Context, teamID string) (pageTokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authPage(teamID), nil)
	if err != nil {
		return pageTokens{}, scrubErr(err)
	}
	desktopNavHeaders(req.Header, s.ua)
	resp, err := s.do(req)
	if err != nil {
		return pageTokens{}, fmt.Errorf("Clack has its own session now but couldn't load Slack's token page (app.slack.com/auth): %w", scrubErr(err))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	resp.Body.Close()
	if err != nil {
		return pageTokens{}, scrubErr(err)
	}
	toks := parseIncomingConfig(b, teamID)
	if len(toks.Team) == 0 {
		return pageTokens{}, fmt.Errorf("Slack gave Clack its own session cookie, but its token page (app.slack.com/auth, HTTP %d) had no tokens in it, so Clack has no token to call Slack with. "+
			"Clack doesn't try any other page. Use Advanced (paste a token) for now", resp.StatusCode)
	}
	return toks, nil
}

// pageTokens is what the token page hands a session: workspace token(s),
// teamID's first, and the org's token and address on Grid.
type pageTokens struct {
	Team   []string
	Org    string
	OrgURL string
}

// parseIncomingConfig reads the teams map from authPage's
// "incomingConfig = JSON.stringify({...})". Only xoxc- tokens count, and an
// org address only on an https enterprise.slack.com host.
func parseIncomingConfig(page []byte, teamID string) pageTokens {
	var out pageTokens
	loc := incomingConfigRe.FindIndex(page)
	if loc == nil {
		return out
	}
	var cfg struct {
		Teams map[string]struct {
			ID         string `json:"id"`
			URL        string `json:"url"`
			Token      string `json:"token"`
			Enterprise string `json:"enterprise_api_token"`
		} `json:"teams"`
	}
	if err := json.NewDecoder(bytes.NewReader(page[loc[1]:])).Decode(&cfg); err != nil {
		return out
	}
	keys := make([]string, 0, len(cfg.Teams))
	for k := range cfg.Teams {
		keys = append(keys, k)
	}
	sort.Strings(keys) // map order is random; keep the choice stable
	var first, others []string
	for _, k := range keys {
		t := cfg.Teams[k]
		id := t.ID
		if id == "" {
			id = k
		}
		if strings.HasPrefix(id, "E") {
			if strings.HasPrefix(t.Token, "xoxc-") && out.Org == "" {
				out.Org = t.Token
			}
			if slackURL(t.URL, "https") && strings.Contains(strings.ToLower(t.URL), ".enterprise.slack.com") && out.OrgURL == "" {
				out.OrgURL = strings.TrimSuffix(t.URL, "/") + "/"
			}
			continue
		}
		if strings.HasPrefix(t.Token, "xoxc-") {
			if strings.EqualFold(id, teamID) {
				first = append(first, t.Token)
			} else {
				others = append(others, t.Token)
			}
		}
		if strings.HasPrefix(t.Enterprise, "xoxc-") && out.Org == "" {
			out.Org = t.Enterprise
		}
	}
	out.Team = append(first, others...)
	return out
}

// fillFromAuthTest checks the workspace token with one auth.test on the team
// host and fills in the ids. The org token is never sent to auth.test: every
// Grid sign-out so far, this live test's included (docs/DESKTOP-SIGNIN.md),
// followed an auth.test with the org token on the workspace host, and Slack's
// own client never makes that call. The workspace's auth.test names the org.
func (s *magicSession) fillFromAuthTest(ctx context.Context, c *Creds, pt pageTokens) error {
	if len(pt.Team) == 0 {
		return errors.New("Clack got its own Slack session, but only for the org, not a workspace. Sign in to a workspace in the browser and get a new link")
	}
	a, err := s.authTest(ctx, c.TeamURL, pt.Team[0])
	if err != nil {
		return err
	}
	if strings.HasPrefix(a.TeamID, "E") {
		return errors.New("Slack's token page gave the org token where the workspace token should be; get a new link and try again")
	}
	c.Token, c.Team, c.UserID, c.EnterpriseID = pt.Team[0], a.TeamID, a.UserID, a.EnterpriseID
	if slackURL(a.URL, "https") {
		c.TeamURL = a.URL
	}
	if pt.Org != "" && a.EnterpriseID != "" && pt.OrgURL != "" {
		c.EnterpriseToken, c.EnterpriseURL = pt.Org, pt.OrgURL
	}
	return nil
}

func (s *magicSession) authTest(ctx context.Context, teamURL, tok string) (*AuthInfo, error) {
	u := strings.TrimSuffix(teamURL, "/") + "/api/auth.test"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(url.Values{"token": {tok}}.Encode()))
	if err != nil {
		return nil, scrubErr(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://app.slack.com")
	req.Header.Set("User-Agent", s.ua)
	resp, err := s.do(req)
	if err != nil {
		return nil, scrubErr(err)
	}
	defer resp.Body.Close()
	var a struct {
		AuthInfo
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&a); err != nil {
		return nil, fmt.Errorf("auth.test: http %d", resp.StatusCode)
	}
	if !a.OK {
		return nil, fmt.Errorf("Slack rejected the new session's token: %s", a.Error)
	}
	return &a.AuthInfo, nil
}

// jarCookies lists the cookies the jar would send to each of urls (by name,
// first wins), so host-only cookies of the team host are kept too.
func jarCookies(jar http.CookieJar, urls ...string) []StoredCookie {
	var out []StoredCookie
	seen := map[string]bool{}
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil || raw == "" {
			continue
		}
		for _, k := range jar.Cookies(u) {
			if !seen[k.Name] {
				seen[k.Name] = true
				out = append(out, StoredCookie{Name: k.Name, Value: k.Value})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cookieValue(cs []StoredCookie, name string) string {
	for _, k := range cs {
		if k.Name == name {
			return k.Value
		}
	}
	return ""
}

func withoutCookie(cs []StoredCookie, name string) []StoredCookie {
	out := cs[:0:0]
	for _, k := range cs {
		if k.Name != name {
			out = append(out, k)
		}
	}
	return out
}
