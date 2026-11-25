package richview

import (
	"strings"
	"testing"
	"time"

	rt "github.com/some-du6e/clack/internal/richtext"
)

// Synthetic fixtures only.

// spHandler records share_prompt sends.
type spHandler struct {
	recHandler
	sent []string
}

func (h *spHandler) SharePromptSend(p *rt.SharePrompt) { h.sent = append(h.sent, p.PromptID) }

func promptDoc(p rt.SharePrompt) rt.Doc {
	return doc(rt.Block{Kind: rt.SharePromptBlock, Kit: true, BlockID: "sp", Prompt: &p})
}

var pipelinePrompt = rt.SharePrompt{Label: "Weekly pipeline", Prompt: "Summarise this week's pipeline changes", PromptID: "P1",
	BotUserID: "UAF", AgentName: "Pipeline Agent", AgentAvatar: "https://img.test/a.png", Shown: true}

// hasWord reports whether a text frag of v reads s.
func hasWord(v *View, s string) bool {
	for _, f := range textFrags(&v.d) {
		if f.sh.str == s {
			return true
		}
	}
	return false
}

// SharePromptButton's card: 426px at most outside canvases (the view's
// width in one), the label over "Prompt" in the header, the prompt, and
// Send to {agentName} 12px under it inside the card. A prompt Slack
// wouldn't draw (Shown false) draws nothing and has no margins.
func TestSharePromptLayout(t *testing.T) {
	st := newTestStyle()
	v := layoutAt(st, promptDoc(pipelinePrompt), Options{}, 600)
	rs := v.SharePromptRects()
	if len(rs) != 1 {
		t.Fatalf("%d share prompts", len(rs))
	}
	r := rs[0]
	if r.Card.Dx() != 426 {
		t.Errorf("card %v: want 426 wide (.promptCardMaxWidth)", r.Card)
	}
	if !r.Send.In(r.Card) || r.Send.Dy() != 36 || r.Send.Min.X != r.Card.Min.X+12 || r.Card.Max.Y-r.Send.Max.Y != 12 {
		t.Errorf("Send %v in card %v: want a 36px button 12px in from the left and the bottom", r.Send, r.Card)
	}
	for _, w := range []string{"Weekly", "Prompt", "Summarise", "Send", "Pipeline", "Agent"} {
		if !hasWord(v, w) {
			t.Errorf("no %q", w)
		}
	}
	if v := layoutAt(st, promptDoc(pipelinePrompt), Options{Canvas: true}, 600); len(v.SharePromptRects()) != 1 || v.SharePromptRects()[0].Card.Dx() <= 426 {
		t.Errorf("in a canvas: card %v, want past 426 (no max-width there)", v.SharePromptRects())
	}
	hidden := pipelinePrompt
	hidden.Shown = false
	if v := layoutAt(st, promptDoc(hidden), Options{}, 600); len(v.SharePromptRects()) != 0 || v.d.size.Y != 0 {
		t.Errorf("not shown: %d cards, height %d; want nothing", len(v.SharePromptRects()), v.d.size.Y)
	}
	// with no prompt there's no preview: Send sits 12px under the header's
	// 12px padding and 40px tile, plus the body's 12px
	empty := pipelinePrompt
	empty.Prompt = ""
	v = layoutAt(st, promptDoc(empty), Options{}, 600)
	if rs := v.SharePromptRects(); len(rs) != 1 || rs[0].Send.Min.Y-rs[0].Card.Min.Y != 12+40+12+12 {
		t.Errorf("no prompt: %v, want Send 76px under the card's top", rs)
	}
}

// Send to {agentName} is the handler's SharePromptSend.
func TestSharePromptSend(t *testing.T) {
	h := &spHandler{}
	hs := newHarness(promptDoc(pipelinePrompt), h)
	hs.w = 600
	hs.frame()
	rs := hs.v.SharePromptRects()
	if len(rs) != 1 {
		t.Fatalf("%d share prompts", len(rs))
	}
	hs.clickRect(rs[0].Send, 0)
	if strings.Join(h.sent, ",") != "P1" {
		t.Errorf("sent %q, want [P1]", h.sent)
	}
}

// A plain prompt is TruncatedExpandableText's: three lines and "Show
// more" under them when it runs over; expanded, all of it and "Less"
// after it, which folds it again. A prompt that fits has neither.
func TestSharePromptShowMore(t *testing.T) {
	long := pipelinePrompt
	long.Prompt = strings.Repeat("Summarise this week's pipeline changes by region and owner. ", 12) + "Finally"
	hs := newHarness(promptDoc(long), &spHandler{})
	hs.w = 600
	hs.frame()
	if !hasWord(hs.v, "Show") || hasWord(hs.v, "Finally") || hasWord(hs.v, "Less") {
		t.Fatalf("folded: want Show more and the text cut before its last word")
	}
	h0 := hs.v.SharePromptRects()[0].Card.Dy()
	hs.clickRect(fragRect(findFrag(t, &hs.v.d, "Show")), 0)
	hs.frame()
	if hasWord(hs.v, "Show") || !hasWord(hs.v, "Finally") || !hasWord(hs.v, "Less") {
		t.Fatalf("expanded: want every word and Less, no Show more")
	}
	if h1 := hs.v.SharePromptRects()[0].Card.Dy(); h1 <= h0 {
		t.Errorf("expanded card %d tall, folded %d", h1, h0)
	}
	hs.clickRect(fragRect(findFrag(t, &hs.v.d, "Less")), time.Second)
	hs.frame()
	if !hasWord(hs.v, "Show") || hasWord(hs.v, "Less") {
		t.Errorf("after Less: want it folded again")
	}
	if v := layoutAt(newTestStyle(), promptDoc(pipelinePrompt), Options{}, 600); hasWord(v, "Show") || hasWord(v, "Less") {
		t.Errorf("a prompt that fits shows Show more or Less")
	}
}

// prompt_rich_text draws instead of the prompt, cut as TruncatedBlocks
// cuts it (150 characters) with " Show more"; expanded, "Show less".
func TestSharePromptRichText(t *testing.T) {
	rich := pipelinePrompt
	words := strings.Repeat("Summarise the pipeline. ", 8) + "Finally"
	rich.Rich = []rt.Block{{Kind: rt.Paragraph, Spans: []rt.Span{{Kind: rt.Text, Text: words}}}}
	hs := newHarness(promptDoc(rich), &spHandler{})
	hs.w = 600
	hs.frame()
	if !hasWord(hs.v, "Show") || hasWord(hs.v, "Finally") {
		t.Fatalf("rich text over 150 characters: want it cut with Show more")
	}
	hs.clickRect(fragRect(findFrag(t, &hs.v.d, "Show")), 0)
	hs.frame()
	if !hasWord(hs.v, "Finally") || !hasWord(hs.v, "less") {
		t.Errorf("expanded: want every word and Show less")
	}
}
