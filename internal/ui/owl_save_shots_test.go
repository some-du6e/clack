package ui

// Taut's OwlMode itself (TAUT_PLUGINS_DIR: ui-taut and flake-check's
// owl-save job build it at TAUT_PIN), in the whole window, under (demo)
// Owl's confirmation in its DM: Karim's α.113 report ("pls fix, long
// button. fucked styling"). The plugin's MessageBlocks patch adds Save in
// Taut after the blocks: a Block Kit-style actions row around Slack's
// small outline Button (its glasses icon and label, display:flex with a
// 6px gap, in the demo Owl's red). In Slack the button is 28px tall and
// as wide as its content: a 1px border, 8px sides
// (.p-block_kit_button_element), the 16px icon, 6px, the label. α.113
// drew it across the message, its icon on a line above its label, in
// Clack's grey. Screenshots go to TAUT_SHOTS_DIR. Synthetic data only.

import (
	"encoding/json"
	"image"
	"image/color"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var owlSaveShape = regexp.MustCompile(`@Button\(([a-z-]+) (-?\d+),(-?\d+) (\d+)x(\d+)\)`)

// owlRed is .taut-owl-demo's --taut-owl in light mode
var owlRed = color.NRGBA{R: 214, G: 48, B: 49, A: 0xff}

func owlRedAt(c color.RGBA, tol int) bool {
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	return d(c.R, owlRed.R) <= tol && d(c.G, owlRed.G) <= tol && d(c.B, owlRed.B) <= tol
}

func TestTautViewOwlSaveButton(t *testing.T) {
	if os.Getenv("TAUT_PLUGINS_DIR") == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the ui-taut workflow builds Taut's plugins and runs this")
	}
	const dm, ts, bot = "D0OWLDEMO1", "1700000200.000100", "B0OWLDEMO1"
	const text = "Confession #40459 submitted and awaiting review. Your Slack account will be recognized through its salted hash. No reply key is needed."
	e := newSVIn(t, "", func(home string, a *App, ca *core.App) {
		blocks, _ := json.Marshal([]any{map[string]any{"type": "section", "block_id": "owl1",
			"text": map[string]any{"type": "mrkdwn", "text": text}}})
		ca.S.PutConv(slack.Conversation{ID: dm, IsIM: true, User: "U0OWLDEMO1", IsMember: true})
		ca.S.SetLatest(dm, []slack.Message{{Type: "message", Subtype: "bot_message", TS: ts, BotID: bot, Username: "(demo) Owl",
			Text: text, Blocks: blocks, BotProfile: &slack.BotProfile{ID: bot, Name: "(demo) Owl", AppID: "A0CDBA8FXDL"}}}, false)
	})
	e.tautInstall("OwlMode")
	e.a.Navigate("c/" + dm)
	e.until(20*time.Second, "Save in Taut under owl's confirmation", func() bool { return e.tree("taut-owl-save__button") != "" })
	var m []string
	e.until(5*time.Second, "the button laid out", func() bool {
		m = owlSaveShape.FindStringSubmatch(slackc.LayoutShape("MessageBlocks"))
		return m != nil
	})
	bw, _ := strconv.Atoi(m[4])
	bh, _ := strconv.Atoi(m[5])
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1}, Constraints: layout.Constraints{Max: e.sz}}
	want := 1 + 8 + 16 + 6 + theme.New(false).Measure(gtx, unit.Sp(13), theme.Bold, "Save in Taut") + 8 + 1
	if bh != 28 || bw < want-2 || bw > want+2 {
		t.Errorf("Save in Taut %dx%d (%s), want %dx28: as wide as its icon, gap and label, Slack's small button's height (MessageBlocks: %s)",
			bw, bh, m[1], want, slackc.LayoutShape("MessageBlocks"))
	}
	// what the frame draws: the demo Owl's red border, 28 rows tall, and
	// inside it the icon, a clear 6px gap, the label (the message pane:
	// right of the rail and the sidebar, under the top nav)
	img := pmShot(t, e, os.Getenv("TAUT_SHOTS_DIR"), "owl-save-button-taut")
	pane := image.Rect(int(theme.RailW)+e.a.sidebar.width, int(theme.TopNavH), e.sz.X, e.sz.Y)
	n, b := 0, image.Rectangle{}
	for y := pane.Min.Y; y < pane.Max.Y; y++ {
		for x := pane.Min.X; x < pane.Max.X; x++ {
			if owlRedAt(img.RGBAAt(x, y), 24) {
				n++
				b = b.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if n < 100 || b.Dy() < 27 || b.Dy() > 29 || b.Dx() < want-2 || b.Dx() > want+3 {
		t.Fatalf("owl-red pixels: %d in %v, want the %dx28 button's border (MessageBlocks: %s)", n, b, want, slackc.LayoutShape("MessageBlocks"))
	}
	colInk := func(x int) int {
		k := 0
		for y := b.Min.Y + 2; y < b.Max.Y-2; y++ {
			if owlRedAt(img.RGBAAt(x, y), 90) {
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
