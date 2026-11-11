package views

// The Unreads view's model: Slack's UnreadsView frames and
// getRowsAndKeys rows (shared chunk 2cdE, CDN build bv1-13, module
// "2cdE" in gantry-v2-async-shared-js_modern_components_unreads-view_…),
// without drawing. unreads.go draws them.
//
//   - Frames (computeInitialState): one per unread conversation the
//     model counts (shouldCountUnreadsInModel: not muted, a member, not
//     archived), in all_unreads_sort_order (getAllUnreadsSortOrder: a
//     valid pref, else "priority" on a paid team, "sidebar" otherwise),
//     filtered to all_unreads_section_filter's section, each from its
//     last read to its latest message. Frames are built when the view
//     opens and on refresh only: Slack's list doesn't reorder under you.
//   - Rows: per frame a header, then (expanded) day dividers when the
//     first unread message isn't today (else a 20px top spacer), an
//     unread divider at the read cursor, the messages after the frame's
//     start up to its end, then "Loading more messages…" while the frame
//     isn't fully fetched (the list stops there, as getRowsAndKeys does),
//     "N new messages" for messages after its end, or a 20px bottom
//     spacer. The footer ("Mark All Messages Read") comes once every
//     frame is loaded.

import (
	"sort"
	"strings"
	"time"

	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
)

// unrSortOrders are getAllUnreadsSortOrder's valid values (nq).
var unrSortOrders = []string{"alphabetical", "priority", "oldest", "newest", "sidebar"}

// unrAllSections is the section filter's "all" value (APnt _2).
const unrAllSections = "all_sections"

// unrFrame is one conversation in the view (Slack's frame).
type unrFrame struct {
	ch, kind, name   string
	start, end       string // shown: start < ts <= end
	initial          string // initialLastRead
	marked           int    // markedMsgCount
	msgs             []core.Message
	reachedEnd       bool
	loading          bool
	collapsed        bool
	created          int64
	priority         float64
	starred          bool
	mentions         int
	sliceEnd         string // newest fetched ts (fetchPost's oldest)
	gone             bool   // removed (empty after fetching)
	toggle, nameLink widget.Clickable
	markBtn, undo    widget.Clickable
	newMsgs, newer   widget.Clickable
}

// visible are the frame's messages after start up to end.
func (f *unrFrame) visible() []core.Message {
	var out []core.Message
	for i := range f.msgs {
		ts := f.msgs[i].TS
		if actTSLess(f.start, ts) && !actTSLess(f.end, ts) {
			out = append(out, f.msgs[i])
		}
	}
	return out
}

// after counts the messages newer than end (show-newer).
func (f *unrFrame) after() int {
	n := 0
	for i := range f.msgs {
		if actTSLess(f.end, f.msgs[i].TS) {
			n++
		}
	}
	return n
}

// latest is the newest message ts the frame knows (the hydrated frame's
// latest), else its end.
func (f *unrFrame) latest() string {
	l := f.end
	for i := range f.msgs {
		if actTSLess(l, f.msgs[i].TS) {
			l = f.msgs[i].TS
		}
	}
	return l
}

// add merges fetched or realtime messages, keeping ts order.
func (f *unrFrame) add(ms []core.Message) {
	seen := make(map[string]int, len(f.msgs))
	for i := range f.msgs {
		seen[f.msgs[i].TS] = i
	}
	for _, m := range ms {
		if i, ok := seen[m.TS]; ok {
			f.msgs[i] = m
			continue
		}
		seen[m.TS] = len(f.msgs)
		f.msgs = append(f.msgs, m)
	}
	sort.SliceStable(f.msgs, func(i, j int) bool { return actTSLess(f.msgs[i].TS, f.msgs[j].TS) })
}

// headerState is Header's I: 0 messages shown, 1 collapsed, 2 marked
// read, 3 newer messages only.
func (f *unrFrame) headerState(shown, after int) int {
	switch {
	case shown == 0 && f.marked > 0 && after == 0:
		return 2
	case shown == 0 && after > 0:
		return 3
	case f.collapsed:
		return 1
	}
	return 0
}

type unrRowKind uint8

const (
	unrHeader unrRowKind = iota
	unrSpacerTop
	unrDay
	unrUnreadLine
	unrMsgRow
	unrLoading
	unrShowNewer
	unrSpacerBottom
	unrFooter
)

type unrRow struct {
	kind   unrRowKind
	f      *unrFrame
	msg    *core.Message
	header bool // a message row that shows its sender (not adjacent)
	day    time.Time
	unread bool // a day divider that starts the unread messages
	count  int
}

// unrCand is an unread conversation before it becomes a frame
// (unreads_view_mapStateToProps' channels).
type unrCand struct {
	id, name, kind, lastRead, latest string
	starred                          bool
	priority                         float64
	mentions                         int
	created                          int64
}

