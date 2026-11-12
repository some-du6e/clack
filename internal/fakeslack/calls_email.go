package fakeslack

// Channel details' Start a Call ▾ and channel email address methods
// (core/chdetails_email.go). Every call is recorded (CallsEmailCalls).
// Nothing is dialled or mailed: calls.request answers a URL on the fake
// server, and the addresses are made up.
//
//   - calls.request {channel, app, type, phone_number}: {url,
//     desktop_protocol_url}, or the error SetCallsEmailError set.
//   - conversations.emailaddresses.info {conversation_id}: the channel's
//     address in conversation_email_addresses (none until create).
//   - conversations.emailaddresses.create: a new address
//     (conversation_email_address), "<id>@fakeclub.slack.example".
//   - conversations.emailaddresses.update {name, avatar_id, emoji,
//     use_default_icon}: the address with the new name and icon.
//   - conversations.emailaddresses.delete {address}: forgets it.
//   - avatars.upload (multipart image) and avatars.crop {avatar, cropbox}.

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
)

type callsEmailState struct {
	mu     sync.Mutex
	calls  []CallsEmailCall
	errs   map[string]string // method → error code
	addrs  map[string]map[string]any
	nextAv int
}

// CallsEmailCall is one of these methods' calls.
type CallsEmailCall struct {
	Method string
	Form   url.Values
}

// CallsEmailCalls returns the calls so far, in order.
func (s *Server) CallsEmailCalls() []CallsEmailCall {
	s.cem.mu.Lock()
	defer s.cem.mu.Unlock()
	return append([]CallsEmailCall(nil), s.cem.calls...)
}

// SetCallsEmailError makes method fail with code ("" clears it).
func (s *Server) SetCallsEmailError(method, code string) {
	s.cem.mu.Lock()
	defer s.cem.mu.Unlock()
	if s.cem.errs == nil {
		s.cem.errs = map[string]string{}
	}
	if code == "" {
		delete(s.cem.errs, method)
		return
	}
	s.cem.errs[method] = code
}

// SetChannelEmail gives ch an address before the calls (a fixture).
func (s *Server) SetChannelEmail(ch, address, name string) {
	s.cem.mu.Lock()
	defer s.cem.mu.Unlock()
	if s.cem.addrs == nil {
		s.cem.addrs = map[string]map[string]any{}
	}
	s.cem.addrs[ch] = map[string]any{"address": address, "name": name,
		"icons": map[string]any{"image_36": "https://a.slack-edge.com/80588/img/plugins/channel_email_address/service_36.png"}}
}

func (s *Server) handleCallsEmail(method string, f url.Values) (any, bool) {
	switch method {
	case "calls.request", "conversations.emailaddresses.info", "conversations.emailaddresses.create",
		"conversations.emailaddresses.update", "conversations.emailaddresses.delete", "avatars.upload", "avatars.crop":
	default:
		return nil, false
	}
	st := &s.cem
	st.mu.Lock()
	defer st.mu.Unlock()
	st.calls = append(st.calls, CallsEmailCall{Method: method, Form: f})
	if code := st.errs[method]; code != "" {
		return map[string]any{"ok": false, "error": code}, true
	}
	if st.addrs == nil {
		st.addrs = map[string]map[string]any{}
	}
	ch := f.Get("conversation_id")
	switch method {
	case "calls.request":
		u := s.URL + "fake-call/" + f.Get("app") + "?channel=" + url.QueryEscape(f.Get("channel")) + "&type=" + f.Get("type")
		return ok("url", u, "desktop_protocol_url", "fakecall://"+f.Get("app")), true
	case "conversations.emailaddresses.info":
		var list []any
		if a, found := st.addrs[ch]; found {
			list = append(list, a)
		}
		return ok("conversation_email_addresses", list), true
	case "conversations.emailaddresses.create":
		a := map[string]any{"address": strings.ToLower(ch) + "-a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5@fakeclub.slack.example", "name": "",
			"icons": map[string]any{"image_36": "https://a.slack-edge.com/80588/img/plugins/channel_email_address/service_36.png"}}
		st.addrs[ch] = a
		return ok("conversation_email_address", a), true
	case "conversations.emailaddresses.update":
		a, found := st.addrs[ch]
		if !found || a["address"] != f.Get("address") {
			return map[string]any{"ok": false, "error": "invalid_address"}, true
		}
		a["name"] = f.Get("name")
		icons := map[string]any{}
		switch {
		case f.Get("avatar_id") != "":
			icons["image_36"] = s.URL + "img/36x36/" + f.Get("avatar_id") + ".png"
		case f.Get("emoji") != "":
			icons["emoji"] = f.Get("emoji")
		default:
			icons["image_36"] = "https://a.slack-edge.com/80588/img/plugins/channel_email_address/service_36.png"
		}
		a["icons"] = icons
		return ok("conversation_email_addresses", []any{a}), true
	case "conversations.emailaddresses.delete":
		a, found := st.addrs[ch]
		if !found || a["address"] != f.Get("address") {
			return map[string]any{"ok": false, "error": "invalid_address"}, true
		}
		delete(st.addrs, ch)
		return ok(), true
	case "avatars.upload":
		st.nextAv++
		id := fmt.Sprintf("AV%05d", st.nextAv)
		return ok("id", id, "url", s.URL+"img/72x72/"+id+".png"), true
	default: // avatars.crop
		id := f.Get("avatar")
		return ok("id", id, "url", s.URL+"img/36x36/"+id+"-crop.png"), true
	}
}
