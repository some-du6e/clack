package canvasview

// What Slack's useEmbeddedUnfurlType (client-boot-shared gZnA) reads of a
// link card before it picks a kind: the card URL as a Slack permalink
// (client-boot ui7s parsePermalink), whether the URL is safe to show
// (I5QS isUrlSafeFromXSS, EmptyUnfurl's "Double-check this link"), and
// whether the container's attachment draws as a public link card
// (shouldRenderPublicLinkCard). Build 889ac449a32d; the notes are in
// /root/clack-ops/scratch/link-url-box-2/slack-cards.md.

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/some-du6e/clack/internal/slack"
)

// permalink is a card URL parsed as a Slack permalink: typ is
// getPermalinkType's (message, file, member, quip, canvas, list, …) and
// ts a message permalink's ts ("" for a channel's).
type permalink struct {
	typ, ts string
	// the ids the cards read: a message's or a channel canvas's channel
	// and a reply's thread_ts, a file's, canvas's or list's file id, a
	// member's id, a list's record_id and view_id
	channel, threadTS, fileID, memberID, recordID, viewID string
}

// slackPermalinkHosts are isSlackUrl's (/bpz): a host's last two labels.
var slackPermalinkHosts = map[string]bool{"slack.com": true, "slack-dev.com": true, "slack-gov.com": true,
	"slack-gov-dev.com": true, "slack.mil": true, "slack-mil-dev.com": true}

// permalinkHostRe is isValidSlackPermalinkHostName's shape (mMpZ).
var permalinkHostRe = regexp.MustCompile(`^[^.]*\.?(((enterprise\.)?(staging|staging-master|staging-sandbox|dev\d*|qa\d*)|enterprise)\.)?[^.]+\.[^.]+$`)

var (
	permalinkTSRe  = regexp.MustCompile(`^p?(\d{10})\.?(\d{6})$`) // DTNe formatMessageTimestamp
	permalinkFile  = regexp.MustCompile(`^F[A-Z0-9]{8,14}$`)      // oRye
	permalinkUser  = regexp.MustCompile(`^@?(U|W)[A-Z0-9]{8,}$`)  // kflk looksLikeMemberId
	permalinkUsers = map[string]bool{"USLACKBOT": true, "USEARCH": true, "UCHANNELEXPERT": true, "USLACK": true, "UTBD": true}
)

// permalinkTypes is getPermalinkType: a path's first segment's type.
var permalinkTypes = map[string]string{
	"archives": "message", "messages": "message", "conversation": "message",
	"file": "file", "files": "file", "slides": "file", "surfaces": "file",
	"team": "member", "calls": "room", "docs": "quip", "lists": "list", "canvas": "canvas", "shortcuts": "shortcuts",
	"huddle": "huddle", "external-huddle": "huddle", "search": "search", "slackhelp": "slackhelp", "help": "slackhelp",
	"org_chart": "org_chart", "widgets": "widgets", "create": "create", "prompts": "shared_prompt", "work-object": "work-object",
	"calendar": "calendar", "channel-section": "channel-section", "salesforce": "salesforce-record-flexpane", "skills": "skill",
}

// parsePermalink is parsePermalink for the shapes a card routes by; ok is
// false for a URL that isn't one (a web link, app.slack.com/client, …).
func parsePermalink(raw string) (permalink, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.Path == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return permalink{}, false
	}
	host := strings.ToLower(u.Hostname())
	labels := strings.Split(host, ".")
	if len(labels) < 2 || !slackPermalinkHosts[strings.Join(labels[len(labels)-2:], ".")] || !permalinkHostRe.MatchString(host) {
		return permalink{}, false
	}
	sub := ""
	if host != "slack.com" {
		sub = labels[0]
	}
	var l []string // lodash compact: empty segments drop out
	for _, s := range strings.Split(u.Path, "/") {
		if s != "" {
			l = append(l, s)
		}
	}
	if len(l) == 0 {
		return permalink{}, false
	}
	typ, ok := permalinkTypes[l[0]]
	if !ok {
		return permalink{}, false
	}
	at := func(i int) string {
		if i < len(l) {
			return l[i]
		}
		return ""
	}
	q := u.Query()
	switch typ {
	case "message":
		if at(1) == "" {
			return permalink{}, false
		}
		p := permalink{typ: "message", channel: at(1), threadTS: q.Get("thread_ts")}
		if m := permalinkTSRe.FindStringSubmatch(at(2)); m != nil {
			p.ts = m[1] + "." + m[2]
		}
		return p, true
	case "file":
		return permalink{typ: typ, fileID: at(2)}, permalinkFile.MatchString(at(2))
	case "member":
		id := strings.TrimPrefix(at(1), "@")
		return permalink{typ: typ, memberID: id}, permalinkUser.MatchString(at(1)) || permalinkUsers[at(1)]
	case "quip": // /docs/<team>/<F…>, and a workspace host (never app.slack.com)
		return permalink{typ: typ, fileID: at(2)}, permalinkFile.MatchString(at(2)) && sub != "" && sub != "app"
	case "list":
		return permalink{typ: typ, fileID: at(2), recordID: q.Get("record_id"), viewID: q.Get("view_id")}, permalinkFile.MatchString(at(2))
	case "canvas":
		return permalink{typ: typ, channel: at(1)}, at(1) != ""
	}
	return permalink{typ: typ}, true
}

