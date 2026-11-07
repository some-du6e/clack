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
)

// Creds is the on-disk credential file (~/.config/clack/creds.json).
type Creds struct {
	Token           string `json:"token"`               // team xoxc token
	Cookie          string `json:"cookie"`              // d cookie (xoxd-..., URL-encoded as stored by the browser)
	CookieDS        string `json:"cookie_ds,omitempty"` // optional d-s cookie sent alongside d
	Team            string `json:"team"`                // team id, e.g. T0266FRGM
	EnterpriseToken string `json:"enterprise_token"`    // org xoxc token (optional)
	EnterpriseID    string `json:"enterprise_id"`
	UserID          string `json:"user_id"`
	TeamURL         string `json:"team_url"`       // e.g. https://hackclub.slack.com/
	EnterpriseURL   string `json:"enterprise_url"` // e.g. https://hackclub.enterprise.slack.com/
}

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
	"client.channels": true, "users.prefs.get": true, "subscriptions.thread.getView": true,
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
}

// New builds a client with a tuned, keep-alive HTTP/2 transport.
func New(c Creds) *Client {
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     5 * time.Minute,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	base := "https://slack.com/api/"
	if c.TeamURL != "" {
		base = strings.TrimSuffix(c.TeamURL, "/") + "/api/"
	}
	ent := base
	if c.EnterpriseURL != "" {
		ent = strings.TrimSuffix(c.EnterpriseURL, "/") + "/api/"
	}
	cl := &Client{Creds: c, base: base, ent: ent, sem: make(chan struct{}, MaxConcurrent), learned: map[string]bool{}}
	cl.HTTP = &http.Client{Transport: tr, Timeout: 60 * time.Second, CheckRedirect: cl.checkRedirect}
	return cl
}

// credOK reports whether u may receive the session cookie: https on
// slack.com or a subdomain, or the configured API host itself (tests run
// against an httptest server there).
func (c *Client) credOK(u *url.URL) bool {
	if u == nil || u.User != nil {
		return false
	}
	if u.Scheme == "https" && IsSlackHost(u.Hostname()) {
		return true
	}
	for _, b := range []string{c.base, c.ent} {
		if bu, err := url.Parse(b); err == nil && bu.Scheme == u.Scheme && strings.EqualFold(bu.Host, u.Host) {
			return true
		}
	}
	return false
}

// checkRedirect never follows https to http, and drops the Cookie header
// on a hop to a host that mustn't see it (net/http only strips it for
// hosts that aren't a subdomain of the first one, so files.slack.com to
// an http:// or non-Slack host would otherwise keep it in some cases).
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("slack: too many redirects")
	}
	if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("slack: refusing redirect from https to " + req.URL.Scheme)
	}
	if !c.credOK(req.URL) {
		req.Header.Del("Cookie")
		req.Header.Del("Authorization")
	}
	return nil
}

// Error is a Slack API {"ok":false} error.
type Error struct {
	Method string
	Code   string
}

func (e *Error) Error() string { return "slack " + e.Method + ": " + e.Code }

// RateLimited is returned when Slack answers 429 too many times in a row.
var RateLimited = errors.New("slack: rate limited")

func (c *Client) cookieHeader() string {
	if c.Creds.CookieDS != "" {
		return "d=" + c.Creds.Cookie + "; d-s=" + c.Creds.CookieDS
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
var forbiddenMethods = map[string]bool{
	"auth.signout": true, "auth.revoke": true, "auth.enterpriseSignout": true, "enterpriseSignout": true,
}

// ErrForbiddenMethod is returned for methods that would end the session.
var ErrForbiddenMethod = errors.New("slack: method refused (it would sign you out everywhere)")

func (c *Client) callOn(ctx context.Context, method string, ent bool, params url.Values, out any) error {
	if forbiddenMethods[method] {
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
	if forbiddenMethods[method] {
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
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
		if err != nil {
			return scrubErr(err)
		}
		req.Header.Set("Content-Type", ctype)
		req.Header.Set("Cookie", c.cookieHeader())
		req.Header.Set("Origin", "https://app.slack.com")
		req.Header.Set("User-Agent", UserAgent)
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
			return &Error{Method: method, Code: head.Error}
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

// Download fetches an authenticated URL (files.slack.com etc) with the
// session cookie. The cookie only goes to https Slack hosts (see credOK);
// any other URL is fetched without it.
func (c *Client) Download(ctx context.Context, rawURL string) (*http.Response, error) {
	if c.SignedOut() {
		return nil, ErrSignedOut
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, scrubErr(err)
	}
	if c.credOK(req.URL) {
		req.Header.Set("Cookie", c.cookieHeader())
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.HTTP.Do(req)
	return resp, scrubErr(err)
}

// UserAgent mimics a desktop Chrome so Slack serves the normal web payloads.
const UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36"

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
	req.ContentLength = total
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if !c.credOK(req.URL) { // the file and the cookie only go to Slack over https
		return errors.New("slack upload: refusing upload URL on " + req.URL.Scheme + "://" + req.URL.Host)
	}
	req.Header.Set("Cookie", c.cookieHeader())
	req.Header.Set("Origin", "https://app.slack.com")
	req.Header.Set("User-Agent", UserAgent)
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
