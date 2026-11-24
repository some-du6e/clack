package blockkit

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// Synthetic fixtures only.

// splitEnv shows a message whose one actions block holds split_button el,
// as a gallery message of the fake's app (its channel and ts).
func splitEnv(t *testing.T, el map[string]any) *bkEnv {
	t.Helper()
	e := newBKEnv(t, false, false)
	base, ok := e.msgs["overflow"]
	if !ok {
		t.Fatal("no overflow gallery message to borrow")
	}
	raw, _ := json.Marshal([]any{map[string]any{"type": "actions", "block_id": "bk_split", "elements": []any{el}}})
	sm := slack.Message{TS: base.TS, User: base.User, BotID: base.BotID, Blocks: raw}
	m := base
	m.Blocks, m.Doc = raw, richtext.FromMessage(nilResolver{}, &sm)
	e.msgs["split"] = m
	e.show("split")
	return e
}

func splitDeploy() map[string]any {
	pt := func(s string) map[string]any { return map[string]any{"type": "plain_text", "text": s} }
	return map[string]any{"type": "split_button", "action_id": "deploy", "style": "primary",
		"primary_action": map[string]any{"text": pt("Deploy"), "value": "deploy-prod"},
		"secondary_actions": []any{
			map[string]any{"text": pt("Deploy to staging"), "value": "deploy-staging"},
			map[string]any{"text": pt("Roll back"), "value": "rollback"},
		}}
}

// Slack's SplitButtonElementBase (wAPv) with bk_refresh_split_button off,
// as on the accounts Clack follows: the primary action's Button alone. A
// click sends {type: split_button, action_id, block_id, text, value}; a
// second within a second of it sends nothing; after that one sends.
func TestSplitButtonPrimary(t *testing.T) {
	e := splitEnv(t, splitDeploy())
	// (no index past what decoded: a panic would end the package's tests)
	if bs := e.msgs["split"].Doc.Blocks; len(bs) != 1 || len(bs[0].Buttons) != 1 || bs[0].Buttons[0].Kind != "button" ||
		bs[0].Buttons[0].Text != "Deploy" || bs[0].Buttons[0].Style != "primary" {
		t.Errorf("richtext's blocks %+v, want one actions block with the primary's button", bs)
	}
	w, r := e.widget("bk_split", "deploy")
	if w.splitOn() {
		t.Fatal("bk_refresh_split_button is on in the fake: want the account's (off)")
	}
	if r.Dy() != 28 {
		t.Errorf("the Button is %dpx tall, want c-button--small's 28", r.Dy())
	}
	if w.more.sz != (image.Point{}) {
		t.Errorf("with bk_refresh_split_button off a chevron (%v) was drawn: want the primary's Button alone", w.more.sz)
	}
	want := map[string]any{"type": "split_button", "block_id": "bk_split", "action_id": "deploy",
		"text": map[string]any{"type": "plain_text", "text": "Deploy"}, "value": "deploy-prod"}
	pressed := time.Now()
	e.click(center(r))
	sameJSON(t, "the primary", e.calls(1)[0].Actions[0], want)
	// the action's answer back, so only the press's one-second hold can
	// refuse the next click (the block isn't loading)
	e.idle("bk_split")
	if time.Since(pressed) < 700*time.Millisecond {
		e.click(center(r))
		e.noNewCall(1, 300*time.Millisecond, "a second click within a second")
	} else {
		t.Logf("the answer took %v: the in-second click isn't checked", time.Since(pressed))
	}
	e.settle(800 * time.Millisecond)
	e.click(center(r))
	sameJSON(t, "the primary after a second", e.calls(2)[1].Actions[0], want)
}

