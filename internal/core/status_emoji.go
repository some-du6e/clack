package core

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// A member's status emoji and the emoji inside its status text, as
// Slack draws them (client-boot, build 35b42de8b870):
//
//   - computeStatusEmojiDisplayInfo keys profile.status_emoji_display_info
//     by each entry's emoji_name (profile.statusEmojiDisplayInfo);
//   - BaseCustomStatusEmoji (sMHw) and FormatTextWithEmoji (HOzI, the
//     status text's :codes:) draw each code through
//     getEmojiDisplayInfoFromMap (iz33 H): the code's entry, when there is
//     one, gives the name (display_alias, else the code's own name, with
//     its skin tone) and the picture (display_url); without an entry the
//     code stays as it is and the emoji component resolves it.
//
// So status_emoji ":speech_balloon:" with display info [ping-9+ (custom),
// speech_balloon (unicode 1f4ac)] draws 💬, not the first entry's picture.

// StatusEmojiDisplay is one status_emoji_display_info entry.
type StatusEmojiDisplay struct {
	Alias, URL, Unicode string
}

// parseStatusEmojiInfo is computeStatusEmojiDisplayInfo's map.
func parseStatusEmojiInfo(raw json.RawMessage) map[string]StatusEmojiDisplay {
	if len(raw) <= 2 {
		return nil
	}
	var di []struct {
		Name    string `json:"emoji_name"`
		Alias   string `json:"display_alias"`
		URL     string `json:"display_url"`
		Unicode string `json:"unicode"`
	}
	if json.Unmarshal(raw, &di) != nil || len(di) == 0 {
		return nil
	}
	m := make(map[string]StatusEmojiDisplay, len(di))
	for _, d := range di {
		m[d.Name] = StatusEmojiDisplay{Alias: d.Alias, URL: d.URL, Unicode: d.Unicode}
	}
	return m
}

// emojiToneRE is ue25's skin-tone suffix.
var emojiToneRE = regexp.MustCompile(`::[^:]+-([2-6]-[2-6])|::[^:]+-([2-6])$`)

// parseEmojiCode is ue25 A: a code (":thumbsup::skin-tone-3:") as its
// lowercased name and skin tone ("" for none); ok false when it isn't one
// emoji's code.
func parseEmojiCode(code string) (name, tone string, ok bool) {
	if code == "" {
		return "", "", false
	}
	n := strings.ToLower(code)
	n = strings.TrimPrefix(n, ":")
	n = strings.TrimSuffix(n, ":")
	m := emojiToneRE.FindStringSubmatchIndex(n)
	name = n
	if m != nil {
		name = n[:m[0]] + n[m[1]:]
		if m[2] >= 0 {
			tone = n[m[2]:m[3]]
		} else {
			tone = n[m[4]:m[5]]
		}
		for _, p := range strings.Split(tone, "-") {
			if v, err := strconv.Atoi(p); err != nil || v < 2 || v > 6 {
				return "", "", false
			}
		}
	}
	if name == "" || strings.Contains(name, ":") {
		return "", "", false
	}
	return name, tone, true
}

// EmojiDisplayInfo is getEmojiDisplayInfoFromMap: the code to draw and
// its picture ("" when the emoji component resolves the code itself).
func EmojiDisplayInfo(code string, m map[string]StatusEmojiDisplay) (string, string) {
	if len(m) == 0 {
		return code, ""
	}
	name, tone, ok := parseEmojiCode(code)
	if !ok {
		return code, ""
	}
	d, ok := m[name]
	if !ok {
		return code, ""
	}
	l := d.Alias
	if l == "" {
		l = name
	}
	if tone != "" {
		return ":" + l + "::skin-tone-" + tone + ":", d.URL
	}
	return ":" + l + ":", d.URL
}

// statusEmojiCodeRE is pH4l A, the emoji code FormatTextWithEmoji finds
// in a status text (one optional skin tone after it).
var statusEmojiCodeRE = regexp.MustCompile(":[a-zA-Z0-9" +
	"\u00e0\u00e1\u00e2\u00e4\u00e3\u00e8\u00e9\u00ea\u00eb\u00ed\u00ee\u00ef\u00f3\u00f6\u00f4\u00f5\u0153\u00fa\u00f9\u00fb\u00fc\u00ff\u00e7\u00df\u00f1" +
	"\u00c0\u00c1\u00c2\u00c4\u00c3\u00c8\u00c9\u00ca\u00cb\u00cd\u00ce\u00cf\u00d3\u00d6\u00d4\u00d5\u0152\u00da\u00d9\u00db\u00dc\u0178\u00c7\u00d1" +
	"\u1100-\u11ff\ua960-\ua97f\uac00-\ud7a3\ud7b0-\ud7ff\u3139-\u318f\u3041-\u3096\u30a0-\u30ff\uff61-\uff9f\u3400-\u4db5\u4e00-\u9fcb\uf900-\ufa6a\u2e80-\u2fd5\uff41-\uff5a\uff10-\uff19" +
	"\\-_+'\uff3f\uff0b\u30fc\u3005]+:(:skin-tone-[2-6](?:-[2-6])?:)?")

// StatusTextPart is one run of a status text: plain text, or an emoji
// code (Code set) with its picture when the display info has one.
type StatusTextPart struct {
	Text, Code, URL string
}

// StatusTextParts is FormatTextWithEmoji over a status text: the text
// between codes as it is, each code through EmojiDisplayInfo.
func StatusTextParts(s string, m map[string]StatusEmojiDisplay) []StatusTextPart {
	var out []StatusTextPart
	p := 0
	for _, loc := range statusEmojiCodeRE.FindAllStringIndex(s, -1) {
		if loc[0] > p {
			out = append(out, StatusTextPart{Text: s[p:loc[0]]})
		}
		code, u := EmojiDisplayInfo(s[loc[0]:loc[1]], m)
		out = append(out, StatusTextPart{Code: code, URL: u})
		p = loc[1]
	}
	if p < len(s) {
		out = append(out, StatusTextPart{Text: s[p:]})
	}
	return out
}
