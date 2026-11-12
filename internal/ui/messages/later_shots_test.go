package messages

// Screenshots of gap-w7 (the Later labels and the to-do button), light
// and dark, on compact_shots_test.go's synthetic #release channel
// (made-up people, fakeslack's pictures), drawn headlessly through Gio's
// GPU renderer (MESSAGES_AUDIT_SHOTS_DIR, the messages-audit-shots
// workflow). Nothing talks to Slack.
//
// gap-later-labels-{light,dark}.png: a bookmark ("Saved for later"), a
// to-do ("To-do added · View to-dos"), one due in 3 hours and one 2 hours
// overdue, the pointer on the third so the toolbar's to-do button shows
// "Complete to-do"'s check-circle-filled in content-hgl-2.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

func TestLaterLabelShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("MESSAGES_AUDIT_SHOTS_DIR not set (the messages-audit-shots workflow)")
	}
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		app, fs, ts := shotsApp(t)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		now := time.Now()
		for _, err := range []error{
			app.Save(ctx, tShots, ts["a1"], 0),
			app.AddTodo(ctx, tShots, ts["a2"], 0),
			app.AddTodo(ctx, tShots, ts["b1"], now.Add(3*time.Hour+30*time.Second).Unix()),
			app.AddTodo(ctx, tShots, ts["b2"], now.Add(-2*time.Hour-10*time.Second).Unix()),
		} {
			if err != nil {
				t.Fatal(err)
			}
		}
		cancel()
		th := theme.New(dark)
		host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}
		const w, h = 1100, 1500
		v := NewChannelView(host, tShots)
		v.Preview = true
		ops := new(op.Ops)
		f := func() { shotFrame(v, th, w, h, ops) }
		loadList(v, f)
		until(t, 5*time.Second, "the labels", f, func() bool {
			r := v.s.rows[ts["b2"]]
			return r != nil && r.bgDrawn == colorsFor(th).CustomTint
		})
		hoverExp(v.s, f, ts["b1"], 600)
		for i := 0; i < 40; i++ { // avatars
			f()
			time.Sleep(10 * time.Millisecond)
		}
		f()
		img := pixeltest.Render(t, w, h, ops)
		y0, y1 := -1, 0
		for i, it := range v.s.items {
			if it.kind != itMsg || i >= len(v.s.pos) {
				continue
			}
			switch it.key {
			case ts["a1"]:
				y0 = v.s.pos[i] + v.s.origin.Y
			case ts["b2"]:
				y1 = v.s.pos[i] + v.s.heights[i] + v.s.origin.Y
			}
		}
		if y0 < 0 || y1 <= y0 {
			t.Fatalf("%s: rows a1..b2 at %d..%d", mode, y0, y1)
		}
		savePNG(t, filepath.Join(dir, "gap-later-labels-"+mode+".png"), crop(img, max(y0-40, 0), min(y1+8, h)))
	}
}
