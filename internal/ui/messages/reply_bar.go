package messages

import (
	"image"
	"strconv"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The channel's reply bar (Slack's ReplyBar, client-boot-shared v8ac,
// build 407aff01646b): the repliers' faces, "N replies", the last reply
// ("View thread" on hover) and a caret.

// ThreadComposerFocuser is optional: the reply bar opens the thread with
// its reply composer focused (Slack's openThread sets focusInput for the
// thread, and the pane's input focuses on mount).
type ThreadComposerFocuser interface {
	FocusThreadComposer(channel, ts string)
}

// replyBarFaces is renderFaces: the repliers (reply_users without
// "B00"), all of them up to five; past five the first four and a fifth
// carrying "+N" for reply_users_count (else the list's length) minus 5.
func replyBarFaces(users []string, count int) (faces []string, overlay int) {
	for _, u := range users {
		if u != "B00" {
			faces = append(faces, u)
		}
	}
	if len(faces) <= 5 {
		return faces, 0
	}
	n := count
	if n == 0 {
		n = len(faces)
	}
	return faces[:5], n - 5
}

// replyOverlay draws the fifth face's c-avatar__additional_overlay: the
// avatar under rgba(0,0,0,.5) with "+N" ("9+" past nine) in white 15px
// bold, centred.
func replyOverlay(gtx layout.Context, th *theme.Theme, n, sz int) {
	txt := "+" + strconv.Itoa(n)
	if n > 9 {
		txt = "9+"
	}
	r := image.Rectangle{Max: image.Pt(sz, sz)}
	rrect(gtx.Ops, r, avatarRadius(sz), rgba(0x000000, 0.5))
	ts := textStyle{size: 15, weight: theme.Bold, col: rgb(0xffffff)}
	d := measure(gtx, th, ts, txt)
	at(gtx, (sz-d.X)/2, (sz-d.Y)/2, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max = image.Pt(1<<20, gtx.Sp(unit.Sp(30)))
		return label(gtx, th, ts, txt)
	})
}

// replyBarLink is ReplyBar's getLinkTextWithScheduledDrafts (v8ac, CDN
// build 4d0c78085232), the count link's text, from the replies, whether
// the thread has a draft (ThreadReplies' channelThreadDraft, getChannelDraft
// for the thread), its scheduled replies (getScheduledDraftsForChannel,
// QV8V) and whether the thread is the one open in the secondary view
// (isCurrentThreadOpen: its composer is the draft, so "& 1 draft" goes):
//
//	replies  draft  scheduled  closed                          open
//	N        no     no         N replies                       N replies
//	N        yes    no         N replies & 1 draft             N replies
//	N        no     K          N replies & K scheduled         (same)
//	N        yes    K          N replies, K scheduled, 1 draft N replies & K scheduled
//	0        yes    K          K scheduled & 1 draft           K scheduled
//	0        yes    no         ✏ 1 draft                       ✏
//	0        no     K          ⏲ K scheduled                   (same)
//
// icon is the inline Icon ("pencil" or "scheduled-send") the last two
// start with; the strings are the threads I18n's.
func replyBarLink(replies int, draft bool, scheduled int, open bool) (text, icon string) {
	c, d, u := replies > 0, draft, scheduled > 0
	switch {
	case c && !d && !u:
		return repliesLabel(replies), ""
	case c && d && !u:
		if open {
			return repliesLabel(replies), ""
		}
		return threadsNS.T("{replyCount, plural, =1 {# reply} other {# replies}} & 1 draft", "replyCount", replies), ""
	case c && d && u:
		if open {
			return threadsNS.T("{replyCount, plural, =1 {# reply} other {# replies}} & {numberOfScheduledDraftsInDestination} scheduled",
				"replyCount", replies, "numberOfScheduledDraftsInDestination", scheduled), ""
		}
		return threadsNS.T("{replyCount, plural, =1 {# reply} other {# replies}}, {numberOfScheduledDraftsInDestination} scheduled, 1 draft",
			"replyCount", replies, "numberOfScheduledDraftsInDestination", scheduled), ""
	case c && !d && u:
		return threadsNS.T("{replyCount, plural, =1 {# reply} other {# replies}} & {numberOfScheduledDraftsInDestination} scheduled",
			"replyCount", replies, "numberOfScheduledDraftsInDestination", scheduled), ""
	case d && u: // no replies
		if open {
			return threadsNS.T("{numberOfScheduledDraftsInDestination} scheduled", "numberOfScheduledDraftsInDestination", scheduled), ""
		}
		return threadsNS.T("{numberOfScheduledDraftsInDestination} scheduled & 1 draft", "numberOfScheduledDraftsInDestination", scheduled), ""
	case d:
		if open {
			return "", "pencil"
		}
		return threadsNS.T("1 draft"), "pencil"
	case u:
		return threadsNS.T("{numberOfScheduledDraftsInDestination} scheduled", "numberOfScheduledDraftsInDestination", scheduled), "scheduled-send"
	}
	return "", ""
}

