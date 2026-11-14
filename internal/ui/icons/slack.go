package icons

import "strings"

// Slack SvgIcon names. Taut plugins and Slack-shaped props name icons the
// way Slack's SvgIcon does ("eye-closed", "notifications-off", …); Slack
// resolves such a name to an icon in this set.

// SlackFallback is drawn for Slack names with no counterpart: a quiet dot
// rather than the red "missing" box. Each such name is logged once and
// listed by Missing, so a plugin icon the set lacks shows up in test logs
// instead of passing as a dot.
const SlackFallback = "dot"

// slackIconNames maps Slack icon names to Clack icon names where they
// differ. Names Clack already registers under Slack's own name (calendar,
// caret-down, close, code, copy, edit, emoji, info, lock, minus, plus,
// settings, warning, eye-closed, glasses, megaphone, plug, bug, …) need no
// entry: Slack tries the registry first.
var slackIconNames = map[string]string{
	// the legacy font Icon type "circle-fill" (the Unreads header's
	// "New messages" dot): a filled circle
	"circle-fill": "dot",
	// the switcher toolbar's View profile (search.go): Directories' glyph
	"user-directory": "directories",
	// catalogue names (docs/PLUGINS-components.md §1.12)
	"arrow-left":           "back",
	"eye-open":             "eye",
	"notifications":        "bell",
	"notifications-filled": "bell-filled",
	"notifications-off":    "bell-off",
	"sparkles-filled":      "sparkles",

	// common Slack names
	"arrow-right":                "forward",
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
	"delete-filled":              "trash-filled",
	"checkmark":                  "check",
	"check-small":                "check",
	"checkmark-circle":           "check-circle",
	"close-circle":               "x-circle",
	"x":                          "close",
	"remove":                     "x-circle",
	"info-circle":                "info",
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
	"channel-filled":             "hash-filled",
	"private":                    "lock",
	"padlock":                    "lock",
	"lock-open":                  "lock",
	"unlock":                     "lock",
	"external-link":              "external",
	"link-external":              "external",
	"open-in-new":                "external",
	"open-in-window":             "external",
	"redo":                       "forward",
	"drag-handle":                "reorder",
	"grabber":                    "reorder",
	"eye-slash":                  "eye-off",
	"visible":                    "eye",
	"hidden":                     "eye-closed",
	"file-download":              "download",
	"wand":                       "magic-wand",
	"sparkle-filled":             "sparkle",
	"announcement":               "megaphone",
	"notification":               "bell",
	"bell-slash":                 "bell-off",
	"calendar-add":               "calendar",
	"add-user":                   "user-plus",
	"person-add":                 "user-plus",
	"users":                      "people",
	"user-group":                 "people",
	"person":                     "user",
	"member":                     "user",
	"profile":                    "user",
	"dm":                         "dms",
	"direct-messages":            "dms",
	"speech-bubble":              "message",
	"mentions":                   "at",
	"at-sign":                    "at",
	"bookmark-outline":           "bookmark",
	"star-outline":               "star",
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
	"robot":                      "bot",
	"duplicate":                  "copy",
	"chain":                      "link",
	"arrow-reply":                "reply",
	"unread":                     "mark-unread",
	"mark-as-unread":             "mark-unread",
	"send-later":                 "schedule",
	"scheduled":                  "schedule",
	"document":                   "file",
	"window":                     "panel-right",
	"code-snippet":               "code-block",
	"headset":                    "headphones",
	"lightbulb":                  "emoji-objects",
	"sliders":                    "filter-sliders",
	"bug-filled":                 "bug",
	"plug-filled":                "plug",
	"sf-cloud":                   "cloud",
	"navigation-tabs-vertical":   "sidebar",
	"chat":                       "message",
	"clipboard":                  "copy",
	"compass":                    "explore",
	"cube":                       "blocks",
	"keyboard-shortcuts":         "keyboard",
	"return":                     "enter",
}

// slackSuffixes are modifiers Slack appends to a base icon name; a name
// with no entry of its own falls back to its base ("calendar-filled" →
// "calendar").
var slackSuffixes = [...]string{"-filled", "-outline", "-outlined", "-small", "-large", "-alt", "-bold", "-circle"}

