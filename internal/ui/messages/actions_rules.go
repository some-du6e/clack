package messages

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The hover toolbar's thread and share buttons, ported from Slack's
// message action selectors (build 4d0c78085232, boot-shared and
// client-boot):
//
//   - The thread action (t8wB) is null in the thread pane (THREAD,
//     MEDIA_VIEWER_THREAD and SAVED_FOR_LATER_PAGE containers) and when
//     canStartOrOpenThread (sSNY) is false: a thread_hidden message, or one
//     with no replies (reply_count, or a broadcast with a thread_ts) that
//     canReplyOrComment (RpcX) rejects. Its label: "View thread" in the
//     Threads view or when you can't reply and it has replies (a reply
//     count, or a broadcast: R+b2 isThreadBroadcast), "Reply to thread"
//     when it has replies or is a reply, else "Reply in thread".
//   - The share action (6bWJ) is null for automated subtypes (KC8h) under
//     system_messages_redesign_m1, redesigned huddle system messages
//     (N4vB), messages that can't be shared (p6HR canBeShared: file
//     comments, unknown, EKM/DLP/DSA-restricted, ephemeral, tombstones) and,
//     under enable_new_channel_settings, channels whose properties
//     restrict_message_and_file_sharing (1oza). Its label follows the
//     team's private_message_forwarding pref (Db0x, teamPrefs[..] ===
//     true): "Forward message…" / "Share message…"; a message that is one
//     file and nothing else outside a thread (iR00 isSingleFileMessageAction)
//     says "Forward file…" / "Share file…", "Share canvas…" (a canvas with
//     forwarding on), "Share list…"; data-qa share_file or share_message.

// isAutomatedSubtype is KC8h's isAutomatedSubtype (popups keeps the
// list: the share action there reads it too).
func isAutomatedSubtype(s string) bool { return popups.IsAutomatedSubtype(s) }

// isReply is azCc isReply: a thread_ts that isn't the message's own ts.
func isReply(m *core.Message) bool { return m.ThreadTS != "" && m.ThreadTS != m.TS }

// hasReplies is the thread action's hasReplies: R+b2 isThreadBroadcast or
// a reply count.
func hasReplies(m *core.Message) bool { return m.Subtype == "thread_broadcast" || m.ReplyCount > 0 }

// canReplyOrComment is RpcX canReplyOrComment for a channel message, as far
// as Clack knows the channel (ignoreMembership and ignoreImWithDeletedMmbr
// false, the thread action's call).
func canReplyOrComment(app *core.App, m *core.Message) bool {
	sub := m.Subtype
	if sub == "welcome_party" {
		return true
	}
	actionableCode := sub == "code_channel_lifecycle" || sub == "code_channel_review_submitted" // 6B6N br
	switch {
	case isAutomatedSubtype(sub) && !actionableCode,
		m.Pending || m.Failed, // vITy isTempMsg: a local ts
		sub == "thread_hidden",
		isReply(m),
		sub == "file_comment" || sub == "file_mention" || sub == "file_share" || sub == "file_upload": // rXmn
		return false
	}
	if app == nil {
		return true
	}
	if !app.PostingIn(m.Channel).CanReply() { // Opi6 canThread: is_non_threadable or read-only
		return false
	}
	conv, ok := app.S.Conv(m.Channel)
	if !ok {
		return true
	}
	if barrier(conv.Properties) { // Opi6 hasBarrier
		return false
	}
	if conv.IsIM {
		if conv.User == "USLACK" { // ZgxJ isChannelSystemNotificationIm
			return false
		}
		if u, ok := app.User(conv.User); ok && u.Deleted { // Opi6 isImWithDeletedMember
			return false
		}
	}
	// a public channel the viewer only previews (aK5v isPublicChannel, JUrK
	// isMemberOfChannel)
	if conv.IsChannel && !conv.IsPrivate && !conv.IsMPIM && !conv.IsMember {
		return false
	}
	return true
}

// barrier reads the conversation's has_barrier (an information barrier).
func barrier(props json.RawMessage) bool {
	if len(props) == 0 {
		return false
	}
	var p struct {
		HasBarrier bool `json:"has_barrier"`
	}
	_ = json.Unmarshal(props, &p)
	return p.HasBarrier
}

// replyLabel is the thread action's name for m, "" when the toolbar has
// no thread button. thread: the list is the thread pane.
func replyLabel(app *core.App, m *core.Message, thread bool) string {
	if thread {
		return ""
	}
	canReply := canReplyOrComment(app, m)
	// sSNY canStartOrOpenThread
	if m.Subtype == "thread_hidden" || !(m.ReplyCount > 0 || (m.Subtype == "thread_broadcast" && m.ThreadTS != "") || canReply) {
		return ""
	}
	switch {
	case !canReply && hasReplies(m):
		return "View thread"
	case hasReplies(m) || isReply(m):
		return "Reply to thread"
	}
	return "Reply in thread"
}

// shareAction is the share action for m (popups.ShareAction, which the
// right-click menu's Forward row reads as well): its label and data-qa,
// ok false when the toolbar has none.
func shareAction(app *core.App, m *core.Message, thread bool) (label, qa string, ok bool) {
	a, ok := popups.ShareAction(app, m, thread)
	return a.Label, a.QA, ok
}

