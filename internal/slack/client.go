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
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/some-du6e/clack/internal/slack/tlsfp"
	"golang.org/x/text/unicode/norm"
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
	"users.channelSections.set": true, "users.channelSections.createLink": true, // the menus (core/section_menu.go)
	"activity.feed": true, "activity.views": true, "saved.list": true, "saved.add": true, "saved.delete": true,
	"saved.update": true, "later.list": true, "client.dms": true, "drafts.list": true, "client.init": true,
	"client.channels": true, "users.prefs.get": true, "users.prefs.set": true, "subscriptions.thread.getView": true,
	"subscriptions.thread.add": true, "subscriptions.thread.remove": true, "subscriptions.thread.mark": true,
	"messages.list": true, "search.modules.people": true, "search.modules.channels": true,
	"search.modules.messages": true, "search.modules.files": true, "commands.list": true,
	// the results page's DMs tab and the popover's Recent messages: the
	// team host answers team_is_restricted for search.inline (verified
	// 2026-11-08)
	"search.modules.dms": true, "search.inline": true,
	"sidebar.dms": true, // team host: team_is_restricted (verified 2026-11-08)
	"bots.info":   true, // the team host answers bots_not_found for org apps (verified)
	// the full profile (popups.ProfileView): team host answers
	// team_is_restricted (verified 2026-11-08)
	"users.profile.getSections": true,
	// the huddle themes (HuddleBackground): the team host answers
	// team_is_restricted (verified 2026-11-11)
	"huddles.listBackgrounds": true,
}

// MaxConcurrent bounds in-flight Web API requests (be gentle with rate
// limits, which Slack counts per method per minute, not in flight). A cold
// start sends ~14 boot calls plus the open channel's 3–4 at once.
const MaxConcurrent = 16

// Client talks to Slack. It is safe for concurrent use.
type Client struct {
	HTTP  *http.Client
	Creds Creds
	// Allow, when set, is the only methods the client sends (Web API names,
	// edge calls as "edge:<path>"); anything else fails with ErrNotAllowed
	// before it leaves, and so do file uploads. Clack's introduction
	// renderer allows only the reads it needs. Set it before the first call.
	Allow func(method string) bool
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
	cl.HTTP = &http.Client{Transport: rt, Timeout: RequestTimeout, CheckRedirect: cl.checkRedirect}
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
			return &redirectRefused{fmt.Sprintf("slack: refusing redirect off Slack to %q", req.URL.Hostname())}
		}
	}
	// A file download redirected to a Slack page (sign-in, /ssb/…): stop.
	// Loading Slack's web pages with the cookie from outside the browser
	// can end the session.
	if cookieURL(req.URL) {
		if req.Method == "" || req.Method == http.MethodGet {
			if !FileURL(req.URL) {
				return ErrNotAFile
			}
		} else if !EndpointHop(req.URL, via) {
			// A 307 or 308 keeps a POST (an API call, an upload) and the
			// cookie Go copies to the hop: it may only move to another
			// Web API method or the same path, never to a page.
			return ErrNotAFile
		}
	}
	return nil
}

// EndpointHop reports whether a non-GET redirect hop to u that keeps the
// session cookie stays on an endpoint, not a page: a plain path
// (plainPath) that is a Web API method (/api/…) other than the ones that
// end the session (SignOutURL), or the path the first request went to.
// Client.checkRedirect follows a 307 or 308 that keeps a POST only when it
// holds, and so does Taut's plugin fetch.
func EndpointHop(u *url.URL, via []*http.Request) bool {
	if u == nil || !plainPath(u) || SignOutURL(u) {
		return false
	}
	wire := u.EscapedPath()
	if strings.HasPrefix(wire, "/api/") {
		return true
	}
	return len(via) > 0 && via[0].URL != nil && wire == via[0].URL.EscapedPath()
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
	// Status is the HTTP status the answer came with (Slack answers
	// {"ok":false} with 200); 0 when it isn't known.
	Status int
}

func (e *Error) Error() string { return "slack " + e.Method + ": " + e.Code }

