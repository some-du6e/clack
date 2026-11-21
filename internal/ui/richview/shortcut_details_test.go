package richview

// The workflow card's details against Slack's styles (client-boot-styles
// and Slack Kit's Panda recipes, build 031dbe6a7400; boot-shared KrN7,
// VLgN, b2Ud, OClq, JYoX, uJed):
//
//   - the byline is WorkflowTriggerTypeLabel's span
//     (.p-workflow_trigger_type_label: content-sec, over StackedLabels'
//     tertiary); the deleted card's caption stays tertiary;
//   - the loading card's skeletons are LoadingBox (surface.secondary),
//     the 86px content one clipped by the card's overflow: hidden;
//   - the "Uses AI" badge sits 20px after the wider label (the header's
//     12px column gap, .badge's 8px margin), in every state;
//   - the description is a text node without pre-wrap: newlines collapse;
//   - the ⋮ turns content-pry while it is hovered (ActionButton's _hover),
//     and its menu opens from the button;
//   - withHover in every state: the deleted card's otl-sec border while
//     hovered (it has no click).
//
// Synthetic blocks.

import (
	"encoding/json"
	"image"
	"image/color"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

func TestShortcutCardDetails(t *testing.T) {
	st := newTestStyle()
	tok := func(name string) color.NRGBA { return (&lay{st: st}).tok(name) }

	// an event trigger with AI steps: the byline, the badge, the description
	d := kitDoc(t, `[{"type":"share_shortcut","function_trigger_id":"Ft0SHORT04","title":"Thanks","description":"one\ntwo",
		"trigger_type":"event","trigger_subtype":"reaction_added","contains_ai_steps":true,"workflow_id":"Wf4"}]`)
	v := layoutAt(st, d, Options{}, 800)
	by := findFrag(t, &v.d, "reaction")
	if by.col != tok("content-sec") {
		t.Errorf("byline color %v, want content-sec %v", by.col, tok("content-sec"))
	}
	end := 0 // the labels' right edge: the byline's, the wider one here
	for _, f := range textFrags(&v.d) {
		if f.top == by.top {
			end = max(end, int(f.x+f.w+0.999))
		}
	}
	if sp := imgsWithURL(&v.d, iconURL+"sparkle"); len(sp) != 1 || sp[0].r.Min.X < end+19 || sp[0].r.Min.X > end+21 {
		t.Errorf("Uses AI badge %+v, want one 20px after the labels' end %d", sp, end)
	}
	one, two := findFrag(t, &v.d, "one"), findFrag(t, &v.d, "two")
	if one.top != two.top {
		t.Errorf("the description's newline broke the line (one at %d, two at %d)", one.top, two.top)
	}

	// the ⋮: its glyph again, content-pry, in a layer shown while it is
	// hovered; its menu's anchor is the button
	var more int16 = -1
	for i, tg := range v.d.targets {
		if tg.kind == tWorkflowCard && tg.id == "more" {
			more = int16(i)
		}
	}
	if more < 0 {
		t.Fatal("no more target")
	}
	var btn image.Rectangle
	for _, h := range v.d.hits {
		if h.t == more {
			btn = h.r
		}
	}
	pry := false
	for _, im := range imgsWithURL(&v.d, iconURL+"ellipsis-vertical-filled") {
		if im.tint == tok("content-pry") && im.hov > 0 && v.d.hovers[im.hov-1].shown(more) && !v.d.hovers[im.hov-1].shown(more-1) {
			pry = true
		}
	}
	if !pry {
		t.Error("no content-pry ⋮ shown while the ⋮ (only) is hovered")
	}
	if r, ok := workflowActRect(v, "more", image.Pt(5, 5)); !ok || r != btn {
		t.Errorf("More actions anchored at %v, want the ⋮ button %v", r, btn)
	}

	// loading: surface.secondary skeletons, the content one rounded where
	// the card's bottom corners clip it; the badge in this state too
	m := slack.Message{TS: "1.0", Text: "x", Blocks: json.RawMessage(`[{"type":"share_shortcut","function_trigger_id":"Ft0SHORT05","title":"Soon",
		"trigger_type":"shortcut","url":"https://slack.com/shortcuts/Ft0SHORT05/x","bot_user_id":"U0BOT00009","contains_ai_steps":true}]`)}
	v = layoutAt(st, rt.FromMessage(fetchingRes{}, &m), Options{}, 800)
	skel, content := 0, false
	for _, b := range v.d.boxes {
		if b.kind != bFill {
			continue
		}
		if b.fill == tok("base-ter") {
			t.Errorf("a base-ter skeleton %v (LoadingBox is surface.secondary)", b.r)
		}
		if b.fill == tok("surf-sec") && b.r.Dy() != 36 {
			skel++
			if b.r.Dy() == 86 {
				content = b.perCorner && b.corners[0] == 0 && b.corners[1] == 0 && b.corners[2] == 11 && b.corners[3] == 11
			}
		}
	}
	if skel != 4 || !content {
		t.Errorf("%d surface.secondary skeletons (want 3 labels and the content), content clipped to the corners %v", skel, content)
	}
	if sp := imgsWithURL(&v.d, iconURL+"sparkle"); len(sp) != 1 {
		t.Errorf("the loading card's Uses AI badge: %+v", sp)
	}

	// deleted: the caption stays tertiary
	m = slack.Message{TS: "1.0", Text: "x", Blocks: json.RawMessage(`[{"type":"share_shortcut","function_trigger_id":"Ft0SHORT06","title":"Gone",
		"trigger_type":"shortcut","url":"https://slack.com/shortcuts/Ft0SHORT06/x","bot_user_id":"U0BOTDELE1"}]`)}
	v = layoutAt(st, rt.FromMessage(deletedBotRes{}, &m), Options{}, 800)
	if f := findFrag(t, &v.d, "exists."); f.col != tok("content-ter") {
		t.Errorf("deleted caption color %v, want content-ter %v", f.col, tok("content-ter"))
	}
	// withHover in every state: the deleted card's otl-sec border while
	// hovered, and no click
	hovered := false
	for i, tg := range v.d.targets {
		if tg.kind != tWorkflowCard {
			continue
		}
		if !tg.menuOnly {
			t.Errorf("the deleted card has a clickable %q target", tg.id)
			continue
		}
		for _, b := range v.d.boxes {
			if b.kind == bBorder && b.stroke == tok("otl-sec") && b.hov > 0 && v.d.hovers[b.hov-1].shown(int16(i)) {
				hovered = true
			}
		}
	}
	if !hovered {
		t.Error("no otl-sec border shown while the deleted card is hovered")
	}
}

// deletedBotRes is codeRes whose members are all deleted.
type deletedBotRes struct{ codeRes }

func (deletedBotRes) MemberDeleted(string) (bool, bool) { return true, true }
