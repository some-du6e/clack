package ui

// The space between a channel's newest message and the composer, Karim's
// 2026-11-11 report (Windows, dark: a pinned last row ran into the
// composer's top edge). Slack's message pane puts BaseMessageList's
// bottomSpacer (space.150, 24px) after the newest message; the composer
// box (.p-message_pane_input, padding 0 20px, no top margin) starts right
// under it. .p-message_pane .c-virtual_list__sticky_container's
// padding-bottom:24px is the sticky day pill's overlay box (absolutely
// positioned from one divider to the next), not space in the list.
//
// Checked in light and dark, for a plain, a pinned and a highlighted newest
// row, a row pinned while the channel is open (it grows by the label line)
// and a message that arrives while it's open. With SPACING_SHOTS_DIR set
// (the spacing-shots workflow) the bottom of the channel is saved and the
// gap is measured in pixels too: pane background from the composer's top
// border up to the row's tint, which must be Chromium's sRGB blend of
// rgba(242,199,68,.1) over the pane.

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/gpu/headless"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/messages"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

const gapChan = "C0CLACKGAP"

// slackGap is Slack's space between the newest row and the composer box
// (getBottomSpacerHeight without a context bar), in dp.
const slackGap = 24

func TestComposerGap(t *testing.T) {
	dir := os.Getenv("SPACING_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := ctxShell(t)
			now := time.Now()
			e.ca.S.PutConv(slack.Conversation{ID: gapChan, Name: "gap", IsChannel: true, IsMember: true, NumMembers: 3})
			msg := func(at time.Time, text string) slack.Message {
				return slack.Message{Type: "message", TS: shotTS(at), User: ttSelf, Text: text, Blocks: shotBlocks(text)}
			}
			msgs := []slack.Message{msg(now.Add(-40*time.Second), "first"), msg(now.Add(-30*time.Second), "second"), msg(now.Add(-20*time.Second), "anchored test")}
			last := &msgs[2]
			last.PinnedTo = []string{gapChan}
			last.PinnedInfo = &slack.PinnedInfo{Channel: gapChan, PinnedBy: ttSelf, PinnedTS: now.Add(-15 * time.Second).Unix()}
			e.ca.S.SetLatest(gapChan, msgs, false)
			b := e.a.data
			b.fetched[gapChan] = time.Now() // offline: no tabs/facepile fetches
			if len(b.m.Sections) > 0 {
				b.m.Sections[0].Rows = append(b.m.Sections[0].Rows, gapChan)
				b.m.version++
			}
			if dark {
				e.a.setDark(true)
			}
			var win *headless.Window
			if dir != "" {
				w, err := pixeltest.NewWindow(e.sz.X, e.sz.Y)
				if err != nil {
					t.Fatalf("headless window: %v", err)
				}
				t.Cleanup(w.Release)
				win = w
			}
			e.a.Navigate("c/" + gapChan)
			settle := func() {
				for i := 0; i < 40; i++ {
					e.frame()
					time.Sleep(10 * time.Millisecond)
				}
			}
			view := func() (*convo, *messages.ChannelView) {
				cv := e.a.convos[gapChan+"/"]
				if cv == nil || cv.comp == nil || cv.compRect.Empty() {
					t.Fatalf("no composer in #gap")
				}
				v, ok := cv.list.(channelList)
				if !ok {
					t.Fatalf("#gap's list is a %T", cv.list)
				}
				return cv, v.ChannelView
			}
			check := func(name string, pinned bool) {
				t.Helper()
				settle()
				cv, v := view()
				bottom, ok := v.NewestRowBottom()
				if !ok {
					t.Fatalf("%s: the newest message isn't on screen", name)
				}
				if gap := cv.compRect.Min.Y - bottom; gap != slackGap {
					t.Errorf("%s: %d px between the newest message and the composer, Slack %d", name, gap, slackGap)
				}
				if win == nil {
					return
				}
				if err := win.Frame(e.ops); err != nil {
					t.Fatal(err)
				}
				img := image.NewRGBA(image.Rectangle{Max: e.sz})
				if err := win.Screenshot(img); err != nil {
					t.Fatal(err)
				}
				r := cv.compRect
				crop := img.SubImage(image.Rect(r.Min.X, max(0, r.Min.Y-160), r.Max.X, min(e.sz.Y, r.Min.Y+80)))
				f, err := os.Create(filepath.Join(dir, "composer-gap-"+name+"-"+mode+".png"))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := png.Encode(f, crop); err != nil {
					t.Fatal(err)
				}
				gapPixels(t, name, img, r, bottom, pinned)
			}
			check("pinned", true)
			_, v := view()
			v.Highlight(last.TS)
			check("pinned-highlighted", false) // the highlight layers over the tint

			// pinned while the channel is open: the row grows by its label
			ts := shotTS(now.Add(-5 * time.Second))
			e.inject("message", map[string]any{"type": "message", "channel": gapChan, "user": ttSelf, "text": "fresh", "ts": ts})
			check("new-message", false)
			e.inject("pin_added", map[string]any{"type": "pin_added", "channel_id": gapChan, "user": ttSelf, "event_ts": shotTS(now),
				"item": map[string]any{"type": "message", "channel": gapChan, "created_by": ttSelf, "created": now.Unix(),
					"message": map[string]any{"type": "message", "ts": ts, "user": ttSelf, "text": "fresh"}}})
			check("pinned-while-open", true)
		})
	}
}

