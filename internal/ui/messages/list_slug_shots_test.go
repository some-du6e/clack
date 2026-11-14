package messages

// List slugs in a channel, on compact_shots_test.go's synthetic #release
// (made-up people and text; nothing talks to Slack): rich_text
// list_record elements (0MEu ListRecord) for a whole list, a list view,
// an item and a deleted item, and, under search_file_channel_messages, a
// canvas's and a list's file channel mentioned as <#C…> (lH2I
// renderFileChannelSlug: the canvas and list slugs). The list, its item
// and the files are in the store, as core's resolve loop leaves them.
// With MESSAGES_AUDIT_SHOTS_DIR set (the messages-audit-shots workflow)
// each mode is saved, light and dark.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

const (
	lsList   = "F0LISTSLG1"
	lsCanvas = "F0CANVSLG1"
)

func listSlugMessages(t *testing.T, app *core.App, s *store.Store) map[string]string {
	t.Helper()
	s.PutCanvasRef(lsList, richtext.CanvasRef{Title: "Launch tracker", Status: "normal", Kind: "list", Views: map[string]string{"View0BRD": "Board"}})
	s.PutCanvasRef(lsCanvas, richtext.CanvasRef{Title: "Release plan", Status: "normal", Kind: "canvas"})
	s.PutListRecordName(lsList, "Rec0SLUG01", json.RawMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Write the launch announcement"}]}]}]`), "base")
	s.PutListRecordName(lsList, "Rec0GONE01", nil, "deleted")
	s.PutConv(slack.Conversation{ID: "C0LISTSLG1", Name: "launch-tracker", IsChannel: true, IsPrivate: true, IsMember: true, IsFile: true})
	s.PutConv(slack.Conversation{ID: "C0CANVSLG1", Name: "release-plan", IsChannel: true, IsPrivate: true, IsMember: true, IsFile: true})
	app.SetExperimentGroup(core.ExpSearchFileChannelMessages, "on")
	day := time.Now()
	at := func(h, m int) string {
		return fmt.Sprintf("%d.000100", time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.Local).Unix())
	}
	ts := map[string]string{"ls1": at(11, 1), "ls2": at(11, 2)}
	link := "https://fakeclub.enterprise.slack.com/lists/T0FAKE/" + lsList
	el := func(extra string) string {
		return `{"type":"list_record","file_id":"` + lsList + `"` + extra + `,"url":"` + link + `"}`
	}
	raw := []string{
		`{"type":"message","ts":"` + ts["ls1"] + `","user":"UANA00001","text":"x",
			"blocks":[{"type":"rich_text","block_id":"r","elements":[{"type":"rich_text_section","elements":[
			{"type":"text","text":"The tracker "},` + el(``) + `,{"type":"text","text":", its board "},` + el(`,"view_id":"View0BRD"`) +
			`,{"type":"text","text":", one item "},` + el(`,"record_id":"Rec0SLUG01"`) + `,{"type":"text","text":" and a deleted one "},` + el(`,"record_id":"Rec0GONE01"`) + `]}]}]}`,
		`{"type":"message","ts":"` + ts["ls2"] + `","user":"UBEN00001","text":"Comments in <#C0CANVSLG1> and <#C0LISTSLG1|launch-tracker>"}`,
	}
	var msgs []slack.Message
	if tl := s.Timeline(tShots); tl != nil {
		msgs = append(msgs, tl.Msgs...)
	}
	for _, r := range raw {
		var m slack.Message
		if err := json.Unmarshal([]byte(r), &m); err != nil {
			t.Fatal(err)
		}
		msgs = append(msgs, m)
	}
	s.SetLatest(tShots, msgs, false)
	return ts
}

func TestListSlugShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		app, fs, _ := shotsApp(t)
		ts := listSlugMessages(t, app, app.S)
		th := theme.New(dark)
		host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}
		const w, h = 1100, 1500
		v := NewChannelView(host, tShots)
		v.Preview = true
		ops := new(op.Ops)
		f := func() { shotFrame(v, th, w, h, ops) }
		loadList(v, f)
		until(t, 5*time.Second, "the list slug messages", f, func() bool {
			return v.s.rows[ts["ls2"]] != nil && itemHeight(v.s, ts["ls2"]) > 0 && v.s.rows[ts["ls1"]] != nil
		})
		for i := 0; i < 30; i++ {
			f()
			time.Sleep(10 * time.Millisecond)
		}
		var slugs []string
		for _, key := range []string{"ls1", "ls2"} {
			for _, b := range v.s.rows[ts[key]].m.Doc.Blocks {
				for _, sp := range b.Spans {
					if sp.Kind == richtext.Canvas {
						slugs = append(slugs, sp.Icon+":"+sp.Text)
					}
				}
			}
		}
		want := "lists:Launch tracker|lists:Launch tracker: Board|list-item:Write the launch announce…|trash:Deleted item|canvas-content:Release plan|lists:Launch tracker"
		if got := strings.Join(slugs, "|"); got != want {
			t.Errorf("%s: slugs\n got %s\nwant %s", mode, got, want)
		}
		if dir == "" {
			continue
		}
		img := pixeltest.Render(t, w, h, ops)
		y0, y1 := -1, 0
		for i, it := range v.s.items {
			if it.kind != itMsg || i >= len(v.s.pos) || i >= len(v.s.heights) {
				continue
			}
			switch it.key {
			case ts["ls1"]:
				y0 = v.s.pos[i]
			case ts["ls2"]:
				y1 = v.s.pos[i] + v.s.heights[i]
			}
		}
		if y0 < 0 || y1 <= y0 {
			t.Fatalf("%s: rows at %d..%d", mode, y0, y1)
		}
		o := v.s.origin.Y
		savePNG(t, filepath.Join(dir, "list-slugs-"+mode+".png"), crop(img, max(y0+o-8, 0), min(y1+o+8, h)))
	}
}
