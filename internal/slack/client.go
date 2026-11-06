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
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Creds is the on-disk credential file (~/.config/yoloslack/creds.json).
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
// (they answer team_is_restricted on the team host).
var entMethods = map[string]bool{
	"client.counts": true, "users.channelSections.list": true, "client.getWebSocketURL": true,
	"users.channelSections.channels.bulkUpdate": true, "users.channelSections.create": true,
	"users.channelSections.update": true, "users.channelSections.delete": true,
	"activity.feed": true, "activity.views": true, "saved.list": true, "later.list": true, "client.dms": true,
	"drafts.list": true, "client.init": true, "client.channels": true, "users.prefs.get": true,
}

// Client talks to Slack. It is safe for concurrent use.
type Client struct {
	HTTP  *http.Client
	Creds Creds
	base  string // https://<team>.slack.com/api/
	ent   string // https://<org>.enterprise.slack.com/api/
}

// New builds a client with a tuned, keep-alive HTTP/2 transport.
func New(c Creds) *Client {
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
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
	return &Client{HTTP: &http.Client{Transport: tr, Timeout: 60 * time.Second}, Creds: c, base: base, ent: ent}
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

// Call invokes a Web API method with form params using the team token.
// out may be nil or a pointer to decode the JSON into.
func (c *Client) Call(ctx context.Context, method string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	base, tok := c.base, c.Creds.Token
	if entMethods[method] && c.Creds.EnterpriseToken != "" {
		base, tok = c.ent, c.Creds.EnterpriseToken
	}
	params.Set("token", tok)
	return c.post(ctx, method, base+method, "application/x-www-form-urlencoded", []byte(params.Encode()), out)
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

func (c *Client) post(ctx context.Context, method, u, ctype string, body []byte, out any) error {
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
