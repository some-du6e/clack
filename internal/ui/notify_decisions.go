package ui

import (
	"log"

	"github.com/some-du6e/clack/internal/logtag"
)

// The notification decision log (docs/INSPECT.md): one line per
// desktop_notification and per message the notification rules decide on,
// "notify: show ch:…/ts:…" or "notify: skip <reason> ch:…/ts:…". Slack's
// maybeShowNotification logs each decision's number the same way
// ("shouldShowNotificationForChannelMessage: '6'", and "Desktop
// notification shown" with the channel and timestamp; boot-render /+fI,
// build 35b42de8b870), so the reasons are Slack's numbers, as its rules
// return them (client-boot Fpyc rr, RM and gA) and
// messageForSuppressedNotification names them. A check Slack logs by text
// instead carries a word: "pref-disabled" (Slack's "Skip pref-disabled
// push notification"), and the few Clack has alone their own. Ids are
// tags (logtag: one salt per run, never written), never names or text
// (docs/SECURITY.md).

type noteSkip string

const (
	noteShow noteSkip = ""

	noteSkipNoMention noteSkip = "1"  // the level asks for mentions and there's none (NO_MENTIONS)
	noteSkipNotMember noteSkip = "2"  // a channel the viewer isn't in
	noteSkipNoModel   noteSkip = "3"  // no message, or no conversation for it
	noteSkipNoDisplay noteSkip = "4"  // no_display
	noteSkipNoNotify  noteSkip = "5"  // no_notifications
	noteSkipReply     noteSkip = "6"  // a reply that doesn't mention the viewer (any reply in a DM)
	noteSkipNotReply  noteSkip = "7"  // a desktop_notification that isn't a thread's reply
	noteSkipOwn       noteSkip = "11" // the viewer's own
	noteSkipMuted     noteSkip = "12"
	noteSkipDND       noteSkip = "13"
	noteSkipOnScreen  noteSkip = "16" // its conversation, or its thread, on screen in the focused window
	noteSkipTooNew    noteSkip = "19" // newer than the newest message the store took (notifications.maxTs)
	noteSkipRead      noteSkip = "20" // its thread read to it already
	noteSkipShown     noteSkip = "21" // shown already
	noteSkipHidden    noteSkip = "29" // from a member the viewer hid
	noteSkipCanvasUpd noteSkip = "30" // a channel canvas's update
	noteSkipTabCanvas noteSkip = "31" // a tab canvas's update
	noteSkipAssistant noteSkip = "32" // an assistant thread's root or first message
	noteSkipStreaming noteSkip = "33" // a chat stream still in progress
	noteSkipTabAdded  noteSkip = "34" // a tab added
	noteSkipFolder    noteSkip = "35" // a folder's item added

	noteSkipPushOff   noteSkip = "pref-disabled" // the conversation's desktop push is off
	noteSkipEphemeral noteSkip = "ephemeral"     // an ephemeral message (Clack's rule)
	noteSkipDeleted   noteSkip = "deleted"       // deleted while the lookups it waited for ran (Clack's)
)

// noteDecided writes the decision line for the notification of message ts
// of ch.
func noteDecided(ch, ts string, why noteSkip) {
	at := logtag.ID(ch) + "/" + logtag.ID(ts)
	if why == noteShow {
		log.Printf("notify: show %s", at)
		return
	}
	log.Printf("notify: skip %s %s", why, at)
}