// xssPatterns are I5QS's.
var xssPatterns = func() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, p := range []string{`javascript:`, `vbscript:`, `<script`, `</script`, `onerror=`, `onload=`, `onclick=`, `onmouseover=`,
		`onfocus=`, `onblur=`, `alert\s*?\(`, `confirm\s*?\(`, `prompt\s*?\(`, `eval\s*?\(`, `expression\s*?\(`,
		`setInterval\s*?\(`, `setTimeout\s*?\(`, `%3Cscript`, `%3C/script`, `%3Conerror`, `%3Conload`, `%3Conclick`,
		`%3Conmouseover`, `%3Confocus`, `%3Conblur`, `%3Calert`, `%3Cconfirm`, `%3Cprompt`, `%3Ceval`,
		`%3Cexpression`, `%3CsetInterval`, `%3CsetTimeout`, `<alert`, `<confirm`, `<prompt`, `<eval`,
		`<expression`, `<setInterval`, `<setTimeout`} {
		out = append(out, regexp.MustCompile(`(?i)`+p))
	}
	return out
}()

func xssMatch(s string) bool {
	for _, re := range xssPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// decodeURIComponent is JavaScript's: every %XX decoded, false on a bad
// escape or bytes that aren't UTF-8 (a URIError).
func decodeURIComponent(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", false
		}
		h, ok1 := unhexByte(s[i+1])
		l, ok2 := unhexByte(s[i+2])
		if !ok1 || !ok2 {
			return "", false
		}
		b.WriteByte(h<<4 | l)
		i += 2
	}
	out := b.String()
	return out, utf8.ValidString(out)
}

func unhexByte(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// urlSafe is isUrlSafeFromXSS: false shows EmptyUnfurl's "Double-check
// this link" banner. A string that isn't an absolute URL at all is safe
// unless a pattern matches it (the catch). The quote and semicolon test
// runs on the browser's serialized href: WHATWG writes " as %22 and, in
// an http(s) query, ' as %27, so what's left is ; anywhere and ' outside
// the query.
func urlSafe(raw string) bool {
	a := strings.TrimSpace(raw)
	if a == "" {
		return true
	}
	if strings.HasPrefix(a, "data:") {
		if i := strings.IndexByte(a, ','); i >= 0 && i+1 < len(a) {
			part := a[i+1:]
			if j := strings.IndexByte(part, ','); j >= 0 { // split(",")[1]
				part = part[:j]
			}
			dec, err := base64.StdEncoding.DecodeString(part)
			if err != nil {
				return false
			}
			return !xssMatch(string(dec))
		}
	}
	u, err := url.Parse(a)
	dec, decOK := decodeURIComponent(a)
	if err != nil || u.Scheme == "" || !decOK { // new URL or decodeURIComponent threw
		return !xssMatch(raw)
	}
	if xssMatch(dec) {
		return false
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" || u.User != nil {
		return false
	}
	if strings.Contains(a, ";") {
		return false
	}
	beforeQuery, frag := a, ""
	if i := strings.IndexByte(a, '#'); i >= 0 {
		beforeQuery, frag = a[:i], a[i:]
	}
	if i := strings.IndexByte(beforeQuery, '?'); i >= 0 {
		beforeQuery = beforeQuery[:i]
	}
	return !strings.Contains(beforeQuery, "'") && !strings.Contains(frag, "'")
}

// knownBlockTypes are Slack's Block Kit block types (client-boot ZOnG WP).
var knownBlockTypes = map[string]bool{}

func init() {
	for _, t := range strings.Fields(`callout canvas_table context context_actions section image divider actions file
		message expandable canvas error rich_text call input group header prototype table loading event contact_card
		share_shortcut share_prompt share_skill share_workflow video message_embed condition channel_set channel_section
		timeline plan task_card alert card related_automation_decorator carousel data_table container
		salesforce_record_card search_top_results mcp_app data_visualization layout page auth_provider
		attachment_reference progress_bar question fallback_canary`) {
		knownBlockTypes[t] = true
	}
}

// publicLinkCallbacks are the callback_ids shouldRenderPublicLinkCard
// leaves out.
var publicLinkCallbacks = map[string]bool{"tractorWorkspaceSetupComplete": true, "teamcreatorprivacysettings": true,
	"slackbotresponse_button": true, "pending_scsc_trigger_scdm": true, "slackbot_sms_download_app_button": true,
	"slack_connect_switch_workspaces": true, "slackbotAddUserToMpdm": true}

// publicLinkCard is shouldRenderPublicLinkCard: the attachment draws as
// EmbeddedPublicLinkUnfurl (a web link's own preview, not an app's).
func publicLinkCard(a *slack.Attachment) bool {
	if a.ServiceName == "twitter" {
		return true
	}
	var blocks []struct {
		Type string `json:"type"`
	}
	hasBlocks := len(a.Blocks) > 0 && string(a.Blocks) != "null" && json.Unmarshal(a.Blocks, &blocks) == nil && blocks != nil
	allKnown := true
	for _, b := range blocks {
		if !knownBlockTypes[b.Type] {
			allKnown = false
		}
	}
	listRecord := truthy(a.ListRecord)
	legacy := truthy(a.FallbackToLegacy)
	var callback string
	_ = json.Unmarshal(a.CallbackID, &callback)
	switch {
	case hasBlocks && !legacy, hasBlocks && legacy && allKnown,
		a.ServiceName == "salesforce", a.ServiceName == "slackhelp", a.IsMsgUnfurl, listRecord, a.IsAppUnfurl,
		a.AudioHTML != "", a.VideoHTML != "", a.Title == "" && a.Text == "", publicLinkCallbacks[callback]:
		return false
	}
	return true
}

// truthy is JavaScript's truthiness of a raw JSON value (absent, null,
// false, 0 and "" are false).
func truthy(raw json.RawMessage) bool {
	switch v := strings.TrimSpace(string(raw)); v {
	case "", "null", "false", "0", `""`:
		return false
	}
	return true
}
