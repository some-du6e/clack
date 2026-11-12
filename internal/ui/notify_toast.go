package ui

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// The Windows toast, as Slack desktop builds it (main.bundle.cjs 4.53.276
// win32 x64: class EH, NativeWindowsNotification.createToastNotification,
// fed by gX, the NOTIFY IPC's payload normaliser) from the payload the
// webapp sends (gantry-v2-async-client-boot-shared, build 2cd9270e786c:
// gaJC desktopNotificationOptions and desktopHuddleNotificationOptions,
// awVE's action buttons, HAag's notification ids).
//
// EH has two branches. With the windows_com_activation desktop feature
// (which the server's desktop.features turns on), the toast, its header
// and its buttons activate the app in the foreground with JSON arguments,
// and handleToastActivation (EY) reads them back. Without it, they carry
// slack:// links (formatSlackUrlForMessage plus &origin=notification) and
// Windows opens the link with whichever app has slack://. Clack builds
// the first branch: Windows hands its activation to the process that
// showed the toast, where the second would open Slack desktop on a
// machine that has it. Both branches draw the same toast apart from the
// first one's reply box.

// desktopNote is a desktop notification as the webapp hands it to the
// shell (desktopNotificationOptions, desktopHuddleNotificationOptions),
// with gX's defaults.
type desktopNote struct {
	ID          string // the toast's Tag (Electron Notification id)
	GroupID     string // its Group, and the header's id when set
	GroupTitle  string // the header's title when set
	Title, Body string
	Team        string // teamId
	Workspace   string // win32.workspaceName: the team's name
	Channel     string
	ChannelName string // getDisplayNameForChannel, without the @ for DMs
	Msg         string // msg: the message's ts, or a huddle notification's tag
	ThreadTS    string
	TraceID     string
	Avatar      string // avatarImage: the sender's 192 px picture
	Grouped     bool   // groupWindowsNotifications: the toast's title is the channel's name
	HasReply    bool
	Buttons     []noteButton // actionButtons
	// Activated runs when the toast, its header or one of its buttons is
	// clicked (any goroutine; nil: nothing happens).
	Activated func(toastActivation)
}

// noteButton is one of actionButtons: buildNotificationActionButtons'
// {content, launchUri}. One without a launch URI is inert: Windows'
// own dismiss.
type noteButton struct {
	Content   string
	LaunchURI string
	Do        func() // what Clack does for the click (not in Slack's payload)
}

