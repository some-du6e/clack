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
	// catalogue names (docs/PLUGINS-components.md §1.12)
	"arrow-left":           "back",
	"eye-open":             "eye",
	"notifications":        "bell",
	"notifications-filled": "bell-filled",
	"notifications-off":    "bell-off",
	"sparkles-filled":      "sparkles",

	// common Slack names
	"arrow-right":                "forward",
	"caret-left":                 "chevron-left",
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
	"paper-plane":                "send",
	"pencil":                     "edit",
	"compose-message":            "compose",
	"new-message":                "compose",
	"delete":                     "trash",
	"delete-filled":              "trash-filled",
	"checkmark":                  "check",
	"check-small":                "check",
	"check-circle-filled":        "check-filled",
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
	"emoji-filled":               "emoji",
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
	"redo":                       "forward",
	"drag-handle":                "reorder",
	"grabber":                    "reorder",
	"eye-slash":                  "eye-off",
	"visible":                    "eye",
	"hidden":                     "eye-off",
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
	"keyboard":                   "shortcut",
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
	"headset":                    "headphones",
	"lightbulb":                  "sparkle",
	"sliders":                    "filter-sliders",
	"bug-filled":                 "bug",
	"plug-filled":                "plug",
	"sf-cloud":                   "cloud",
	"navigation-tabs-vertical":   "sidebar",
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
	reg("channel", s("M9 2.75 L6.5 17.25 M13.75 2.75 L11.25 17.25 M3.75 6.75 H17.25 M2.75 13.75 H16.25"))
	reg("user-add", s("M9 2.75 C10.9 2.75 12.75 4.1 12.75 6.67 C12.75 9.3 10.6 11.1 9 11.1 C7.4 11.1 5.25 9.3 5.25 6.67 C5.25 4.1 7.1 2.75 9 2.75 Z"),
		s("M12.25 11.8 C11.2 11.4 10.05 11.25 9 11.25 C7.75 11.25 6.2 11.5 4.9 12.15 C3.5 12.8 2.3 13.85 1.85 15.25 C1.5 16.3 2.4 17.25 3.5 17.25 H12.25"),
		s("M15.5 11.75 V17.25 M12.75 14.5 H18.25"))
	reg("channel-search", s("M6.25 3.75 L3.75 16.25 M8.7 11.5 L7.75 16.25 M1.75 12.75 H11.25 M2.75 7.25 H6.75"),
		s(circle(13, 7, 3.25)+" M15.3 9.3 L16.75 10.75"))
	reg("mark-as-read", s("M10.6 6.6 L12.6 8.55 L16.62 3.25 M12.23 3.36 H4.81 A1.5 1.5 0 0 0 3.31 4.86 V15.15 A1.5 1.5 0 0 0 4.81 16.65 H15.17 A1.5 1.5 0 0 0 16.67 15.15 V8.77"))
	reg("share-message", s("M2.25 16.25 V14 C2.25 11.6 2.8 9.9 4 8.85 C5.2 7.75 7.1 7.25 9.5 7.25 H10.75 V3.3 L17.65 10 L10.75 16.7 V12.75 H6 C4.5 12.75 3.55 13.2 3 13.85 C2.45 14.5 2.25 15.3 2.25 16.25"))
	reg("lists", s(rrect(2.25, 2.25, 15.5, 15.5, 3)), s("M4.88 12.78 L6.06 13.92 L8.38 11.67 M10.62 6.36 H15.12 M10.62 10.05 H15.12 M10.62 13.74 H15.12"),
		f("M6.53 8.83 C6.73 8.83 6.93 8.72 7.27 8.49 C8.12 7.9 8.78 7.17 8.78 6.4 C8.78 5.81 8.3 5.33 7.61 5.33 C6.78 5.33 6.53 6.09 6.53 6.09 C6.53 6.09 6.27 5.33 5.45 5.33 C4.77 5.33 4.28 5.81 4.28 6.4 C4.28 7.17 4.94 7.9 5.79 8.48 C6.13 8.71 6.33 8.83 6.53 8.83 Z"))
	reg("add-channel-canvas", s("M8.25 2.25 H14.75 A3 3 0 0 1 17.75 5.25 V11.5 L11.5 17.75 H5.25 A3 3 0 0 1 2.25 14.75 V9.25 M11.5 17.75 V12.25 A.75 .75 0 0 1 12.25 11.5 H17.75 M4 1.75 V6.25 M1.75 4 H6.25"))

	reg("eye-closed", s("M2.75 8.25 C5.75 12.75 14.25 12.75 17.25 8.25"), s("M10 11.6 V14.5 M6.1 10.9 L4.75 13.25 M13.9 10.9 L15.25 13.25"))
	reg("eye-off", s("M2.5 10 C4.4 6.4 7 4.75 10 4.75 C13 4.75 15.6 6.4 17.5 10 C15.6 13.6 13 15.25 10 15.25 C7 15.25 4.4 13.6 2.5 10 Z"), s(circle(10, 10, 2.5)), s("M3.5 3.5 L16.5 16.5"))
	reg("glasses", s(circle(5.75, 11.75, 3)), s(circle(14.25, 11.75, 3)), s("M8.75 11.25 A1.6 1.6 0 0 1 11.25 11.25"), s("M2.75 11.75 L3.75 6.25 M17.25 11.75 L16.25 6.25"))
	reg("glasses-filled", f(circle(5.75, 11.75, 3.4)), f(circle(14.25, 11.75, 3.4)), s("M8.75 11.25 A1.6 1.6 0 0 1 11.25 11.25"), s("M2.75 11.75 L3.75 6.25 M17.25 11.75 L16.25 6.25"))
	reg("megaphone", s("M3.25 8 A1 1 0 0 1 4.25 7 H7 L15.25 3.5 V15.5 L7 12 H4.25 A1 1 0 0 1 3.25 11 Z"), s("M6 12 L7.25 16.25 H9.5 L8.6 12.65"), s("M17.5 8 V11"))
	reg("megaphone-filled", f("M2.75 7.9 A1.4 1.4 0 0 1 4.15 6.5 H6.9 L15 3.05 A.6.6 0 0 1 15.85 3.6 V15.4 A.6.6 0 0 1 15 15.95 L6.9 12.5 H4.15 A1.4 1.4 0 0 1 2.75 11.1 Z"), s("M6 12 L7.25 16.25 H9.5 L8.6 12.65"), s("M17.75 8 V11"))
	reg("magic-wand", s("M3.5 16.5 L11.25 8.75"), s("M14.5 2.5 C14.8 4.6 15.4 5.2 17.5 5.5 C15.4 5.8 14.8 6.4 14.5 8.5 C14.2 6.4 13.6 5.8 11.5 5.5 C13.6 5.2 14.2 4.6 14.5 2.5 Z"), f(circle(6.5, 4.75, 0.95)), f(circle(16, 13, 0.95)))
	reg("paintbrush", s("M10 10 L16.25 3.75"), s("M9 9 L11 11 L8.75 13.75 C7.75 15 6.6 15.4 3.5 16.5 C4.6 13.4 5 12.25 6.25 11.25 Z"))
	reg("plug", s("M7.5 3 V6.5 M12.5 3 V6.5"), s("M5 6.5 H15 V9.5 A5 5 0 0 1 5 9.5 Z"), s("M10 14.5 V17.5"))
	reg("bug", s(rrect(6.25, 6.75, 7.5, 10, 3.75)), s("M7.6 7.1 A2.5 2.5 0 0 1 12.4 7.1"), s("M8.25 4.9 L6.75 3 M11.75 4.9 L13.25 3"),
		s("M6.25 10.5 H3.5 M13.75 10.5 H16.5 M6.4 13.75 L4 15.25 M13.6 13.75 L16 15.25 M6.6 8.25 L4.25 6.75 M13.4 8.25 L15.75 6.75"), s("M10 9.5 V16.5"))
	reg("sparkles", s("M8.5 4.5 C9 7.9 10.6 9.5 14 10 C10.6 10.5 9 12.1 8.5 15.5 C8 12.1 6.4 10.5 3 10 C6.4 9.5 8 7.9 8.5 4.5 Z"),
		s("M15 2.25 C15.2 3.75 15.75 4.3 17.25 4.5 C15.75 4.7 15.2 5.25 15 6.75 C14.8 5.25 14.25 4.7 12.75 4.5 C14.25 4.3 14.8 3.75 15 2.25 Z"),
		f(circle(15.25, 15.25, 1.1)))
	reg("caret-up-down", f("M6.25 8.25 L10 4.25 L13.75 8.25 Z"), f("M6.25 11.75 L10 15.75 L13.75 11.75 Z"))
	reg("reorder", f(circle(7.5, 5, 1.25)), f(circle(12.5, 5, 1.25)), f(circle(7.5, 10, 1.25)), f(circle(12.5, 10, 1.25)), f(circle(7.5, 15, 1.25)), f(circle(12.5, 15, 1.25)))
	reg("file-upload", s("M5 3.25 H11.25 L15.5 7.5 V15.75 A1 1 0 0 1 14.5 16.75 H5 A1 1 0 0 1 4 15.75 V4.25 A1 1 0 0 1 5 3.25 Z"), s("M11.25 3.25 V7.5 H15.5"), s("M9.75 14.25 V9.25 M7.5 11.5 L9.75 9.25 L12 11.5"))
	reg("undo", s("M7.25 4.25 L3.5 8 L7.25 11.75"), s("M3.5 8 H12 A4.25 4.25 0 0 1 12 16.5 H8.5"))
	reg("trash-filled", f(rrect(3.25, 4.5, 13.5, 1.9, 0.6)), s("M8 4.75 V4 A.75.75 0 0 1 8.75 3.25 H11.25 A.75.75 0 0 1 12 4 V4.75"),
		f("M5 7.25 H15 L14.2 15.9 A1.3 1.3 0 0 1 12.9 17.1 H7.1 A1.3 1.3 0 0 1 5.8 15.9 Z"))
	// A filled disc with the check cut out (the hole runs the other way,
	// so it is empty under both fill rules).
	reg("check-filled", f(circle(10, 10, 7.25)+" M5.9 10.9 L9 13.9 L14.1 8.3 L12.7 6.9 L9 11.1 L7.3 9.5 Z"))
}
