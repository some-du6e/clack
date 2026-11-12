package kit

import (
	"image"
	"strings"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// slackIcons maps Slack's SvgIcon names (and the names Taut's plugins
// pass) to the closest icon in Clack's set. Names Clack already has are
// used as they are; see clackIcon.
var slackIcons = map[string]string{
	"ellipsis-vertical":          "kebab",
	"ellipsis-vertical-filled":   "kebab",
	"ellipsis-horizontal":        "more",
	"ellipsis-horizontal-filled": "more",
	"ellipsis":                   "more",
	"more-horizontal":            "more",
	"more-vertical":              "kebab",
	"gear":                       "settings",
	"cog":                        "settings",
	"preferences":                "settings",
	"thread":                     "threads",
	"paper-plane":                "drafts", // Slack's paper-plane (TestIconsMatchSlack holds "drafts" to it)
	"pencil":                     "edit",
	"compose-message":            "compose",
	"new-message":                "compose",
	"delete":                     "trash",
	"checkmark":                  "check",
	"check-small":                "check",
	"check-filled":               "check-circle",
	"check-circle-filled":        "check-circle",
	"checkmark-circle":           "check-circle",
	"close-circle":               "x-circle",
	"close-circle-filled":        "x-circle",
	"x":                          "close",
	"remove":                     "x-circle",
	"info-circle":                "info",
	"info-filled":                "info",
	"warning-filled":             "warning",
	"alert":                      "warning",
	"exclamation-triangle":       "warning",
	"question":                   "help",
	"question-circle":            "help",
	"help-circle":                "help",
	"emoji-add":                  "add-reaction",
	"reaction":                   "add-reaction",
	"add-emoji":                  "add-reaction",
	"smile":                      "emoji",
	"emoji-smile":                "emoji",
	"hash-filled":                "hash",
	"private":                    "lock",
	"padlock":                    "lock",
	"lock-open":                  "lock",
	"unlock":                     "lock",
	"key":                        "lock",
	"external-link":              "external",
	"link-external":              "external",
	"open-in-new":                "external",
	"open-in-window":             "external",
	"undo":                       "history-back",
	"redo":                       "forward",
	"arrow-left":                 "back",
	"arrow-right":                "forward",
	"caret-left":                 "chevron-left",
	"caret-up-down":              "sort",
	"reorder":                    "list-view",
	"drag-handle":                "list-view",
	"grabber":                    "list-view",
	"glasses":                    "eye",
	"glasses-filled":             "eye",
	"eye-closed":                 "eye",
	"eye-slash":                  "eye",
	"visible":                    "eye",
	"hidden":                     "eye",
	"file-upload":                "upload",
	"file-download":              "download",
	"paintbrush":                 "sparkle",
	"magic-wand":                 "sparkle",
	"wand":                       "sparkle",
	"sparkles":                   "sparkle",
	"megaphone":                  "bell",
	"announcement":               "bell",
	"notifications":              "bell",
	"notification":               "bell",
	"bell-slash":                 "bell-off",
	"notifications-off":          "bell-off",
	"calendar-add":               "calendar",
	"add-user":                   "user-plus",
	"person-add":                 "user-plus",
	"users":                      "people",
	"user-group":                 "people",
	"user-groups":                "people",
	"person":                     "user",
	"member":                     "user",
	"profile":                    "user",
	"dm":                         "dms",
	"direct-messages":            "dms",
	"comment":                    "dms",
	"message":                    "dms",
	"speech-bubble":              "dms",
	"mentions":                   "at",
	"at-sign":                    "at",
	"bookmark-outline":           "bookmark",
	"star-outline":               "star",
	"pin-filled":                 "pin",
	"time":                       "clock",
	"alarm":                      "remind",
	"alarm-clock":                "remind",
	"reminder":                   "remind",
	"magnifying-glass":           "search",
	"funnel":                     "filter",
	"add":                        "plus",
	"add-circle":                 "plus-circle",
	"plus-circle-filled":         "plus-circle",
	"subtract":                   "minus",
	"reload":                     "refresh",
	"sync":                       "refresh",
	"photo":                      "image",
	"picture":                    "image",
	"video-camera":               "video",
	"microphone":                 "mic",
	"sound":                      "volume",
	"volume-off":                 "mute",
	"world":                      "globe",
	"email":                      "mail",
	"envelope":                   "mail",
	"strikethrough":              "strike",
	"bulleted-list":              "list-bullet",
	"list-bullets":               "list-bullet",
	"numbered-list":              "list-ordered",
	"blockquote":                 "quote",
	"attachment":                 "attach",
	"paperclip":                  "attach",
	"shortcuts":                  "shortcut",
	"bolt":                       "lightning",
	"list":                       "list-view",
	"robot":                      "agents",
	"bot":                        "agents",
	"duplicate":                  "copy",
	"chain":                      "link",
	"arrow-reply":                "reply",
	"unread":                     "mark-unread",
	"send-later":                 "schedule",
	"scheduled":                  "schedule",
	"file":                       "files",
	"document":                   "files",
	"folder":                     "files",
	"window":                     "panel-right",
	"code-snippet":               "code-block",
	"keyboard":                   "shortcut",
	"emoji-filled":               "emoji",
	"headset":                    "headphones",
	"lightbulb":                  "sparkle",
	"filter-sliders":             "filter-sliders",
	"sliders":                    "filter-sliders",
	"bell-filled":                "bell-filled",
	"dot":                        "dot",
}

// fallbackIcon is drawn for names with no counterpart: a quiet dot rather
// than icons' red "missing" box.
const fallbackIcon = "dot"

// clackIcon maps a Slack icon name to a Clack icon name.
func clackIcon(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return fallbackIcon
	}
	if icons.Has(n) {
		return n
	}
	if m, ok := slackIcons[n]; ok && icons.Has(m) {
		return m
	}
	// "trash-filled" → "trash", "bell-outline" → "bell", "close-small" → "close".
	for _, suf := range []string{"-filled", "-outline", "-outlined", "-small", "-large", "-alt", "-bold", "-circle"} {
		if b, ok := strings.CutSuffix(n, suf); ok {
			if icons.Has(b) {
				return b
			}
			if m, ok := slackIcons[b]; ok && icons.Has(m) {
				return m
			}
		}
	}
	// the icon set's own table of Slack's names ("eye-open" → "eye":
	// StreamerMode's toggle), which this one predates
	if icons.HasSlack(n) {
		return icons.Slack(n)
	}
	return fallbackIcon
}

// SvgIcon: name (Slack's icon name), size (px, default 20), inline. Drawn
// in currentColor (the inherited CSS color), like Slack's SVG icons.
func svgIcon(gtx layout.Context, h *slackc.Host) layout.Dimensions {
	th := thm()
	size := float32(h.Props.IntOr("size", 20))
	if size <= 0 {
		size = 20
	}
	c := th.C.Fg
	if h.Style != nil && h.Style.Color.A > 0 {
		c = h.Style.Color
	}
	d := drawIcon(gtx, h.Props.StrOr("name", ""), size, c)
	if h.Props.Bool("inline") {
		// inline icons sit in the text line (vertical-align: text-bottom).
		return layout.Dimensions{Size: d.Size, Baseline: d.Size.Y / 5}
	}
	return layout.Dimensions{Size: image.Pt(d.Size.X, d.Size.Y)}
}

func init() { add(svgIcon, "SvgIcon") }