// HTTPError is a request answered with an HTTP error status (a Web API
// call's 4xx or 5xx, a download's non-200). Its text is the one the client
// has always reported.
type HTTPError struct {
	Method string // the Web API method; "" for a download
	Status int
	// RetryAfter is the answer's Retry-After in whole seconds (0: none or
	// not a number), as Slack's BaseRequest reads it.
	RetryAfter int
	// Body is the answer (memory only, never logged) when the call's
	// observer asked for answers (ObserveAttempts): Slack's request hands
	// it on as the error's response.
	Body []byte
	text string
}

func (e *HTTPError) Error() string { return e.text }

// ErrRedirectRefused is a redirect Clack won't follow: off Slack's hosts
// from a request with the session's credentials, or, for a fetch without
// them, to a URL it doesn't load directly.
var ErrRedirectRefused = errors.New("slack: redirect refused")

// redirectRefused is ErrRedirectRefused with the text the client has
// always given it.
type redirectRefused struct{ text string }

func (e *redirectRefused) Error() string        { return e.text }
func (e *redirectRefused) Is(target error) bool { return target == ErrRedirectRefused }

// NewHTTPError is an HTTPError with text, for a download made outside this
// package (core.Fetch).
func NewHTTPError(status int, text string) *HTTPError {
	return &HTTPError{Status: status, text: text}
}

// FailureDetail describes a failed call for the log, with nothing the call
// sent or got back beyond its outcome: Slack's error code and the HTTP
// status for an {"ok":false} answer, the status for an HTTP error, else
// what went wrong before an answer came (timeout, network error, a Slack
// page where a file was asked for, a refused redirect or host, an answer
// that isn't the JSON asked for, …). Never a URL, a token or a body.
func FailureDetail(err error) string {
	var se *Error
	var he *HTTPError
	var ne net.Error
	var te *transportError
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &se):
		d := "error=" + logCode(se.Code)
		if se.Status != 0 {
			d += " http=" + strconv.Itoa(se.Status)
		}
		return d
	case errors.As(err, &he):
		return "http=" + strconv.Itoa(he.Status)
	case errors.Is(err, RateLimited):
		return "http=429 (rate limited)"
	case errors.Is(err, ErrSignedOut):
		return "signed out"
	case errors.Is(err, ErrNotAFile):
		return "answered with a Slack page, not a file"
	case errors.Is(err, ErrRedirectRefused):
		return "redirect off Slack refused"
	case errors.Is(err, ErrUntrustedHost), errors.Is(err, ErrBlockedAddr):
		return "refused (not a host Clack loads from)"
	case errors.Is(err, ErrNotAllowed), errors.Is(err, ErrForbiddenMethod):
		return "not allowed"
	case errors.Is(err, ErrTooLarge):
		return "response too large"
	case errors.As(err, &syn), errors.As(err, &typ):
		return "unexpected answer"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.As(err, &ne), errors.As(err, &te):
		return "network error"
	}
	return "failed"
}

// logCode is a Slack error code as FailureDetail logs it: codes are
// snake_case words, so anything else in one (it's the server's text) is
// dropped, and it's cut at 64 bytes.
func logCode(code string) string {
	b := make([]byte, 0, min(len(code), 64))
	for i := 0; i < len(code) && len(b) < 64; i++ {
		if c := code[i]; c == '_' || c == '.' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			b = append(b, c)
		}
	}
	if len(b) == 0 {
		return "?"
	}
	return string(b)
}

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

// OnEnterprise reports whether Call sends method to the enterprise host.
func (c *Client) OnEnterprise(method string) bool { return c.useEnt(method) }

// CallTeam is Call on the team host and token, whatever the method's
// route: for an answer the enterprise host can't give (bots.info's
// bots_not_found for a workspace's own bot).
func (c *Client) CallTeam(ctx context.Context, method string, params url.Values, out any) error {
	return c.callOn(ctx, method, false, params, out)
}

// forbiddenMethods would end the user's Slack session for every client.
// Clack never calls them; "Sign out" only forgets local credentials.
// Keys are lower case; refusedMethod compares case-insensitively.
var forbiddenMethods = map[string]bool{
	"auth.signout": true, "auth.revoke": true, "auth.enterprisesignout": true, "enterprisesignout": true,
}

