package richview

import (
	"image/color"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gioui.org/op"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/slackc"
)

// slackMessageMarkup is the element Slack's message renderers draw for
// each part of a message body, read from the public CDN (build
// 32bb3cc8205b, no cookie), and from the live DOM capture where the code
// branches on an experiment (docs/PLUGINS-props-live.md). Static
// attributes only: data-member-id, data-stringify-text and the like carry
// the element's own values.
var slackMessageMarkup = []struct {
	part    string
	def     *slackc.ClassDef
	tag     string
	classes string
	attrs   []string
}{
	// 2xBI RichTextSection
	{"section", richSectionClass, "div", "p-rich_text_section", nil},
	{"block", richBlockClass, "div", "p-rich_text_block", nil},
	// 2xBI RichTextList: classnames("p-rich_text_list", {…}, "p-rich_text_list--nested")
	{"bullet list", listBulletClass, "ul", "p-rich_text_list p-rich_text_list__bullet p-rich_text_list--nested", []string{"data-stringify-type", "unordered-list", "data-list-tree", "true"}},
	{"ordered list", listOrderedClass, "ol", "p-rich_text_list p-rich_text_list__ordered p-rich_text_list--nested", []string{"data-stringify-type", "ordered-list", "data-list-tree", "true"}},
	// 8jHQ: blockquote type cite
	{"quote", quoteClass, "blockquote", "c-mrkdwn__quote", []string{"type", "cite", "data-stringify-type", "quote"}},
	// 2xBI RichTextPreformatted
	{"code block", preClass, "pre", "c-mrkdwn__pre", []string{"data-copy-code-block", "true", "data-stringify-type", "pre"}},
	// UqHX WithFormattingStyles
	{"bold", boldClass, "b", "", []string{"data-stringify-type", "bold"}},
	{"italic", italicClass, "i", "", []string{"data-stringify-type", "italic"}},
	{"strike", strikeClass, "s", "", []string{"data-stringify-type", "strike"}},
	{"underline", underlineClass, "u", "", []string{"data-stringify-type", "underline"}},
	{"inline code", codeClass, "code", "c-mrkdwn__code", []string{"data-stringify-type", "code"}},
	{"highlight word", highlightClass, "span", "c-mrkdwn__highlight", nil},
	{"find in thread", findHighlightClass, "span", "c-mrkdwn__highlight--find-in-thread", nil},
	// kQRT DraggableMrkdwnLink around XOOl's a.c-link
	{"link wrapper", draggableLinkClass, "span", "c-mrkdwn__draggable-link", nil},
	{"link", linkClass, "a", "c-link", []string{"data-stringify-link", "true", "target", "_blank", "rel", "noopener noreferrer"}},
	// ld4x MemberSlug: c-member_slug, c-member_slug--{clientTheme}, --link, --mention
	{"user", memberClass, "a", "c-link c-member_slug c-member_slug--light c-member_slug--link", []string{"data-stringify-type", "mention"}},
	{"user (you)", memberSelfClass, "a", "c-link c-member_slug c-member_slug--light c-member_slug--link c-member_slug--mention", []string{"data-stringify-type", "mention"}},
	{"user, dark", memberDarkClass, "a", "c-link c-member_slug c-member_slug--dark c-member_slug--link", []string{"data-stringify-type", "mention"}},
	{"user (you), dark", memberDSelfClass, "a", "c-link c-member_slug c-member_slug--dark c-member_slug--link c-member_slug--mention", []string{"data-stringify-type", "mention"}},
	// lH2I BaseMrkdwnChannel (+ InlineChannelEntity's c-inline_channel_entity, which plugins select)
	{"channel", channelClass, "a", "c-link c-mrkdwn__channel c-member_slug internal_channel_link c-member_slug--link c-inline_channel_entity", []string{"data-qa", "channel_link", "data-stringify-type", "channel"}},
	{"channel, not visible", channelOffClass, "span", "c-mrkdwn__channel c-member_slug", nil},
	// fg3x UserGroup
	{"user group", usergroupClass, "a", "c-link c-mrkdwn__user_group c-mrkdwn__user_group--link", []string{"data-stringify-type", "mention"}},
	{"user group (yours)", usergroupSelfClass, "a", "c-link c-mrkdwn__user_group c-mrkdwn__user_group--link c-mrkdwn__user_group--mention", []string{"data-stringify-type", "mention"}},
	// Potv MrkdwnBroadcastCommand
	{"broadcast", broadcastClass, "span", "c-mrkdwn__broadcast c-mrkdwn__broadcast--link c-mrkdwn__broadcast--mention", nil},
	// cnmt DateCommand
	{"date", dateClass, "span", "c-mrkdwn__date", []string{"data-qa", "mrkdwn_date"}},
	{"date with a url", dateLinkClass, "a", "c-link", []string{"data-qa", "mrkdwn_date"}},
	// vbPY Emoji (inline)
	{"emoji", emojiClass, "span", "c-emoji c-emoji__medium c-emoji--inline", []string{"data-qa", "emoji"}},
	// 2xBI EditedLabel / Op5x
	{"edited", editedClass, "span", "c-message__edited_label", nil},
}

