package inspect

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"

	"github.com/some-du6e/clack/internal/ui/slackc"
)

// Redaction. A report goes to #clack, so nothing in it may say whose
// workspace, which conversation or what anyone wrote, but the maintainer
// still has to tell "the same message" from "another one". Every id
// becomes a tag: a kind and the first 6 hex digits of SHA-256(salt‖id),
// with a random salt per report. Within one report the same channel
// always has the same tag; across reports, and for anyone without the
// salt (it is never written anywhere), a tag says nothing. Text the user
// typed or read (messages, names, search queries, topics, emails) is
// never collected at all: the report is built from component names,
// classes, sizes, colours and these tags.

// Redactor turns ids into a report's tags.
type Redactor struct {
	salt [16]byte
}

// NewRedactor has a new random salt.
func NewRedactor() *Redactor {
	r := &Redactor{}
	if _, err := rand.Read(r.salt[:]); err != nil {
		panic("inspect: no randomness for the report salt: " + err.Error())
	}
	return r
}

// Tag is kind:hash for id ("" for an empty id).
func (r *Redactor) Tag(kind, id string) string {
	if id == "" {
		return ""
	}
	h := sha256.New()
	h.Write(r.salt[:])
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(id))
	return kind + ":" + hex.EncodeToString(h.Sum(nil))[:6]
}

// ID tags a Slack id by what its first letter says it is (C… channel,
// D… DM, U…/W… user, B… bot, T…/E… team, F… file, a timestamp ts), "id"
// for anything else. The kind comes from the id itself, so one id has
// one tag wherever it shows up in a report.
func (r *Redactor) ID(id string) string { return r.Tag(idKind(id), id) }

func idKind(id string) string {
	if !isSlackID(id) {
		if slackTS.MatchString(id) {
			return "ts"
		}
		return "id"
	}
	switch id[0] {
	case 'C', 'G':
		return "ch"
	case 'D':
		return "dm"
	case 'U', 'W':
		return "user"
	case 'B':
		return "bot"
	case 'T', 'E':
		return "team"
	case 'F':
		return "file"
	}
	return "id"
}

var (
	slackID = regexp.MustCompile(`^[A-Z][A-Z0-9]{6,}$`)
	slackTS = regexp.MustCompile(`^\d{9,11}\.\d{1,6}$`)
)

// isSlackID is an uppercase id with a digit in it (C0B4DEFKFCJ), not a
// shouted word.
func isSlackID(s string) bool {
	return slackID.MatchString(s) && strings.ContainsAny(s, "0123456789")
}

// propIDKeys are the props whose string values are ids, with the kind
// their tag gets: what Clack's natives pass a patching plugin
// (messages/row.go, views, popups) and Slack's message objects' fields.
var propIDKeys = map[string]string{
	"$key":        "key",
	"channelId":   "ch",
	"channel":     "ch",
	"channel_id":  "ch",
	"userId":      "user",
	"user":        "user",
	"user_id":     "user",
	"currentUser": "user",
	"botId":       "bot",
	"bot_id":      "bot",
	"teamId":      "team",
	"team":        "team",
	"ts":          "ts",
	"messageTs":   "ts",
	"threadTs":    "ts",
	"thread_ts":   "ts",
	"parentId":    "ts",
	"fileId":      "file",
}

// propSafeKeys are props whose values are kept as they are: Slack's
// enumerations, never text anyone wrote.
var propSafeKeys = map[string]bool{
	"subtype": true, "type": true, "viewContext": true, "messageContainerType": true,
	"size": true, "isInteractive": true, "showCard": true, "isMpdm": true,
}

// PropIDs collects a component's ids from its props (and the message
// object in "msg", one level down) as "key=tag" pairs, and the safe
// enumerations as "key=value", sorted. Everything else in the props
// (text, names, blocks, files) is skipped.
func (r *Redactor) PropIDs(p map[string]any) []string {
	set := map[string]bool{}
	var walk func(p map[string]any, prefix string, depth int)
	walk = func(p map[string]any, prefix string, depth int) {
		for k, v := range p {
			switch x := v.(type) {
			case string:
				if kind, ok := propIDKeys[k]; ok && x != "" {
					if ik := idKind(x); ik != "id" {
						kind = ik // the id's own kind: one tag per id
					}
					set[prefix+k+"="+r.Tag(kind, x)] = true
				} else if propSafeKeys[k] && safeValue(x) {
					set[prefix+k+"="+x] = true
				}
			case bool:
				if propSafeKeys[k] {
					if x {
						set[prefix+k+"=true"] = true
					} else {
						set[prefix+k+"=false"] = true
					}
				}
			case map[string]any:
				if depth == 0 && (k == "msg" || k == "message") {
					walk(x, k+".", depth+1)
				}
			case slackc.Props:
				if depth == 0 && (k == "msg" || k == "message") {
					walk(x, k+".", depth+1)
				}
			}
		}
	}
	walk(p, "", 0)
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// PropChannels are the channel ids in a component's props (and its
// message), for asking what kind of conversation they are. The ids
// themselves never go into a report.
func PropChannels(p map[string]any) []string {
	var out []string
	var walk func(p map[string]any, depth int)
	walk = func(p map[string]any, depth int) {
		for _, k := range []string{"channelId", "channel", "channel_id"} {
			if id, ok := p[k].(string); ok && isSlackID(id) {
				out = append(out, id)
			}
		}
		if depth == 0 {
			for _, k := range []string{"msg", "message"} {
				switch m := p[k].(type) {
				case map[string]any:
					walk(m, 1)
				case slackc.Props:
					walk(m, 1)
				}
			}
		}
	}
	if p != nil {
		walk(p, 0)
	}
	return out
}

// safeValue is a short identifier-like enumeration value ("bot_message",
// "message"), never free text.
func safeValue(s string) bool {
	if len(s) == 0 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// pages are the first route segments Clack has (route.go): kept, as they
// say which page, not whose.
var pages = map[string]bool{
	"c": true, "dms": true, "activity": true, "later": true, "threads": true, "unreads": true,
	"files": true, "search": true, "people": true, "channels": true, "huddles": true, "bbm": true,
	"home": true, "drafts": true, "canvases": true, "lists": true, "agents": true, "tools": true,
	"apps": true, "workflows": true, "directories": true, "saved": true, "mentions": true,
}

// Route redacts a Clack route ("c/C123/1700000000.000100"): the page
// stays, every other segment becomes a tag (search queries included).
func (r *Redactor) Route(route string) string {
	if route == "" {
		return ""
	}
	segs := strings.Split(route, "/")
	for i, s := range segs {
		switch {
		case i == 0 && pages[s]:
		case s == "":
		default:
			segs[i] = r.ID(s)
		}
	}
	return strings.Join(segs, "/")
}

// enumAttrs are attributes whose values Slack's DOM uses as enumerations
// (data-qa="add_reaction", data-type="channel").
var enumAttrs = map[string]bool{"data-qa": true, "data-type": true, "role": true}

// Attr redacts the value of an attribute a native set at run time
// (EnterAttr): an enumeration attribute's identifier-like value stays,
// anything else (a channel's id, an item key) becomes a tag.
func (r *Redactor) Attr(name, v string) string {
	if enumAttrs[name] && safeValue(v) {
		return v
	}
	return r.ID(v)
}
