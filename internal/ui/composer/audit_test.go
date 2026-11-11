package composer

import (
	"context"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// The composer audit's fixes (docs/COMPOSER-AUDIT.md), each against the
// Slack code it ports. Synthetic data only.

// freshComposer is New for #general without a draft left by another
// test (Close keeps the composer's text as the in-memory draft).
func freshComposer(t *testing.T, h Host) *Composer {
	t.Helper()
	clearDraft(tChGen, "")
	t.Cleanup(func() { clearDraft(tChGen, "") })
	c := New(h, tChGen, "")
	t.Cleanup(c.Close)
	return c
}

// getMessageLengthForDelta (9ysi): text in UTF-16 units, an emoji its
// ":name:" text, a mention its id, a broadcast its label, a link whose
// text isn't its URL both; the final newline doesn't count.
func TestSlackLength(t *testing.T) {
	d := NewDoc()
	end := d.Insert(0, "hi ", Attr{})
	end = d.InsertAtom(end, &Atom{Kind: AtomUser, ID: "U012ABCDEF", Text: "@alice"})
	end = d.InsertAtom(end, &Atom{Kind: AtomEmoji, ID: "tada"})
	end = d.InsertAtom(end, &Atom{Kind: AtomBroadcast, ID: "here", Text: "@here"})
	end = d.Insert(end, "😀\n", Attr{})
	a := d.Insert(end, "docs", Attr{Link: "https://x.io"})
	d.Insert(a, "https://y.io", Attr{Link: "https://y.io"})
	want := len("hi ") + len("U012ABCDEF") + len(":tada:") + len("@here") + 2 + 1 + len("docs") + len("https://x.io") + len("https://y.io")
	if got := slackLength(d); got != want {
		t.Fatalf("slackLength = %d, want %d", got, want)
	}
	if lengthWarningText(1234) != "-1,234" || lengthWarningTip(1) != "Your message is 1 character too long" ||
		lengthWarningTip(12345) != "Your message is 12,345 characters too long" {
		t.Fatalf("warning %q / %q", lengthWarningText(1234), lengthWarningTip(12345))
	}
}

// Over 12,000 characters (maxLength 12e3): the count shows, the send
// button is disabled and Enter sends nothing (isOverMaxLength).
func TestOverLimitBlocksSend(t *testing.T) {
	app, fs := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	h := &slickHost{app: app, th: theme.New(false)}
	c := freshComposer(t, h)
	d := NewDoc()
	d.Insert(0, strings.Repeat("a", composerMaxLength+1), Attr{})
	c.ed.SetDoc(d)
	if got := c.overLimit(); got != 1 {
		t.Fatalf("overLimit = %d, want 1", got)
	}
	st := chromeState{CanSend: true, OverLimit: c.overLimit()}
	if !st.sendOff() {
		t.Fatal("send stays enabled over the limit")
	}
	c.submit()
	time.Sleep(150 * time.Millisecond)
	if n := len(fs.called("chat.postMessage")); n != 0 {
		t.Fatalf("%d chat.postMessage calls over the limit", n)
	}
	if c.ed.Doc().Len() != composerMaxLength+1 {
		t.Fatal("the composer was cleared over the limit")
	}
	// back at the limit it sends
	c.ed.SetDoc(func() *Doc { d := NewDoc(); d.Insert(0, strings.Repeat("a", composerMaxLength), Attr{}); return d }())
	if c.overLimit() != 0 {
		t.Fatalf("overLimit at the limit = %d", c.overLimit())
	}
	// below half the limit Slack doesn't count at all
	if charsOverLimit(func() *Doc { d := NewDoc(); d.Insert(0, "short", Attr{}); return d }()) != 0 {
		t.Fatal("short message counted over")
	}
}

// MessagePaneInput's isStoriesButtonEnabled / isAudioButtonEnabled need
// window.MediaRecorder: without a recorder the clip buttons aren't there.
func TestClipButtonsNeedARecorder(t *testing.T) {
	app, _ := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	c := freshComposer(t, &slickHost{app: app, th: theme.New(false)})
	p := c.inputProps()
	if p.Bool("enableStoryButton") || p.Bool("enableAudioButton") {
		t.Fatalf("clip buttons enabled: story %v audio %v", p.Bool("enableStoryButton"), p.Bool("enableAudioButton"))
	}
	if !p.Bool("enableEmojiButton") || !p.Bool("enableSlashCommandsButton") {
		t.Fatal("other buttons turned off")
	}
	gtx := gtxForProps()
	slackc.Frame(gtx)
	c.Layout(gtx)
	if c.cs.Hide&crNoRecorder != crNoRecorder {
		t.Fatalf("Hide %b lacks the clip buttons", c.cs.Hide)
	}
}

// SlashCommandsButton isDisabled: !isTextyInputEmpty; the @ button's
// maybeTriggerMentionAutocomplete spacing.
func TestShortcutAndMentionButtons(t *testing.T) {
	app, _ := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	c := freshComposer(t, &slickHost{app: app, th: theme.New(false)})
	var ch chrome
	st := c.chromeState(gtxForProps())
	if !st.InputEmpty || ch.disabled(&st, crShortcut) {
		t.Fatal("Run shortcut disabled in an empty composer")
	}
	edType(c.ed, "x")
	st = c.chromeState(gtxForProps())
	if st.InputEmpty || !ch.disabled(&st, crShortcut) {
		t.Fatal("Run shortcut enabled with text in the composer")
	}
	cases := []struct{ in, want string }{
		{"", "@"},
		{"hi", "hi @"},
		{"hi ", "hi @"},
		{"@", "@"},
		{"a\n", "a\n @"},
	}
	for _, tc := range cases {
		d := NewDoc()
		d.Insert(0, tc.in, Attr{})
		c.ed.SetDoc(d)
		c.ed.SetSelection(d.Len(), d.Len())
		c.mentionTrigger()
		if got := c.ed.Doc().Text(); got != tc.want {
			t.Errorf("@ after %q = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// cjbg maybeSave: saving an emptied edit opens the delete dialog instead
// of editing or toasting.
func TestEmptyEditAsksToDelete(t *testing.T) {
	app, fs := syntheticApp(t, func(*store.Store) {})
	stubTyping(t)
	h := &keysHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	m := core.Message{Channel: tChGen, TS: "1700000000.000200", User: tSelf, Text: "typo"}
	c := NewEdit(h, m, func() {})
	t.Cleanup(c.Close)
	c.ed.SetDoc(NewDoc())
	c.submit()
	if len(h.shown) != 1 {
		t.Fatalf("%d overlays, want the delete dialog", len(h.shown))
	}
	if c.Done() {
		t.Fatal("the editor closed")
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(fs.called("chat.update")); n != 0 {
		t.Fatalf("%d chat.update calls for an empty edit", n)
	}
}

// SlackMarkdown and the keyboard module's live formatting.
func TestMarkdownShortcutsLikeSlack(t *testing.T) {
	typed := func(s string) *Editor {
		e := NewEditor()
		edType(e, s)
		return e
	}
	// ">" at a line start quotes at once
	if e := typed(">"); e.doc.Lines[0].Kind != Quote || e.doc.Len() != 0 {
		t.Fatalf("> : %q %+v", e.doc.Text(), e.doc.Lines)
	}
	if e := typed("a\n>"); e.doc.Lines[1].Kind != Quote || e.doc.Text() != "a\n" {
		t.Fatalf("second line >: %q %+v", e.doc.Text(), e.doc.Lines)
	}
	if e := typed("x>"); e.doc.Lines[0].Kind != Para || e.doc.Text() != "x>" {
		t.Fatalf("mid-line >: %q", e.doc.Text())
	}
	// list autofill: 1. 1) - * • after any spaces; not 2.
	for in, want := range map[string]BlockKind{"1. ": Ordered, "1) ": Ordered, "- ": Bullet, "* ": Bullet, "• ": Bullet, "  - ": Bullet} {
		if e := typed(in); e.doc.Lines[0].Kind != want || e.doc.Len() != 0 {
			t.Errorf("%q: kind %v text %q", in, e.doc.Lines[0].Kind, e.doc.Text())
		}
	}
	for _, in := range []string{"2. ", "12. ", "a. ", "-x "} {
		if e := typed(in); e.doc.Lines[0].Kind != Para || e.doc.Text() != in {
			t.Errorf("%q became %v %q", in, e.doc.Lines[0].Kind, e.doc.Text())
		}
	}
	// headings (convertHeaders, maxHeaderLevel 3) and the divider
	for in, want := range map[string]BlockKind{"# ": Header1, "## ": Header2, "### ": Header3} {
		if e := typed(in + "Title"); e.doc.Lines[0].Kind != want || e.doc.Text() != "Title" {
			t.Errorf("%q: kind %v text %q", in, e.doc.Lines[0].Kind, e.doc.Text())
		}
	}
	if e := typed("#### "); e.doc.Lines[0].Kind != Para {
		t.Error("#### became a heading")
	}
	e := typed("--- after")
	if len(e.doc.Lines) != 2 || e.doc.Lines[0].Kind != Divider || e.doc.Lines[1].Kind != Para || e.doc.Text() != "\nafter" {
		t.Fatalf("---: %q %+v", e.doc.Text(), e.doc.Lines)
	}
	// maybeLinkify on space and Enter
	e = typed("see https://example.com/a. ok")
	if _, _, url := e.doc.LinkAt(6); url != "https://example.com/a" {
		t.Fatalf("no link after space: %q", url)
	}
	e = typed("www.example.com")
	edPress(e, key.NameReturn, key.ModShift)
	if _, _, url := e.doc.LinkAt(2); url != "http://www.example.com" {
		t.Fatalf("no link after Enter: %q", url)
	}
	// generateMarkdownRegex's neighbours
	inline := []struct {
		in, out string
		bold    bool
	}{
		{"(*b*", "(b", true},
		{"a+*b*", "a+b", true},
		{"a$*b*", "a$*b*", false}, // $ is a symbol, not punctuation
		{"_*b*", "_b", true},      // _ is punctuation (Pc)
	}
	for _, c := range inline {
		e := typed(c.in)
		if e.doc.Text() != c.out {
			t.Errorf("%q → %q, want %q", c.in, e.doc.Text(), c.out)
		}
	}
	// the closing marker needs a boundary after it; ` also takes a word
	e = typed("*b")
	edType(e, "c")
	e.SetSelection(2, 2)
	edType(e, "*")
	if e.doc.Text() != "*b*c" {
		t.Fatalf("*b*c formatted: %q", e.doc.Text())
	}
	e = typed("`x")
	edType(e, "y")
	e.SetSelection(2, 2)
	edType(e, "`")
	if e.doc.Text() != "xy" || !e.doc.HasStyle(0, 1, Code) {
		t.Fatalf("`x`y: %q", e.doc.Text())
	}
	// an opening marker typed before a closed run formats it too
	e = typed("hello*")
	e.SetSelection(0, 0)
	edType(e, "*")
	if e.doc.Text() != "hello" || !e.doc.HasStyle(0, 5, Bold) {
		t.Fatalf("opening marker: %q %v", e.doc.Text(), edStyles(e.doc))
	}
}

// The keyboard module's bindings Clack lacked.
func TestComposerKeysLikeSlack(t *testing.T) {
	sc := key.ModShortcut
	e := NewEditor()
	edType(e, "quote me")
	edPress(e, ".", sc|key.ModShift) // blockquote-alt
	if e.doc.Lines[0].Kind != Quote {
		t.Fatalf("Mod+Shift+. kind %v", e.doc.Lines[0].Kind)
	}
	// Escape on an empty quote line clears it (and isn't EvCancel)
	e = NewEditor()
	edType(e, ">")
	if !e.escapeClearsFormat() {
		t.Fatal("Escape doesn't claim the empty quote line")
	}
	e.events = nil
	edPress(e, key.NameEscape, 0)
	if e.doc.Lines[0].Kind != Para || edHas(e, EvCancel) {
		t.Fatalf("Escape: kind %v cancel %v", e.doc.Lines[0].Kind, edHas(e, EvCancel))
	}
	// Tab indents a list item only at its start (or a selection)
	e = NewEditor()
	edType(e, "- item")
	edPress(e, key.NameTab, 0)
	if e.doc.Lines[0].Indent != 0 {
		t.Fatal("Tab mid-item indented")
	}
	e.SetSelection(0, 0)
	edPress(e, key.NameTab, 0)
	if e.doc.Lines[0].Indent != 1 {
		t.Fatal("Tab at the item start didn't indent")
	}
	// Mod+Left / Mod+Right in an empty composer navigate; Mod+O the + menu
	e = NewEditor()
	edPress(e, key.NameLeftArrow, sc)
	edPress(e, key.NameRightArrow, sc)
	edPress(e, "O", sc)
	if !edHas(e, EvNavBack) || !edHas(e, EvNavForward) || !edHas(e, EvPlusMenu) {
		t.Fatalf("events %+v", e.events)
	}
	edType(e, "x")
	e.events = nil
	edPress(e, key.NameLeftArrow, sc)
	if edHas(e, EvNavBack) {
		t.Fatal("Mod+Left navigated with text in the composer")
	}
}

// getScheduledSendMenuOptions over the week (BQDv).
func TestScheduleMenuRows(t *testing.T) {
	loc := time.FixedZone("T", 0)
	at := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, loc) } // Nov 9 2026 is a Monday
	labels := func(rows []schedRow) string {
		var s []string
		for _, r := range rows {
			s = append(s, r.label)
		}
		return strings.Join(s, "; ")
	}
	cases := []struct {
		now  time.Time
		want string
	}{
		{at(10, 7, 0), "Later today at 9:00 AM; Monday at 9:00 AM; Custom time"},
		{at(10, 10, 0), "Later today at 1:00 PM; Monday at 9:00 AM; Custom time"},
		{at(11, 15, 0), "Tomorrow at 9:00 AM; Monday at 9:00 AM; Custom time"},
		{at(13, 15, 0), "Monday at 9:00 AM; Custom time"},
		{at(15, 10, 0), "Monday at 9:00 AM; Custom time"},
		{at(9, 7, 0), "Later today at 9:00 AM; Next Monday at 9:00 AM; Custom time"},
		{at(9, 15, 0), "Tomorrow at 9:00 AM; Next Monday at 9:00 AM; Custom time"},
	}
	for _, c := range cases {
		if got := labels(scheduleRows(c.now, false, false, nil)); got != c.want {
			t.Errorf("%s: %s, want %s", c.now.Format("Mon 15:04"), got, c.want)
		}
	}
	if got := schedStandardTime(schedMonday9, at(15, 10, 0)); !got.Equal(at(16, 9, 0)) {
		t.Errorf("Monday from Sunday = %v", got)
	}
	if got := schedStandardTime(schedNextMonday9, at(9, 7, 0)); !got.Equal(at(16, 9, 0)) {
		t.Errorf("Next Monday from Monday = %v", got)
	}
	if got := labels(scheduleRows(at(11, 15, 0), true, false, nil)); got != "Tomorrow  at 09:00; Monday at 09:00; Custom time" {
		t.Errorf("24-hour labels: %s", got)
	}
	if got := labels(scheduleRows(at(13, 15, 0), false, true, nil)); got != "Tomorrow at 9:00 AM; Monday at 9:00 AM; Custom time" {
		t.Errorf("updated_scheduled_sends_desktop: %s", got)
	}
	// a DM partner 9 hours ahead, at 23:00 for them: their 9:00 leads,
	// each row says when it is for them
	m := &schedMemberInfo{name: "alice", loc: time.FixedZone("A", 9*3600), offset: true}
	rows := scheduleRows(at(10, 14, 0), false, false, m)
	if len(rows) != 4 || rows[0].opt != schedMember || rows[0].label != "Tomorrow at 12:00 AM" || rows[0].desc != "9:00 AM for alice" {
		t.Fatalf("member rows %+v", rows)
	}
	if rows[1].label != "Tomorrow at 9:00 AM" || rows[1].desc != "6:00 PM for alice" {
		t.Errorf("tomorrow row %+v", rows[1])
	}
	if rows[3].opt != schedCustom || rows[3].desc != "" {
		t.Errorf("custom row %+v", rows[3])
	}
	// getDefaultCustomTime
	if got := schedCustomDefault(at(10, 10, 20), nil); !got.Equal(at(10, 11, 0)) {
		t.Errorf("custom default at 10:20 = %v", got)
	}
	if got := schedCustomDefault(at(10, 10, 57), nil); !got.Equal(at(10, 12, 0)) {
		t.Errorf("custom default at 10:57 = %v", got)
	}
}

// ComposerButton writes msg_input_sticky_composer (fakeslack).
func TestFormattingTogglePref(t *testing.T) {
	fs, err := fakeslack.Start(fakeslack.Small)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	app := core.New(slack.New(fs.Creds()), store.New(""))
	app.ServeOffline()
	if _, err := app.RefreshPrefs(context.Background()); err != nil {
		t.Fatal(err)
	}
	stubTyping(t)
	old := showFormatting
	t.Cleanup(func() { showFormatting = old })
	c := New(&slickHost{app: app, th: theme.New(false)}, fs.ChannelIDs()[0], "")
	t.Cleanup(c.Close)
	syncShowFormatting(c.api)
	before := showFormatting
	c.toggleShowFormatting()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if v, ok := fs.Pref("msg_input_sticky_composer"); ok && prefIsTrue(v) == !before {
			break
		}
		if time.Now().After(deadline) {
			v, _ := fs.Pref("msg_input_sticky_composer")
			t.Fatalf("msg_input_sticky_composer = %v, want %v", v, !before)
		}
		time.Sleep(20 * time.Millisecond)
	}
	syncShowFormatting(c.api)
	if showFormatting == before {
		t.Fatal("the toolbar didn't follow the pref")
	}
}

// gtxForProps is a headless frame context (no input source: nothing
// is focused).
func gtxForProps() layout.Context {
	return layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Constraints{Max: image.Pt(900, 600)}, Now: time.Now()}
}

func prefIsTrue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return x == "true" || x == "1"
	}
	return false
}