func unrIsDM(kind string) bool { return kind == "im" || kind == "mpim" }

// unrSort is computeInitialState's sort by order.
func unrSort(cs []unrCand, order string, sections []core.Section) []unrCand {
	out := append([]unrCand(nil), cs...)
	less := func(a, b string) bool { return strings.ToLower(a) < strings.ToLower(b) }
	byStar := func(f func([]unrCand) []unrCand) []unrCand {
		var st, no []unrCand
		for _, c := range out {
			if c.starred {
				st = append(st, c)
			} else {
				no = append(no, c)
			}
		}
		return append(f(st), f(no)...)
	}
	byType := func(cs []unrCand) (chans, ims, mpims []unrCand) {
		for _, c := range cs {
			switch c.kind {
			case "im":
				ims = append(ims, c)
			case "mpim":
				mpims = append(mpims, c)
			default:
				chans = append(chans, c)
			}
		}
		return
	}
	alpha := func(cs []unrCand) []unrCand {
		sort.SliceStable(cs, func(i, j int) bool { return less(cs[i].name, cs[j].name) })
		return cs
	}
	prio := func(cs []unrCand) []unrCand {
		sort.SliceStable(cs, func(i, j int) bool {
			if cs[i].priority == cs[j].priority {
				return less(cs[i].name, cs[j].name)
			}
			return cs[i].priority > cs[j].priority
		})
		return cs
	}
	switch order {
	case "priority": // sortScientifically
		return byStar(func(cs []unrCand) []unrCand {
			chans, ims, mpims := byType(cs)
			var hm, nm []unrCand
			for _, c := range chans {
				if c.mentions > 0 {
					hm = append(hm, c)
				} else {
					nm = append(nm, c)
				}
			}
			r := append(prio(hm), prio(nm)...)
			return append(append(r, prio(ims)...), prio(mpims)...)
		})
	case "newest": // reverse(sortBy(latest))
		sort.SliceStable(out, func(i, j int) bool { return actTSLess(out[j].latest, out[i].latest) })
		return out
	case "oldest": // sortBy(first unread ts, else last read)
		key := func(c unrCand) string {
			if c.lastRead == "0000000000.000000" {
				return actTS(c.created)
			}
			return c.lastRead
		}
		sort.SliceStable(out, func(i, j int) bool { return actTSLess(key(out[i]), key(out[j])) })
		return out
	case "sidebar": // sortSidebar: sections' channelsAssociatedOrdered, then the rest
		idx := map[string]int{}
		for i, c := range out {
			idx[c.id] = i
		}
		var r []unrCand
		used := map[string]bool{}
		for _, s := range sections {
			ids := s.All
			if len(ids) == 0 {
				ids = s.Channels
			}
			for _, id := range ids {
				if i, ok := idx[id]; ok && !used[id] {
					used[id] = true
					r = append(r, out[i])
				}
			}
		}
		for _, c := range out {
			if !used[c.id] {
				r = append(r, c)
			}
		}
		return r
	}
	// sortAlphabetically: starred first, channels, then DMs, then group DMs
	return byStar(func(cs []unrCand) []unrCand {
		chans, ims, mpims := byType(cs)
		return append(append(alpha(chans), alpha(ims)...), alpha(mpims)...)
	})
}

func actTS(sec int64) string { return strings.TrimSpace(formatInt(sec)) + ".000000" }