// inject hands the core a realtime event, as the socket would.
func (e *svEnv) inject(typ string, fields map[string]any) {
	raw, _ := json.Marshal(fields)
	ev := slack.Event{Type: typ, Raw: raw}
	if ch, ok := fields["channel"].(string); ok {
		ev.Channel = json.RawMessage(`"` + ch + `"`)
	}
	e.ca.Inject(ev)
}

// gapPixels measures the gap in the screenshot, in a column right of the
// text (the row's tint is full width): pane background (and the focus
// shadow's faint top row) from just above the composer's top border up to
// the row's bottom edge, then the row.
func gapPixels(t *testing.T, name string, img *image.RGBA, comp image.Rectangle, bottom int, pinned bool) {
	// pinned: the row is tinted, so its edge shows; otherwise only the
	// structural check (TestComposerGap) applies.
	t.Helper()
	x := comp.Max.X - 40
	at := func(y int) color.NRGBA {
		c := img.RGBAAt(x, y)
		return color.NRGBA{c.R, c.G, c.B, c.A}
	}
	// the pane, clear of the focused composer's shadow (crShadow: Chrome
	// draws one row of black at 2/255 over it above the box, within
	// near2 of the pane; Navigate focuses the composer, focusAfterNavigate)
	bg := at(comp.Min.Y - 4)
	y := comp.Min.Y - 1
	for y > comp.Min.Y-200 && near2(at(y), bg) {
		y--
	}
	if !pinned {
		return // an untinted row is pane background too
	}
	if gap := comp.Min.Y - 1 - y; gap != slackGap {
		t.Errorf("%s: %d px of pane background above the composer in the screenshot, Slack %d (bg %v, then %v at y=%d)", name, gap, slackGap, bg, at(y), y)
	}
	// The pinned row's tint as Chromium draws it: 8-bit sRGB source-over
	// of rgba(242,199,68,.1) (alpha 26/255) over the pane.
	a := uint32(26)
	over := func(s, d uint8) uint8 { return uint8((uint32(s)*a + uint32(d)*(255-a) + 127) / 255) }
	want := color.NRGBA{over(0xf2, bg.R), over(0xc7, bg.G), over(0x44, bg.B), 0xff}
	if got := at(bottom - 2); !near2(got, want) {
		t.Errorf("%s: the pinned row's tint is %v, Chromium draws %v over %v", name, got, want, bg)
	}
}

func near2(a, b color.NRGBA) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= 2 && d(a.G, b.G) <= 2 && d(a.B, b.B) <= 2
}
