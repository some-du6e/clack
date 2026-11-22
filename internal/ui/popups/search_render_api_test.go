package popups

import (
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
)

// What the search rendering tests (search_card_test.go) read of a card,
// a row and the title. The red evidence run swaps this file for the old
// code's equivalents, so the same tests run on both.

func scClipped(c *srCard) bool        { return c.laid.bodyClip }
func scBodyH(c *srCard) int           { return c.laid.bodyH }
func scText(c *srCard) string         { return c.richPlain() }
func scHasExtra(c *srCard) bool       { return c.rich.extra != nil }
func scHasRich(c *srCard) bool        { return c.rich.body != nil }
func scSegMention(sg srSeg) bool      { return sg.Mention != srMentionNone }
func scTitle(h Host, q string) string { return srTitleQuery(h, q) }

func scBodyDoc(c *srCard) richtext.Doc {
	if c.rich.body == nil {
		return richtext.Doc{}
	}
	return c.rich.body.Doc()
}

func scAttMatched(m *core.Message, key string) bool {
	return m.SearchMatches != nil && m.SearchMatches.Attachments[key]
}

// scBuild builds card i's views, as its first frame would (a card below
// the fold is never laid out by the list).
func scBuild(v *SearchView, i int) { v.srRichOf(i) }

// scRowSegs are a quick message row's segments.
func scRowSegs(h Host, m *core.Message) []srSeg { return srRunSegs(srBodyRuns(h, m), nil, true) }

// scCardHovered is card i's hover (its background and hover bar) as
// cardNative worked it out last frame.
func scCardHovered(v *SearchView, i int) bool { return v.cards[i].hov }

// scPreviewSegs are the dialogs' message preview's segments
// (actMsgPreview).
func scPreviewSegs(h Host, m *core.Message) []srSeg { return actPreviewSegs(h, m) }
