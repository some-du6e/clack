// Package slack is a small, fast client for Slack's web-client API surface
// (session xoxc token + d cookie), including the edge cache API and the
// realtime "flannel" websocket gateway.
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack/tlsfp"
)

// Creds is the on-disk credential file (~/.config/clack/creds.json).
type Creds struct {
	Token           string `json:"token"`               // team xoxc token
	Cookie          string `json:"cookie"`              // d cookie (xoxd-..., URL-encoded as stored by the browser)
	CookieDS        string `json:"cookie_ds,omitempty"` // legacy: read from old creds.json, never sent (see cookieHeader)
	Team            string `json:"team"`                // team id, e.g. T0266FRGM
	EnterpriseToken string `json:"enterprise_token"`    // org xoxc token (optional)
	EnterpriseID    string `json:"enterprise_id"`
	UserID          string `json:"user_id"`
	TeamURL         string `json:"team_url"`       // e.g. https://hackclub.slack.com/
	EnterpriseURL   string `json:"enterprise_url"` // e.g. https://hackclub.enterprise.slack.com/
	// UserAgent is legacy: older builds kept the pasted browser's
	// navigator.userAgent here. It is still read from old creds.json files
	// and never sent: every request carries Clack's own UserAgent() (ua.go).
	UserAgent string `json:"user_agent,omitempty"`

	// Session is "" for a session pasted from the browser and
	// SessionDesktop for one Clack signed in to itself with a slack://
	// magic-login link (LoginMagic). Desktop sessions present as Slack
	// desktop and follow every cookie Slack sets (desktop.go).
	Session string `json:"session,omitempty"`
	// Cookies are the other slack.com cookies Slack vended to a desktop
	// session (lc, d-s, uc, …) in Cookie-header form ("lc=…; uc=…"), kept
	// up to date as Slack rotates them. Cookie still holds d. A string, not
	// a slice, so Creds stays comparable. Unused for pasted sessions.
	Cookies string `json:"cookies,omitempty"`
	// DPoP, set, is the base domain (e.g. slack.com) whose DPoP key this
	// desktop session is bound to (dpop.go): its API and websocket requests
	// carry a proof signed with <config>/dpop-keys/<DPoP>.pem.
	DPoP string `json:"dpop,omitempty"`
}

// SessionDesktop marks Creds that came from desktop-style sign-in.
const SessionDesktop = "desktop"

// StoredCookie is one cookie of a desktop session, scoped to .slack.com.
type StoredCookie struct {
	Name, Value string
}

// encodeCookies and decodeCookies convert Creds.Cookies.
func encodeCookies(cs []StoredCookie) string {
	parts := make([]string, 0, len(cs))
	for _, k := range cs {
		parts = append(parts, k.Name+"="+k.Value)
	}
	return strings.Join(parts, "; ")
}

func decodeCookies(s string) []StoredCookie {
	var out []StoredCookie
	for _, p := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if ok && k != "" && validCookieName(k) && validCookieValue(v) {
			out = append(out, StoredCookie{Name: k, Value: v})
		}
	}
	return out
}

func validCookieName(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= 0x20 || c >= 0x7f || strings.IndexByte("()<>@,;:\\\"/[]?={}", c) >= 0 {
			return false
		}
	}
	return s != ""
}

func validCookieValue(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x21 || c > 0x7e || c == '"' || c == ';' || c == ',' || c == '\\' {
			return false
		}
	}
	return true
}

// Desktop reports whether c is Clack's own desktop-style session.
func (c Creds) Desktop() bool { return c.Session == SessionDesktop }

// entMethods must be called with the org token on the enterprise host
// (they answer team_is_restricted on the team host). Everything else goes to
// the team host first. Call also learns at runtime: a team_is_restricted
// answer moves a method to the enterprise host, enterprise_is_restricted
// moves it back (users.conversations, conversations.members need the team).
var entMethods = map[string]bool{
	"client.counts": true, "users.channelSections.list": true, "client.getWebSocketURL": true,
	"users.channelSections.channels.bulkUpdate": true, "users.channelSections.create": true,
	"users.channelSections.update": true, "users.channelSections.delete": true,
	"activity.feed": true, "activity.views": true, "saved.list": true, "saved.add": true, "saved.delete": true,
	"saved.update": true, "later.list": true, "client.dms": true, "drafts.list": true, "client.init": true,
	"client.channels": true, "users.prefs.get": true, "users.prefs.set": true, "subscriptions.thread.getView": true,
	"subscriptions.thread.add": true, "subscriptions.thread.remove": true, "subscriptions.thread.mark": true,
	"messages.list": true, "search.modules.people": true, "search.modules.channels": true,
	"search.modules.messages": true, "search.modules.files": true, "commands.list": true,
	"bots.info": true, // the team host answers bots_not_found for org apps (verified)
}

