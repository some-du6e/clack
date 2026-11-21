package views

// The reload rule's way in for the tests (activity_order.go
// actReloadKeeps), kept apart so a red run on an older tree can map it
// onto that tree's load options.

import "github.com/some-du6e/clack/internal/core"

// actReloadCase is a reload's view and what happened while it was out.
type actReloadCase struct {
	before  []string // the list's keys when the request went out (nil: cur's)
	touched []string // the keys a realtime entry upserted meanwhile
	marks   []string // the rows marked unread here (Unreads)
	unreads bool     // the Unreads view
	vip     bool     // the VIP sort
	filters bool     // a type tab (Mentions, Threads, DMs) or another filter
}

// actReloadTest joins a reload's page to the list cur as load does.
func actReloadTest(page, cur core.ActivityPage, c actReloadCase) core.ActivityPage {
	o := actUpsertOpts{reload: true, sticky: c.unreads, vip: c.vip, before: map[string]bool{}}
	if c.before == nil {
		for i := range cur.Items {
			o.before[actKey(&cur.Items[i])] = true
		}
	}
	for _, k := range c.before {
		o.before[k] = true
	}
	if len(c.touched) > 0 {
		o.touched = map[string]bool{}
		for _, k := range c.touched {
			o.touched[k] = true
		}
	}
	if c.unreads && len(c.marks) > 0 {
		o.unreadMarks = map[string]bool{}
		for _, k := range c.marks {
			o.unreadMarks[k] = true
		}
	}
	return actUpsertPage(page, cur, o)
}
