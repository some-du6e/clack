package huddleui

import (
	"encoding/json"
	"testing"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/imgcache"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

type nopHost struct{ inval int }

func (h *nopHost) Core() *core.App           { return nil }
func (h *nopHost) Theme() *theme.Theme       { return nil }
func (h *nopHost) Images() *imgcache.Cache   { return nil }
func (h *nopHost) Invalidate()               { h.inval++ }
func (h *nopHost) Navigate(string)           {}
func (h *nopHost) OpenThread(string, string) {}
func (h *nopHost) Toast(string)              {}

func TestLiveRooms(t *testing.T) {
	h := &nopHost{}
	c := New(h)
	ev := func(name, raw string) { c.Handle(core.Event{Type: "huddle", Name: name, Raw: json.RawMessage(raw)}) }
	ev("sh_room_join", `{"user":"U1","room":{"id":"R1","channels":["C1"],"participants":["U1"]}}`)
	if n := c.LiveCount("C1"); n != 1 || !c.HeaderLive("C1") {
		t.Fatalf("after join: %d", n)
	}
	ev("sh_room_join", `{"user":"U2","huddle":{"id":"R1","channel_id":"C1"}}`) // no participant list
	if n := c.LiveCount("C1"); n != 2 {
		t.Fatalf("second join: %d", n)
	}
	ev("sh_room_leave", `{"user":"U2","huddle":{"id":"R1","channel_id":"C1"}}`)
	if n := c.LiveCount("C1"); n != 1 {
		t.Fatalf("leave: %d", n)
	}
	ev("sh_room_update", `{"room":{"id":"R1","channels":["C1"],"participants":["U1"],"has_ended":true}}`)
	if c.LiveCount("C1") != 0 || c.HeaderLive("C1") {
		t.Fatal("ended huddle still live")
	}
	if c.Active() || c.In("C1") || h.inval == 0 {
		t.Fatal("state")
	}
	if got := elapsed(3723e9); got != "1:02:03" {
		t.Fatalf("elapsed %q", got)
	}
	if got := elapsed(65e9); got != "1:05" {
		t.Fatalf("elapsed %q", got)
	}
}