// MaxConcurrent bounds in-flight Web API requests (be gentle with rate limits).
const MaxConcurrent = 12

// Client talks to Slack. It is safe for concurrent use.
type Client struct {
	HTTP  *http.Client
	Creds Creds
	base  string // https://<team>.slack.com/api/
	ent   string // https://<org>.enterprise.slack.com/api/
	sem   chan struct{}

	routeMu sync.RWMutex
	learned map[string]bool // method -> use enterprise host

	g guard // signed-out state + backoff of repeated failures (guard.go)

	// EdgeURL is the edge cache API base (tests point it at a fake server).
	EdgeURL string

	agentOnce sync.Once
	agentVer  string // realtime agent_version (methods.go)

	sess *desktopSession // desktop sessions only (desktop.go)
	dpop *dpopBinding    // DPoP-bound desktop sessions only (dpop.go)
}

// New builds a client with a keep-alive HTTP/1.1 transport (newTransport).
func New(c Creds) *Client {
	tr := newTransport()
	base := "https://slack.com/api/"
	if c.TeamURL != "" {
		base = strings.TrimSuffix(c.TeamURL, "/") + "/api/"
	}
	ent := base
	if c.EnterpriseURL != "" {
		ent = strings.TrimSuffix(c.EnterpriseURL, "/") + "/api/"
	}
	cl := &Client{Creds: c, base: base, ent: ent, sem: make(chan struct{}, MaxConcurrent), learned: map[string]bool{}}
	var rt http.RoundTripper = honestTransport{tr} // Clack's own User-Agent on everything, hops included (ua.go)
	if c.Desktop() {
		cl.sess = newDesktopSession(c)
		if c.DPoP != "" {
			cl.dpop = cl.newDPoPBinding() // sends nothing until EnableDPoP
		}
		rt = &desktopTransport{base: rt, c: cl}
	}
	cl.HTTP = &http.Client{Transport: rt, Timeout: 60 * time.Second, CheckRedirect: cl.checkRedirect}
	if isLoopback(base) {
		cl.EdgeURL = base + "edge/" // a local fake (internal/fakeslack) serves the edge API too
	}
	return cl
}

// apiDialer makes the API transport's TLS connections (tlsfp): the
// ClientHello that Grid doesn't sign out for, system roots, HTTPS_PROXY.
var apiDialer = &tlsfp.Dialer{Proxy: http.ProxyFromEnvironment}

// newTransport is the transport for everything that carries credentials:
// Web API calls, edgeapi, downloads and uploads, the realtime socket's
// handshake and the sign-in exchange. HTTP/1.1 with the OpenSSL
// ClientHello (tlsfp), keep-alive, 16 connections per host.
func newTransport() *http.Transport {
	return tlsfp.NewTransport(apiDialer)
}

// isLoopback reports whether u's host is localhost or a loopback address.
func isLoopback(u string) bool {
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	if h := p.Hostname(); h == "localhost" {
		return true
	} else if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// credsOK reports whether a request to u may carry the session cookie and
// token: https slack.com or a subdomain (CookieURL), or the API origin this
// client was configured with (tests run it on an httptest server).
func (c *Client) credsOK(u *url.URL) bool {
	if cookieURL(u) {
		return true
	}
	for _, b := range []string{c.base, c.ent} {
		if bu, err := url.Parse(b); err == nil && bu.Host != "" && u.Host == bu.Host && u.User == nil &&
			(u.Scheme == bu.Scheme || (bu.Scheme == "http" && u.Scheme == "ws") || (bu.Scheme == "https" && u.Scheme == "wss")) {
			return true
		}
	}
	return false
}

// checkRedirect keeps credentials on Slack. Go's client already drops a
// manually set Cookie header when a redirect leaves the original domain;
// this also drops it on an https->http downgrade or a hop to another
// slack.com sibling it doesn't trust, and refuses to follow a redirect off
// Slack's hosts at all (a slack.com link must not turn into a request,
// with or without credentials, to a host the sender controls).
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("slack: stopped after 10 redirects")
	}
	if !c.credsOK(req.URL) {
		req.Header.Del("Cookie")
		req.Header.Del("Authorization")
		if !slackOwnedURL(req.URL) {
			return fmt.Errorf("slack: refusing redirect off Slack to %q", req.URL.Hostname())
		}
	}
	// A file download redirected to a Slack page (sign-in, /ssb/…): stop.
	// Loading Slack's web pages with the cookie from outside the browser
	// can end the session.
	if (req.Method == "" || req.Method == http.MethodGet) && cookieURL(req.URL) && !FileURL(req.URL) {
		return ErrNotAFile
	}
	return nil
}

