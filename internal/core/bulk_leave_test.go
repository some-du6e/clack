package core

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/slack"
)

func fakeSectionHas(fs *fakeslack.Server, sec, ch string) bool {
	for _, s := range fs.Sections() {
		if s.ID == sec {
			return slices.Contains(s.Channels, ch)
		}
	}
	return false
}

// moveManyChannels: the custom sections' share of a move is one
// bulkUpdate, Starred's stars.add; a conversation both left and entered
// in one section moves nowhere; Undo sends the reverse.
func TestMoveMany(t *testing.T) {
	fs, a := bootFake(t)
	ctx := context.Background()
	var from []string
	for _, s := range fs.Sections() {
		if s.ID == "L0FAKE01" {
			from = append(from, s.Channels...)
		}
	}
	if len(from) < 3 {
		t.Fatalf("Hack Club holds %v", from)
	}
	ids := from[:2]
	mv, err := a.MoveMany(ctx, []SectionItems{{"L0FAKE03", ids}}, []SectionItems{{"L0FAKE01", ids}})
	if err != nil {
		t.Fatal(err)
	}
	if mv.Count != 2 || len(mv.Insert) != 1 || len(mv.Remove) != 1 {
		t.Errorf("move %+v", mv)
	}
	for _, id := range ids {
		if !fakeSectionHas(fs, "L0FAKE03", id) || fakeSectionHas(fs, "L0FAKE01", id) {
			t.Errorf("%s not moved into Events", id)
		}
	}
	if err := a.UndoSectionMove(ctx, mv); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if fakeSectionHas(fs, "L0FAKE03", id) || !fakeSectionHas(fs, "L0FAKE01", id) {
			t.Errorf("%s not back in Hack Club", id)
		}
	}

	// in and out of the same section: nothing moves
	same := from[2]
	mv, err = a.MoveMany(ctx, []SectionItems{{"L0FAKE01", []string{same}}}, []SectionItems{{"L0FAKE01", []string{same}}})
	if err != nil {
		t.Fatal(err)
	}
	if mv.Count != 0 || len(mv.Insert)+len(mv.Remove) != 0 {
		t.Errorf("an ignored movement moved: %+v", mv)
	}

	// into Starred: stars.add
	mv, err = a.MoveMany(ctx, []SectionItems{{"L0FAKE00", []string{same}}}, []SectionItems{{"L0FAKE01", []string{same}}})
	if err != nil {
		t.Fatal(err)
	}
	if !fs.Starred(same) || fakeSectionHas(fs, "L0FAKE01", same) || mv.Count != 1 || !slices.Equal(mv.Starred, []string{same}) {
		t.Errorf("into Starred: starred %v, still in Hack Club %v, move %+v", fs.Starred(same), fakeSectionHas(fs, "L0FAKE01", same), mv)
	}
}

// getChannelsThatCannotBeLeft: DMs and group DMs, #general and
// org-mandatory channels, archived, externally shared, record, huddle and
// meeting channels, and what the viewer isn't in stay out of a bulk leave.
func TestChannelsThatCannotBeLeft(t *testing.T) {
	_, a := bootFake(t)
	props := func(v map[string]any) json.RawMessage { b, _ := json.Marshal(v); return b }
	convs := []slack.Conversation{
		{ID: "C0LEAVEOK1", Name: "fine", IsChannel: true, IsMember: true},
		{ID: "G0LEAVEOK2", Name: "secret", IsGroup: true, IsPrivate: true, IsMember: true},
		{ID: "D0LEAVEDM1", IsIM: true, User: "U0SOMEONE", IsMember: true},
		{ID: "G0LEAVEMP1", IsMPIM: true, IsMember: true},
		{ID: "C0LEAVEGEN", Name: "general", IsChannel: true, IsGeneral: true, IsMember: true},
		{ID: "C0LEAVEMAN", Name: "everyone", IsChannel: true, IsOrgMandatory: true, IsMember: true},
		{ID: "C0LEAVEARC", Name: "old", IsChannel: true, IsArchived: true, IsMember: true},
		{ID: "C0LEAVEEXT", Name: "shared", IsChannel: true, IsExtShared: true, IsMember: true},
		{ID: "C0LEAVEREC", Name: "record", IsChannel: true, IsMember: true, Properties: props(map[string]any{"record_channel": map[string]any{"record_id": "AC:1"}})},
		{ID: "C0LEAVEHUD", Name: "huddle", IsChannel: true, IsMember: true, Properties: props(map[string]any{"huddles": map[string]any{"is_huddle_channel": true}})},
		{ID: "C0LEAVEMTG", Name: "mc:standup", IsChannel: true, IsMember: true},
	}
	var ids []string
	for _, c := range convs {
		a.S.PutConv(c)
		ids = append(ids, c.ID)
	}
	got := a.ChannelsThatCannotBeLeft(append(ids, "C0NOTMINE1"))
	want := []string{"D0LEAVEDM1", "G0LEAVEMP1", "C0LEAVEGEN", "C0LEAVEMAN", "C0LEAVEARC", "C0LEAVEEXT", "C0LEAVEREC", "C0LEAVEHUD", "C0LEAVEMTG", "C0NOTMINE1"}
	if !slices.Equal(got, want) {
		t.Errorf("can't be left:\n got %v\nwant %v", got, want)
	}
	// a restricted guest can't leave a public channel
	a.S.Self.IsRestricted = true
	if got := a.ChannelsThatCannotBeLeft([]string{"C0LEAVEOK1", "G0LEAVEOK2"}); !slices.Equal(got, []string{"C0LEAVEOK1"}) {
		t.Errorf("restricted guest: %v, want the public channel", got)
	}
}

// leaveChannelsInBulk: one conversations.bulkLeave with the ids
// comma-joined; the channels leave the member set.
func TestLeaveChannelsBulk(t *testing.T) {
	fs, a := bootFake(t)
	ctx := context.Background()
	ids := fs.ChannelIDs()[1:3]
	for _, id := range ids {
		if c, ok := a.Conv(id); !ok || c.Preview {
			t.Fatalf("%s isn't a member channel", id)
		}
	}
	if err := a.LeaveChannelsBulk(ctx, ids); err != nil {
		t.Fatal(err)
	}
	calls := fs.BulkLeaveCalls()
	if len(calls) != 1 || !slices.Equal(calls[0], ids) {
		t.Fatalf("bulkLeave calls %v, want one with %v", calls, ids)
	}
	for _, id := range ids {
		if _, ok := a.S.Conv(id); ok {
			t.Errorf("%s is still a member", id)
		}
	}
}
