package ui

// The space between a thread's last reply and its reply box, Karim's
// 2026-11-12 report (Windows, dark: "holy shit the gap here is fucking
// crazy", ~30px under a one-reply thread). Slack's thread list ends with
// its "input" row (getThreadRowsAndKeys, boot-shared), the
// .p-threads_footer__input_container with margin 12px 16px 0: the box's
// top border is 12px under the last reply's row (whose gutter keeps its
// 8px bottom padding). Clack applied the 12px twice (the composer's pane
// body and the chrome), 24px in all.
//
// Checked on internal/fakeslack's #th-audit, light and dark: a short
// thread with one reply (the box placed under the content) and the
// 40-reply thread scrolled to its end (the box at the pane's bottom). With
// THREADS_AUDIT_SHOTS_DIR set (the threads-audit workflow) the bottom of
// the pane is saved and the gap is checked in pixels: pane background
// from the row's bottom edge to the box's focus shadow, then the border.

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/messages"
)

// slackThreadGap is .p-threads_footer__input_container's top margin, dp.
const slackThreadGap = 12

func TestThreadsAuditReplyGap(t *testing.T) {
	dir := os.Getenv("THREADS_AUDIT_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			e := threadsAuditEnv(t, dark)
			ch := fakeslack.ThreadsAuditChannel
			e.openChannel(ch)
			for _, tc := range []struct {
				name string
				root string
				long bool
			}{
				{"one-reply", fakeslack.ThreadsAuditOne, false},
				{"many-replies", fakeslack.ThreadsAuditLong, true},
			} {
				e.a.OpenThread(ch, tc.root)
				var tv *messages.ThreadView
				e.until(tc.name+"'s last reply", 15*time.Second, func() bool {
					if tv = e.threadView(ch, tc.root); tv == nil {
						return false
					}
					if tc.long {
						tv.ScrollToBottom() // it opens at the root (not followed)
					}
					_, ok := tv.NewestRowBottom()
					return ok && (tc.long || tv.ContentHeight() > 0)
				})
				e.settle(800 * time.Millisecond)
				c := e.a.convoFor(ch, tc.root)
				box := replyBoxRect(c)
				if box.Empty() {
					t.Fatalf("%s: no reply box", tc.name)
				}
				bottom, ok := tv.NewestRowBottom()
				if !ok {
					t.Fatalf("%s: the last reply isn't on screen", tc.name)
				}
				if gap := box.Min.Y - bottom; gap != slackThreadGap {
					t.Errorf("%s %s: %d px between the last reply and the reply box, Slack %d", tc.name, mode, gap, slackThreadGap)
				}
				fit := tv.ContentHeight() > 0
				if fit == tc.long {
					t.Errorf("%s: content fits the pane: %v", tc.name, fit)
				}
				if e.gpu == nil || dir == "" {
					continue
				}
				img := image.NewRGBA(image.Rectangle{Max: e.size})
				if err := e.gpu.Screenshot(img); err != nil {
					t.Fatal(err)
				}
				pane := image.Rect(e.size.X-e.a.secondary.drawnW, 0, e.size.X, e.size.Y)
				crop := img.SubImage(image.Rect(pane.Min.X, max(0, bottom-140), pane.Max.X, min(e.size.Y, box.Max.Y+40)))
				f, err := os.Create(filepath.Join(dir, "thread-reply-gap-"+tc.name+"-"+mode+".png"))
				if err != nil {
					t.Fatal(err)
				}
				if err := png.Encode(f, crop); err != nil {
					t.Fatal(err)
				}
				f.Close()
				threadGapPixels(t, tc.name+" "+mode, img, pane, box, bottom)
			}
		})
	}
}

// replyBoxRect is a conversation's reply box in window coordinates.
func replyBoxRect(c *convo) image.Rectangle {
	if c == nil || c.comp == nil {
		return image.Rectangle{}
	}
	if b, ok := c.comp.(interface{ BoxRect() image.Rectangle }); ok {
		return b.BoxRect()
	}
	return image.Rectangle{}
}

// threadGapPixels: between the last reply's bottom edge and the box only
// pane background, except the focused box's shadow (crShadow: a few px
// around it); the box's top border is ink.
func threadGapPixels(t *testing.T, name string, img *image.RGBA, pane, box image.Rectangle, bottom int) {
	t.Helper()
	at := func(x, y int) color.NRGBA {
		c := img.RGBAAt(x, y)
		return color.NRGBA{c.R, c.G, c.B, c.A}
	}
	x0, x1 := pane.Min.X+2, pane.Max.X-14 // clear of the pane's edge and the scrollbar
	bg := at(x0, bottom)
	const shadow = 4
	for y := bottom; y < box.Min.Y-shadow; y++ {
		for x := x0; x < x1; x++ {
			if !near2(at(x, y), bg) {
				t.Errorf("%s: %v at (%d,%d) in the gap over the reply box (pane %v)", name, at(x, y), x, y, bg)
				return
			}
		}
	}
	if b := at((box.Min.X+box.Max.X)/2, box.Min.Y); near2(b, bg) {
		t.Errorf("%s: no border at the box's top (y=%d): %v, pane %v", name, box.Min.Y, b, bg)
	}
}
