package popups

// The density card (search_card.go): for everyone, against a dogfood
// user's "original" density, over fakeslack with a hooked
// search.modules.messages answer.

import (
	"context"
	"fmt"
	"image"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
)

const (
	scThreadTS = "1762444000.000100"
	scMyReply  = "1762444900.000100"
)

// scHook answers the results page's search with five crafted results
// (in this order): a long rich text body, five blocks with the match in
// the fourth, reactions, a thread the viewer replied in, and
// attachments with a file. conversations.replies of the thread has the
// viewer's reply.
func scHook(e *srEnv) *atomic.Int32 {
	var replies atomic.Int32
	ch := e.fs.ChannelIDs()[0]
	rt := func(s string) map[string]any {
		return map[string]any{"type": "rich_text", "elements": []any{map[string]any{"type": "rich_text_section",
			"elements": []any{map[string]any{"type": "text", "text": s}}}}}
	}
	var rows []string
	for i := 1; i <= 40; i++ {
		rows = append(rows, fmt.Sprintf("row%02d", i))
	}
	long := strings.Join(rows, "\n")
	msgs := []map[string]any{
		{"ts": "1762440000.000100", "user": fakeslack.SelfID, "text": long, "blocks": []any{rt(long)}},
		{"ts": "1762441000.000100", "user": fakeslack.SelfID, "text": "blockA blockB blockC zephyr blockE",
			"blocks": []any{rt("blockA"), rt("blockB"), rt("blockC"), rt("zephyr blockD"), rt("blockE")}},
		{"ts": "1762442000.000100", "user": fakeslack.SelfID, "text": "reacted zephyr", "reactions": []any{
			map[string]any{"name": "thumbsup::skin-tone-2", "count": 2, "users": []any{fakeslack.SelfID, "U0000000002"}},
			map[string]any{"name": "tada", "count": 3, "users": []any{"U0000000002"}},
			map[string]any{"name": "thumbsup", "count": 1, "users": []any{"U0000000003"}}}},
		{"ts": scThreadTS, "thread_ts": scThreadTS, "user": "U0000000002", "text": "a thread about zephyr",
			"reply_count": 2, "latest_reply": scMyReply, "requestor_replied": true},
		{"ts": "1762445000.000100", "user": fakeslack.SelfID, "text": "links for zephyr", "attachments": []any{
			map[string]any{"id": 1, "title": "Design doc", "title_link": "https://example.com/doc", "service_name": "Example"},
			map[string]any{"id": 2, "title": "Hidden one", "hidden": true}},
			"files": []any{map[string]any{"id": "F0SC000001", "name": "notes.txt", "title": "notes.txt", "mimetype": "text/plain",
				"filetype": "text", "permalink": "https://fake.slack.com/files/" + fakeslack.SelfID + "/F0SC000001/notes.txt"}}},
	}
	e.fs.SetHook(func(method string, f url.Values) (any, bool) {
		switch method {
		case "search.modules.messages":
			var items []any
			for _, m := range msgs {
				items = append(items, map[string]any{"channel": map[string]any{"id": ch, "name": "general", "is_channel": true},
					"messages": []any{m}})
			}
			return map[string]any{"ok": true, "items": items,
				"pagination": map[string]any{"total_count": len(msgs), "page": 1, "page_count": 1}}, true
		case "conversations.replies":
			if f.Get("ts") != scThreadTS {
				return nil, false
			}
			if f.Get("reason") == "search-engagement-badge-reply-preview" {
				replies.Add(1)
			}
			return map[string]any{"ok": true, "has_more": false, "messages": []any{
				map[string]any{"type": "message", "ts": scThreadTS, "user": "U0000000002", "text": "a thread about zephyr"},
				map[string]any{"type": "message", "ts": "1762444500.000100", "user": "U0000000002", "text": "first", "thread_ts": scThreadTS},
				map[string]any{"type": "message", "ts": scMyReply, "user": fakeslack.SelfID, "text": "my answer, which runs on well past the sixty characters the preview keeps", "thread_ts": scThreadTS}}}, true
		}
		return nil, false
	})
	return &replies
}

