package ui

// An unlabeled canvas tab's leading emoji is its icon (Karim, α.89: a tab
// read "[canvas icon] [emoji] What's on right now?"). Slack's rule is in
// tab_emoji_icon.go. Synthetic fakeslack workspace; nothing talks to Slack.

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestTabDocLabelRule(t *testing.T) {
	const custom, tada, tone = "https://emoji.example/rainbow-h.png", "https://std.example/1f389.png", "https://std.example/1f44d-1f3fb.png"
	emoji := func(name string) (string, string, bool) {
		switch name {
		case "rainbow-h":
			return custom, "", true
		case "tada":
			return tada, "\U0001F389", true
		case "+1::skin-tone-2":
			return tone, "", true
		}
		return "", "", false
	}
	text := func(s string) richtext.Span { return richtext.Span{Kind: richtext.Text, Text: s} }
	emo := func(name, u string) richtext.Span {
		return richtext.Span{Kind: richtext.Emoji, ID: name, Text: ":" + name + ":", URL: u}
	}
	canvas := func(file string, sp ...richtext.Span) core.Tab {
		return core.Tab{Type: "canvas", FileID: "F1", FileTitle: file, Title: file, TitleSpans: sp}
	}
	for _, c := range []struct {
		name          string
		tab           core.Tab
		listViewNames bool
		doc           bool
		icon, label   string
	}{
		// title_blocks: the leading emoji element, custom or standard
		{"custom emoji element", canvas(":rainbow-h: What's on right now?", emo("rainbow-h", custom), text(" What's on right now?")),
			false, true, custom, "What's on right now?"},
		{"unicode emoji element after blank text", canvas(":tada: Launch", text(" "), emo("tada", tada), text("  Launch")),
			false, true, tada, "Launch"},
		{"no emoji: the canvas icon", canvas("Plain notes", text("Plain notes")), false, true, "", "Plain notes"},
		{"emoji later in the title", canvas("Plan :tada:", text("Plan "), emo("tada", tada)), false, true, "", "Plan :tada:"},
		{"an emoji that doesn't resolve", canvas(":nope: Plan", text(":nope: Plan")), false, true, "", ":nope: Plan"},
		// title_blocks whose first text carries the emoji
		{"text :code: that exists", canvas(":tada: Plan", text(":tada: Plan")), false, true, tada, "Plan"},
		{"text unicode emoji: no icon, dropped", canvas("\U0001F389 Plan", text("\U0001F389 Plan")), false, true, "", "Plan"},
		// no title_blocks: the title string
		{"title :code: (all codes go)", core.Tab{Type: "canvas", FileID: "F1", FileTitle: ":tada: Plan :nope: x", Title: ":tada: Plan :nope: x"},
			false, true, tada, "Plan x"},
		{"title unknown :code:", core.Tab{Type: "canvas", FileID: "F1", FileTitle: ":nope: Plan", Title: ":nope: Plan"},
			false, true, "", "Plan"},
		{"title skin tone", core.Tab{Type: "canvas", FileID: "F1", FileTitle: ":+1::skin-tone-2: Yes", Title: ":+1::skin-tone-2: Yes"},
			false, true, tone, "Yes"},
		// a tab label of its own: the type icon, the label as it is
		{"labeled canvas", core.Tab{Type: "canvas", FileID: "F1", Label: ":tada: Notes", FileTitle: ":tada: Plan", Title: ":tada: Plan"},
			false, false, "", ":tada: Notes"},
		{"deleted canvas", core.Tab{Type: "canvas", FileID: "F1", Title: "Deleted file", Deleted: true}, false, true, "", "Deleted file"},
		// lists: a document label only with list_channel_tab_show_view_name_fe off
		{"list, view names on", core.Tab{Type: "list", FileID: "F2", FileTitle: ":tada: Groceries", Title: ":tada: Groceries"},
			true, false, "", ":tada: Groceries"},
		{"list, view names off", core.Tab{Type: "list", FileID: "F2", FileTitle: ":tada: Groceries", Title: ":tada: Groceries"},
			false, true, tada, "Groceries"},
		{"workflows tab", core.Tab{Type: "workflows", Title: "Workflows"}, false, false, "", "Workflows"},
	} {
		label := c.tab.Title
		if c.tab.Label != "" && !c.tab.Deleted {
			label = c.tab.Label
		}
		spans := tabSpans(c.tab, label, emoji)
		doc, icon, gotLabel, gotSpans := docTab(c.tab, label, spans, c.listViewNames, emoji)
		if doc != c.doc || icon != c.icon || gotLabel != c.label {
			t.Errorf("%s: doc %v icon %q label %q, want %v %q %q", c.name, doc, icon, gotLabel, c.doc, c.icon, c.label)
		}
		if gotSpans != nil && richtext.PlainSpans(gotSpans) != gotLabel {
			t.Errorf("%s: spans %q, label %q", c.name, richtext.PlainSpans(gotSpans), gotLabel)
		}
		if c.icon != "" {
			for _, s := range gotSpans {
				if s.Kind == richtext.Emoji && s.URL == c.icon && strings.HasPrefix(richtext.PlainSpans(gotSpans), s.Text) {
					t.Errorf("%s: the icon's emoji still leads the label: %+v", c.name, gotSpans)
				}
			}
		}
	}

	for _, c := range []struct {
		in   string
		want int
	}{
		{"\U0001F1FA\U0001F1F8 trip", 8}, // a flag: both regional indicators
		{"❤️ love", 3},                   // the dingbat alone; its variation selector stays
		{"1️⃣ one", 7},                   // a keycap
		{"→ next", 3},                    // an arrow
		{"\U0001F44D\U0001F3FD ok", 4},   // the thumb; its skin tone stays
		{"2024 plan", 0},
		{"plan", 0},
	} {
		if got := leadingUnicodeEmoji(c.in); got != c.want {
			t.Errorf("leadingUnicodeEmoji(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	for in, want := range map[string]string{
		":+1::skin-tone-2: hi":       ":+1::skin-tone-2:",
		":ok_hand::skin-tone-2-3: x": ":ok_hand::skin-tone-2-3:",
		":Café: x":                   ":Café:",
		":a b: x":                    "",
		"x :tada:":                   "",
		"::":                         "",
	} {
		if got := leadingEmojiCode(in); got != want {
			t.Errorf("leadingEmojiCode(%q) = %q, want %q", in, got, want)
		}
	}
	if got := stripEmojiCodes(":a: b :c::skin-tone-3: d 10:30 e"); got != " b  d 10:30 e" {
		t.Errorf("stripEmojiCodes: %q", got)
	}
}

// The tab bar: canvases titled with a custom and a Unicode leading emoji
// draw that emoji as the tab's icon and drop it from the label; one with
// no emoji keeps the canvas icon. A document label's icon box is 20px
// whatever it holds, so the custom-emoji tab is exactly as wide as a tab
// titled with the same words and no emoji. Light and dark shots
// (tab-emoji-icon-*.png) with PREVIEW_SHOTS_DIR.
func TestTabEmojiIcon(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	e := startEnv(t, fs, t.TempDir())
	if os.Getenv("PREVIEW_SHOTS_DIR") != "" {
		w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
		if err != nil {
			t.Fatalf("headless window: %v", err)
		}
		e.gpu = w
	}
	e.waitReady()
	name, _ := fs.FixtureEmoji()
	if name == "" {
		t.Fatal("fakeslack has no custom emoji")
	}
	e.until("the custom emoji", 15*time.Second, func() bool {
		u, _, ok := e.a.Core().Emoji(name)
		return ok && u != ""
	})
	customURL, _, _ := e.a.Core().Emoji(name)
	tadaURL, _, _ := e.a.Core().Emoji("tada")
	const ch = "C0FAKE00001"
	const fCustom, fUni, fPlain, fSame = "F0TABEMO001", "F0TABEMO002", "F0TABEMO003", "F0TABEMO004"
	section := func(els ...any) []any {
		return []any{map[string]any{"type": "rich_text", "block_id": "t", "elements": []any{
			map[string]any{"type": "rich_text_section", "elements": els}}}}
	}
	text := func(s string) any { return map[string]any{"type": "text", "text": s} }
	fs.AddCanvas(fakeslack.FakeCanvas{ID: fCustom, Channel: ch, Title: ":" + name + ": What's on right now?",
		TitleBlocks: section(map[string]any{"type": "emoji", "name": name}, text(" What's on right now?"))})
	fs.AddCanvas(fakeslack.FakeCanvas{ID: fUni, Channel: ch, Title: ":tada: Launch party",
		TitleBlocks: section(map[string]any{"type": "emoji", "name": "tada", "unicode": "1f389"}, text(" Launch party"))})
	fs.AddCanvas(fakeslack.FakeCanvas{ID: fPlain, Channel: ch, Title: "Plain notes", TitleBlocks: section(text("Plain notes"))})
	fs.AddCanvas(fakeslack.FakeCanvas{ID: fSame, Channel: ch, Title: "What's on right now?", TitleBlocks: section(text("What's on right now?"))})
	e.openChannel(ch)
	byFile := map[string]chTab{}
	e.until("the canvas tabs", 15*time.Second, func() bool {
		if !e.a.data.tabsDone[ch] {
			return false
		}
		for _, tb := range e.a.channelTabs(ch) {
			if tb.fileID != "" {
				byFile[tb.fileID] = tb
			}
		}
		return len(byFile) >= 4
	})
	for _, c := range []struct{ file, icon, label string }{
		{fCustom, customURL, "What's on right now?"},
		{fUni, tadaURL, "Launch party"},
		{fPlain, "", "Plain notes"},
		{fSame, "", "What's on right now?"},
	} {
		tb, ok := byFile[c.file]
		if !ok {
			t.Errorf("no tab for %s", c.file)
			continue
		}
		if !tb.doc || tb.emojiIcon != c.icon || tb.label != c.label || tb.icon != "canvas" {
			t.Errorf("%s: doc %v emojiIcon %q label %q icon %q, want a document label, %q, %q", c.file, tb.doc, tb.emojiIcon, tb.label, tb.icon, c.icon, c.label)
		}
		if tb.spans != nil && strings.HasPrefix(richtext.PlainSpans(tb.spans), " ") {
			t.Errorf("%s: the label keeps the space after the emoji: %q", c.file, richtext.PlainSpans(tb.spans))
		}
	}
	e.settle(500 * time.Millisecond) // the emoji images
	widths := func() map[string]int {
		out := map[string]int{}
		for _, r := range e.a.tabDrag().rects {
			if r.fileID != "" {
				out[r.fileID] = r.r.Dx()
			}
		}
		return out
	}
	w1 := widths()
	e.frame()
	w2 := widths()
	if w1[fCustom] == 0 || w1[fCustom] != w2[fCustom] || w1[fUni] != w2[fUni] {
		t.Errorf("tab widths moved between frames: %v then %v", w1, w2)
	}
	if w1[fCustom] != w1[fSame] {
		t.Errorf("the custom-emoji tab is %dpx, the same title without the emoji %dpx", w1[fCustom], w1[fSame])
	}
	pfShot(e, "tab-emoji-icon")
}