// TextyInput's onPasteLimit: a paste over 50,000 characters is refused
// and the composer shows PasteLimitDialog.
func TestPasteLimit(t *testing.T) {
	e := NewEditor()
	e.paste(strings.Repeat("a", pasteMaxLength+1))
	if e.doc.Len() != 0 || !edHas(e, EvPasteLimit) {
		t.Fatalf("over the limit: len %d, refused %v", e.doc.Len(), edHas(e, EvPasteLimit))
	}
	e.events = nil
	e.paste(strings.Repeat("a", 100))
	if e.doc.Len() != 100 || edHas(e, EvPasteLimit) {
		t.Fatalf("a short paste: len %d", e.doc.Len())
	}
}

// More of the keyboard module: "outdent backspace", a code line with
// text joining the line above, "inline code right arrow" and "block code
// down arrow".
func TestComposerEditingKeysLikeSlack(t *testing.T) {
	e := NewEditor()
	edType(e, "- item")
	e.SetSelection(0, 0)
	edPress(e, key.NameTab, 0)
	edPress(e, key.NameDeleteBackward, 0)
	if l := e.doc.Lines[0]; l.Kind != Bullet || l.Indent != 0 {
		t.Fatalf("Backspace on a nested item: %+v", l)
	}
	edPress(e, key.NameDeleteBackward, 0)
	if l := e.doc.Lines[0]; l.Kind != Para {
		t.Fatalf("Backspace on a top-level item: %+v", l)
	}

	e = NewEditor()
	edType(e, "a\n```b")
	if e.doc.Lines[1].Kind != CodeBlock {
		t.Fatalf("no code line: %+v", e.doc.Lines)
	}
	e.SetSelection(2, 2)
	edPress(e, key.NameDeleteBackward, 0)
	if e.doc.Text() != "ab" || len(e.doc.Lines) != 1 {
		t.Fatalf("Backspace at a code line's start: %q %+v", e.doc.Text(), e.doc.Lines)
	}

	e = NewEditor()
	edType(e, "run `ls`")
	edPress(e, key.NameRightArrow, 0)
	edType(e, "x")
	if e.doc.Text() != "run ls x" || e.doc.Attrs[6].Style&Code != 0 || e.doc.Attrs[7].Style&Code != 0 {
		t.Fatalf("Right out of inline code: %q %v", e.doc.Text(), edStyles(e.doc))
	}

	e = NewEditor()
	edType(e, "```code")
	edPress(e, key.NameDownArrow, 0)
	edType(e, "after")
	if len(e.doc.Lines) != 2 || e.doc.Lines[0].Kind != CodeBlock || e.doc.Lines[1].Kind != Para || e.doc.Text() != "code\nafter" {
		t.Fatalf("Down out of a trailing code block: %q %+v", e.doc.Text(), e.doc.Lines)
	}
}