func TestSearchDensityCard(t *testing.T) {
	e := srNewEnv(t, false)
	scHook(e)
	if !e.h.a.SearchCardExplore() {
		t.Fatal("no density card")
	}
	v, d := srView(t, e, fakeslack.SearchWord, false)
	if len(v.cards) != 5 {
		t.Fatalf("%d cards", len(v.cards))
	}
	d.until(t, "the viewer's reply", func() bool { return v.cards[3].reply.preview != "" })
	x := d.scan()

	// the long body: six lines, faded, "... Show more" under it
	for i := 1; i <= 40; i++ {
		if w := fmt.Sprintf("row%02d", i); srHas(x, w) != (i <= srClampLines) {
			t.Errorf("collapsed: %s drawn %v", w, srHas(x, w))
		}
	}
	if c := &v.cards[0]; !c.dense.on || c.dense.heur || !c.dOver || c.dLines != srClampLines {
		t.Errorf("long body: on %v heur %v over %v lines %d", c.dense.on, c.dense.heur, c.dOver, c.dLines)
	}
	if !srHas(x, "... Show more") || srHas(x, "Show less") || srHasPrefix(x, "… Show more") {
		t.Errorf("expand button: %q", x)
	}
	// compactCardBlocks: the three blocks from the match, clipped by the
	// heuristic
	if c := &v.cards[1]; !c.dense.heur {
		t.Error("five blocks: not clipped")
	}
	if srHas(x, "blockA") || srHas(x, "blockC") || !srHas(x, "blockD") || !srHas(x, "blockE") {
		t.Errorf("compacted body: blockA %v blockC %v blockD %v blockE %v", srHas(x, "blockA"), srHas(x, "blockC"), srHas(x, "blockD"), srHas(x, "blockE"))
	}

	// the pills
	kinds := func(i int) []int {
		var out []int
		for _, p := range srPills(&v.cards[i], &v.msgs[i]) {
			out = append(out, p.kind)
		}
		return out
	}
	if !slices.Equal(kinds(2), []int{srPillReactions}) || !slices.Equal(kinds(3), []int{srPillReplies}) ||
		!slices.Equal(kinds(4), []int{srPillAttachments}) || len(kinds(0)) != 0 {
		t.Errorf("pills: %v %v %v %v", kinds(0), kinds(2), kinds(3), kinds(4))
	}
	if p := srPills(&v.cards[2], &v.msgs[2]); p[0].count != 6 || p[0].icon != "reaction" {
		t.Errorf("reactions pill %+v, want 6 and reaction", p[0])
	}
	if p := srPills(&v.cards[3], &v.msgs[3]); p[0].count != 2 || p[0].icon != "comment" {
		t.Errorf("replies pill %+v, want 2 and comment", p[0])
	}
	if v.cards[4].attN != 2 {
		t.Errorf("attachments %d, want 2 (one shown attachment and the file)", v.cards[4].attN)
	}
	// SearchReactionsTooltip's groups: skin tones fold into thumbsup
	gs := srCondense(v.msgs[2].Reactions)
	if len(gs) != 2 || gs[0] != (srReactionGroup{"thumbsup", 3}) || gs[1] != (srReactionGroup{"tada", 3}) {
		t.Errorf("reaction groups %v", gs)
	}
	if mine := srCondense(srOwnReactions(v.msgs[2].Reactions)); len(mine) != 1 || mine[0].name != "thumbsup" {
		t.Errorf("the viewer's reactions %v", mine)
	}
	// the replies tooltip's quote
	if got, want := v.cards[3].reply.preview, "my answer, which runs on well past the sixty characters the…"; got != want {
		t.Errorf("reply preview %q, want %q", got, want)
	}
	if !v.msgs[3].RequestorReplied {
		t.Error("requestor_replied not read")
	}

	// the Attachments card
	v.cards[4].atts.Click()
	d.frame()
	var pop *Popover
	e.h.mu.Lock()
	for _, o := range e.h.shown {
		if p, ok := o.(*Popover); ok {
			pop = p
		}
	}
	e.h.mu.Unlock()
	if pop == nil {
		t.Fatal("no Attachments card")
	}
	px := (&srDrv{size: image.Pt(1280, 800), w: pop}).scan()
	for _, want := range []string{"Attachments", "Design doc", "notes.txt"} {
		if !srHas(px, want) {
			t.Errorf("Attachments card lacks %q: %q", want, px)
		}
	}
	if srHas(px, "Hidden one") {
		t.Error("a hidden attachment is listed")
	}

	// expanded: the whole body, no Show less, taller
	v.cards[1].more.Click()
	d.frame()
	x = d.scan()
	if !srHas(x, "blockA") || srHas(x, "Show less") {
		t.Errorf("five blocks expanded: blockA %v Show less %v", srHas(x, "blockA"), srHas(x, "Show less"))
	}
	h0 := v.cards[0].h
	v.cards[0].more.Click()
	d.frame()
	x = d.scan()
	if !srHas(x, "row12") || srHas(x, "Show less") {
		t.Errorf("long body expanded: row12 %v Show less %v", srHas(x, "row12"), srHas(x, "Show less"))
	}
	if v.cards[0].h <= h0 {
		t.Errorf("expanded card %d px, collapsed %d", v.cards[0].h, h0)
	}
}

