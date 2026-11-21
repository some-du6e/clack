package messages

// Taut's OwlMode itself (TAUT_PLUGINS_DIR: messages-taut and flake-check's
// owl-save job build it at TAUT_PIN) under (demo) Owl's confirmation in its
// DM, Karim's α.113 report: "Confession #40459 submitted and awaiting
// review. Your Slack account will be recognized through its salted hash."
// The plugin's MessageBlocks patch adds Save in Taut after the blocks: a
// Block Kit-style actions row around Slack's small outline Button (an
// icon and its label, display:flex with a 6px gap, the demo Owl's red).
// In Slack the button is 28px tall and as wide as its content: 1px
// border, 8px sides (.p-block_kit_button_element), the 16px icon, 6px,
// the label. α.113 drew it across the message, the icon on a line above
// the label, in Clack's grey.

import (
	"encoding/json"
	"image"
	"regexp"
	"strconv"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

var owlButtonShape = regexp.MustCompile(`@Button\(([a-z-]+) (-?\d+),(-?\d+) (\d+)x(\d+)\)`)

func TestPluginOwlModeSaveButton(t *testing.T) {
	code := pluginFile(t, "OwlMode")
	app, _, tt, u, h := pluginEnv(t)
	const dm, ts, owlBot = "D0OWLDEMO1", "1700000200.000100", "B0OWLDEMO1"
	const text = "Confession #40459 submitted and awaiting review. Your Slack account will be recognized through its salted hash. No reply key is needed."
	blocks, _ := json.Marshal([]any{map[string]any{"type": "section", "block_id": "owl1",
		"text": map[string]any{"type": "mrkdwn", "text": text}}})
	app.S.PutConv(slack.Conversation{ID: dm, IsIM: true, User: "U0OWLDEMO1", IsMember: true})
	app.S.SetLatest(dm, []slack.Message{{Type: "message", Subtype: "bot_message", TS: ts, BotID: owlBot, Username: "(demo) Owl",
		Text: text, Blocks: blocks, BotProfile: &slack.BotProfile{ID: owlBot, Name: "(demo) Owl", AppID: "A0CDBA8FXDL"}}}, false)
	install(t, tt, code)
	until(t, 10*time.Second, "OwlMode patches MessageBlocks", func() {}, func() bool { return slackc.Patched("MessageBlocks") })
	v := NewChannelView(h, dm)
	v.Preview = true
	const w, hgt = 760, 520
	ops := new(op.Ops)
	frame := func() { shotFrame(v, h.th, w, hgt, ops) }
	until(t, 15*time.Second, "Save in Taut under owl's confirmation", frame, func() bool {
		return u.anyTree("taut-owl-save__button") && v.s.rows[ts] != nil
	})
	var m []string
	until(t, 5*time.Second, "the button laid out", frame, func() bool {
		m = owlButtonShape.FindStringSubmatch(slackc.LayoutShape("MessageBlocks"))
		return m != nil
	})
	bw, _ := strconv.Atoi(m[4])
	bh, _ := strconv.Atoi(m[5])
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: image.Pt(w, hgt)}}
	want := 1 + 8 + 16 + 6 + h.th.Measure(gtx, unit.Sp(13), theme.Bold, "Save in Taut") + 8 + 1
	if bh != 28 || bw < want-2 || bw > want+2 {
		t.Errorf("Save in Taut %dx%d (%s), want %dx28: as wide as its icon, gap and label, Slack's small button's height (MessageBlocks: %s)",
			bw, bh, m[1], want, slackc.LayoutShape("MessageBlocks"))
	}
	// what the frame draws: the demo Owl's red border, 28 rows tall, and
	// inside it the icon, a clear 6px gap, the label
	red := ringRed // .taut-owl-demo's 214, 48, 49 in light mode
	img := pixeltest.Render(t, w, hgt, ops)
	r := v.s.rows[ts]
	var row image.Rectangle
	for i := v.s.visible[0]; i < v.s.visible[1] && i < len(v.s.items); i++ {
		if v.s.items[i].key == ts && v.s.heights[i] > 0 {
			row = image.Rect(0, v.s.pos[i], w, v.s.pos[i]+v.s.heights[i])
		}
	}
	n, b := ringPixels(img, row, red, 40)
	if n < 100 || b.Dy() < 27 || b.Dy() > 29 || b.Dx() < want-2 || b.Dx() > want+3 {
		t.Fatalf("owl-red pixels: %d in %v, want the %dx28 button's border (row %v, body %v)", n, b, want, row, r.bodyRect)
	}
	colInk := func(x int) int {
		k := 0
		for y := b.Min.Y + 2; y < b.Max.Y-2; y++ {
			if ringNear(img.RGBAAt(x, y), red, 90) {
				k++
			}
		}
		return k
	}
	icon, gap, label := 0, 0, 0
	for x := b.Min.X + 9; x < b.Min.X+25; x++ {
		icon += colInk(x)
	}
	for x := b.Min.X + 26; x < b.Min.X+30; x++ {
		gap += colInk(x)
	}
	for x := b.Min.X + 31; x < b.Max.X-9; x++ {
		label += colInk(x)
	}
	if icon < 15 || gap > 0 || label < 40 {
		t.Errorf("red ink in the button: %d in the icon's columns, %d in the 6px gap, %d in the label's; want the icon, a clear gap, the label", icon, gap, label)
	}
}
