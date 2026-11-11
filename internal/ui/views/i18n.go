package views

import (
	"strings"
	"time"

	"github.com/some-du6e/clack/internal/i18n"
)

// Slack's strings for the views (internal/i18n, docs/I18N.md). Each I18n
// instance is the namespace of the Slack component a view follows: the
// `X = new I18n("ns")` its `X.t(…)` belongs to.
var (
	nsThreads       = i18n.NS("threads")  // client-threads-view, the thread cards (BroadcastControls, LoadOlderMessage, getSimpleNameList)
	nsMessage       = i18n.NS("message")  // the message timestamp formatter (QvGr)
	nsMessages      = i18n.NS("messages") // UnreadDivider's "New"
	nsGeneral       = i18n.NS("general")  // listify's "and" and "or"
	nsActivity      = i18n.NS("activity") // the Activity inbox and the triage views' location lines
	nsMentions      = i18n.NS("mentions") // the Activity filter labels (g7lN) and empty states (MmpO)
	nsSidebar       = i18n.NS("channel_sidebar")
	nsLater         = i18n.NS("saved_for_later") // Later, To-dos, the reminder menus (ccoC ReminderTimeMenu)
	nsIntro         = i18n.NS("introductions")   // the Slack Connect invitation Ignore modals
	nsSlackConnect  = i18n.NS("slack_connect")
	nsSharedInvites = i18n.NS("shared_channel_invites") // SlackConnectConfirmTrustModal
)

// localized reports whether Slack's strings differ from the English
// sources Clack's en-US code paths write out (any locale but en-US).
func localized() bool { return i18n.Locale() != i18n.EnUS }

// listify is Slack's listify (client-boot nTGe) as plain text: "A and B",
// "A, B, and C" (the serial comma in en-US only), "A、B、C" in ja-JP (no
// conjunction), "A、B y C" … with the general namespace's "and" (or "or"
// when or is set).
func listify(items []string, or bool) string {
	n := len(items)
	if n == 0 {
		return ""
	}
	sep, serial, conj := ",", false, true
	word := nsGeneral.T("and")
	if or {
		word = nsGeneral.T("or")
	}
	switch i18n.Locale() {
	case i18n.EnUS:
		serial = true
	case "ja-JP":
		if !or {
			conj = false
		}
		sep = "、"
	case "ko-KR":
		if or {
			serial = true
		}
	case "zh-CN", "zh-TW":
		sep = "、"
	}
	last := sep + " "
	switch {
	case serial && conj && n > 2:
		last = sep + " " + word + " "
	case conj:
		last = " " + word + " "
	}
	var b strings.Builder
	for i, s := range items {
		b.WriteString(s)
		switch {
		case i < n-2:
			b.WriteString(sep + " ")
		case i < n-1:
			b.WriteString(last)
		}
	}
	return b.String()
}

// msgTimeIn is Slack's message timestamp formatter (QvGr, the
// THREAD_DETAIL_VIEW format) in a locale: "Just now", toTimeAgo under an
// hour, "Today at {time}", "Yesterday at {time}", "{dayOfTheWeek} at
// {time}" within a week, else "{calendarDate} at {time}".
func msgTimeIn(l string, t, now time.Time) string {
	age := now.Sub(t)
	if age < 0 {
		age = -age
	}
	tm := clock(t)
	switch {
	case age < time.Minute:
		return nsMessage.T("Just now")
	case age < time.Hour:
		return i18n.TimeAgo(l, t, now, false, true)
	case sameDay(t, now):
		return nsMessage.T("Today at {time}", "time", tm)
	case sameDay(t, now.AddDate(0, 0, -1)):
		return nsMessage.T("Yesterday at {time}", "time", tm)
	case age < 7*24*time.Hour:
		return nsMessage.T("{dayOfTheWeek} at {time}", "dayOfTheWeek", i18n.Weekday(l, t, false), "time", tm)
	}
	d := i18n.CalendarDate(l, t, i18n.CalendarOpts{ShortenMonth: true, ExcludeYear: t.Year() == now.Year()})
	return nsMessage.T("{calendarDate} at {time}", "calendarDate", d, "time", tm)
}

// tagged replaces a Slack string's one empty tag ("<span></span>") with s:
// Clack draws the tag's content (a channel name) as plain text, in the
// place the locale's word order puts it.
func tagged(src, tag, s string) string {
	return strings.Replace(src, "<"+tag+"></"+tag+">", s, 1)
}

// dmFallbackName is a DM's place name when the other person is unknown:
// Slack's LocationLabel/ActivityDirectMessageLabel "Direct message" (the
// activity namespace, which the triage views' location lines share).
func dmFallbackName() string { return nsActivity.T("Direct message") }

// around formats a Slack string whose placeholder key stands for
// something Clack draws itself (an emoji, a bold name) and returns the
// text before and after it, tags stripped ("Reacted {emoji} to:" →
// "Reacted ", " to:"; ja-JP "次に対して ", " でリアクションしました : ").
func around(ns i18n.NS, src, key string, args ...any) (before, after string) {
	s := i18n.StripTags(ns.T(src, append([]any{key, "\x00"}, args...)...))
	if i := strings.IndexByte(s, 0); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// dateSlash is formatDate's {date_slash}: "1/2/2006" in en-US, dayjs's L
// elsewhere ("02/01/2006", "2006/01/02").
func dateSlash(t time.Time) string {
	if l := i18n.Locale(); l != i18n.EnUS {
		return i18n.FormatDayjs(l, t, "L")
	}
	return t.Format("1/2/2006")
}

// monthYear is a month and year ("Jan 2006"): toCalendarDate with the
// month shortened and no day, in the active locale.
func monthYear(t time.Time) string {
	if l := i18n.Locale(); l != i18n.EnUS {
		return i18n.CalendarDate(l, t, i18n.CalendarOpts{ShortenMonth: true, ExcludeDay: true})
	}
	return t.Format("Jan 2006")
}

// weekday is toDayOfTheWeek: "Monday", "lunes", "月曜日".
func weekday(t time.Time) string {
	if l := i18n.Locale(); l != i18n.EnUS {
		return i18n.Weekday(l, t, false)
	}
	return t.Format("Monday")
}
