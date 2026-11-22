package core

// Wave 7 (exp_wave7.go): the density card's gate (FvhF Ui), the search
// answer's requestor_replied and first highlighted block, and
// SearchRepliesAction's fetch of the viewer's latest reply. Fake Slack
// only.

import (
	"context"
	"net/url"
	"sync"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
)

func TestSearchCardExploreGate(t *testing.T) {
	a, _ := canvasApp(t)
	// every group of the dropped srp_card_explore (the 2026-11-21 drift)
	for _, g := range []string{"on", "control", "off", ""} {
		a.SetExperimentGroup("srp_card_explore", g)
		if !a.SearchCardExplore() {
			t.Errorf("srp_card_explore %q: no density card", g)
		}
	}
	a.ClearExperimentGroup("srp_card_explore")
	// a dogfood user keeps it unless they picked "original"
	a.SetExperimentGroup(ExpPersistentTSDogfood, "on")
	if !a.SearchCardExplore() {
		t.Error("dogfood without a density pref: no density card")
	}
	if err := a.SetPref(context.Background(), "search_results_card_density", "original"); err != nil {
		t.Fatal(err)
	}
	if a.SearchCardExplore() {
		t.Error(`dogfood with search_results_card_density "original": the density card`)
	}
	a.ClearExperimentGroup(ExpPersistentTSDogfood)
	if !a.SearchCardExplore() {
		t.Error("the density pref outside dogfood turned the card off")
	}
}

func TestSearchHitBlockAndRequestorReplied(t *testing.T) {
	a, fs := canvasApp(t)
	ch := fs.ChannelIDs()[0]
	rt := func(s string) map[string]any {
		return map[string]any{"type": "rich_text", "elements": []any{map[string]any{"type": "rich_text_section",
			"elements": []any{map[string]any{"type": "text", "text": s}}}}}
	}
	mark := func(s string) string { return "" + s + "" }
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "search.modules.messages" {
			return nil, false
		}
		msgs := []any{
			// five blocks, the match in the fourth
			map[string]any{"ts": "1700000001.000100", "user": fakeslack.SelfID, "text": "a b c " + mark("zephyr") + " e",
				"blocks": []any{rt("a"), rt("b"), rt("c"), rt(mark("zephyr")), rt("e")}, "requestor_replied": true,
				"reply_count": 2, "thread_ts": "1700000001.000100"},
			// three blocks: compactCardBlocks keeps them whole
			map[string]any{"ts": "1700000002.000100", "user": fakeslack.SelfID, "text": "x",
				"blocks": []any{rt("x"), rt("y"), rt(mark("zephyr"))}},
			// no match marker at all
			map[string]any{"ts": "1700000003.000100", "user": fakeslack.SelfID, "text": "p",
				"blocks": []any{rt("p"), rt("q"), rt("r"), rt("s")}},
		}
		var items []any
		for _, m := range msgs {
			items = append(items, map[string]any{"channel": map[string]any{"id": ch, "name": "general", "is_channel": true},
				"messages": []any{m}})
		}
		return map[string]any{"ok": true, "items": items, "pagination": map[string]any{"total_count": 3, "page": 1, "page_count": 1}}, true
	})
	defer fs.SetHook(nil)
	r, err := a.SearchMessages(context.Background(), SearchOpts{Query: fakeslack.SearchWord})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Messages) != 3 {
		t.Fatalf("%d results", len(r.Messages))
	}
	m0, m1, m2 := r.Messages[0], r.Messages[1], r.Messages[2]
	if m0.SearchHitBlock != 3 || !m0.RequestorReplied {
		t.Errorf("five blocks: hit block %d, requestor_replied %v; want 3 and true", m0.SearchHitBlock, m0.RequestorReplied)
	}
	if m1.SearchHitBlock != 0 || m1.RequestorReplied {
		t.Errorf("three blocks: hit block %d, requestor_replied %v; want 0 and false", m1.SearchHitBlock, m1.RequestorReplied)
	}
	if m2.SearchHitBlock != 0 {
		t.Errorf("no marker: hit block %d, want 0", m2.SearchHitBlock)
	}
	if m0.Text != "a b c zephyr e" {
		t.Errorf("markers left in the text: %q", m0.Text)
	}
}

func TestSearchViewerReply(t *testing.T) {
	a, fs := canvasApp(t)
	ch := fs.ChannelIDs()[0]
	root := "1700000000.000100"
	var mu sync.Mutex
	var forms []url.Values
	fs.SetHook(func(method string, f url.Values) (any, bool) {
		if method != "conversations.replies" || f.Get("ts") != root {
			return nil, false
		}
		mu.Lock()
		forms = append(forms, f)
		mu.Unlock()
		msg := func(ts, user, text string) map[string]any {
			return map[string]any{"type": "message", "ts": ts, "user": user, "text": text, "thread_ts": root}
		}
		if f.Get("latest") == "" {
			// the first page: the root and others' replies, more before
			return map[string]any{"ok": true, "has_more": true, "messages": []any{
				msg(root, "U0000000002", "root"), msg("1700000005.000100", "U0000000002", "theirs"),
				msg("1700000006.000100", "U0000000003", "theirs too")}}, true
		}
		return map[string]any{"ok": true, "has_more": false, "messages": []any{
			msg("1700000002.000100", fakeslack.SelfID, "my first answer"), msg("1700000003.000100", fakeslack.SelfID, "my last answer"),
			msg("1700000004.000100", "U0000000002", "after mine")}}, true
	})
	defer fs.SetHook(nil)
	m, ok, err := a.SearchViewerReply(context.Background(), ch, root)
	if err != nil || !ok {
		t.Fatalf("SearchViewerReply: ok %v, %v", ok, err)
	}
	if m.TS != "1700000003.000100" || m.Text != "my last answer" {
		t.Errorf("the viewer's latest reply: %s %q", m.TS, m.Text)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(forms) != 2 {
		t.Fatalf("%d conversations.replies calls, want 2", len(forms))
	}
	want := []struct{ limit, latest string }{{"100", ""}, {"400", "1700000005.000100"}}
	for i, f := range forms {
		if f.Get("channel") != ch || f.Get("limit") != want[i].limit || f.Get("latest") != want[i].latest ||
			f.Get("reason") != "search-engagement-badge-reply-preview" {
			t.Errorf("call %d: %v", i+1, f)
		}
	}
}
