package ui

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// The Windows toast, in Chrome's layout (Karim's choice, 2026-11-12: "this
// is normal chrome's its cleaner"). Web Slack in Chrome hands the strings
// to new Notification(title, {body, icon, tag}) (boot-shared build
// f132f0e78468, notifyBrowser, fed by sharedNotificationOptions: the
// title from getNotificationTitle, the body from
// formatMessageForNotification; browserNotificationOptions' icon is the
// webapp's img.app_icon, Slack's logo), and Chromium writes the toast
// (chrome/browser/notifications/win/notification_template_builder.cc,
// BuildNotificationTemplate): the title, the body, an attribution line
// with the page's origin, the icon as appLogoOverride uncropped, the
// buttons, and a displayTimestamp. No header line, no reply box, no
// line limits: Windows shows the body in up to 4 lines.
//
// This deliberately differs from Slack desktop's win32 toast (class EH,
// NativeWindowsNotification.createToastNotification in main.bundle.cjs
// 4.53.276, which gap-w17 copied): its <header> with the workspace's name,
// the channel's name as the title, the sender's picture cropped to a
// circle, a 10-line body and the reply box are gone. The toast, and a
// huddle invite's buttons, still activate Clack with the JSON arguments
// of EH's COM-activation branch (handleToastActivation reads them back),
// which is how a click opens the conversation.

// desktopNote is a desktop notification as the webapp hands it to the
// shell (desktopNotificationOptions, desktopHuddleNotificationOptions).
type desktopNote struct {
	ID          string // the toast's Tag (Electron Notification id)
	GroupID     string // its Group (Notification Center's grouping; not drawn)
	Title, Body string
	// Subtitle is sharedNotificationOptions' (a message's: only with more
	// than one signed-in account, so never in Clack) or
	// sharedHuddleNotificationOptions' (the invite's conversation). macOS
	// shows it; Chrome's toast has no line for it.
	Subtitle string
	// HasReply is the payload's hasReply (a message's !messageSendFailed);
	// Silent its silent (a message's and a huddle invite's: the webapp
	// plays their sounds). The macOS notification reads them.
	HasReply, Silent bool
	Team             string // teamId
	Workspace        string // the team's name: the attribution line
	Channel          string
	Msg              string // msg: the message's ts, or a huddle notification's tag
	ThreadTS         string
	TraceID          string
	When             time.Time    // the Notification's timestamp (displayTimestamp; zero: none)
	Buttons          []noteButton // actionButtons
	// Activated runs when the toast or one of its buttons is clicked (any
	// goroutine; nil: nothing happens).
	Activated func(toastActivation)
}

// noteButton is one of actionButtons: buildNotificationActionButtons'
// {content, launchUri}. One without a launch URI is Windows' own dismiss;
// on macOS its press goes to the webapp's handleNotificationAction, which
// declines an invite on Decline and does nothing for the rest (Do).
type noteButton struct {
	Content   string
	LaunchURI string
	Do        func() // what Clack does for the click (not in Slack's payload)
}

// toastActivation is a click on a toast, read from its arguments by
// handleToastActivation (the payload schemas EV and Ez), or what the user
// did with a macOS notification (notify_mac.go).
type toastActivation struct {
	Type string // notification (the toast), reply (macOS: its reply box), action (a button)
	// notification and reply
	Team, Channel, MessageID, NotificationID, TraceID, ThreadTS string
	Reply                                                       string // reply: the text
	// action
	LaunchURI, ID string
	Index         int // the button's index, -1 when absent
}

// The JSON arguments, in JSON.stringify's key order.
type (
	toastLaunchArgs struct {
		Type            string `json:"type"`
		TeamID          string `json:"teamId"`
		Channel         string `json:"channel"`
		MessageID       string `json:"messageId"`
		NotificationID  string `json:"notificationId"`
		TraceID         string `json:"traceId"`
		ThreadTimestamp string `json:"threadTimestamp,omitempty"`
	}
	toastActionArgs struct {
		Type      string `json:"type"`
		LaunchURI string `json:"launchUri"`
		ID        string `json:"id"`
		Index     int    `json:"index"`
	}
)