// CredentialsOK reports whether raw may be fetched with the session cookie
// (Download): https slack.com or a subdomain, or this client's API origin.
func (c *Client) CredentialsOK(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && c.credsOK(u)
}

// ErrUntrustedHost is returned when a URL that would carry credentials
// isn't on slack.com.
var ErrUntrustedHost = errors.New("slack: refusing to send credentials to a non-Slack host")

// Error is a Slack API {"ok":false} error.
type Error struct {
	Method string
	Code   string
	// Body is the whole response (memory only): some errors carry details,
	// e.g. views.submit's validation errors or blocks.actions' message.
	Body json.RawMessage
}

func (e *Error) Error() string { return "slack " + e.Method + ": " + e.Code }

// RateLimited is returned when Slack answers 429 too many times in a row.
var RateLimited = errors.New("slack: rate limited")

// cookieHeader sends d only. d-s is never sent: Slack re-sets it on every
// response to the current Unix time (a last-seen stamp), so a pasted d-s is
// already older than the one the browser sends next. Presenting an older
// stamp for the same session is what looks like a cloned cookie, and it is
// the leading suspect for Enterprise Grid ending the session everywhere the
// moment Clack signed in (docs/SIGNOUT-AUDIT.md). The Creds field stays so
// existing creds.json files still load.
//
// A desktop session is different: its cookies are Clack's own and follow
// Slack's Set-Cookie, so it sends all of them, d-s included (desktop.go).
func (c *Client) cookieHeader() string {
	if c.sess != nil {
		return c.sess.header()
	}
	return "d=" + c.Creds.Cookie
}

// useEnt reports whether method should go to the enterprise host.
func (c *Client) useEnt(method string) bool {
	if c.Creds.EnterpriseToken == "" {
		return false
	}
	c.routeMu.RLock()
	v, ok := c.learned[method]
	c.routeMu.RUnlock()
	if ok {
		return v
	}
	return entMethods[method]
}

// Call invokes a Web API method with form params (team host and token,
// or the enterprise host and token for org-level methods; see entMethods).
// out may be nil or a pointer to decode the JSON into.
func (c *Client) Call(ctx context.Context, method string, params url.Values, out any) error {
	ent := c.useEnt(method)
	err := c.callOn(ctx, method, ent, params, out)
	var se *Error
	if c.Creds.EnterpriseToken != "" && errors.As(err, &se) &&
		((!ent && se.Code == "team_is_restricted") || (ent && se.Code == "enterprise_is_restricted")) {
		c.routeMu.Lock()
		c.learned[method] = !ent
		c.routeMu.Unlock()
		err = c.callOn(ctx, method, !ent, params, out)
	}
	return err
}

// forbiddenMethods would end the user's Slack session for every client.
// Clack never calls them; "Sign out" only forgets local credentials.
// Keys are lower case; refusedMethod compares case-insensitively.
var forbiddenMethods = map[string]bool{
	"auth.signout": true, "auth.revoke": true, "auth.enterprisesignout": true, "enterprisesignout": true,
}

// ErrForbiddenMethod is returned for methods that would end the session.
var ErrForbiddenMethod = errors.New("slack: method refused (it would sign you out everywhere)")

