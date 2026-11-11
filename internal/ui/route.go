package ui

import (
	"strings"
	"time"
)

// Route strings (see docs/NATIVE.md): "c/<id>", "c/<id>/t/<ts>", "c/<id>/m/<ts>",
// "activity", "dms", "later", "files", "threads", "huddles", "drafts",
// "directories", "search?q=", "home".
type Route string

// Channel returns the channel id for c/ routes.
func (r Route) Channel() string {
	s := string(r)
	if !strings.HasPrefix(s, "c/") {
		return ""
	}
	s = s[2:]
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i]
	}
	return s
}

// Thread returns the thread ts for c/<id>/t/<ts> routes.
func (r Route) Thread() string {
	s := string(r)
	if i := strings.Index(s, "/t/"); i >= 0 && strings.HasPrefix(s, "c/") {
		return s[i+3:]
	}
	return ""
}

// Message returns the message ts of c/<id>/m/<ts> routes (a permalink:
// the channel scrolled to the message, which flashes).
func (r Route) Message() string {
	s := string(r)
	if i := strings.Index(s, "/m/"); i >= 0 && strings.HasPrefix(s, "c/") {
		return s[i+3:]
	}
	return ""
}

// Page is the first path element ("c", "activity", "search", ...).
func (r Route) Page() string {
	s := string(r)
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		return s[:i]
	}
	return s
}

// Query returns the q= value of search routes.
func (r Route) Query() string {
	s := string(r)
	if i := strings.Index(s, "?q="); i >= 0 {
		return s[i+3:]
	}
	return ""
}

// router keeps a browser-style back/forward history, and Slack's recents
// (the client store's "recents", which the history menus list): every
// place shown, most recent first, with when it was shown.
type router struct {
	stack   []Route
	pos     int
	recents []visit
}

// visit is one entry of the recents.
type visit struct {
	Route Route
	At    time.Time
}

// maxRecents caps the recents (the history menus dedupe them by place).
const maxRecents = 200

// note records r as just shown.
func (h *router) note(r Route) {
	if r == "" {
		return
	}
	h.recents = append(h.recents, visit{})
	copy(h.recents[1:], h.recents)
	h.recents[0] = visit{Route: r, At: time.Now()}
	if len(h.recents) > maxRecents {
		h.recents = h.recents[:maxRecents]
	}
}

// Recents returns the recents, most recent first (shared: don't modify).
func (h *router) Recents() []visit { return h.recents }

// ClearRecents is the History dialog's Clear history.
func (h *router) ClearRecents() { h.recents = nil }

func (h *router) Current() Route {
	if len(h.stack) == 0 {
		return ""
	}
	return h.stack[h.pos]
}

func (h *router) Push(r Route) {
	h.note(r)
	if len(h.stack) > 0 && h.stack[h.pos] == r {
		return
	}
	if len(h.stack) > 0 {
		h.stack = h.stack[:h.pos+1]
	}
	h.stack = append(h.stack, r)
	if len(h.stack) > 200 {
		h.stack = h.stack[len(h.stack)-200:]
	}
	h.pos = len(h.stack) - 1
}

// Replace swaps the current entry (e.g. thread open/close inside a channel).
func (h *router) Replace(r Route) {
	if len(h.stack) == 0 {
		h.Push(r)
		return
	}
	h.stack[h.pos] = r
	h.note(r)
}

func (h *router) CanBack() bool    { return h.pos > 0 }
func (h *router) CanForward() bool { return h.pos < len(h.stack)-1 }

func (h *router) Back() (Route, bool) {
	if !h.CanBack() {
		return "", false
	}
	h.pos--
	h.note(h.stack[h.pos])
	return h.stack[h.pos], true
}

func (h *router) Forward() (Route, bool) {
	if !h.CanForward() {
		return "", false
	}
	h.pos++
	h.note(h.stack[h.pos])
	return h.stack[h.pos], true
}

// Recent returns up to n distinct past routes, most recent first.
func (h *router) Recent(n int) []Route {
	seen := map[Route]bool{}
	var out []Route
	for i := len(h.stack) - 1; i >= 0 && len(out) < n; i-- {
		r := h.stack[i]
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}
