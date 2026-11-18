package ui

import (
	"log"
	"path"
	"runtime"
	"strconv"
	"strings"

	"github.com/some-du6e/clack/internal/logtag"
)

// What clack.log says about navigations a report may ask about: one line
// per thread pane that opens (the thread, the reply it starts at, and
// where the open came from), per jumpToMessage, and per click on a
// notification (notify.go). The ids are docs/INSPECT.md's tags
// (internal/logtag: kind and six hex digits of a salted SHA-256,
// "ch:3fa2b1", "ts:9c01de"), with one random salt per run that is never
// written anywhere: within one log the same message keeps its tag, so a
// line can show that a thread's root was a reply, and the history, tab
// and member lines a navigation writes tag theirs with the same salt, so
// nothing names a conversation or a message. No message text is ever
// logged.

// logTag is id's tag ("-" for none).
func logTag(id string) string { return logtag.ID(id) }

// navVia is the source of the navigation under way: what its entry point
// named (withVia: a notification, a link), else the first caller outside
// the router (callerOutsideRouter).
func (a *App) navVia() string {
	if a.openVia != "" {
		return a.openVia
	}
	return callerOutsideRouter()
}

// withVia runs f with via as the source a log line names, for an entry
// point that hands the navigation on (maybe later, after a lookup).
func (a *App) withVia(via string, f func()) {
	old := a.openVia
	a.openVia = via
	defer func() { a.openVia = old }()
	f()
}

// logThreadOpen is the line for a thread pane opening: the thread, the
// reply it starts at ("" none), where it opens ("" the secondary pane)
// and where the open came from.
func (a *App) logThreadOpen(ch, ts, at, where string) {
	s := ""
	if at != "" {
		s = " at " + logTag(at)
	}
	if where != "" {
		s += " " + where
	}
	log.Printf("ui: thread opened: %s %s%s (%s)", logTag(ch), logTag(ts), s, a.navVia())
}

// routerFuncs are the ui root's navigation plumbing; their closures
// (".funcN") count too.
var routerFuncs = []string{
	"(*App).OpenThread", "(*App).openThread", "(*App).OpenThreadAt", "(*App).openThreadAt", "(*App).openThreadAtIn",
	"(*App).OpenThreadReply", "(*App).Navigate", "(*App).afterRoute", "(*App).jumpToMessage", "(*App).jumpMessage",
	"(*App).jumpAfterLookup", "(*App).jumpViaRoutes", "(*App).jumpSplit", "(*App).jumpLatest", "(*App).JumpToMessage",
	"(*App).OpenPermalink", "(*App).openLinkIn", "(*App).FollowLink", "(*App).withVia", "(*App).navVia", "(*App).logThreadOpen",
	"(*App).OpenSplit", "(*App).inPane", "(*App).OpenAtMessage", "(*App).replyAsThread", "callerOutsideRouter",
}

// callerOutsideRouter is the first caller outside the router's plumbing,
// as "ui/messages.(*richHandler).open host.go:229".
func callerOutsideRouter() string {
	var pcs [40]uintptr
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs[:])])
	for {
		f, more := frames.Next()
		name := strings.TrimPrefix(f.Function, "github.com/some-du6e/clack/internal/")
		if f.Function != "" && !isRouterFunc(name) {
			return name + " " + path.Base(f.File) + ":" + strconv.Itoa(f.Line)
		}
		if !more {
			return "?"
		}
	}
}

func isRouterFunc(name string) bool {
	rest, ok := strings.CutPrefix(name, "ui.")
	if !ok {
		return false
	}
	for _, f := range routerFuncs {
		if rest == f || strings.HasPrefix(rest, f+".func") {
			return true
		}
	}
	return false
}
