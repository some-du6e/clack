package richtext

import "testing"

// rich_text_header and rich_text_divider (Slack's RichTextRenderer J825:
// RichTextHeader by level, nothing without elements; RichTextDivider).
// Synthetic fixtures.
func TestRichTextHeaderDivider(t *testing.T) {
	sec := func(s string) string {
		return `{"type":"rich_text_section","elements":[{"type":"text","text":"` + s + `"}]}`
	}
	hdr := func(level, s string) string {
		lv := ""
		if level != "" {
			lv = `"level":` + level + `,`
		}
		return `{"type":"rich_text_header",` + lv + `"elements":[{"type":"text","text":"` + s + `","style":{"bold":true}}]}`
	}
	d := blocksMsg(t, `[{"type":"rich_text","elements":[`+hdr("1", "Title")+`,`+sec("intro")+`,{"type":"rich_text_divider"},`+
		hdr("3", "Small")+`,`+hdr("", "Default")+`,`+hdr("9", "Odd")+`,`+hdr("4", "Four")+`,{"type":"rich_text_header","level":1,"elements":[]},`+sec("end")+`]}]`)
	eq(t, "header and divider", richOf(*d), []string{"header 1 first: Title", "Paragraph", "divider", "header 3: Small",
		"header 2: Default", "header 2: Odd", "header 4: Four", "Paragraph"})
	eq(t, "plain", PlainText(*d), "Title\nintro\nSmall\nDefault\nOdd\nFour\nend")
}
