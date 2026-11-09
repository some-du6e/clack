package views

import "testing"

type huddleStartHost struct {
	tautTestHost
	opened  []string
	started []string
}

func (h *huddleStartHost) OpenHuddleStart(entry string) { h.opened = append(h.opened, entry) }
func (h *huddleStartHost) StartHuddle(ch string)        { h.started = append(h.started, ch) }

// The Huddles page's New Huddle (useStartHuddleClick without standalone
// huddles) opens HuddleInviteStartModal, the "Start a Huddle" picker,
// instead of telling you to go use a channel header.
func TestHuddlesPageNewHuddle(t *testing.T) {
	h := &huddleStartHost{}
	newHuddle(h, nil)
	if len(h.opened) != 1 || h.opened[0] != "huddles_page_start_huddle" || len(h.started) != 0 {
		t.Fatalf("opened %q, started %q", h.opened, h.started)
	}
	// a person on the page: a huddle in your DM with them, natively
	startHuddle(h, nil, startHuddleArgs{ChannelID: "D0TESTDM01"})
	if len(h.started) != 1 || h.started[0] != "D0TESTDM01" {
		t.Fatalf("started %q", h.started)
	}
}