// todoAction is the to-do action (Yoyn) for m: popups.TodoActionFor,
// which the right-click menu's row reads as well.
func todoAction(app *core.App, m *core.Message) popups.TodoAction {
	return popups.TodoActionFor(app, m)
}

// The Later label above a saved message, from Slack's CDN build
// d3de3a487b1c (boot-shared):
//
//   - MessageLabels (meLQ) draws one primary label: ephemeral, then a
//     broadcast, then pinned (a pinned saved message shows PinnedLabel),
//     then Later when isLater (p6HR isSavedButUncompleted: saved and no
//     date_completed). Its type is none for an archived item, "later_todo"
//     when date_due > 1, else "later": the icon (ec) bookmark-filled, or
//     the SVG check-circle-filled for a to-do, the text and icon in
//     sk_highlight and bold (.c-message_kit__labels--later, --later_todo),
//     the row #1d9bd11a (.c-message_kit__background--labels--later, in
//     the rule after --hovered, so it stays under the pointer).
//   - LaterLabel (wRj5, the message I18n, addDueText): isTodo is
//     later_todos (6owI tj) and date_due > 1. "To-do added" or "Saved for
//     later"; without later_todos a due date adds Qg's " • Due {toTimeAgo}".
//     With the saved page (hq: later_todos and the has_starred_item pref)
//     and no to-do: "{label} · {viewBookmarksLink}", the link "View Saved"
//     opening the Saved page. A to-do: "{label} · {viewLink}" with "View
//     to-dos", the label TodoDueLabel when the item has a due date to show
//     (qb0V Ep, and its date_due isn't its date_created): "To-do due
//     {dateString}", or "To-do overdue by {dateString}" (isOverdue, with
//     "ago" taken out), the date getDateDuePillString(due, true, true)
//     (T8t/ BM: toTimeAgo today, else formatDate "{ago}" lowercased).

// laterLabel is LaterLabel's content for a row.
type laterLabel struct {
	icon   string // bookmark-filled (later) or check-circle-filled (later_todo)
	text   string // the label, before the link
	format string // the rich string around it, "" for the label alone
	arg    string // its link argument (viewBookmarksLink or viewLink)
	link   string // "View Saved" or "View to-dos"
	route  string // where the link goes: "saved" or "later"
}

// laterLabelFor is m's Later label; ok is false when MessageLabels draws
// none for it (not saved, completed or archived).
func laterLabelFor(app *core.App, m *core.Message, now time.Time) (laterLabel, bool) {
	if app == nil || m == nil {
		return laterLabel{}, false
	}
	meta, saved := app.SavedMeta(m.Channel, m.TS)
	if !saved || meta.Completed() || meta.Archived { // isLater; primaryLabelType none
		return laterLabel{}, false
	}
	todos := app.Experiment("later_todos") == "on" // 6owI tj
	synth := todos && app.Prefs().HasStarredItem   // 6owI hq
	isTodo := todos && meta.DateDue > 1
	l := laterLabel{icon: "bookmark-filled", text: msgNS.T("Saved for later")}
	if meta.DateDue > 1 {
		l.icon = "check-circle-filled"
	}
	if isTodo {
		l.text = msgNS.T("To-do added")
	}
	if meta.DateDue > 0 && !isTodo {
		// T8t/ Qg(dateDue, addDueText): " • Due {dateString}" (saved_for_later)
		l.text += " • " + savedNS.T("Due {dateString}", "dateString", i18n.TimeAgo(i18n.Locale(), time.Unix(meta.DateDue, 0), now, false, false))
	}
	switch {
	case synth && !isTodo:
		l.format, l.arg, l.link, l.route = "{label} · {viewBookmarksLink}", "viewBookmarksLink", msgNS.T("View Saved"), "saved"
	case isTodo:
		l.format, l.arg, l.link, l.route = "{label} · {viewLink}", "viewLink", msgNS.T("View to-dos"), "later"
		if meta.DateDue != meta.DateCreated && meta.ShowsDue() { // useTodoDueItem's dueItem
			l.text = todoDueText(meta, now)
		}
	}
	return l, true
}

// todoDueText is TodoDueLabel: "To-do due {dateString}" or "To-do
// overdue by {dateString}" (the message I18n).
func todoDueText(meta core.SavedMeta, now time.Time) string {
	due := time.Unix(meta.DateDue, 0)
	loc := i18n.Locale()
	// getDateDuePillString(due, true, true): toTimeAgo on its day, else
	// formatDate(due, "{ago}") lowercased
	d := i18n.TimeAgo(loc, due, now, false, false)
	if y1, m1, d1 := due.Date(); !(y1 == now.Year() && m1 == now.Month() && d1 == now.Day()) {
		d = strings.ToLower(d)
	}
	if meta.Overdue(now.Unix()) {
		return msgNS.T("To-do overdue by {dateString}", "dateString", strings.TrimSpace(strings.Replace(d, "ago", "", 1)))
	}
	return msgNS.T("To-do due {dateString}", "dateString", d)
}
