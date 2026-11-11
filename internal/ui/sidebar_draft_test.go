package ui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func draftBlocks(text string) json.RawMessage {
	b, _ := json.Marshal([]any{map[string]any{"type": "rich_text", "elements": []any{
		map[string]any{"type": "rich_text_section", "elements": []any{map[string]any{"type": "text", "text": text}}}}}})
	return b
}

func draftDest(ch, thread string) json.RawMessage {
	d := map[string]any{"channel_id": ch}
	if thread != "" {
		d["thread_ts"] = thread
	}
	b, _ := json.Marshal([]any{d})
	return b
}

// The sidebar's draft pencil: the account's groups (neither draft
// experiment) give the plain pencil with ChannelSuffixDraftIconTooltipContent;
// draft_management_in_composer (treatment) with
// sidebar_draft_indicator_menu_and_thread_inclusion (on) count the
// channel's thread drafts too, the tooltip says "N drafts" and the pencil
// opens "Jump to draft…" (the four newest, then "View N more drafts").
func TestSidebarDraftPencilMenu(t *testing.T) {
	long := "hello there, this draft keeps going well past fifty characters of text"
	var ch string
	e, _ := filterEnv(t, func(fs *fakeslack.Server) {
		ch = fs.ChannelIDs()[3]
		fs.AddDraft(fakeslack.Draft{Blocks: draftBlocks(long), Destinations: draftDest(ch, ""), LastUpdatedTS: "1762500000000"})
		fs.AddDraft(fakeslack.Draft{Blocks: draftBlocks("a reply draft"), Destinations: draftDest(ch, "1762400000.000100"), LastUpdatedTS: "1762600000000"})
		for i := 0; i < 4; i++ {
			fs.AddDraft(fakeslack.Draft{Blocks: draftBlocks("older " + string(rune('a'+i))), Destinations: draftDest(ch, ""),
				LastUpdatedTS: "17621000" + string(rune('0'+i)) + "0000"})
		}
	})
	a := e.a
	c := a.Core()
	e.until("the row's drafts", 10*time.Second, func() bool { return a.data != nil && a.data.draftChans[ch] })
	if a.sbDraftMenuOn() {
		t.Fatal("the draft menu is on for an account without the draft experiments")
	}
	if n := len(a.sbDrafts(ch)); n != 5 {
		t.Errorf("without thread inclusion the row counts %d drafts, want its 5 channel drafts", n)
	}
	want := "Draft\n“" + string([]rune(long)[:50]) + "…”"
	if got := a.draftTipText(ch); got != want {
		t.Errorf("tooltip:\n got %q\nwant %q", got, want)
	}

	c.SetExperimentGroup("draft_management_in_composer", "treatment")
	c.SetExperimentGroup("sidebar_draft_indicator_menu_and_thread_inclusion", "on")
	e.until("thread drafts counted", 10*time.Second, func() bool { return len(a.sbDrafts(ch)) == 6 })
	if !a.sbDraftMenuOn() {
		t.Fatal("the draft menu is off under both experiments")
	}
	if got := a.draftTipText(ch); got != "6 drafts" {
		t.Errorf("tooltip with several drafts: %q", got)
	}
	items := a.draftMenuItems(ch)
	var labels []string
	for _, it := range items {
		labels = append(labels, it.Label)
	}
	wantMenu := "Jump to draft… | Thread: a reply draft | " + string([]rune(long)[:50]) + "… | older d | older c | View 2 more drafts"
	if got := strings.Join(labels, " | "); got != wantMenu {
		t.Errorf("menu:\n got %s\nwant %s", got, wantMenu)
	}
	if !items[0].Header {
		t.Error("Jump to draft… isn't the menu's heading")
	}
	items[2].Do() // a channel draft: the channel
	e.frame()
	if r := string(a.router.Current()); r != "c/"+ch {
		t.Errorf("a channel draft opened %q", r)
	}
	items[len(items)-1].Do()
	e.frame()
	if r := string(a.router.Current()); r != "drafts" {
		t.Errorf("View 2 more drafts opened %q", r)
	}
	// only treatment counts for draft_management_in_composer
	c.SetExperimentGroup("draft_management_in_composer", "on")
	e.until("back to channel drafts", 10*time.Second, func() bool { return len(a.sbDrafts(ch)) == 5 })
	if a.sbDraftMenuOn() {
		t.Error("draft_management_in_composer on (not treatment) turned the menu on")
	}
}