// toastActivation is a click on a toast, read from its arguments by
// handleToastActivation (the payload schemas EV, Ez and EX).
type toastActivation struct {
	Type string // notification (the toast), action (a button), header
	// notification
	Team, Channel, MessageID, NotificationID, TraceID, ThreadTS string
	Reply                                                       string // the reply box's text (userInputs.replyBox)
	// action, header
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
	toastHeaderArgs struct {
		Type      string `json:"type"`
		LaunchURI string `json:"launchUri"`
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

// toastTitle and toastBody are gX's title and body.
func (n *desktopNote) toastTitle() string { return shellTruncate(n.Title, 1500, 1700) }
func (n *desktopNote) toastBody() string  { return toastUnescape(shellTruncate(n.Body, 1500, 2000)) }

// collapseTags is createToastNotification's .replace(/\>\s+\</g, "><").
var collapseTags = regexp.MustCompile(`>[\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]+<`)

func collapse(s string) string { return collapseTags.ReplaceAllString(s, "><") }

// isSlackURI is the shell's aw: a slack: link.
func isSlackURI(s string) bool { return len(s) >= 6 && strings.EqualFold(s[:6], "slack:") }

// toastXML is createToastNotification's toastXml for the COM-activation
// branch. avatar is the sender's picture as a local file ("" for none),
// as the shell downloads it for an unpackaged app.
func (n *desktopNote) toastXML(avatar string) string {
	esc := toastEscape
	launch := esc(jsonStringify(toastLaunchArgs{Type: "notification", TeamID: n.Team, Channel: n.Channel,
		MessageID: n.Msg, NotificationID: n.ID, TraceID: n.TraceID, ThreadTimestamp: n.ThreadTS}))
	header := ""
	hid, htitle := n.GroupID, n.GroupTitle
	if hid == "" {
		hid = n.Team
	}
	if htitle == "" {
		htitle = n.Workspace
	}
	if hid != "" && htitle != "" {
		uri := "slack://noop"
		if n.Team != "" {
			uri = "slack://channel?team=" + esc(n.Team)
		}
		header = `<header id="` + esc(hid) + `" title="` + esc(htitle) + `" activationType="foreground" arguments="` +
			esc(jsonStringify(toastHeaderArgs{Type: "header", LaunchURI: uri})) + `"></header>`
	}
	title := esc(n.toastTitle())
	if n.Grouped && n.ChannelName != "" && header != "" {
		title = esc(n.ChannelName)
	}
	extras := ""
	if avatar != "" {
		extras = `<image placement="appLogoOverride" hint-crop="circle" src="` + esc(avatar) + `" />`
	}
	actions := ""
	if len(n.Buttons) > 0 || n.HasReply {
		var parts []string
		if n.HasReply {
			parts = append(parts, `<input id="replyBox" type="text" placeHolderContent="Reply"/>`,
				`<action content="➤" hint-inputId="replyBox" activationType="foreground" arguments="`+launch+`"/>`)
		}
		for i, b := range n.Buttons {
			content := esc(b.Content)
			if b.LaunchURI == "" {
				parts = append(parts, "\n      <action activationType=\"system\" arguments=\"dismiss\" content=\""+content+"\"/>")
				continue
			}
			uri := esc(b.LaunchURI)
			if !isSlackURI(uri) {
				uri = "slack://noop" // guardLaunchUri
			}
			args := esc(jsonStringify(toastActionArgs{Type: "action", LaunchURI: strings.ReplaceAll(uri, "&amp;", "&"), ID: n.ID, Index: i}))
			parts = append(parts, "\n      <action activationType=\"foreground\" arguments=\""+args+"\" content=\""+content+"\"/>")
		}
		actions = strings.TrimSpace(collapse("\n    <actions>\n      " + strings.Join(parts, "") + "\n    </actions>"))
	}
	return collapse("\n  <toast activationType=\"foreground\" launch=\"" + launch + "\">\n    " + header +
		"\n    <visual>\n      <binding template=\"ToastGeneric\">\n        <text hint-wrap=\"false\" hint-maxLines=\"1\">" + title +
		"</text>\n        <text hint-maxLines=\"10\" hint-style=\"bodySubtle\" hint-wrap=\"true\">" + esc(n.toastBody()) +
		"</text>\n        " + extras + "\n      </binding>\n    </visual>\n    <audio silent=\"true\" />\n    " + actions + "\n  </toast>")
}

// parseToastActivation is parseActivationPayload: the arguments must be
// one of the three payloads (Joi objects: no other keys, required strings
// not empty, traceId may be). replyBox is the reply box's text.
func parseToastActivation(args, replyBox string) (toastActivation, bool) {
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
		a.Reply = replyBox
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
	case "header":
		set(&a.LaunchURI, "launchUri", true, false)
		ok = ok && only("type", "launchUri")
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

// avatarHosts are the hosts the shell takes a toast's pictures from
// (verifyImageSources with module 6235's Lg list; subdomains count).
var avatarHosts = []string{"slack-edge.com", "slack-imgs.com", "slack-edge.mil", "slack-imgs.mil", "slack-imgs-mil-dev.com",
	"slack-edge-mil-dev.com", "slack-imgs-gov.com", "slack-imgs-gov-dev.com", "slack-edge-gov.com"}

// toastImageAllowed is verifyImageSources for one URL.
func toastImageAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	for _, d := range avatarHosts {
		if u.Host == d || strings.HasSuffix(u.Host, "."+d) {
			return true
		}
	}
	return false
}