// refusedMethod reports whether method must not be sent. Anything but a plain
// method name ([A-Za-z0-9._]) is refused too, so "auth.signout?x=1",
// "auth.signout/", "../auth.signout" or "AUTH.SIGNOUT" from a plugin can't
// reach /api/auth.signout around the exact-name check.
func refusedMethod(method string) bool {
	if method == "" {
		return true
	}
	for _, r := range method {
		if !(r == '.' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return forbiddenMethods[strings.ToLower(method)]
}

func (c *Client) callOn(ctx context.Context, method string, ent bool, params url.Values, out any) error {
	if refusedMethod(method) {
		return ErrForbiddenMethod
	}
	p := url.Values{}
	for k, v := range params {
		p[k] = v
	}
	base, tok := c.base, c.Creds.Token
	if ent {
		base, tok = c.ent, c.Creds.EnterpriseToken
	}
	p.Set("token", tok)
	return c.post(ctx, method, base+method, "application/x-www-form-urlencoded", []byte(p.Encode()), out)
}

// CallMultipart invokes a Web API method with a multipart body: fields
// plus one file part (fileField, filename, data). Routing and the
// team/enterprise fallback are the same as Call.
func (c *Client) CallMultipart(ctx context.Context, method string, fields url.Values, fileField, filename string, data []byte, out any) error {
	ent := c.useEnt(method)
	err := c.multipartOn(ctx, method, ent, fields, fileField, filename, data, out)
	var se *Error
	if c.Creds.EnterpriseToken != "" && errors.As(err, &se) &&
		((!ent && se.Code == "team_is_restricted") || (ent && se.Code == "enterprise_is_restricted")) {
		c.routeMu.Lock()
		c.learned[method] = !ent
		c.routeMu.Unlock()
		err = c.multipartOn(ctx, method, !ent, fields, fileField, filename, data, out)
	}
	return err
}

func (c *Client) multipartOn(ctx context.Context, method string, ent bool, fields url.Values, fileField, filename string, data []byte, out any) error {
	if refusedMethod(method) {
		return ErrForbiddenMethod
	}
	base, tok := c.base, c.Creds.Token
	if ent {
		base, tok = c.ent, c.Creds.EnterpriseToken
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("token", tok)
	for k, vs := range fields {
		for _, v := range vs {
			_ = mw.WriteField(k, v)
		}
	}
	fw, err := mw.CreateFormFile(fileField, filename)
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	return c.post(ctx, method, base+method, mw.FormDataContentType(), buf.Bytes(), out)
}

// Raw is like Call but returns the raw JSON body.
func (c *Client) Raw(ctx context.Context, method string, params url.Values) (json.RawMessage, error) {
	var raw json.RawMessage
	err := c.Call(ctx, method, params, &raw)
	return raw, err
}

// Edge calls https://edgeapi.slack.com/cache/<enterprise>/<path> with a JSON body.
func (c *Client) Edge(ctx context.Context, path string, body map[string]any, out any) error {
	tok := c.Creds.EnterpriseToken
	if tok == "" {
		tok = c.Creds.Token
	}
	ent := c.Creds.EnterpriseID
	if ent == "" {
		ent = c.Creds.Team
	}
	body["token"] = tok
	body["enterprise_token"] = tok
	b, _ := json.Marshal(body)
	base := c.EdgeURL
	if base == "" {
		base = "https://edgeapi.slack.com/cache/"
	}
	u := base + ent + "/" + path + "?_x_app_name=client"
	return c.post(ctx, "edge:"+path, u, "text/plain;charset=UTF-8", b, out)
}

// SlowCall is the duration above which a call's method name and timing are logged.
var SlowCall = 1500 * time.Millisecond

// post sends one request through the traffic guards: nothing goes out after
// the session ended, a request that keeps failing identically backs off
// exponentially (BackoffBase … BackoffMax), and a session-ended answer from
// any call trips SignedOut.
func (c *Client) post(ctx context.Context, method, u, ctype string, body []byte, out any) error {
	if c.SignedOut() {
		return ErrSignedOut
	}
	if err := ctx.Err(); err != nil {
		return err // cancelled (e.g. core's background context at sign-out): never send
	}
	k := reqKey(method, u, body)
	if !skipBackoff(ctx) {
		if err := c.blocked(k, time.Now()); err != nil {
			return err
		}
	}
	err := c.doPost(ctx, method, u, ctype, body, out)
	if ctx.Err() != nil && err != nil { // cancelled by the caller: not a server failure
		return err
	}
	c.record(k, err, time.Now())
	var se *Error
	if errors.As(err, &se) && authCodes[se.Code] {
		c.markAuth(se.Code)
	}
	if se != nil && c.dpop != nil {
		c.dpop.failCode(se.Code)
	}
	return err
}

func (c *Client) doPost(ctx context.Context, method, u, ctype string, body []byte, out any) error {
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.sem }()
	t0 := time.Now()
	defer func() {
		if d := time.Since(t0); d > SlowCall {
			log.Printf("slack: slow call %s %s", method, d.Round(time.Millisecond))
		}
	}()
	for attempt := 0; ; attempt++ {
		// re-check before every attempt: waiting for a slot or a rate-limit
		// retry can outlast the session
		if c.SignedOut() {
			return ErrSignedOut
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
		if err != nil {
			return scrubErr(err)
		}
		req.Header.Set("Content-Type", ctype)
		req.Header.Set("Cookie", c.cookieHeader())
		HonestHeaders(req.Header) // no Origin, no client hints (ua.go)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return scrubErr(err)
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			if attempt >= 4 {
				return RateLimited
			}
			wait, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			if wait <= 0 {
				wait = 1 << attempt
			}
			log.Printf("slack: %s rate limited, retrying in %ds", method, wait)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(wait) * time.Second):
			}
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return scrubErr(err)
		}
		if resp.StatusCode >= 400 && resp.StatusCode != 200 {
			return fmt.Errorf("slack %s: http %d", method, resp.StatusCode)
		}
		var head struct {
			OK    *bool  `json:"ok"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(data, &head); err != nil {
			return fmt.Errorf("slack %s: bad json: %w", method, err)
		}
		if head.OK != nil && !*head.OK {
			return &Error{Method: method, Code: head.Error, Body: data}
		}
		if out == nil {
			return nil
		}
		if raw, ok := out.(*json.RawMessage); ok {
			*raw = data
			return nil
		}
		return json.Unmarshal(data, out)
	}
}

// Download fetches an authenticated URL (files.slack.com etc) with the session
// cookie. It refuses any URL credsOK rejects (ErrUntrustedHost).
func (c *Client) Download(ctx context.Context, rawURL string) (*http.Response, error) {
	if c.SignedOut() {
		return nil, ErrSignedOut
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, scrubErr(err)
	}
	if !c.credsOK(req.URL) {
		return nil, ErrUntrustedHost
	}
	if cookieURL(req.URL) && !FileURL(req.URL) {
		return nil, ErrNotAFile
	}
	req.Header.Set("Cookie", c.cookieHeader())
	HonestHeaders(req.Header)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return resp, scrubErr(err)
	}
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
		// a file URL that answers with a page (usually a redirect to
		// sign-in): don't hand Slack's HTML to an image decoder, and don't
		// keep loading it
		resp.Body.Close()
		return nil, ErrNotAFile
	}
	return resp, nil
}

// ErrNotAFile is returned by Download for a slack.com URL that isn't a file
// (a workspace web page, /ssb/…, /signin, a permalink). Clack never loads
// Slack's web pages with the session cookie: fetching them from outside the
// browser can end the session everywhere (see README "Sign in").
var ErrNotAFile = errors.New("slack: not a file URL; refusing to load a Slack page with the session cookie")

// FileURL reports whether u is one of Slack's file hosts or file paths,
// the only slack.com GETs that carry the session cookie (Download).
func FileURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h == "files.slack.com" || (strings.HasPrefix(h, "files-") && strings.HasSuffix(h, ".slack.com")) {
		return true
	}
	for _, p := range []string{"/files-pri/", "/files-tmb/", "/files-pri-safe/", "/files-origin/"} {
		if strings.HasPrefix(u.Path, p) {
			return true
		}
	}
	return false
}

// UploadBytes sends file contents to an upload URL from
// files.getUploadURLExternal. progress (may be nil) is called as the body
// is written: every ~64 KB and once at the end, with the multipart body size
// as total. It runs on the calling goroutine.
func (c *Client) UploadBytes(ctx context.Context, uploadURL, filename string, data []byte, progress func(sent, total int64)) error {
	if c.SignedOut() {
		return ErrSignedOut
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	total := int64(buf.Len())
	var body io.Reader = &buf
	if progress != nil {
		body = &progressReader{r: &buf, total: total, f: progress}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, body)
	if err != nil {
		return scrubErr(err)
	}
	if !c.credsOK(req.URL) {
		return ErrUntrustedHost
	}
	req.ContentLength = total
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Cookie", c.cookieHeader())
	HonestHeaders(req.Header)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return scrubErr(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack upload: http %d", resp.StatusCode)
	}
	if progress != nil {
		progress(total, total)
	}
	return nil
}

type progressReader struct {
	r           io.Reader
	sent, total int64
	last        int64
	f           func(sent, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.sent += int64(n)
	if p.sent-p.last >= 64<<10 || (err == io.EOF && p.sent != p.last) {
		p.last = p.sent
		p.f(p.sent, p.total)
	}
	return n, err
}
