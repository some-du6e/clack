package huddle

import (
	"context"
	"os"
	"testing"
	"time"
)

// The huddle's theme: rooms.join's background_id, then the room updates
// (HtDl's updateHuddle: a room with its channels and thread root sets it
// as it stands, so an update without background_id takes it off). Fake
// Chime only.
func TestRoomBackground(t *testing.T) {
	clip, err := os.ReadFile("testdata/testsrc-320x180.h264")
	if err != nil {
		t.Fatal(err)
	}
	fk := NewFake(clip, t.Errorf)
	t.Cleanup(fk.Close)
	fk.SetBackground("ARIZONA")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Join(ctx, Options{Client: fk.Caller(), SelfID: fakeSelf, Logf: t.Logf, Device: NewToneDevice(440)}, "C0TEST")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Leave)
	if got := s.State().Background; got != "ARIZONA" {
		t.Fatalf("theme %q after rooms.join, want ARIZONA", got)
	}
	s.HandleRoomEvent("sh_room_update", FakeBackgroundUpdate("C0TEST", "GRADIENT_03"))
	if got := s.State().Background; got != "GRADIENT_03" {
		t.Fatalf("theme %q after the update, want GRADIENT_03", got)
	}
	// a ring update (no thread root) says nothing about the theme
	s.HandleRoomEvent("sh_room_update", FakeRingUpdate("C0TEST", "U0OTHER", "pending"))
	if got := s.State().Background; got != "GRADIENT_03" {
		t.Fatalf("theme %q after a ring update, want it kept", got)
	}
	s.HandleRoomEvent("sh_room_update", FakeBackgroundUpdate("C0TEST", ""))
	if got := s.State().Background; got != "" {
		t.Fatalf("theme %q after it was taken off", got)
	}
}