func formatInt(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// unrGroupable is the channel list's isAdjacentMessage: b continues a's
// group (same sender within five minutes, no thread parents).
func unrGroupable(a, b *core.Message) bool {
	if a.System || b.System || a.Tombstone || b.Tombstone || !core.Adjacent(a, b) {
		return false
	}
	if b.Broadcast || a.ReplyCount > 0 || b.ReplyCount > 0 {
		return false
	}
	d := tsTime(b.TS).Sub(tsTime(a.TS))
	return d < 5*time.Minute && d >= 0
}

// unrRows is getRowsAndKeys for the default appearance. now decides
// whether the first unread message is today; lastRead is each
// conversation's current read cursor (the unread divider).
func unrRows(frames []*unrFrame, now time.Time, lastRead func(ch string) string) []unrRow {
	var rows []unrRow
	complete := true
	for _, f := range frames {
		if f.gone {
			continue
		}
		vis := f.visible()
		after := f.after()
		shown := len(vis) > 0 && !f.collapsed
		rows = append(rows, unrRow{kind: unrHeader, f: f, count: len(vis)})
		days := len(vis) > 0 && !sameDay(tsTime(vis[0].TS), now)
		if shown && !days {
			rows = append(rows, unrRow{kind: unrSpacerTop, f: f})
		}
		if shown {
			lr := lastRead(f.ch)
			first := ""
			if lr != "" && actTSLess(f.start, lr) {
				for i := range vis {
					if actTSLess(lr, vis[i].TS) {
						first = vis[i].TS
						break
					}
				}
			}
			for i := range vis {
				m := &vis[i]
				unread := m.TS == first
				var prev *core.Message
				if i > 0 {
					prev = &vis[i-1]
				}
				newDay := prev == nil || !sameDay(tsTime(prev.TS), tsTime(m.TS))
				if days && newDay {
					rows = append(rows, unrRow{kind: unrDay, f: f, day: tsTime(m.TS), unread: unread})
				} else if unread {
					rows = append(rows, unrRow{kind: unrUnreadLine, f: f})
				}
				header := prev == nil || newDay || unread || !unrGroupable(prev, m)
				rows = append(rows, unrRow{kind: unrMsgRow, f: f, msg: m, header: header})
			}
		}
		if !f.reachedEnd && !f.collapsed {
			rows = append(rows, unrRow{kind: unrLoading, f: f})
			complete = false
			break
		}
		if shown {
			if after > 0 {
				rows = append(rows, unrRow{kind: unrShowNewer, f: f, count: after})
			} else {
				rows = append(rows, unrRow{kind: unrSpacerBottom, f: f})
			}
		}
	}
	if complete {
		rows = append(rows, unrRow{kind: unrFooter})
	}
	return rows
}

// unrAnyUnread is renderChannelContent's frames.some(start < end).
func unrAnyUnread(frames []*unrFrame) bool {
	for _, f := range frames {
		if !f.gone && actTSLess(f.start, f.end) {
			return true
		}
	}
	return false
}

// unrEmpty is one of EmptyState's en-US messages (JhsS C["en-US"]).
type unrEmpty struct{ emoji, one, two string }

var unrEmptyMsgs = []unrEmpty{
	{"green_apple", "You’ve read all you needed.", "Take a moment."},
	{"balloon", "There! Caught up.", "Set your mind to something new."},
	{"bee", "You’re up to date.", "Go forth and do great things."},
	{"boom", "Boom.", "You’re up to date."},
	{"clap", "Everything unread is now read.", "You’ve done it."},
	{"seedling", "Everything’s sorted!", "Let’s start something new."},
	{"crystal_ball", "All that was unread is now read.", "What’s next?"},
	{"raised_hands", "That’s everything!", ""},
	{"leaves", "Done and done.", ""},
	{"octopus", "All done.", "The world is your oyster."},
	{"horse", "You’re all read.", "Here’s a pony."},
	{"racing_car", "There.", "All caught up."},
	{"rocket", "All done.", "The future is yours."},
	{"sunny", "You’re all caught up!", "Clear screens ahead."},
	{"sunglasses", "All clear.", ""},
	{"sweet_potato", "Sweet potato!", "You’re all done."},
	{"tada", "Ta-da!", "You’ve read everything there is to read."},
	{"tada", "Ta-da!", "You’re up to date."},
	{"thumbsup", "That’s that.", "You’re good to go."},
	{"tractor", "You’re all read.", "Here’s a tractor."},
	{"v", "All caught up.", "What’s next?"},
	{"seedling", "Lovely!", "Your work here is done."},
}

// unrSkinToned are the en-US emoji that take a skin tone (getEmojiWithSkinTone).
var unrSkinToned = map[string]bool{"clap": true, "raised_hands": true, "thumbsup": true, "v": true}

// unrSwap is EmptyState's swapEmoji: the octopus line trades places with
// the oyster (and back) on a click.
func unrSwap(e unrEmpty) (unrEmpty, bool) {
	if (e.emoji == "octopus" || e.emoji == "oyster") && (e.two == "The world is your oyster." || e.two == "The world is your octopus.") {
		if e.emoji == "octopus" {
			return unrEmpty{"oyster", e.one, strings.Replace(e.two, "oyster", "octopus", 1)}, true
		}
		return unrEmpty{"octopus", e.one, strings.Replace(e.two, "octopus", "oyster", 1)}, true
	}
	return e, false
}

// unrMsgCount is HeaderDefault's getMessageCountText.
func unrMsgCount(n int, exact bool) string {
	if n == 0 {
		return ""
	}
	if exact {
		return nsUnreads.T("{messageCount, plural, =1 {# message} other {# messages}}", "messageCount", n)
	}
	return nsUnreads.T("{messageCount, plural, =1 {#+ message} other {#+ messages}}", "messageCount", n)
}

// unrNewText is renderNewMsgs' label.
func unrNewText(n int, exact bool) string {
	if !exact {
		return nsUnreads.T("New messages")
	}
	return nsUnreads.T("{messageCount} new {messageCount, plural, =1 {message} other {messages}}", "messageCount", n)
}
