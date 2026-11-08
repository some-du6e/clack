package core

import (
	"strconv"
	"strings"
	"time"

	"github.com/some-du6e/yoloslackgoclient/internal/slack"
	"github.com/some-du6e/yoloslackgoclient/internal/store"
)

// Which DMs and group DMs are sidebar candidates (Slack's rule, see
// sidebar_rules.go): open ones (is_open), plus closed ones with unread
// messages. users.conversations lists closed DMs too and carries no is_open;
// sidebar.dms does (it agrees with client.userBoot's is_open list). A closed
// DM also reopens on a new message, im_open / mpim_open, or Clack opening it.
// Until sidebar.dms answers every DM counts as open (Clack's old behaviour).

type dmState struct {
	known    bool                 // sidebar.dms answered
	open     map[string]bool      // DM -> is_open (missing: closed when known)
	closedAt map[string]string    // closed DM -> newest ts that does not reopen it
	openedAt map[string]time.Time // opened during this session
}

// nowTS is the current time as a Slack ts, for reopen watermarks.
var nowTS = func() string { return strconv.FormatInt(time.Now().Unix(), 10) + ".000000" }

// setSidebarDMs records sidebar.dms' open flags.
func (a *App) setSidebarDMs(list []slack.SidebarDM) {
	now := nowTS()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dms.known = true
	a.dms.open = make(map[string]bool, len(list))
	a.dms.closedAt = map[string]string{}
	for _, d := range list {
		if d.ID == "" {
			continue
		}
		a.dms.open[d.ID] = d.IsOpen
		if !d.IsOpen {
			a.dms.closedAt[d.ID] = a.reopenMark(d.ID, now)
		}
	}
}

// reopenMark is the ts a closed DM's latest message must pass to reopen it:
// the newer of its latest message and now (so only new messages count).
func (a *App) reopenMark(ch, now string) string {
	e, _ := a.S.Count(ch)
	if store.TSLess(now, e.Latest) {
		return e.Latest
	}
	return now
}

// dmOpen reports whether a DM is open (in Slack's sidebar).
func (a *App) dmOpen(ch string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if v, ok := a.dms.open[ch]; ok {
		return v
	}
	return !a.dms.known
}

// markDMOpen opens a DM in the sidebar; it reports whether that changed it.
func (a *App) markDMOpen(ch string) bool {
	if !a.isDM(ch) {
		return false
	}
	return a.openDM(ch)
}

// openDM marks ch open (a known DM or group DM) and reports whether that
// changed the sidebar.
func (a *App) openDM(ch string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	was, ok := a.dms.open[ch]
	if a.dms.open == nil {
		a.dms.open = map[string]bool{}
	}
	if a.dms.openedAt == nil {
		a.dms.openedAt = map[string]time.Time{}
	}
	a.dms.open[ch] = true
	a.dms.openedAt[ch] = time.Now()
	delete(a.dms.closedAt, ch)
	prev := !a.dms.known
	if ok {
		prev = was
	}
	return !prev
}

// markDMClosed drops a DM from the sidebar until it gets a new message.
func (a *App) markDMClosed(ch string) {
	mark := a.reopenMark(ch, nowTS())
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dms.open == nil {
		a.dms.open = map[string]bool{}
	}
	if a.dms.closedAt == nil {
		a.dms.closedAt = map[string]string{}
	}
	a.dms.open[ch] = false
	a.dms.closedAt[ch] = mark
	delete(a.dms.openedAt, ch)
}

// reopenActive reopens closed DMs whose latest message is newer than when
// they were closed (realtime messages and polled counts both land in the
// store's counts). It reports whether any reopened.
func (a *App) reopenActive() bool {
	a.mu.Lock()
	marks := make(map[string]string, len(a.dms.closedAt))
	for id, ts := range a.dms.closedAt {
		marks[id] = ts
	}
	a.mu.Unlock()
	changed := false
	for id, mark := range marks {
		if e, ok := a.S.Count(id); ok && store.TSLess(mark, e.Latest) {
			if a.openDM(id) {
				changed = true
			}
		}
	}
	return changed
}

// isDM reports whether ch is a DM or group DM.
func (a *App) isDM(ch string) bool {
	if c, ok := a.S.Conv(ch); ok {
		return c.IsIM || c.IsMPIM
	}
	return strings.HasPrefix(ch, "D")
}
