package ui

// Work package W26 (docs/GAPS-2026-11-12.md gap 15) in the whole window,
// on internal/fakeslack's thread header fixtures (synthetic; nothing
// talks to Slack): the Threads view's Enter hands the keyboard focus to
// the channel's message list at the root (FocusMessage, Slack's
// transitionFocusTo MESSAGE_PANE_MESSAGE_LIST with uiState.ts), and
// LaterLabel's "View to-dos" lands on To-dos with the message beside it.

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestThreadsEnterFocusesChannelRoot(t *testing.T) {
	e := threadHeaderEnv(t, false)
	c := e.fs.ThreadHeaderCases()[0]
	e.a.Navigate("c/" + c.Channel + "/m/" + fakeslack.ThreadRootTS)
	e.a.FocusMessage(c.Channel, fakeslack.ThreadRootTS)
	sel := func() string {
		if l, ok := e.a.convoFor(c.Channel, "").list.(interface{ KeyboardSelected() string }); ok {
			return l.KeyboardSelected()
		}
		return ""
	}
	e.until("the channel list focused at the root", 10*time.Second, func() bool { return sel() == fakeslack.ThreadRootTS })
}

func TestViewTodosOpensBeside(t *testing.T) {
	e := threadHeaderEnv(t, false)
	c := e.fs.ThreadHeaderCases()[0]
	e.a.ViewTodo(c.Channel, fakeslack.ThreadRootTS, "")
	e.frame()
	if r := e.a.router.Current(); r != "later" {
		t.Fatalf("View to-dos went to %q", r)
	}
	if s := e.a.secondary; s.kind != secChannel || s.channel != c.Channel {
		t.Fatalf("the message isn't beside To-dos: secondary %v %s", s.kind, s.channel)
	}
	if !e.a.SecondaryShows(c.Channel, "") {
		t.Error("SecondaryShows doesn't report the conversation")
	}
	// a reply: its thread
	e.a.ViewTodo(c.Channel, "1700000999.000100", fakeslack.ThreadRootTS)
	e.frame()
	if s := e.a.secondary; s.kind != secThread || s.channel != c.Channel || s.ts != fakeslack.ThreadRootTS {
		t.Fatalf("a reply's View to-dos: secondary %v %s/%s", s.kind, s.channel, s.ts)
	}
	if r := e.a.router.Current(); r != "later" {
		t.Fatalf("a reply's View to-dos left To-dos for %q", r)
	}
}
