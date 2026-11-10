package views

// Synthetic fixtures only (scraping policy): no real message content.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
)

func TestTSTime(t *testing.T) {
	got := tsTime("1730900000.123456")
	if got.Unix() != 1730900000 || got.Nanosecond() != 123456000 {
		t.Fatalf("tsTime = %v", got)
	}
	if !tsTime("").IsZero() || !tsTime("x.y").IsZero() {
		t.Fatal("bad ts should be zero")
	}
}

func TestListTime(t *testing.T) {
	now := time.Now()
	if got := listTime(now); got != clock(now) {
		t.Fatalf("today = %q", got)
	}
	if got := listTime(now.AddDate(0, 0, -1)); got != "Yesterday" {
		t.Fatalf("yesterday = %q", got)
	}
	if listTime(time.Time{}) != "" {
		t.Fatal("zero time should be empty")
	}
}

func TestOrdinalAndCounts(t *testing.T) {
	for d, want := range map[int]string{1: "st", 2: "nd", 3: "rd", 4: "th", 11: "th", 12: "th", 13: "th", 21: "st", 22: "nd", 23: "rd"} {
		if got := ordinal(d); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", d, got, want)
		}
	}
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567"} {
		if got := humanCount(n); got != want {
			t.Errorf("humanCount(%d) = %q, want %q", n, got, want)
		}
	}
	if plural(1, "day") != "1 day" || plural(3, "day") != "3 days" {
		t.Error("plural")
	}
}

func TestMpimName(t *testing.T) {
	if got := mpimName("mpdm-alpha--beta--gamma-1"); got != "alpha, beta, gamma" {
		t.Fatalf("mpimName = %q", got)
	}
}

func TestDueText(t *testing.T) {
	if s, over := dueText(time.Now().Add(-3 * 24 * time.Hour)); !over || s != "Overdue by 3 days" {
		t.Fatalf("overdue = %q %v", s, over)
	}
	if s, over := dueText(time.Now().Add(2 * time.Minute)); over || s == "" {
		t.Fatalf("upcoming = %q %v", s, over)
	}
}

func TestRoomFromEvent(t *testing.T) {
	raw := json.RawMessage(`{"type":"sh_room_join","room":{"id":"R1","date_start":100,"participants":["U1","U2"],"channels":["C1"]}}`)
	r, ok := roomFromEvent(raw)
	if !ok || r.ID != "R1" || !r.Active || len(r.Participants) != 2 || r.Channels[0] != "C1" {
		t.Fatalf("room = %+v ok=%v", r, ok)
	}
	ended := json.RawMessage(`{"type":"sh_room_update","room":{"id":"R1","date_start":100,"date_end":200,"participants":[]}}`)
	if r, ok := roomFromEvent(ended); !ok || r.Active {
		t.Fatalf("ended room = %+v", r)
	}
	if _, ok := roomFromEvent(json.RawMessage(`{"type":"sh_room_join"}`)); ok {
		t.Fatal("event without room should not decode")
	}
}

func TestFileKinds(t *testing.T) {
	canvas := fileItem{core.File{Filetype: "quip", PrettyType: "Canvas"}}
	list := fileItem{core.File{Filetype: "list"}}
	img := fileItem{core.File{Mime: "image/png"}}
	if !canvas.isCanvas() || canvas.isList() || !list.isList() || !img.isImage() {
		t.Fatal("file kind predicates")
	}
	if fileKindLabel(&canvas) != "Canvas" || fileKindLabel(&list) != "List" {
		t.Fatal("file kind labels")
	}
	if !fileMention.MatchString("<@U123ABC>") || !fileMention.MatchString("<#C1|general>") {
		t.Fatal("fileMention")
	}
}