// With bk_refresh_split_button on: the caret-down chevron beside the
// Button (one ButtonGroup, as wide as both) opens a Menu of the secondary
// actions; one picked sends its own text and value. The chevron's label is
// "More options for {primary_text}".
func TestSplitButtonMenu(t *testing.T) {
	e := splitEnv(t, splitDeploy())
	_, off := e.widget("bk_split", "deploy")
	e.h.Core().SetExperimentGroup("bk_refresh_split_button", "on")
	e.frame()
	e.frame()
	w, r := e.widget("bk_split", "deploy")
	if !w.splitOn() {
		t.Fatal("bk_refresh_split_button didn't turn on")
	}
	// the chevron's square, as tall as the Button, at its right (a tap's
	// rect() is where it was at its last click: none yet)
	if w.more.sz != image.Pt(r.Dy(), r.Dy()) {
		t.Fatalf("chevron %v beside the Button %v: want a square as tall", w.more.sz, r)
	}
	chev := image.Rect(r.Max.X, r.Min.Y, r.Max.X+r.Dy(), r.Max.Y)
	if r.Dx() != off.Dx() {
		t.Errorf("the Button is %dpx wide with the chevron, %dpx without: want the same", r.Dx(), off.Dx())
	}
	if got := splitMoreLabel("Deploy"); got != "More options for Deploy" {
		t.Errorf("the chevron's label %q", got)
	}
	e.click(center(chev))
	menu, ok := topOverlay[*popups.Menu](e)
	if !ok {
		t.Fatal("the chevron opened no menu")
	}
	var labels []string
	for _, it := range menu.Items() {
		labels = append(labels, it.Label)
	}
	if strings.Join(labels, "|") != "Deploy to staging|Roll back" {
		t.Fatalf("menu items %v, want the secondary actions", labels)
	}
	menu.Items()[1].Do()
	sameJSON(t, "a secondary action", e.calls(1)[0].Actions[0], map[string]any{"type": "split_button", "block_id": "bk_split", "action_id": "deploy",
		"text": map[string]any{"type": "plain_text", "text": "Roll back"}, "value": "rollback"})
	e.closeOverlays()
}

// disabled is aria-disabled: no click sends; is_loading draws the group's
// box as one LoadingButton that takes no click either.
func TestSplitButtonDisabledLoading(t *testing.T) {
	for _, k := range []string{"disabled", "is_loading"} {
		el := splitDeploy()
		el[k] = true
		e := splitEnv(t, el)
		_, r := e.widget("bk_split", "deploy")
		e.click(center(r))
		e.noNewCall(0, 300*time.Millisecond, k+": a click")
	}
}

// split_button's typed fields share their keys with other payloads: one
// of another type (a string is_loading, a number size, a list where an
// action goes) leaves that field unset instead of failing the element,
// which took its actions block's other elements with it, or as an
// accessory its whole block.
func TestSplitButtonFieldsTolerant(t *testing.T) {
	bad := `{"type":"split_button","action_id":"s","is_loading":"yes","size":3,"disabled":"no",` +
		`"primary_action":{"text":{"type":"plain_text","text":"Go"},"value":"g"},"secondary_actions":{"x":1}}`
	raw := `[{"type":"actions","block_id":"a","elements":[` + bad + `,` +
		`{"type":"button","action_id":"b","text":{"type":"plain_text","text":"Other"}}]},` +
		`{"type":"section","block_id":"acc","text":{"type":"mrkdwn","text":"with it"},"accessory":` + bad + `},` +
		`{"type":"section","block_id":"after","text":{"type":"mrkdwn","text":"after"}}]`
	bs := ParseBlocks(json.RawMessage(raw))
	if len(bs) != 3 || bs[0].BlockID != "a" || bs[1].BlockID != "acc" || bs[2].BlockID != "after" {
		t.Fatalf("blocks %+v, want a, acc and after", bs)
	}
	if len(bs[0].Elements) != 2 || bs[0].Elements[1].ActionID != "b" {
		t.Fatalf("the actions block's elements %+v, want the split button and the button", bs[0].Elements)
	}
	for _, sb := range []*Element{&bs[0].Elements[0], bs[1].Accessory} {
		if sb == nil || sb.Type != "split_button" || sb.IsLoading || sb.Size != "" || sb.Disabled || sb.SecondaryActions != nil ||
			sb.PrimaryAction == nil || textOf(sb.PrimaryAction.Text) != "Go" {
			t.Errorf("the mistyped split_button decoded as %+v, want its primary action and the rest unset", sb)
		}
	}
}
