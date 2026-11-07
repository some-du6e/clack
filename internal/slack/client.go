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
	Token           string `json:"token"`            // team xoxc token
	Cookie          string `json:"cookie"`           // d cookie (xoxd-..., URL-encoded as stored by the browser)
	Team            string `json:"team"`             // team id, e.g. T0266FRGM
	EnterpriseToken string `json:"enterprise_token"` // org xoxc token (optional)
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
	return &Client{HTTP: &http.Client{Transport: tr, Timeout: 60 * time.Second}, Creds: c, base: base, ent: ent,
		sem: make(chan struct{}, MaxConcurrent), learned: map[string]bool{}}
}

// Error is a Slack API {"ok":false} error.
type Error struct {
	Method string
	Code   string
}

func (e *Error) Error() string { return "slack " + e.Method + ": " + e.Code }

// RateLimited is returned when Slack answers 429 too many times in a row.
var RateLimited = errors.New("slack: rate limited")

func (c *Client) cookieHeader() string { return "d=" + c.Creds.Cookie }

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

func (c *Client) callOn(ctx context.Context, method string, ent bool, params url.Values, out any) error {
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
	u := "https://edgeapi.slack.com/cache/" + ent + "/" + path + "?_x_app_name=client"
	return c.post(ctx, "edge:"+path, u, "text/plain;charset=UTF-8", b, out)
}

// SlowCall is the duration above which a call's method name and timing are logged.
var SlowCall = 1500 * time.Millisecond

func (c *Client) post(ctx context.Context, method, u, ctype string, body []byte, out any) error {
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
			return err
		}
		req.Header.Set("Content-Type", ctype)
		req.Header.Set("Cookie", c.cookieHeader())
		req.Header.Set("Origin", "https://app.slack.com")
		req.Header.Set("User-Agent", UserAgent)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return err
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
			return err
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

// Download fetches an authenticated URL (files.slack.com etc) with the session cookie.
func (c *Client) Download(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", c.cookieHeader())
	req.Header.Set("User-Agent", UserAgent)
	return c.HTTP.Do(req)
}

// UserAgent mimics a desktop Chrome so Slack serves the normal web payloads.
const UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 yoloslack/0.1"

// UploadBytes sends file contents to an upload URL from
// files.getUploadURLExternal. progress (may be nil) is called as the body
// is written: every ~64 KB and once at the end, with the multipart body size
// as total. It runs on the calling goroutine.
func (c *Client) UploadBytes(ctx context.Context, uploadURL, filename string, data []byte, progress func(sent, total int64)) error {
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
		return err
	}
	req.ContentLength = total
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Cookie", c.cookieHeader())
	req.Header.Set("Origin", "https://app.slack.com")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
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
