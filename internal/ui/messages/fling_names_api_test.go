package messages

import (
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

// knowNames puts every member and channel the fake's history can name into
// the store before the history loads, so a fling's idle frames have
// nothing arriving that changes a row. fling-22px: a mention of a channel
// the store didn't hold was looked up (conversations.info) as its row first
// drew, and the name landing between two frames in the row on the anchor
// line ("#C0FAKE00004" became "#berka-lashi") made that row a Compact line
// shorter; AnchorLayout keeps that row's top, so the rows under it moved
// 22 px. The harness's edge URL isn't where the fake serves edge (it has
// /edge/ for /api/edge/), so EnsureUsers never fetched a member in it:
// the store takes the fake's directory as users/info would answer it.
// The env's own channel and the viewer keep the names the harness gave
// them.
func (e *flingEnv) knowNames() {
	var us []slack.User
	for _, id := range e.fs.UserIDs() {
		if id == fakeslack.SelfID {
			continue
		}
		if u, ok := e.fs.User(id); ok {
			us = append(us, u)
		}
	}
	e.app.S.PutUsers(us)
	for _, id := range e.fs.ChannelIDs() {
		if id == e.v.ch {
			continue
		}
		if c, ok := e.fs.Conv(id); ok {
			e.app.S.PutConv(c)
		}
	}
}