// ErrNotAllowed is returned for a method Client.Allow doesn't allow.
var ErrNotAllowed = errors.New("slack: method not allowed on this client")

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

// SignOutURL reports whether u calls one of the methods that end the
// session (forbiddenMethods), its path read every way bannedPage reads it
// (readForms): /api/auth.signout, /API/Auth.Revoke;x, /api//auth.signout/,
// /api/%2561uth.signout, a fullwidth "a" (U+FF41) in /api/auth.signout and
// the like. A
// path plainPath refuses could be read as one, so it counts. The method
// name ends at the first character it can't hold (methodPrefix), so
// "auth.signout%3fx" is auth.signout.
func SignOutURL(u *url.URL) bool {
	if u == nil {
		return false
	}
	if !plainPath(u) {
		return true
	}
	forms, _ := readForms(u) // ok: plainPath already checked it
	for _, f := range forms {
		if segs := pathSegments(f); len(segs) >= 2 && segs[0] == "api" && forbiddenMethods[methodPrefix(segs[1])] {
			return true
		}
	}
	return false
}

// methodPrefix is the method name a path segment starts with: up to the
// first character a method name can't hold ("auth.signout%3fx" is
// auth.signout).
func methodPrefix(seg string) string {
	for i, r := range seg {
		if !(r == '.' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return seg[:i]
		}
	}
	return seg
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
// plus one file part (fileField, filename, data; Content-Type
// application/octet-stream). Routing and the team/enterprise fallback are
// the same as Call.
func (c *Client) CallMultipart(ctx context.Context, method string, fields url.Values, fileField, filename string, data []byte, out any) error {
	return c.CallMultipartFile(ctx, method, fields, MultipartFile{Field: fileField, Name: filename, Data: data}, out)
}

// MultipartFile is CallMultipartFile's file part.
type MultipartFile struct {
	Field, Name string
	// Type is the part's Content-Type, as a browser's FormData sends a
	// File's type ("" is application/octet-stream).
	Type string
	Data []byte
}

// CallMultipartFile is CallMultipart with the file part's Content-Type.
// A typed part's filename is escaped the way a browser's FormData escapes
// it (", CR and LF percent-encoded).
func (c *Client) CallMultipartFile(ctx context.Context, method string, fields url.Values, f MultipartFile, out any) error {
	ent := c.useEnt(method)
	err := c.multipartOn(ctx, method, ent, fields, f, out)
	var se *Error
	if c.Creds.EnterpriseToken != "" && errors.As(err, &se) &&
		((!ent && se.Code == "team_is_restricted") || (ent && se.Code == "enterprise_is_restricted")) {
		c.routeMu.Lock()
		c.learned[method] = !ent
		c.routeMu.Unlock()
		err = c.multipartOn(ctx, method, !ent, fields, f, out)
	}
	return err
}

// formDataName is a FormData filename as browsers write it (the HTML
// multipart/form-data encoding: ", CR and LF percent-encoded).
var formDataName = strings.NewReplacer(`"`, "%22", "\r", "%0D", "\n", "%0A")

func (c *Client) multipartOn(ctx context.Context, method string, ent bool, fields url.Values, f MultipartFile, out any) error {
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
	var fw io.Writer
	var err error
	if f.Type == "" {
		fw, err = mw.CreateFormFile(f.Field, f.Name)
	} else {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="`+formDataName.Replace(f.Field)+`"; filename="`+formDataName.Replace(f.Name)+`"`)
		h.Set("Content-Type", f.Type)
		fw, err = mw.CreatePart(h)
	}
	if err != nil {
		return err
	}
	if _, err := fw.Write(f.Data); err != nil {
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
	if c.Allow != nil && !c.Allow(method) {
		return fmt.Errorf("%w: %s", ErrNotAllowed, method)
	}
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
	if err != nil && slackRetrying(ctx) {
		err = c.retrySlack(ctx, method, err, func() error { return c.doPost(ctx, method, u, ctype, body, out) })
	}
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
		resp, err := c.do(req, method, body)
		if err != nil {
			unanswered(ctx, u, err)
			return &transportError{scrubErr(err)}
		}
		if resp.StatusCode == http.StatusTooManyRequests && slackRetrying(ctx) {
			b := observedBody(ctx, resp) // Slack's queue's schedule, in post
			answered(ctx, u, resp, b)
			return &HTTPError{Method: method, Status: resp.StatusCode, RetryAfter: retryAfterSecs(resp.Header.Get("Retry-After")), Body: b,
				text: fmt.Sprintf("slack %s: http %d", method, resp.StatusCode)}
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			answered(ctx, u, resp, observedBody(ctx, resp))
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
			unanswered(ctx, u, err) // cut off before the whole answer came
			return &transportError{scrubErr(err)}
		}
		answered(ctx, u, resp, data)
		if resp.StatusCode >= 400 && resp.StatusCode != 200 {
			return &HTTPError{Method: method, Status: resp.StatusCode, RetryAfter: retryAfterSecs(resp.Header.Get("Retry-After")), Body: kept(ctx, data),
				text: fmt.Sprintf("slack %s: http %d", method, resp.StatusCode)}
		}
		// {"ok":true…: no need to parse the whole body (7 MB for
		// emoji.list) just to read "ok"; the decode below validates it
		if !okFast(data) {
			var head struct {
				OK    *bool  `json:"ok"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(data, &head); err != nil {
				return &ParseError{Method: method, Body: kept(ctx, data), Err: err}
			}
			if head.OK != nil && !*head.OK {
				return &Error{Method: method, Code: head.Error, Body: data, Status: resp.StatusCode}
			}
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

// do sends req, and sends a read once more on a fresh connection when the
// pooled connection it went out on turned out to be dead: the request
// failed before any response with a reset, a closed connection or a lost
// local address (connretry.go). httptrace only observes the connection;
// the request leaves exactly as before, headers and all. The retry is
// built from body like the first attempt (desktop sessions sign a fresh
// DPoP proof for it in their transport). A SlackRetries call is sent once
// more whatever its method, as Chromium resends any request whose reused
// socket was dead (apiq.go).
func (c *Client) do(req *http.Request, method string, body []byte) (*http.Response, error) {
	var reused bool
	trace := &httptrace.ClientTrace{GotConn: func(i httptrace.GotConnInfo) { reused = i.Reused }}
	ctx := req.Context()
	resp, err := c.HTTP.Do(req.WithContext(httptrace.WithClientTrace(ctx, trace)))
	if err == nil || !reused || !connReset(err) || !(readOnlyMethod(method) || slackRetrying(ctx)) || ctx.Err() != nil || c.SignedOut() {
		return resp, err
	}
	log.Printf("slack: %s: pooled connection was dead (%v), retrying on a new one", method, scrubErr(err))
	retry := req.Clone(ctx)
	retry.Body = io.NopCloser(bytes.NewReader(body))
	retry.ContentLength = int64(len(body))
	retry.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	return c.HTTP.Do(retry)
}

// Download fetches an authenticated URL (files.slack.com etc) with the session
// cookie. It refuses any URL credsOK rejects (ErrUntrustedHost).
func (c *Client) Download(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := c.fileRequest(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return c.fileResponse(req)
}

// fileRequest is a GET of a file URL with the session cookie, or Download's
// refusal: signed out, a host that doesn't get credentials, a Slack page.
func (c *Client) fileRequest(ctx context.Context, rawURL string) (*http.Request, error) {
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
	return req, nil
}

// idleBody cuts a streaming body that brings nothing for its timer's
// length, and ends the request when closed.
type idleBody struct {
	io.ReadCloser
	timer  *time.Timer
	cancel context.CancelFunc
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.timer.Reset(StreamIdleTimeout)
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.Stop()
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// RetryAfterDuration is the answer's Retry-After, 0 for none.
func (e *HTTPError) RetryAfterDuration() time.Duration {
	return time.Duration(e.RetryAfter) * time.Second
}

// fileResponse sends a fileRequest (checkRedirect refuses hops off Slack
// and to its pages) and refuses an answer that is a page.
func (c *Client) fileResponse(req *http.Request) (*http.Response, error) {
	return c.fileResponseWith(c.HTTP, req)
}

func (c *Client) fileResponseWith(hc *http.Client, req *http.Request) (*http.Response, error) {
	resp, err := hc.Do(req)
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

// RangeInfo is what a ranged download was answered (DownloadRange).
type RangeInfo struct {
	// Status is the answer's HTTP status: 206 for the range, 200 for the
	// whole file.
	Status int
	// Start is the answer's first byte: the offset asked for on a 206, 0
	// on a 200.
	Start int64
	// Total is the file's size, from Content-Range on a 206 and
	// Content-Length on a 200; -1 when the answer doesn't say.
	Total int64
	// Seekable is whether the server answers ranges (a 206): another offset
	// can be asked for.
	Seekable bool
	// ETag and LastModified are the answer's validators: a later answer
	// with others is another version of the file.
	ETag, LastModified string
}

// The streaming GET's timers (DownloadRange): the client's RequestTimeout
// would cut a body that plays for more than a minute, so a ranged
// download has none; it waits at most StreamHeaderTimeout for its answer
// and is cut when its body brings nothing for StreamIdleTimeout.
var (
	StreamHeaderTimeout = RequestTimeout
	StreamIdleTimeout   = 30 * time.Second
)

// DownloadRange is Download with Range: bytes=off- (end < 0) or
// bytes=off-end. It keeps every guard of Download (signed out, credsOK,
// FileURL, the text/html refusal, checkRedirect refusing /ssb/ and hops
// off Slack) and returns the total from Content-Range. A 200 is the whole
// file from byte 0: not seekable. Any other status is an *HTTPError (a
// 416's RangeInfo still has the total), and the body is closed. Media
// requests ask for no content coding, as Chromium's media loader does: the
// offsets are the file's.
func (c *Client) DownloadRange(ctx context.Context, rawURL string, off, end int64) (*http.Response, RangeInfo, error) {
	info := RangeInfo{Total: -1}
	if off < 0 || (end >= 0 && end < off) {
		return nil, info, fmt.Errorf("slack: DownloadRange: bad range %d-%d", off, end)
	}
	req, err := c.fileRequest(ctx, rawURL)
	if err != nil {
		return nil, info, err
	}
	rng := "bytes=" + strconv.FormatInt(off, 10) + "-"
	if end >= 0 {
		rng += strconv.FormatInt(end, 10)
	}
	req.Header.Set("Range", rng)
	req.Header.Set("Accept-Encoding", "identity;q=1, *;q=0")
	// the client's transport and redirect checks, without its timeout
	stream := &http.Client{Transport: c.HTTP.Transport, CheckRedirect: c.HTTP.CheckRedirect, Jar: c.HTTP.Jar}
	sctx, cancel := context.WithCancel(ctx)
	timer := time.AfterFunc(StreamHeaderTimeout, cancel)
	resp, err := c.fileResponseWith(stream, req.WithContext(sctx))
	if err != nil {
		timer.Stop()
		cancel()
		return nil, info, err
	}
	timer.Reset(StreamIdleTimeout)
	resp.Body = &idleBody{ReadCloser: resp.Body, timer: timer, cancel: cancel}
	info.Status = resp.StatusCode
	info.ETag, info.LastModified = resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")
	switch resp.StatusCode {
	case http.StatusPartialContent:
		start, _, total, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok || start != off {
			resp.Body.Close()
			return nil, info, fmt.Errorf("slack: DownloadRange: answered %q for bytes %d-", resp.Header.Get("Content-Range"), off)
		}
		info.Start, info.Total, info.Seekable = start, total, true
		return resp, info, nil
	case http.StatusOK:
		info.Total = resp.ContentLength
		return resp, info, nil
	case http.StatusRequestedRangeNotSatisfiable:
		if _, _, total, ok := parseContentRange(resp.Header.Get("Content-Range")); ok {
			info.Total = total
		}
	}
	resp.Body.Close()
	he := NewHTTPError(resp.StatusCode, fmt.Sprintf("slack: DownloadRange: http %d", resp.StatusCode))
	he.RetryAfter = retryAfterSecs(resp.Header.Get("Retry-After"))
	return nil, info, he
}

// parseContentRange reads "bytes first-last/total" (total -1 for "*"), or
// a 416's "bytes */total" (first and last -1).
func parseContentRange(v string) (first, last, total int64, ok bool) {
	unit, rest, found := strings.Cut(strings.TrimSpace(v), " ")
	if !found || !strings.EqualFold(unit, "bytes") {
		return 0, 0, 0, false
	}
	span, size, found := strings.Cut(rest, "/")
	if !found {
		return 0, 0, 0, false
	}
	total = -1
	if size != "*" {
		n, err := strconv.ParseInt(size, 10, 64)
		if err != nil || n < 0 {
			return 0, 0, 0, false
		}
		total = n
	}
	if span == "*" {
		return -1, -1, total, total >= 0
	}
	a, b, found := strings.Cut(span, "-")
	if !found {
		return 0, 0, 0, false
	}
	first, err1 := strconv.ParseInt(a, 10, 64)
	last, err2 := strconv.ParseInt(b, 10, 64)
	if err1 != nil || err2 != nil || first < 0 || last < first || (total >= 0 && last >= total) {
		return 0, 0, 0, false
	}
	return first, last, total, true
}

// ErrNotAFile is returned by Download for a slack.com URL that isn't a file
// (a workspace web page, /ssb/…, /signin, a permalink). Clack never loads
// Slack's web pages with the session cookie: fetching them from outside the
// browser can end the session everywhere (see README "Sign in").
var ErrNotAFile = errors.New("slack: not a file URL; refusing to load a Slack page with the session cookie")

// filePaths are the path prefixes Slack serves files under, on its file
// hosts (files.slack.com, files-origin.slack.com, …) and on a workspace's
// own host: url_private and its downloads (/files-pri/), thumbnails
// (/files-tmb/), video streams (/files-preview/video-stream, the
// preview_url Slack's file viewer plays) and the safe and origin variants.
var filePaths = []string{"/files-pri/", "/files-tmb/", "/files-pri-safe/", "/files-origin/", "/files-preview/"}

// FileURL reports whether u is one of Slack's file paths, the only
// slack.com GETs that carry the session cookie (Download). The path
// decides on every host, the file hosts too: files.slack.com/ssb/redirect
// is a page like any other. The prefix is matched as it goes on the wire
// (EscapedPath, what RequestURI sends), so /%66iles-pri/… isn't one. A
// path a normalising server could read as another path (plainPath: dot
// segments however they are encoded) is never one either: Go sends
// /files-pri/../ssb/redirect as written, and a server that normalises it
// would answer the sign-out redirect to a cookie-bearing request. Slack's
// own file URLs never have them.
func FileURL(u *url.URL) bool {
	if u == nil || !plainPath(u) {
		return false
	}
	wire := u.EscapedPath()
	for _, p := range filePaths {
		if strings.HasPrefix(wire, p) {
			return true
		}
	}
	return false
}

// dotLike is "." and the characters Unicode compatibility normalisation
// (NFKC) folds to one or more dots: the fullwidth and small full stops, the
// one- and two-dot leaders, the ellipsis and the vertical presentation
// forms of the ellipsis and the two-dot leader, plus the space a segment
// like ". ." pads with, and U+FFFD, which a byte that isn't UTF-8 reads as
// (plainForm). readForm's NFKC already folds the look-alikes to '.' (and
// NBSP and the other compatibility spaces to ' '); they're listed too so
// the test doesn't rest on the tables alone. Written as code points so no
// look-alike sits in source.
var dotLike = ". " + string(rune(0xFF0E)) + string(rune(0xFE52)) + string(rune(0x2024)) + string(rune(0x2025)) +
	string(rune(0x2026)) + string(rune(0xFE19)) + string(rune(0xFE30)) + string(utf8.RuneError)

// maxDecodes is how many times plainPath decodes a path the way a lenient
// server might. A path that still changes after that many decodes is no
// path Slack makes, so it's refused.
const maxDecodes = 4

// plainPath reports whether u's path can't be read as another path by a
// server that normalises it, however many times it decodes and
// NFKC-normalises. Two checks, each over its own set of forms:
//   - The raw decode chain (lenientUnescape from the wire path, up to
//     maxDecodes) holds, at every stage, no byte a lenient UTF-8 decoder
//     could read as '.', '/' or '\' (plainBytes: an overlong lead, or
//     0xAE/0xAF/0xDC), no backslash, NUL or control byte.
//   - No form a normalising server reads (readForms) has a dot segment
//     (hasDotSegment). readForm folds the separators, so a '\' or a
//     fullwidth solidus there is a separator, not a refusal; only the wire
//     bytes are held to the stricter rule above. What each of those forms
//     decodes to is still checked for smuggled and control bytes
//     (plainDecoded), because NFKC can turn a fullwidth percent into an
//     escape the raw chain never decoded.
//
// An encoded slash that decodes to a real one only adds a segment, so a
// file named "uploads%2F2024.jpg" still works; what it decodes to is
// checked like the rest.
func plainPath(u *url.URL) bool {
	p := u.EscapedPath()
	for i := 0; ; i++ {
		if !plainBytes(p) {
			return false
		}
		next := lenientUnescape(p)
		if next == p {
			break
		}
		if i == maxDecodes {
			return false
		}
		p = next
	}
	forms, ok := readForms(u)
	if !ok {
		return false
	}
	for _, s := range forms {
		if hasDotSegment(s) {
			return false
		}
	}
	return true
}

// plainBytes checks one raw decode form for plainPath: a byte that isn't
// part of valid UTF-8 is refused when a lenient decoder could read it as
// '.', '/' or '\' (smuggles); any other reads as U+FFFD elsewhere. No
// backslash, NUL or other control byte. So a name with a literal "%"
// before two hex digits that aren't UTF-8 (100%Beef.jpg, a Latin-1
// caf%E9.pdf typed as it was saved) still works, and "..%BE" doesn't.
func plainBytes(p string) bool {
	for i := 0; i < len(p); {
		r, n := utf8.DecodeRuneInString(p[i:])
		if r == utf8.RuneError && n == 1 && smuggles(p, i) {
			return false
		}
		if r == '\\' || r < 0x20 || r == 0x7f {
			return false
		}
		i += n
	}
	return true
}

// readForms returns the forms a server that normalises Unicode could read
// u's path in: readForm (NFKC, invalid bytes as U+FFFD, separators folded)
// of the wire path, then of each further decode, until it stops changing
// or maxDecodes. ok is false if it still changes after that. NFKC folds a
// fullwidth or small percent (U+FF05, U+FE6A) to '%', so decoding must
// continue over readForm's output and not only over the raw escapes:
// "%EF%BC%852e" reads as "%2e", then as ".". These are the forms the dot,
// bannedPage and SignOutURL checks all compare, so each catches a path a
// decode→NFKC→decode server would resolve to a dot segment, /ssb/redirect
// or a sign-out method.
func readForms(u *url.URL) (forms []string, ok bool) {
	s := readForm(u.EscapedPath())
	for i := 0; ; i++ {
		forms = append(forms, s)
		dec := lenientUnescape(s)
		if !plainDecoded(dec) {
			return forms, false
		}
		next := readForm(dec)
		if next == s {
			return forms, true
		}
		if i == maxDecodes {
			return forms, false
		}
		s = next
	}
}

// plainDecoded checks one stage readForms decoded, before readForm reads
// its invalid bytes as U+FFFD. NFKC can make a '%' (U+FF05, U+FE6A), so a
// decode→NFKC→decode server meets bytes the raw chain never held:
// "%EF%BC%85c0%EF%BC%85af" reads as "%c0%af", the overlong '/'. A byte a
// lenient decoder could read as '.', '/' or '\' (smuggles) or a control
// byte is refused there as on the raw chain. A backslash isn't: readForm
// folds the fullwidth reverse solidi to one, and hasDotSegment splits on
// it, so a name drawn with the fullwidth solidi (the (^o^) smiley the
// tests use) keeps working.
func plainDecoded(p string) bool {
	for i := 0; i < len(p); {
		r, n := utf8.DecodeRuneInString(p[i:])
		if r == utf8.RuneError && n == 1 && smuggles(p, i) {
			return false
		}
		if r < 0x20 || r == 0x7f {
			return false
		}
		i += n
	}
	return true
}

// hasDotSegment reports whether s, a form readForms already NFKC-read, has
// a path segment — split on '/' and '\', a ";…" parameter cut — made only
// of dots, dot look-alikes, padding spaces or U+FFFD (dotLike).
func hasDotSegment(s string) bool {
	for _, seg := range strings.FieldsFunc(s, pathSep) {
		if i := strings.IndexByte(seg, ';'); i >= 0 {
			seg = seg[:i]
		}
		if seg != "" && strings.Trim(seg, dotLike) == "" {
			return true
		}
	}
	return false
}

// readForm is form p as a server that normalises Unicode could read it:
// each byte that isn't UTF-8 as U+FFFD, then NFKC, which folds the
// fullwidth solidus U+FF0F to '/', the fullwidth and small reverse solidi
// U+FF3C and U+FE68 to '\', the fullwidth and small percent U+FF05 and
// U+FE6A to '%', the dot look-alikes to dots and NBSP and the other
// compatibility spaces to ' '. So "..%EF%BC%8Fssb" reads as "../ssb".
// Names keep working: NFKC composes an NFD "cafe" + U+0301 to the NFC one,
// and Japanese and the like are unchanged.
func readForm(p string) string {
	return norm.NFKC.String(strings.ToValidUTF8(p, string(utf8.RuneError)))
}

// pathSegments is a form readForms produced as bannedPage and SignOutURL
// compare it: lower-cased, split on '/' and '\', ";…" parameters cut,
// empty segments dropped.
func pathSegments(p string) []string {
	var segs []string
	for _, s := range strings.FieldsFunc(strings.ToLower(p), pathSep) {
		if i := strings.IndexByte(s, ';'); i >= 0 {
			s = s[:i]
		}
		if s != "" {
			segs = append(segs, s)
		}
	}
	return segs
}

// pathSep reports whether r separates segments for a server that reads a
// backslash as a slash, as some do.
func pathSep(r rune) bool { return r == '/' || r == '\\' }

// smuggles reports whether p[i], a byte that isn't part of valid UTF-8,
// is one a lenient decoder could read as an ASCII '.', '/' or '\': the
// lead of an overlong form (C0 and C1, E0 before 80-9F, F0 before 80-8F)
// or of a sequence longer than UTF-8 allows (F5-FF, when a continuation
// byte follows: the 5- and 6-byte overlongs of RFC 2279 and Perl's
// extended forms all need one, so a literal "%Fe" in 10%Fee.pdf is only
// an invalid byte), or 0xAE, 0xAF and 0xDC, which are '.', '/' and '\'
// with the high bit set, for a decoder that strips it.
func smuggles(p string, i int) bool {
	c, next := p[i], byte(0)
	if i+1 < len(p) {
		next = p[i+1]
	}
	switch {
	case c == 0xC0 || c == 0xC1:
		return true
	case c == 0xE0:
		return 0x80 <= next && next <= 0x9F
	case c == 0xF0:
		return 0x80 <= next && next <= 0x8F
	case c >= 0xF5:
		return 0x80 <= next && next <= 0xBF
	}
	return c == 0xAE || c == 0xAF || c == 0xDC
}

// lenientUnescape decodes every valid %XX escape in s and leaves any other
// "%" as it is, as the most permissive server would.
func lenientUnescape(s string) string {
	if strings.IndexByte(s, '%') < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b.WriteByte(unhex(s[i+1])<<4 | unhex(s[i+2]))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isHex(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

// UploadBytes sends file contents to an upload URL from
// files.getUploadURL the way Slack's UploadQueue does: a multipart POST of
// one "file" part. (Slack's XMLHttpRequest has no withCredentials, so a
// browser sends no cookie to files.slack.com; Clack keeps sending the
// session's cookies on every request, as its session tests require.)
// progress (may be nil) is called as the body is written: every ~64 KB
// and once at the end, with the multipart body size as total. It runs on
// the calling goroutine.
func (c *Client) UploadBytes(ctx context.Context, uploadURL, filename string, data []byte, progress func(sent, total int64)) error {
	if c.Allow != nil {
		return fmt.Errorf("%w: a file upload", ErrNotAllowed)
	}
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
