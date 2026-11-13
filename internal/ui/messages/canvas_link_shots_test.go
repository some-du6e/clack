package messages

// Canvas links in a channel, on compact_shots_test.go's synthetic
// #release (made-up people and text; nothing talks to Slack), in the
// shape of a live canvas-section link (2026-11-12, keys and types only):
// the message's rich_text "canvas" element (Slack's canvas slug, "Release
// plan 🐱 : Ship notes go out Tuesday", the title_blocks' trailing space
// kept) over its document_section
// unfurl (MessageAttachmentQuipAnchorLink: the section's text, then the
// canvas-content icon and the title linked to the section, "View
// section", "Canvas"), and a bare canvas element (the canvas's name).
// The canvas and its section are in the store, as core's resolve loop
// leaves them. With MESSAGES_AUDIT_SHOTS_DIR set (the
// messages-audit-shots workflow) each is saved, light and dark.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gioui.org/op"

	"github.com/some-du6e/clack/internal/imgcache"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

const (
	clFile = "F0CANVLINK1"
	clSec  = "temp:C:CLS0000000000000000000000007"
)

func canvasLinkMessages(t *testing.T, s *store.Store, imgBase string) map[string]string {
	t.Helper()
	s.SetEmojiList(map[string]string{"release-cat": imgBase + "img/64x64/releasecat.png"})
	s.PutCanvasRef(clFile, richtext.CanvasRef{Title: "Release plan :release-cat:", Status: "normal",
		TitleBlocks: json.RawMessage(`[{"type":"rich_text","block_id":"t","elements":[{"type":"rich_text_section","elements":[
			{"type":"text","text":"Release plan "},{"type":"emoji","name":"release-cat"},{"type":"text","text":" "}]}]}]`)})
	s.PutCanvasSection(clFile, clSec, json.RawMessage(`{"type":"rich_text","block_id":"`+clSec+`","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Ship notes go out Tuesday"}]}]}`))
	day := time.Now()
	at := func(h, m int) string {
		return fmt.Sprintf("%d.000100", time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.Local).Unix())
	}
	ts := map[string]string{"cl1": at(10, 1), "cl2": at(10, 2)}
	link := "https://fakeclub.enterprise.slack.com/docs/T0FAKE/" + clFile + "?focus_section_id=" + strings.ReplaceAll(clSec, ":", "%3A")
	raw := []string{
		`{"type":"message","ts":"` + ts["cl1"] + `","user":"UANA00001","text":"<` + link + `>",
			"blocks":[{"type":"rich_text","block_id":"r","elements":[{"type":"rich_text_section","elements":[
			{"type":"canvas","file_id":"` + clFile + `","section_id":"` + clSec + `","text":"` + link + `","url":"` + link + `"}]}]}],
			"attachments":[{"from_url":"` + link + `","text":"Ship notes go out Tuesday","fallback":"Ship notes go out Tuesday","file_id":"` + clFile + `",
			"document_section":{"section_id":"` + clSec + `","snippet":{"authors":[{"user_id":"UANA00001","is_author":true,"is_recent_editor":false,"is_most_recent_editor":true}],
			"section_edited_ts":1762515690123456,"file_ids":[]}},"ts":1762515690,"footer":"Release plan :release-cat:",
			"author_name":"​","author_link":"https://fakeclub.enterprise.slack.com/team/UANA00001","author_id":"UANA00001",
			"author_icon":"https://avatars.invalid/UANA00001-48.png",
			"blocks":[{"type":"rich_text","block_id":"` + clSec + `","elements":[{"type":"rich_text_section","elements":[{"type":"text","text":"Ship notes go out Tuesday"}]}]}],
			"original_url":"` + link + `","id":1}]}`,
		`{"type":"message","ts":"` + ts["cl2"] + `","user":"UBEN00001","text":"updated the canvas",
			"blocks":[{"type":"rich_text","block_id":"r","elements":[{"type":"rich_text_section","elements":[
			{"type":"text","text":"updated the canvas "},{"type":"canvas","file_id":"` + clFile + `"}]}]}]}`,
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

func TestCanvasLinkShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		app, fs, _ := shotsApp(t)
		ts := canvasLinkMessages(t, app.S, fs.URL)
		th := theme.New(dark)
		host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}
		const w, h = 1100, 1500
		v := NewChannelView(host, tShots)
		v.Preview = true
		ops := new(op.Ops)
		f := func() { shotFrame(v, th, w, h, ops) }
		loadList(v, f)
		until(t, 5*time.Second, "the canvas link messages", f, func() bool {
			return v.s.rows[ts["cl2"]] != nil && itemHeight(v.s, ts["cl2"]) > 0 && v.s.rows[ts["cl1"]] != nil
		})
		for i := 0; i < 60; i++ { // pictures
			f()
			time.Sleep(10 * time.Millisecond)
		}
		f()
		var slugs []string
		var anchor *richtext.Attachment
		for _, key := range []string{"cl1", "cl2"} {
			for _, b := range v.s.rows[ts[key]].m.Doc.Blocks {
				for _, sp := range b.Spans {
					if sp.Kind == richtext.Canvas {
						slugs = append(slugs, sp.Text)
					}
				}
				if b.Att != nil && b.Att.Anchor != nil {
					anchor = b.Att
				}
			}
		}
		if strings.Join(slugs, "|") != "Release plan :release-cat: : Ship notes go out Tuesday|Release plan :release-cat: " {
			t.Errorf("%s: slugs %q", mode, slugs)
		}
		if anchor == nil || richtext.PlainSpans(anchor.Anchor.FooterTitle) != "Release plan :release-cat: " || !anchor.Anchor.FooterFromBlocks {
			t.Fatalf("%s: the canvas-section card %+v", mode, anchor)
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
			case ts["cl1"]:
				y0 = v.s.pos[i]
			case ts["cl2"]:
				y1 = v.s.pos[i] + v.s.heights[i]
			}
		}
		if y0 < 0 || y1 <= y0 {
			t.Fatalf("%s: rows at %d..%d", mode, y0, y1)
		}
		o := v.s.origin.Y
		savePNG(t, filepath.Join(dir, "canvas-links-"+mode+".png"), crop(img, max(y0+o-8, 0), min(y1+o+8, h)))
	}
}
