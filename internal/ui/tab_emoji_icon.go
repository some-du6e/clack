package ui

// A file tab's document label: an unlabeled canvas tab's leading emoji is
// its icon (Karim, α.89: a tab read "[canvas icon] [emoji] What's on right
// now?"). Build c07ab895506f:
//
//   - StyledChannelTabLabel (lazy chunk tabbed-channels_thunks_handle-tab-
//     change…fe5881, module GPMo) draws a tab with a file and no label of
//     its own as channel_tab_document_label: SmallFileListEntity (boot-
//     shared 6bLV → 7JHD SmallFileEntity) with useLeadingEmojiAsIcon. A
//     list tab goes that way only with list_channel_tab_show_view_name_fe
//     off; with it on (the account) useTabLabel's label is never empty, so
//     a list keeps its type icon and its title as it is. A labeled tab
//     keeps its type icon (useTabIconProps) and draws the label with its
//     emoji (FormatTextWithEmoji).
//   - The icon (SmallFileEntity): for a canvas, the title_blocks' leading
//     emoji element (yG5G getLeadingTitleEmoji: the first element of the
//     first section that isn't blank text), else a :code: at the start of
//     the file's title string (SDZ5 parseLeadingEmoji, p7O4's pattern). It
//     shows when the element has an image or the emoji exists
//     (canRenderLeadingTitleEmoji, doesEmojiExist); otherwise the canvas
//     icon. A list reads only the title's leading :code:.
//   - The label (c9YI Name, GF/w QuipTitleParsedId) drops what the icon
//     shows: title_blocks lose the elements up to the emoji; without one,
//     the first text element loses a leading :code: that exists or a
//     leading Unicode emoji (GF/w's pattern E), then is trimmed. A title
//     string without title_blocks that starts with a :code: loses every
//     :code: (c9YI replaces p7O4's global pattern, no existence check); a
//     list title loses its leading one.
//   - Geometry (client-boot-styles): .c-file_entity__icon is a 20px box
//     with margin-right 4px in the tab (.channel_tab_document_label__xSKTO
//     .c-file_entity__icon), the svg or the 16px emoji (c-emoji__small in
//     .c-file_entity_emoji_container, 16px tall) at its left edge, centred
//     on the 20px line; the title's own emoji are 16px wide. The label
//     path is the 16px icon and a 4px gap (.channel_tab_label__WjErI).
//     Under white-space: normal the space left after the emoji collapses.
//
// Elsewhere Slack uses the leading emoji as the icon in a canvas thread's
// flexpane header (NCqL), Activity's destination tag (PcpC) and the browse
// sidebar's file items (Fl3U); never in the canvas header or the channel
// sidebar.

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// docTab is a tab's document label: whether it draws as one, the leading
// emoji's image for its icon ("" the type icon) and its label and spans
// with that emoji dropped. label and spans are what the tab draws
// otherwise (backend tabs, tabSpans).
func docTab(t core.Tab, label string, spans []richtext.Span, listViewNames bool,
	emoji func(name string) (url, text string, ok bool)) (doc bool, icon, outLabel string, outSpans []richtext.Span) {
	doc = t.FileID != "" && t.Label == "" && (t.Type == "canvas" || t.Type == "list" && !listViewNames)
	if !doc || t.Deleted { // a deleted file's icon is the trash can
		return doc, "", label, spans
	}
	exists := func(code string) string {
		if emoji == nil {
			return ""
		}
		u, _, ok := emoji(strings.Trim(code, ":"))
		if !ok {
			return ""
		}
		return u
	}
	titleIcon := func() string {
		if code := leadingEmojiCode(strings.TrimSpace(t.FileTitle)); code != "" {
			return exists(code)
		}
		return ""
	}
	if t.Type == "list" { // SmallFileEntity's isList branch, c9YI's list title
		e := strings.TrimSpace(label)
		if code := leadingEmojiCode(e); code != "" {
			label = strings.TrimSpace(e[len(code):])
			spans = tabLabelSpans(label, emoji)
		}
		return true, titleIcon(), label, spans
	}
	if len(t.TitleSpans) > 0 {
		sp := append([]richtext.Span(nil), resolveEmoji(t.TitleSpans, emoji)...)
		stripped := false
		if k := leadingTitleEmoji(sp); k >= 0 {
			icon, stripped = sp[k].URL, true
			sp = trimLeadingSpace(sp[k+1:])
		} else {
			icon = titleIcon()
			if sp[0].Kind == richtext.Text {
				e := strings.TrimSpace(sp[0].Text)
				if code := leadingEmojiCode(e); code != "" && exists(code) != "" {
					sp[0].Text, stripped = strings.TrimSpace(e[len(code):]), true
				} else if n := leadingUnicodeEmoji(e); n > 0 {
					sp[0].Text, stripped = strings.TrimSpace(e[n:]), true
				}
			}
		}
		if stripped {
			spans = tabTitleSpansEmoji(sp, emoji)
			label = richtext.PlainSpans(spans)
		}
		return true, icon, label, spans
	}
	icon = titleIcon()
	if e := strings.TrimSpace(label); leadingEmojiCode(e) != "" {
		label = strings.Join(strings.Fields(stripEmojiCodes(e)), " ")
		spans = tabLabelSpans(label, emoji)
	}
	return true, icon, label, spans
}

