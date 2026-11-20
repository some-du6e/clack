package richview

import (
	"encoding/json"
	"testing"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// TestShortcutCardLayout: a share_shortcut card lays out as Slack's
// WorkflowEntity (boot-shared KrN7 on VLgN): 420 wide, radius 12; the
// 12px-padded header with the 36px icon and the title 12px right of it,
// centred on the row; the description 16px in under it; the primary
// Start Workflow button 16px inside the bottom-left corner, under 64px
// of padding. The card opens the details, the button trips the link, the
// hover bar's ⋮ opens More actions.
func TestShortcutCardLayout(t *testing.T) {
	st := newTestStyle()
	d := kitDoc(t, `[{"type":"share_shortcut","function_trigger_id":"Ft0001","title":"Alpha","description":"Bravo",
		"trigger_type":"shortcut","url":"https://slack.com/shortcuts/Ft0001/x","workflow_id":"Wf1","share_url":"https://slack.com/shortcuts/Ft0001/s","bot_user_id":"U1"}]`)
	v := layoutAt(st, d, Options{}, 800)
	var card *box
	for i := range v.d.boxes {
		if b := &v.d.boxes[i]; b.kind == bBorder && b.radius == 12 && b.r.Dx() == 420 && b.fill == st.Palette.Panel {
			card = b
			break
		}
	}
	if card == nil {
		t.Fatalf("no 420px workflow card; boxes %+v", v.d.boxes)
	}
	a, b := findFrag(t, &v.d, "Alpha"), findFrag(t, &v.d, "Bravo")
	// header: 1 + 12 padding; the 36px row; the 22px title centred on it
	if int(a.x) != 1+12+36+12 || a.top != card.r.Min.Y+13+7 {
		t.Errorf("title at (%.0f, %d), want (61, %d)", a.x, a.top, card.r.Min.Y+20)
	}
	if want := card.r.Min.Y + 13 + 36 + 12; int(b.x) != 17 || b.top != want {
		t.Errorf("description at (%.0f, %d), want (17, %d)", b.x, b.top, want)
	}
	if want := b.top + 23 + 64 + 1; card.r.Max.Y != want {
		t.Errorf("card bottom %d, want %d (the description's 22.5px line, 64px for the button, the border)", card.r.Max.Y, want)
	}
	var btn *box
	for i := range v.d.boxes {
		if bx := &v.d.boxes[i]; bx.kind == bFill && bx.fill == st.Palette.Primary && bx.r.Dy() == 36 {
			btn = bx
		}
	}
	if btn == nil || btn.r.Min.X != 17 || btn.r.Max.Y != card.r.Max.Y-17 || !hasFrag(&v.d, "Start") {
		t.Fatalf("Start Workflow button %+v, want 36px at x 17, 16px over the card's bottom %d", btn, card.r.Max.Y)
	}
	for _, c := range []struct{ act, want string }{
		{"open", "open Ft0001 https://slack.com/shortcuts/Ft0001/x"},
		{"start", "start Ft0001 https://slack.com/shortcuts/Ft0001/x"},
		{"more", "more Ft0001 https://slack.com/shortcuts/Ft0001/x"},
	} {
		got, ok := workflowAct(v, c.act)
		if !ok || got != c.want {
			t.Errorf("%s: got %q (target %v), want %q", c.act, got, ok, c.want)
		}
	}

	// a bot still being fetched: the loading card, which still opens the
	// details and has its actions bar (KrN7: interactive but when deleted)
	m := slack.Message{TS: "1.0", Text: "x", Blocks: json.RawMessage(`[{"type":"share_shortcut","function_trigger_id":"Ft0002","title":"Soon",
		"trigger_type":"shortcut","url":"https://slack.com/shortcuts/Ft0002/x","bot_user_id":"U0BOT00009"}]`)}
	v = layoutAt(st, rt.FromMessage(fetchingRes{}, &m), Options{}, 800)
	if hasFrag(&v.d, "Soon") {
		t.Error("the loading card drew its title (want the skeletons)")
	}
	for _, act := range []string{"open", "more"} {
		if _, ok := workflowAct(v, act); !ok {
			t.Errorf("the loading card has no %q target", act)
		}
	}
	// no bot at all: a non-existent member, the enabled card
	d = kitDoc(t, `[{"type":"share_shortcut","function_trigger_id":"Ft0003","title":"Here","trigger_type":"shortcut","url":"https://slack.com/shortcuts/Ft0003/x"}]`)
	v = layoutAt(st, d, Options{}, 800)
	if !hasFrag(&v.d, "Here") || !hasFrag(&v.d, "Start") {
		t.Errorf("a card with no bot: %q, want the enabled card with Start Workflow", fragStrings(&v.d))
	}
}

// fetchingRes is codeRes whose members are all still being fetched.
type fetchingRes struct{ codeRes }

func (fetchingRes) MemberDeleted(string) (bool, bool) { return false, false }
