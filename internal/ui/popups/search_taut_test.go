package popups

import (
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
)

// Taut's Censorship in search results (popups-taut): the results page
// hands its page to the Redux mirror as Slack's store keeps it
// (searchResults.main.response.results.messages.items[page]), and each
// card takes its MessageListItem result back from the slice as plugins
// present it, so patchMessageText's mask (its path
// response.results.messages.items[*][*].messages[*]) shows on the card as
// in Slack. The DMs tab's rows sit under the dms module, which the
// plugin's path doesn't cover, as in Slack.
func TestTautCensorshipSearch(t *testing.T) {
	e := tzNewEnv(t)
	e.load(t, "Censorship") // its default terms: job, employment
	tzUntil(t, 10*time.Second, "searchResults patched", func() bool { return e.tt.SlicePatched("searchResults") })

	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(800, 600))}
	th := e.h.Theme()
	cl := colorsOf(th)
	res := func(text string) core.SearchResult {
		return core.SearchResult{Messages: []core.Message{
			{Channel: tzGen, TS: "1700000010.000100", User: "UALICE", Text: text},
			{Channel: tzGen, TS: "1700000011.000100", User: "UBOB", Text: "nothing to hide here"},
		}, Total: 2, Page: 1, Pages: 1}
	}

	v := NewSearchView(e.h, "openings")
	v.addMessages(res("any job openings for the employment fair?"), 1)
	tzUntil(t, 10*time.Second, "the first card masked", func() bool {
		gtx.Ops.Reset()
		v.card(gtx, th, cl, 0, 760)
		v.card(gtx, th, cl, 1, 760)
		return v.cards[0].ovText != ""
	})
	c := v.cards[0]
	if strings.Contains(c.ovText, "job") || strings.Contains(c.ovText, "employment") || !strings.Contains(c.ovText, "***") {
		t.Errorf("card text %q, want the terms masked", c.ovText)
	}
	var flat strings.Builder
	for _, sg := range c.flat {
		flat.WriteString(sg.Text)
	}
	if f := flat.String(); strings.Contains(f, "job") || !strings.Contains(f, "openings") {
		t.Errorf("the card draws %q", f)
	}
	if v.cards[1].ovText != "" {
		t.Errorf("the second card took an override (%q) with nothing to mask", v.cards[1].ovText)
	}
	if r := v.storeResult(0); r == nil {
		t.Error("no store row for the first card")
	}

	// the DMs tab: results.dms, outside the plugin's path, stay as sent
	dm := NewSearchView(e.h, "openings&module=dms")
	dm.addMessages(res("a job in a DM"), 1)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		Pump()
		gtx.Ops.Reset()
		dm.card(gtx, th, cl, 0, 760)
		time.Sleep(20 * time.Millisecond)
	}
	if got := dm.cards[0].ovText; got != "" {
		t.Errorf("the DMs tab's card was masked (%q): Slack's patch path covers the messages module only", got)
	}
}