func classSet(s string) []string {
	f := strings.Fields(s)
	sort.Strings(f)
	return f
}

// Every element of a message body Clack tags for plugin CSS has Slack's
// tag, classes and static attributes.
func TestMessageClassesLikeSlack(t *testing.T) {
	for _, m := range slackMessageMarkup {
		if got := m.def.TagName(); got != m.tag {
			t.Errorf("%s: <%s>, Slack's is <%s>", m.part, got, m.tag)
		}
		if got, want := classSet(m.def.ClassAttr()), classSet(m.classes); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: classes %q, Slack's %q", m.part, m.def.ClassAttr(), m.classes)
		}
		if got := m.def.Attrs(); !reflect.DeepEqual(got, m.attrs) && !(len(got) == 0 && len(m.attrs) == 0) {
			t.Errorf("%s: attributes %q, Slack's %q", m.part, got, m.attrs)
		}
	}
	// the names Clack used before that Slack never had
	for _, m := range slackMessageMarkup {
		for _, c := range strings.Fields(m.def.ClassAttr()) {
			if c == "c-mrkdwn__subteam" || c == "c-mrkdwn__subteam--link" || c == "c-search__highlight" {
				t.Errorf("%s: %s isn't Slack's (no build has it)", m.part, c)
			}
		}
	}
}

// Selectors written against Slack's DOM reach the parts: the formatting
// chain (code > highlight > b > u > i > s > the element, WithFormattingStyles
// UqHX), the link wrapper, and the mention, channel, group and date
// elements by their classes and attributes.
func TestMessageSelectorsLikeSlack(t *testing.T) {
	slackc.SetStyle("test:slackdom", `
		.x-row-dom b > i > s > .c-mrkdwn__draggable-link > a.c-link[data-stringify-link] { color: #0000ff !important }
		.x-row-dom code.c-mrkdwn__code > b > u { color: #ff0000 !important }
		.x-row-dom span.c-mrkdwn__highlight > b { color: #ff8800 !important }
		.x-row-dom a.c-link.c-mrkdwn__user_group.c-mrkdwn__user_group--mention[data-stringify-type="mention"] { color: #00ff00 !important }
		.x-row-dom a.c-mrkdwn__channel.c-member_slug.internal_channel_link[data-stringify-type="channel"] { color: #ff00ff !important }
		.x-row-dom span.c-mrkdwn__date[data-qa="mrkdwn_date"] { color: #00ffff !important }
		.x-row-dom .c-mrkdwn__broadcast.c-mrkdwn__broadcast--mention { color: #888800 !important }`)
	defer slackc.RemoveStyle("test:slackdom")
	d := doc(para(
		rt.Span{Kind: rt.Link, Text: "chainword", URL: "https://example.invalid/x", Style: rt.Bold | rt.Italic | rt.Strike}, txt(" "),
		rt.Span{Kind: rt.Text, Text: "codebold", Style: rt.Code | rt.Bold | rt.Underline}, txt(" "),
		rt.Span{Kind: rt.Text, Text: "hlbold", Style: rt.Highlight | rt.Bold}, txt(" "),
		rt.Span{Kind: rt.Usergroup, Text: "@mygroup", ID: "S0FAKE1", Self: true}, txt(" "),
		rt.Span{Kind: rt.Channel, Text: "#chanword", ID: "C0FAKE1"}, txt(" "),
		rt.Span{Kind: rt.Date, Text: "dateword"}, txt(" "),
		rt.Span{Kind: rt.Broadcast, Text: "@here", ID: "here", Self: true}))
	st := newTestStyle()
	v := New(d, Options{})
	ops := new(op.Ops)
	frame := func() {
		ops.Reset()
		gtx := testGtx(ops, 900)
		row := rowDef.EnterClass(gtx, 0, 0, "x-row-dom")
		v.Layout(gtx, st, nil)
		row.Leave()
	}
	frame()
	frame()
	runs := map[string]Run{}
	v.Runs(func(r Run) { runs[r.Text] = r })
	rgb := func(c uint32) color.NRGBA {
		return color.NRGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}
	}
	for w, want := range map[string]color.NRGBA{
		"chainword": rgb(0x0000ff), "codebold": rgb(0xff0000), "hlbold": rgb(0xff8800),
		"@mygroup": rgb(0x00ff00), "#chanword": rgb(0xff00ff), "dateword": rgb(0x00ffff), "@here": rgb(0x888800),
	} {
		r, ok := runs[w]
		if !ok {
			t.Errorf("%q: not drawn (runs %v)", w, keys(runs))
			continue
		}
		if r.Color != want {
			t.Errorf("%q: %v, want %v (the selector written for Slack's DOM didn't match)", w, r.Color, want)
		}
	}
}

func keys(m map[string]Run) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