// replyBarShown is ThreadReplies' and ReplyBar's: no bar without replies,
// a draft or a scheduled reply, and none for the open thread when all it
// has is the draft being written in its composer.
func replyBarShown(replies int, draft bool, scheduled int, open bool) bool {
	if replies == 0 && !draft && scheduled == 0 {
		return false
	}
	return !(replies == 0 && open && scheduled == 0)
}

// threadDrafts is the reply bar's draft and scheduled replies for the
// thread rooted at m: core's drafts as last seen (DraftFor, ScheduledFor;
// neither blocks), looked up again only when they change (DraftsGen).
// A draft counts while it has text or files, as Activity's draft rows
// count one (views/activity_rows.go).
func (r *row) threadDrafts(app *core.App) (draft bool, scheduled int) {
	if app == nil {
		return false, 0
	}
	if g := app.DraftsGen(); g != r.draftGen || !r.draftSeen {
		r.draftGen, r.draftSeen = g, true
		d, ok := app.DraftFor(r.m.Channel, r.m.TS)
		r.hasDraft = ok && (strings.TrimSpace(d.Text) != "" || d.Files > 0)
		r.draftAt = d.LastUpdated
		s, _ := app.ScheduledFor(r.m.Channel, r.m.TS)
		r.scheduled, r.schedAt = len(s), 0
		if len(s) > 0 {
			r.schedAt = s[0].PostAt
		}
	}
	return r.hasDraft, r.scheduled
}

// replyBarNoReplies is ReplyBar's description for a thread with no
// replies yet: with no draft and one scheduled reply its time
// ("{draftScheduledDate} at {draftScheduledTime}": date_short_pretty,
// capitalized, and the time) at rest, else renderLastReplyText of the
// draft's last_updated (getThreadLatestDraftText, BZhr: "Last edited today
// at {time}", "Last edited {time_ago}"); "View thread" under the pointer.
func (r *row) replyBarNoReplies(now time.Time, hov bool) string {
	switch {
	case hov:
		return threadsNS.T("View thread")
	case !r.hasDraft && r.scheduled == 1 && r.schedAt > 0:
		t := time.Unix(r.schedAt, 0)
		return threadsNS.T("{draftScheduledDate} at {draftScheduledTime}",
			"draftScheduledDate", i18n.ShortDatePretty(i18n.Locale(), t, now, false), "draftScheduledTime", clock(t))
	case r.hasDraft && !draftUpdated(r.draftAt).IsZero():
		t := draftUpdated(r.draftAt)
		if sameDay(t, now) {
			return threadsNS.T("Last edited today at {time}", "time", clock(t))
		}
		return threadsNS.T("Last edited {time_ago}", "time_ago", i18n.TimeAgo(i18n.Locale(), t, now, false, false))
	}
	return ""
}

// threadOpen is isCurrentThreadOpen: the secondary view shows this
// message's thread.
func (r *row) threadOpen() bool {
	return r.rc != nil && r.rc.list != nil && r.rc.list.activeTS == r.m.TS
}

// draftUpdated is a draft's last_updated_ts: milliseconds (the
// client_last_updated_ts the client sends back, as views/drafts.go reads
// it), else a ts.
func draftUpdated(s string) time.Time {
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil && ms > 1e11 {
		return time.UnixMilli(ms)
	}
	return tsTime(s)
}
