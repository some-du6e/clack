package messages

import (
	"encoding/json"
	"image"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// A Slackbot custom response (synthetic: subtype slackbot_response from
// USLACKBOT, no bot_profile, icons or username, the shape Hack Club's
// arrive in) is drawn as the team: its name and icon, an info badge with
// Slack's tooltip, and no profile on click. A plain Slackbot message keeps
// Slackbot. With reskin_custom_responses off it's Slackbot again. Either
// way MessageLabels draws no label line for it (meLQ renders none for the
// custom_response type), only the row's #1d9bd11a tint.
func TestCustomResponseAsTeam(t *testing.T) {
	const ch, tsCustom, tsPlain = "C0CRESP", "1700000900.000100", "1700008100.000100" // two hours apart: both headers
	const teamIcon = "https://avatars.invalid/team-88.png"
	app, _ := newTestApp(t)
	app.S.PutConv(slack.Conversation{ID: ch, Name: "faq", IsChannel: true, IsMember: true, NumMembers: 3})
	app.S.SetLatest(ch, []slack.Message{
		{Type: "message", TS: tsCustom, User: tBot, Subtype: core.SubtypeSlackbotResponse, Text: "a synthetic automated answer"},
		{Type: "message", TS: tsPlain, User: tBot, Text: "a plain synthetic Slackbot note"},
	}, false)
	icon, _ := json.Marshal(map[string]any{"type": "team_icon_change", "team_id": "E09V59WQY1E",
		"icon": map[string]any{"image_34": "https://avatars.invalid/team-34.png", "image_88": teamIcon, "image_132": "https://avatars.invalid/team-132.png"}})
	app.Inject(slack.Event{Type: "team_icon_change", Raw: icon})

	h := &profileHost{coreHost: coreHost{app: app, th: theme.New(false)}}
	v := NewChannelView(h, ch)
	v.Preview = true
	var r input.Router
	ops := new(op.Ops)
	now := time.Now()
	frame := func() {
		ops.Reset()
		now = now.Add(20 * time.Millisecond)
		gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Exact(image.Pt(900, 700)), Now: now, Source: r.Source()}
		v.Layout(gtx)
		r.Frame(ops)
	}
	until(t, 5*time.Second, "both rows", frame, func() bool {
		return v.s.rows[tsCustom] != nil && v.s.rows[tsPlain] != nil && v.s.rows[tsCustom].avDrawn != ""
	})
	cr, pr := v.s.rows[tsCustom], v.s.rows[tsPlain]
	if !cr.custom || cr.senderName() != "Synthetic Club" || cr.nameShown != "Synthetic Club" {
		t.Fatalf("custom response: custom=%v name %q shown %q, want the team's name", cr.custom, cr.senderName(), cr.nameShown)
	}
	if cr.avDrawn != teamIcon {
		t.Errorf("custom response avatar %q, want the team icon (image_88 for 36px)", cr.avDrawn)
	}
	if pr.custom || pr.avDrawn == teamIcon || pr.senderName() == "Synthetic Club" {
		t.Errorf("plain Slackbot message drawn as the team: custom=%v avatar %q name %q", pr.custom, pr.avDrawn, pr.senderName())
	}
	if cr.col.Y != pr.col.Y {
		t.Errorf("the custom response's body starts at %d, the plain message's at %d: Slack draws no label line", cr.col.Y, pr.col.Y)
	}
	if tint := colorsFor(h.th).CustomTint; cr.bgDrawn != tint || pr.bgDrawn == tint {
		t.Errorf("backgrounds: custom response %v, plain %v; want only the custom response tinted %v", cr.bgDrawn, pr.bgDrawn, tint)
	}

	// the header line of a row: its list position
	rowY := func(ts string) int {
		for i := v.s.visible[0]; i < v.s.visible[1] && i < len(v.s.items); i++ {
			if it := v.s.items[i]; it.kind == itMsg && it.key == ts {
				return v.s.pos[i]
			}
		}
		t.Fatalf("row %s not visible", ts)
		return 0
	}
	var evT time.Duration
	clickAt := func(p f32.Point) {
		evT += 30 * time.Millisecond
		r.Queue(
			pointer.Event{Kind: pointer.Move, Position: p, Source: pointer.Mouse, Time: evT},
			pointer.Event{Kind: pointer.Press, Position: p, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Time: evT + time.Millisecond},
			pointer.Event{Kind: pointer.Release, Position: p, Source: pointer.Mouse, Time: evT + 2*time.Millisecond},
		)
		for k := 0; k < 4; k++ {
			frame()
		}
	}
	// control: the plain Slackbot name opens Slackbot's profile
	for _, dy := range []int{8, 12, 16, 20} {
		clickAt(f32.Pt(70, float32(rowY(tsPlain)+dy)))
		if h.count() > 0 {
			break
		}
	}
	if h.count() == 0 {
		t.Fatal("control: clicking the plain Slackbot name opened no profile (coordinates are off)")
	}
	before := h.count()
	y := rowY(tsCustom)
	for _, dy := range []int{8, 12, 16, 20} {
		clickAt(f32.Pt(70, float32(y+dy))) // name
		clickAt(f32.Pt(38, float32(y+dy))) // avatar
	}
	if h.count() != before {
		t.Error("clicking the custom response's name or team icon opened a profile; Slack's are not interactive")
	}

	// hover along the name line until the info badge reports it
	hovered := false
	for x := 70; x < 400 && !hovered; x += 3 {
		for _, dy := range []int{11, 14} {
			evT += 10 * time.Millisecond
			r.Queue(pointer.Event{Kind: pointer.Move, Position: f32.Pt(float32(x), float32(y+dy)), Source: pointer.Mouse, Time: evT})
			frame()
			frame()
			if cr.crHovered {
				hovered = true
				break
			}
		}
	}
	if !hovered {
		t.Fatal("never hovered the info badge after the name")
	}
	if cr.crTip.Showing(now) {
		t.Error("the tooltip showed at once; Slack waits 150 ms")
	}
	for k := 0; k < 9; k++ { // 180 ms
		frame()
	}
	if !cr.crTip.Showing(now) {
		t.Error("the badge tooltip isn't up after 180 ms")
	}
	if want := "This automated message was set up by someone in your workspace."; cr.crTipText != want {
		t.Errorf("tooltip %q, want %q", cr.crTipText, want)
	}

	// reskin off: Slackbot again (BringBackSlackbot forces this)
	app.SetExperimentGroup("reskin_custom_responses", "off")
	until(t, 3*time.Second, "Slackbot's look back", frame, func() bool { return !cr.custom && cr.avDrawn != teamIcon })
	if strings.Contains(cr.nameShown, "Synthetic Club") {
		t.Errorf("reskin off: the name is still the team's (%q)", cr.nameShown)
	}
	for k := 0; k < 3; k++ {
		frame()
	}
	if cr.col.Y != pr.col.Y {
		t.Errorf("reskin off: the custom response's body starts at %d, the plain message's at %d: no \"Custom response\" label in Slack", cr.col.Y, pr.col.Y)
	}
	if tint := colorsFor(h.th).CustomTint; cr.bgDrawn != tint {
		t.Errorf("reskin off: the custom response's background is %v, want the tint %v", cr.bgDrawn, tint)
	}
}

func TestTeamInitials(t *testing.T) {
	for in, want := range map[string]string{"Hack Club": "HC", "synthetic": "s", "": "", "  A  b c": "Ab", "(x) y": "y"} {
		if got := teamInitials(in); got != want {
			t.Errorf("teamInitials(%q) = %q, want %q", in, got, want)
		}
	}
}