// slackLookup resolves a Slack name, reporting whether it found one.
// Allocation-free: map lookups and suffix cuts only.
func slackLookup(name string) (string, bool) {
	if _, ok := registry[name]; ok {
		return name, true
	}
	if m, ok := slackIconNames[name]; ok {
		return m, true
	}
	for _, suf := range slackSuffixes {
		if b, ok := strings.CutSuffix(name, suf); ok {
			if _, ok := registry[b]; ok {
				return b, true
			}
			if m, ok := slackIconNames[b]; ok {
				return m, true
			}
		}
	}
	return SlackFallback, false
}

// Slack returns the Clack icon name for a Slack SvgIcon name: the name
// itself when registered, then the alias table, then the name without a
// -filled/-outline/-small… suffix, else SlackFallback.
func Slack(name string) string {
	n, ok := slackLookup(name)
	if !ok {
		noteMissing(name, n) // logged once; Missing lists it for tests
	}
	return n
}

// HasSlack reports whether a Slack icon name has a real counterpart (not
// the fallback).
func HasSlack(name string) bool {
	_, ok := slackLookup(name)
	return ok
}

// GetSlack returns the icon for a Slack SvgIcon name (see Slack).
func GetSlack(name string) Icon { return Get(Slack(name)) }

// Icons Slack's plugins name that the base set lacked, redrawn on the
// same 20×20 grid and stroke.
func init() {
	// The sidebar section and Create menus' glyphs, redrawn on the
	// centerlines of Slack's own (channel, user-add, channel-search,
	// mark-as-read, share-message, lists, add-channel-canvas; held to
	// Slack's ink and shape by TestIconsMatchSlack).
	// Activity's "Cleared items" filter: Slack's eraser on the centrelines
	// of its 1.5-unit outline (body, the band, and the two crumbs).
	reg("eraser", s("M10.244 4.01 A1.644 1.644 0 0 1 12.569 4.01 L16.345 7.786 A1.644 1.644 0 0 1 16.345 10.111 L10.498 15.96 A1.65 1.65 0 0 1 9.331 16.443 H6.51 A1.65 1.65 0 0 1 5.343 15.96 L2.98 13.596 A1.644 1.644 0 0 1 2.98 11.271 Z"),
		s("M5.93 9.38 L10.97 14.42"), s("M15.75 16.47 H17.5"), s("M15.75 14.23 L17.39 13.62"))
	reg("channel", s("M9 2.75 L6.5 17.25 M13.75 2.75 L11.25 17.25 M3.75 6.75 H17.25 M2.75 13.75 H16.25"))
	reg("user-add", s("M9 2.75 C10.9 2.75 12.75 4.1 12.75 6.67 C12.75 9.3 10.6 11.1 9 11.1 C7.4 11.1 5.25 9.3 5.25 6.67 C5.25 4.1 7.1 2.75 9 2.75 Z"),
		s("M12.25 11.8 C11.2 11.4 10.05 11.25 9 11.25 C7.75 11.25 6.2 11.5 4.9 12.15 C3.5 12.8 2.3 13.85 1.85 15.25 C1.5 16.3 2.4 17.25 3.5 17.25 H12.25"),
		s("M15.5 11.75 V17.25 M12.75 14.5 H18.25"))
	reg("channel-search", s("M6.25 3.75 L3.75 16.25 M8.7 11.5 L7.75 16.25 M1.75 12.75 H11.25 M2.75 7.25 H6.75"),
		s(circle(13, 7, 3.25)+" M15.3 9.3 L16.75 10.75"))
	reg("mark-as-read", s("M10.6 6.6 L12.6 8.55 L16.62 3.25 M12.23 3.36 H4.81 A1.5 1.5 0 0 0 3.31 4.86 V15.15 A1.5 1.5 0 0 0 4.81 16.65 H15.17 A1.5 1.5 0 0 0 16.67 15.15 V8.77"))
	reg("share-message", s("M2.25 16.25 V14 C2.25 11.6 2.8 9.9 4 8.85 C5.2 7.75 7.1 7.25 9.5 7.25 H10.75 V3.3 L17.65 10 L10.75 16.7 V12.75 H6 C4.5 12.75 3.55 13.2 3 13.85 C2.45 14.5 2.25 15.3 2.25 16.25"))
	// Slack's list-item (ListItemName before a list item's title in
	// Activity), on the centrelines of Slack's outlines: the frame open at
	// the bottom left (r 3), the three lines, the heart, and the disc with
	// its check cut out (that subpath runs backwards: Slack fills evenodd,
	// Clack nonzero).
	reg("list-item", s("M2.25 8.25 V5.25 A3 3 0 0 1 5.25 2.25 H14.75 A3 3 0 0 1 17.75 5.25 V14.75 A3 3 0 0 1 14.75 17.75 H11.75"),
		s("M10.62 6.36 H15.12 M11.12 10.05 H15.12 M12.62 13.74 H15.12"),
		f("M7.325 7.997c-.335.23-.537.343-.737.343s-.403-.116-.74-.347c-.85-.58-1.51-1.313-1.51-2.078 0-.596.488-1.075 1.168-1.075.824 0 1.082.757 1.082.757s.25-.757 1.082-.757c.682 0 1.168.48 1.168 1.075 0 .765-.663 1.496-1.513 2.082Z"),
		f("M10.358 14a4.359 4.359 0 1 1-8.717 0 4.359 4.359 0 0 1 8.718 0ZM8.053 12.632A.75.75 0 0 0 6.992 12.615L5.347 14.205 4.86 13.733A.75.75 0 1 0 3.816 14.811L4.825 15.788A.75.75 0 0 0 5.868 15.788L8.035 13.692A.75.75 0 0 0 8.053 12.632Z"))
	// Slack's table and menu, the Activity layout toggle's Detailed and
	// Dense (ActivityFilterLayoutToggle), on their outlines' centrelines.
	reg("table", s(rrect(2.25, 3.25, 15.5, 13.5, 2.25)), s("M2.25 7.75 H17.75 M2.25 12.25 H17.75"))
	reg("menu", s("M1.75 5.25 H18.25 M1.75 10 H18.25 M1.75 15 H18.25"))
	reg("lists", s(rrect(2.25, 2.25, 15.5, 15.5, 3)), s("M4.88 12.78 L6.06 13.92 L8.38 11.67 M10.62 6.36 H15.12 M10.62 10.05 H15.12 M10.62 13.74 H15.12"),
		f("M6.53 8.83 C6.73 8.83 6.93 8.72 7.27 8.49 C8.12 7.9 8.78 7.17 8.78 6.4 C8.78 5.81 8.3 5.33 7.61 5.33 C6.78 5.33 6.53 6.09 6.53 6.09 C6.53 6.09 6.27 5.33 5.45 5.33 C4.77 5.33 4.28 5.81 4.28 6.4 C4.28 7.17 4.94 7.9 5.79 8.48 C6.13 8.71 6.33 8.83 6.53 8.83 Z"))
	// Slack's caret-down-filled / caret-right-filled (icons.json): each a
	// triangle rounded by 0.75 (its arcs' centres are the corners below),
	// drawn as fl parts on that triangle, 0.72 inside the edge, so the ink
	// matches in dark mode too. The Files sidebar's file collection header
	// shows them on hover.
	reg("caret-down-filled", fl("M6.25 8 H13.75 L10 12.5 Z"))
	reg("caret-right-filled", fl("M8.25 6.25 L12.74 10 L8.25 13.75 Z"))
	reg("add-channel-canvas", s("M8.25 2.25 H14.75 A3 3 0 0 1 17.75 5.25 V11.5 L11.5 17.75 H5.25 A3 3 0 0 1 2.25 14.75 V9.25 M11.5 17.75 V12.25 A.75 .75 0 0 1 12.25 11.5 H17.75 M4 1.75 V6.25 M1.75 4 H6.25"))

	// Slack's rotate (icons.json), the comment thread header's Re-open
	// thread (ArchiveCommentButton): the 1.5-unit ring's centreline, open
	// at the right, and the arrow at its top end.
	reg("rotate", s("M16.37 5.59 A7.75 7.75 0 1 0 17.02 13.28"), s("M17.75 2.25 V6.75 H13.25"))
	reg("eye-off", s("M2.5 10 C4.4 6.4 7 4.75 10 4.75 C13 4.75 15.6 6.4 17.5 10 C15.6 13.6 13 15.25 10 15.25 C7 15.25 4.4 13.6 2.5 10 Z"), s(circle(10, 10, 2.5)), s("M3.5 3.5 L16.5 16.5"))
}
