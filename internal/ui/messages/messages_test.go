package messages

import (
	"image"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/popups"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// nilHost is a Host without a core (synthetic data only).
type nilHost struct{}

func (nilHost) Core() *core.App                           { return nil }
func (nilHost) Theme() *theme.Theme                       { return nil }
func (nilHost) Images() *imgcache.Cache                   { return nil }
func (nilHost) Invalidate()                               {}
func (nilHost) Navigate(string)                           {}
func (nilHost) OpenThread(string, string)                 {}
func (nilHost) CloseSecondary()                           {}
func (nilHost) ShowOverlay(popups.Overlay)                {}
func (nilHost) OpenEmojiPicker(image.Point, func(string)) {}
func (nilHost) OpenProfile(image.Point, string)           {}
func (nilHost) OpenMessageMenu(image.Point, core.Message) {}
func (nilHost) OpenLightbox(richtext.Image, core.Message) {}
func (nilHost) Toast(string)                              {}

func msg(ts, user, subtype string) entry {
	return entry{m: core.Message{TS: ts, User: user, Subtype: subtype, System: subtype != "", Channel: "C1"}, rev: 1}
}

func TestJoinRollup(t *testing.T) {
	base := time.Date(2026, 11, 6, 10, 0, 0, 0, time.Local).Unix()
	ts := func(sec int64) string { return itoa(int(base+sec)) + ".000100" }
	s := &listState{rows: map[string]*row{}}
	s.snap.loaded = true
	s.snap.msgs = []entry{
		msg(ts(0), "U1", ""),
		msg(ts(10), "U2", "channel_join"),
		msg(ts(20), "U3", "channel_join"),
		msg(ts(30), "U4", "channel_join"),
		msg(ts(40), "U5", "channel_join"),
		msg(ts(50), "U6", "channel_leave"),
		msg(ts(60), "U1", ""),
	}
	s.build(nilHost{}, time.Unix(base+100, 0))
	var kinds []string
	var group *item
	for i := range s.items {
		it := &s.items[i]
		switch {
		case it.kind == itMsg && len(it.group) > 0:
			kinds = append(kinds, "group")
			group = it
		case it.kind == itMsg:
			kinds = append(kinds, "msg")
		default:
			kinds = append(kinds, "other")
		}
	}
	// intro, day, msg, [4 joins], leave (single), msg, the bottom spacer
	want := "other other msg group msg msg other"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("items %q, want %q", got, want)
	}
	if len(group.group) != 4 || !group.header {
		t.Fatalf("group %+v", group)
	}
	e := s.groupEntry(nilHost{}, group.group)
	txt := richtext.SpansText(e.m.Doc.Blocks[0].Spans)
	if txt != "@U2, @U3 and 2 others have joined the channel" {
		t.Errorf("summary %q", txt)
	}
	e2 := s.groupEntry(nilHost{}, group.group[:2])
	if got := richtext.SpansText(e2.m.Doc.Blocks[0].Spans); got != "@U2 and @U3 have joined the channel" {
		t.Errorf("two-user summary %q", got)
	}
}

// The reply bar's last reply (getThreadLatestReplyText over Slack's
// toTimeAgo, en-US): today by the clock, else rounded relative times;
// one reply drops "Last reply".
func TestReplyBarLastReply(t *testing.T) {
	now := time.Date(2026, 11, 6, 15, 30, 0, 0, time.Local)
	ts := func(t time.Time) string { return strconv.FormatInt(t.Unix(), 10) + ".000100" }
	cases := []struct {
		at    time.Time
		count int
		want  string
	}{
		{time.Date(2026, 11, 6, 15, 23, 0, 0, time.Local), 4, "Last reply today at 3:23 PM"},
		{time.Date(2026, 11, 6, 9, 5, 0, 0, time.Local), 1, "Today at 9:05 AM"},
		{time.Date(2026, 11, 5, 23, 5, 0, 0, time.Local), 2, "Last reply 16 hours ago"},
		{time.Date(2026, 11, 5, 9, 5, 0, 0, time.Local), 2, "Last reply 1 day ago"},
		{time.Date(2026, 11, 5, 9, 5, 0, 0, time.Local), 1, "1 day ago"},
		{time.Date(2026, 11, 1, 9, 5, 0, 0, time.Local), 3, "Last reply 5 days ago"},
		{time.Date(2026, 10, 20, 9, 5, 0, 0, time.Local), 3, "Last reply 17 days ago"},
		{time.Date(2026, 10, 1, 9, 5, 0, 0, time.Local), 3, "Last reply 1 month ago"},
		{time.Date(2026, 8, 1, 9, 5, 0, 0, time.Local), 3, "Last reply 3 months ago"},
		{time.Date(2025, 8, 1, 9, 5, 0, 0, time.Local), 3, "Last reply 1 year ago"},
		{time.Date(2024, 8, 1, 9, 5, 0, 0, time.Local), 3, "Last reply 2 years ago"},
	}
	for _, c := range cases {
		if got := lastReply(tsTime(ts(c.at)), now, c.count); got != c.want {
			t.Errorf("%v, %d replies: %q, want %q", c.at, c.count, got, c.want)
		}
	}
}

// renderFaces: up to five faces; past five, four and a fifth with "+N".
func TestReplyBarFaces(t *testing.T) {
	f, n := replyBarFaces([]string{"U1", "B00", "U2", "U3"}, 0)
	if len(f) != 3 || n != 0 {
		t.Errorf("faces %v +%d, want three, no overlay", f, n)
	}
	f, n = replyBarFaces([]string{"U1", "U2", "U3", "U4", "U5", "U6", "U7"}, 9)
	if len(f) != 5 || f[4] != "U5" || n != 4 {
		t.Errorf("faces %v +%d, want five with +4", f, n)
	}
}

func TestDayLabel(t *testing.T) {
	now := time.Date(2026, 11, 6, 15, 30, 0, 0, time.Local)
	if got := dayLabel(time.Date(2026, 11, 3, 1, 0, 0, 0, time.Local), now); got != "Tuesday, November 3rd" {
		t.Errorf("got %q", got)
	}
	if got := dayLabel(time.Date(2025, 12, 31, 1, 0, 0, 0, time.Local), now); got != "Wednesday, December 31st, 2025" {
		t.Errorf("got %q", got)
	}
}

func TestSlackLink(t *testing.T) {
	ch, ts, ok := slackLink("https://hackclub.slack.com/archives/C0123/p1700000000123456")
	if !ok || ch != "C0123" || ts != "1700000000.123456" {
		t.Errorf("got %q %q %v", ch, ts, ok)
	}
	if _, _, ok := slackLink("https://example.com/archives/C1/p1"); ok {
		t.Errorf("non-slack link accepted")
	}
}

func TestRawChannel(t *testing.T) {
	bs := []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Channel, ID: "C0BJ6LTLAFJ", Text: "#C0BJ6LTLAFJ"}}}}
	if !blocksNeedChan(bs) {
		t.Fatal("raw channel mention not detected")
	}
	ok := []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{{Kind: richtext.Channel, ID: "C1", Text: "#general"}}}}
	if blocksNeedChan(ok) {
		t.Fatal("resolved mention flagged")
	}
}