// leadingTitleEmoji is getLeadingTitleEmoji (yG5G) over a title's spans:
// the index of the leading emoji (the first span that isn't blank text,
// within the first line) when it has an image, else -1.
func leadingTitleEmoji(sp []richtext.Span) int {
	for i, s := range sp {
		switch {
		case s.Kind == richtext.Text && strings.TrimSpace(s.Text) == "":
		case s.Kind == richtext.Emoji && s.URL != "":
			return i
		default:
			return -1
		}
	}
	return -1
}

// trimLeadingSpace drops the whitespace a line starts with (white-space:
// normal): blank text spans, then the next one's leading spaces.
func trimLeadingSpace(sp []richtext.Span) []richtext.Span {
	for len(sp) > 0 && sp[0].Kind == richtext.Text {
		if t := strings.TrimLeftFunc(sp[0].Text, unicode.IsSpace); t != "" {
			sp[0].Text = t
			break
		}
		sp = sp[1:]
	}
	return sp
}

// leadingEmojiCode is parseLeadingEmoji (SDZ5): the :code: (with its
// :skin-tone-N: suffix) that s starts with, "" when it doesn't.
func leadingEmojiCode(s string) string {
	return emojiCodeAt(s, 0)
}

// emojiCodeAt matches p7O4's pattern at s[i]: ":", one or more name
// characters, ":", optionally ":skin-tone-N:" or ":skin-tone-N-M:".
func emojiCodeAt(s string, i int) string {
	if i >= len(s) || s[i] != ':' {
		return ""
	}
	j := i + 1
	for j < len(s) {
		r, n := utf8.DecodeRuneInString(s[j:])
		if !isEmojiCodeRune(r) {
			break
		}
		j += n
	}
	if j == i+1 || j >= len(s) || s[j] != ':' {
		return ""
	}
	j++
	if t := s[j:]; strings.HasPrefix(t, ":skin-tone-") && len(t) >= 13 && t[11] >= '2' && t[11] <= '6' {
		switch {
		case t[12] == ':':
			j += 13
		case len(t) >= 15 && t[12] == '-' && t[13] >= '2' && t[13] <= '6' && t[14] == ':':
			j += 15
		}
	}
	return s[i:j]
}

// stripEmojiCodes removes every match of p7O4's global pattern from s.
func stripEmojiCodes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if c := emojiCodeAt(s, i); c != "" {
			i += len(c)
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// isEmojiCodeRune is p7O4's name character class.
func isEmojiCodeRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case strings.ContainsRune("àáâäãèéêëíîïóöôõœúùûüÿçßñÀÁÂÄÃÈÉÊËÍÎÏÓÖÔÕŒÚÙÛÜŸÇÑ-_+'＿＋ー々", r):
		return true
	}
	for _, rg := range [...][2]rune{{0x1100, 0x11FF}, {0xA960, 0xA97F}, {0xAC00, 0xD7A3}, {0xD7B0, 0xD7FF},
		{0x3139, 0x318F}, {0x3041, 0x3096}, {0x30A0, 0x30FF}, {0xFF61, 0xFF9F}, {0x3400, 0x4DB5},
		{0x4E00, 0x9FCB}, {0xF900, 0xFA6A}, {0x2E80, 0x2FD5}, {0xFF41, 0xFF5A}, {0xFF10, 0xFF19}} {
		if r >= rg[0] && r <= rg[1] {
			return true
		}
	}
	return false
}

// leadingUnicodeEmoji is the length of GF/w's pattern E at the start of s
// (0: no match): one dingbat or symbol, a regional-indicator pair, any
// character past the BMP, or a keycap. Only that much goes; a variation
// selector or skin tone after it stays, as in Slack.
func leadingUnicodeEmoji(s string) int {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 || r == utf8.RuneError {
		return 0
	}
	isRI := func(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }
	switch {
	case r >= 0x2700 && r <= 0x27BF:
		return n
	case isRI(r):
		if r2, n2 := utf8.DecodeRuneInString(s[n:]); isRI(r2) {
			return n + n2
		}
		return n
	case r >= 0x10000:
		return n
	case r >= 0x23 && r <= 0x39:
		m := n
		if r2, n2 := utf8.DecodeRuneInString(s[m:]); r2 == 0xFE0F {
			m += n2
		}
		if r2, n2 := utf8.DecodeRuneInString(s[m:]); r2 == 0x20E3 {
			return m + n2
		}
		return 0
	}
	switch r {
	case 0x3299, 0x3297, 0x303D, 0x3030, 0x24C2, 0x203C, 0x2049, 0x25B6, 0x25C0, 0xA9, 0xAE, 0x2122, 0x2139,
		0x2B05, 0x2B06, 0x2B07, 0x2B1B, 0x2B1C, 0x2B50, 0x2B55, 0x231A, 0x231B, 0x2328, 0x23CF, 0x2934, 0x2935:
		return n
	}
	for _, rg := range [...][2]rune{{0x25AA, 0x25AB}, {0x25FB, 0x25FE}, {0x2600, 0x26FF}, {0x23E9, 0x23F3},
		{0x23F8, 0x23FA}, {0x2190, 0x21FF}} {
		if r >= rg[0] && r <= rg[1] {
			return n
		}
	}
	return 0
}