// jsonStringify is JSON.stringify: no HTML escaping, no newline.
func jsonStringify(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return ""
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// toastEscape is EH's Ek: the XML entities for & < > " '.
var toastEscape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace

// toastUnescape is lodash unescape (gX's E9/E6), for a body the webapp
// sent with HTML entities in it.
var toastUnescape = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'").Replace

// isJSSpace is JavaScript's \s.
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// shellTruncate is the shell's ch(e, t, r): text longer than t UTF-16
// units keeps whole words (split on \s, joined with spaces) until it
// reaches t, at most r-1 units, then an ellipsis.
func shellTruncate(s string, t, r int) string {
	if utf16Len(s) <= t {
		return s
	}
	n := ""
	for _, w := range splitJSSpace(s) {
		if n != "" {
			n += " "
		}
		n += w
		if utf16Len(n) >= t {
			break
		}
	}
	if r > 0 && utf16Len(n) > r {
		u := utf16.Encode([]rune(n))
		n = string(utf16.Decode(u[:r-1]))
	}
	if utf16Len(n) < utf16Len(s) {
		return n + "…"
	}
	return n
}

// splitJSSpace is e.split(/\s/): every whitespace character separates,
// so runs of them leave empty strings.
func splitJSSpace(s string) []string {
	var out []string
	start := 0
	for i, r := range s {
		if isJSSpace(r) {
			out = append(out, s[start:i])
			start = i + len(string(r))
		}
	}
	return append(out, s[start:])
}

// toastTitle and toastBody are the texts, capped as the shell's gX caps
// them (Chrome passes them whole, but Windows draws 2 lines of the title
// and 4 of the body, far short of the cap), and the body's HTML entities decoded
// (notifyBrowser's decodeHtmlEntities, for content the server sent).
func (n *desktopNote) toastTitle() string { return shellTruncate(n.Title, 1500, 1700) }
func (n *desktopNote) toastBody() string  { return toastUnescape(shellTruncate(n.Body, 1500, 2000)) }

// isSlackURI is the shell's aw: a slack: link.
func isSlackURI(s string) bool { return len(s) >= 6 && strings.EqualFold(s[:6], "slack:") }

// toastXML is BuildNotificationTemplate's toast for n, without libxml's
// indentation (Windows ignores it): <toast launch displayTimestamp>, the
// title and body <text>s, the attribution <text>, the icon, <actions>
// (Chromium writes the element even when it's empty) and the silent
// <audio>. logo is Clack's icon as a local file ("" for none): the
// webapp's Notification icon is Slack's logo, which Chromium draws as
// appLogoOverride with hint-crop="none".
//
// The attribution is the workspace's name where Chrome has the page's
// origin (app.slack.com): both say where the message came from, and
// Clack's app line already says "Clack" where Chrome's says "Google
// Chrome". The toast is silent, as Slack desktop's is: Clack plays
// Slack's own message sound (notifyMessage), where Chrome's Notification
// isn't silent and Windows would ding on top of it. Chromium's right-click
// "notification settings" item isn't there: Clack has no handler for it.
func (n *desktopNote) toastXML(logo string) string {
	esc := toastEscape
	var b strings.Builder
	b.WriteString(`<toast launch="` + esc(jsonStringify(toastLaunchArgs{Type: "notification", TeamID: n.Team, Channel: n.Channel,
		MessageID: n.Msg, NotificationID: n.ID, TraceID: n.TraceID, ThreadTimestamp: n.ThreadTS})) + `"`)
	if !n.When.IsZero() {
		b.WriteString(` displayTimestamp="` + n.When.UTC().Format("2006-01-02T15:04:05Z") + `"`)
	}
	b.WriteString(`><visual><binding template="ToastGeneric"><text>` + esc(n.toastTitle()) + `</text><text>` + esc(n.toastBody()) + `</text>`)
	if n.Workspace != "" {
		b.WriteString(`<text placement="attribution">` + esc(n.Workspace) + `</text>`)
	}
	if logo != "" {
		b.WriteString(`<image placement="appLogoOverride" src="` + esc(logo) + `" hint-crop="none"/>`)
	}
	b.WriteString(`</binding></visual>`)
	if len(n.Buttons) == 0 {
		b.WriteString(`<actions/>`)
	} else {
		b.WriteString(`<actions>`)
		for i, bt := range n.Buttons {
			content := esc(bt.Content)
			if bt.LaunchURI == "" { // Windows' own dismiss, as the shell makes a button without a link
				b.WriteString(`<action activationType="system" content="` + content + `" arguments="dismiss"/>`)
				continue
			}
			uri := bt.LaunchURI
			if !isSlackURI(uri) {
				uri = "slack://noop" // guardLaunchUri
			}
			b.WriteString(`<action activationType="foreground" content="` + content + `" arguments="` +
				esc(jsonStringify(toastActionArgs{Type: "action", LaunchURI: uri, ID: n.ID, Index: i})) + `"/>`)
		}
		b.WriteString(`</actions>`)
	}
	b.WriteString(`<audio silent="true"/></toast>`)
	return b.String()
}

// parseToastActivation is parseActivationPayload for the toast's and its
// buttons' payloads (Joi objects: no other keys, required strings not
// empty, traceId may be).
func parseToastActivation(args string) (toastActivation, bool) {
	var m map[string]any
	d := json.NewDecoder(strings.NewReader(args))
	d.UseNumber()
	if d.Decode(&m) != nil || m == nil {
		return toastActivation{}, false
	}
	str := func(k string, need, empty bool) (string, bool) {
		v, has := m[k]
		if !has {
			return "", !need
		}
		s, ok := v.(string)
		return s, ok && (s != "" || empty)
	}
	only := func(keys ...string) bool {
		for k := range m {
			ok := false
			for _, want := range keys {
				ok = ok || k == want
			}
			if !ok {
				return false
			}
		}
		return true
	}
	a := toastActivation{Index: -1}
	a.Type, _ = m["type"].(string)
	ok := true
	set := func(dst *string, k string, need, empty bool) {
		s, good := str(k, need, empty)
		ok = ok && good
		*dst = s
	}
	switch a.Type {
	case "notification":
		set(&a.Team, "teamId", true, false)
		set(&a.Channel, "channel", true, false)
		set(&a.MessageID, "messageId", true, false)
		set(&a.NotificationID, "notificationId", true, false)
		set(&a.TraceID, "traceId", true, true)
		set(&a.ThreadTS, "threadTimestamp", false, false)
		ok = ok && only("type", "teamId", "channel", "messageId", "notificationId", "traceId", "threadTimestamp")
	case "action":
		set(&a.LaunchURI, "launchUri", true, false)
		set(&a.ID, "id", true, false)
		if v, has := m["index"]; has {
			num, isNum := v.(json.Number)
			i, err := strconv.Atoi(string(num))
			if !isNum || err != nil || i < 0 {
				ok = false
			}
			a.Index = i
		}
		ok = ok && only("type", "launchUri", "id", "index")
	default:
		ok = false
	}
	return a, ok
}

// ---- the webapp's side ----

// Notification types and ids (HAag): with desktop_notification_actions
// on, a notification's id is "team|type|payload…" and a click finds its
// target in it (parseNotificationId); otherwise the shell names it
// "team_msg".
const (
	noteTypeMessage      = "msg"
	noteTypeHuddle       = "huddle"
	noteTypeHuddleMissed = "hudmiss"
)

// shortDigest is HAag's FNV-1a 32 over UTF-16 units, in base 36.
func shortDigest(s string) string {
	h := uint32(0x811c9dc5)
	for _, u := range utf16.Encode([]rune(s)) {
		h ^= uint32(u)
		h *= 0x01000193
	}
	return strconv.FormatUint(uint64(h), 36)
}

// buildNotificationID is HAag's buildNotificationId: "team|type|…", or
// "team|type|~digest" past 64 characters.
func buildNotificationID(team, typ string, payload ...string) string {
	id := strings.Join(append([]string{team, typ}, payload...), "|")
	if utf16Len(id) > 64 {
		return strings.Join([]string{team, typ, "~" + shortDigest(id)}, "|")
	}
	return id
}

// parseNotificationID is HAag's parseNotificationId.
func parseNotificationID(id string) (team, typ string, payload []string, truncated bool) {
	parts := strings.Split(id, "|")
	team = parts[0]
	if len(parts) > 1 {
		typ = parts[1]
	}
	if len(parts) > 2 {
		payload = parts[2:]
	}
	if len(payload) == 1 && strings.HasPrefix(payload[0], "~") {
		return team, typ, nil, true
	}
	return team, typ, payload, false
}

// messageNotificationPayload is HAag's: channel and ts, and the thread
// when the message is a reply.
func messageNotificationPayload(ch, ts, thread string) []string {
	if thread != "" && thread != ts {
		return []string{ch, ts, thread}
	}
	return []string{ch, ts}
}

// notificationMinuteBucket is gaJC's: the minute, rounded.
func notificationMinuteBucket(now time.Time) string {
	return strconv.FormatInt((now.UnixMilli()+30000)/60000, 10)
}

// huddleNotificationTag is getHuddleNotificationTag: the msg a huddle
// notification carries (and notificationClicked recognizes).
func huddleNotificationTag(ch string, now time.Time) string {
	return "huddle_" + ch + "_" + notificationMinuteBucket(now)
}

// shellNoteID is the NOTIFY handler's default id: "team_msg".
func shellNoteID(team, msg string) string {
	if team != "" && msg != "" {
		return team + "_" + msg
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}