// A dogfood user who picked the "original" density (FvhF Ui; Slack dropped
// srp_card_explore, the 2026-11-21 drift, and its groups no longer turn
// the card off): Slack's earlier card, Clack's one-line body and reply
// chip, no pills and no reply fetch.
func TestSearchDensityCardOff(t *testing.T) {
	for _, g := range []string{"control", ""} {
		t.Run(fmt.Sprintf("srp_card_explore %q", g), func(t *testing.T) {
			e := srNewEnv(t, false)
			replies := scHook(e)
			e.h.a.SetExperimentGroup("srp_card_explore", g)
			if !e.h.a.SearchCardExplore() {
				t.Fatal("the dropped srp_card_explore still turns the density card off")
			}
			e.h.a.SetExperimentGroup(core.ExpPersistentTSDogfood, "on")
			if err := e.h.a.SetPref(context.Background(), "search_results_card_density", "original"); err != nil {
				t.Fatal(err)
			}
			if e.h.a.SearchCardExplore() {
				t.Fatal("the density card for a dogfood user who picked original")
			}
			v, d := srView(t, e, fakeslack.SearchWord, false)
			for range 5 {
				d.frame()
				time.Sleep(20 * time.Millisecond)
			}
			x := d.scan()
			// the old body: one line of the rows joined by " … ", cut
			if c := &v.cards[0]; c.lines != 1 || !c.over || c.dToks != nil {
				t.Errorf("off: %d lines, over %v, density tokens %d", c.lines, c.over, len(c.dToks))
			}
			if srHas(x, "... Show more") || !srHasPrefix(x, "… Show more") || srHas(x, "row40") {
				t.Errorf("off: ASCII Show more %v, one-line Show more %v, row40 %v", srHas(x, "... Show more"), srHasPrefix(x, "… Show more"), srHas(x, "row40"))
			}
			for i := range v.cards {
				if !v.cards[i].attR.Empty() || v.cards[i].reply.asked {
					t.Errorf("card %d drew the density card's pills (attachments %v, reply asked %v)", i, v.cards[i].attR, v.cards[i].reply.asked)
				}
			}
			if n := replies.Load(); n != 0 {
				t.Errorf("%d reply preview fetches", n)
			}
		})
	}
}
