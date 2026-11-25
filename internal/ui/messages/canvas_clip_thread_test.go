package messages

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Synthetic fixtures only.

// Karim's report on α.165 ("text is over the canvas thingy") was a thread
// reply in the dark theme: there, in the 480px pane, a reply sharing a
// canvas whose preview runs past FileQuip's 240px draws the card's bottom
// border whole (the preview stops at the pillow's padding box) and
// nothing under the card, where an edited files-only reply puts its
// "(edited)". Each preview line starts with a custom emoji, which fills
// its 22px line box, so whatever line the cut crosses draws over the
// border unless the cut stops it.
func TestCanvasPreviewClippedInThread(t *testing.T) {
	for _, dark := range []bool{false, true} {
		t.Run(fmt.Sprintf("dark=%v", dark), func(t *testing.T) {
			app, fs := auditEnv(t)
			app.S.SetEmojiList(map[string]string{"orb-check": fs.URL + "img/64x64/orbcheck.png"})
			ch := ""
			for _, id := range fs.ChannelIDs() {
				if id != fakeslack.ThreadsAuditChannel {
					ch = id
					break
				}
			}
			root, _ := fs.PostQuiet(ch, fakeslack.SelfID, "the plan for the offsite")
			const file = "F0CANVTHRD1"
			var bs []canvas.Block
			for i := range 30 {
				bs = append(bs, canvas.Block{ID: fmt.Sprintf("temp:C:T%d", i), Kind: canvas.Bullet,
					Spans: []richtext.Span{{Kind: richtext.Emoji, ID: "orb-check"}, {Kind: richtext.Text, Text: " drunk+press is a drunk press"}}})
			}
			app.S.PutCanvasPreview(file, core.CanvasPreviewBlocks(canvas.Doc{Blocks: bs}))
			var cf slack.File
			raw := `{"id":"` + file + `","created":1762500000,"timestamp":1762500000,"mimetype":"application/vnd.slack-docs","filetype":"quip",
				"pretty_type":"Canvas","user":"` + fakeslack.SelfID + `","editable":true,"size":3782,"mode":"quip","is_external":false,
				"is_public":true,"name":"Offsite","title":"Offsite","url_private":"` + fs.URL + `files-pri/T0FAKE-` + file + `/canvas",
				"permalink":"` + fs.URL + `docs/T0FAKE/` + file + `","file_access":"visible","canvas_creator_id":"` + fakeslack.SelfID + `"}`
			if err := json.Unmarshal([]byte(raw), &cf); err != nil {
				t.Fatal(err)
			}
			reply := fs.PostReplyFilesQuiet(ch, root, fakeslack.SelfID, "", []slack.File{cf}, true)

			th := theme.New(dark)
			host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}
			const w, h = 480, 1100
			tv := NewThreadView(host, ch, root)
			ops := new(op.Ops)
			frame := func() { shotFrame(tv, th, w, h, ops) }
			until(t, 15*time.Second, "the canvas reply", frame, func() bool {
				r := tv.s.rows[reply]
				return r != nil && itemHeight(tv.s, reply) > 0 && r.rv != nil && len(r.rv.FileRects()) > 0
			})
			for range 60 { // the emoji's image loads
				frame()
				time.Sleep(10 * time.Millisecond)
			}
			frame()
			r := tv.s.rows[reply]
			if !r.rvOpts().Edited {
				t.Fatal("the reply has no (edited) label: nothing follows the card")
			}
			pos := -1
			for i, it := range tv.s.items {
				if it.kind == itMsg && it.key == reply && i < len(tv.s.pos) {
					pos = tv.s.pos[i]
				}
			}
			if pos < 0 {
				t.Fatal("the reply has no row")
			}
			card := r.rv.FileRects()[0].R.Add(tv.s.origin.Add(image.Pt(r.bodyRect.Min.X, pos+r.bodyRect.Min.Y)))
			if card.Dy() != 315 {
				t.Errorf("the reply's card is %dpx tall, want Slack's 315 (a full 240px snippet)", card.Dy())
			}
			img := pixeltest.Render(t, w, h, ops)
			px := func(x, y int) color.RGBA { return img.RGBAAt(x, y) }
			bg := color.RGBA(th.C.Bg)
			// the preview is drawn: its lines ink the card above the cut
			ink := 0
			for y := card.Max.Y - 30; y < card.Max.Y-2; y++ {
				for x := card.Min.X + 13; x < card.Max.X-13; x++ {
					if clipDist(px(x, y), bg) > 40 {
						ink++
					}
				}
			}
			if ink == 0 {
				t.Fatal("no ink in the card's last 30px: the preview isn't drawn, nothing to check")
			}
			// the bottom border row: its colour only
			by := card.Max.Y - 1
			want := px(card.Max.X-13, by)
			bad := 0
			for x := card.Min.X + 13; x < card.Max.X-13; x++ {
				if clipDist(px(x, by), want) > 24 {
					bad++
				}
			}
			if bad > 0 || clipDist(want, bg) < 4 {
				t.Errorf("%d pixels of the card's bottom border row (y %d) aren't its colour %v: the preview paints over it", bad, by, want)
			}
			// under the card, right of the "(edited)" label: the pane
			under := 0
			for y := card.Max.Y; y < card.Max.Y+4; y++ {
				for x := card.Min.X + 90; x < card.Max.X-13; x++ {
					if clipDist(px(x, y), bg) > 40 {
						under++
					}
				}
			}
			if under > 0 {
				t.Errorf("%d inked pixels in the 4 rows under the card (y %d): the preview runs past it", under, card.Max.Y)
			}
		})
	}
}

// clipDist is the sum of two colours' channel differences.
func clipDist(a, b color.RGBA) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
}
