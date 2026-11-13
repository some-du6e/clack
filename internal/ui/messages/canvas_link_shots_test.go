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

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/core"
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

// A canvas shared as an app's message file, in the shape of a live app
// reply (keys and types only): a mrkdwn section linking the canvas and
// two buttons, files[0] the canvas owned by ana (not the app), edited by
// the app's bot in the same second. Slack shows no "(edited)" (a bot_id
// message never does, shouldDisplayAsEdited), "ana | Canvas ▾" over the
// card, the lagoon tile, the title with its emoji, "Canvas", and the
// canvas's opening sections cut at 240px.
func TestCanvasFileShots(t *testing.T) {
	dir := os.Getenv("MESSAGES_AUDIT_SHOTS_DIR")
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		app, fs, _ := shotsApp(t)
		s := app.S
		s.SetEmojiList(map[string]string{"faq-q": fs.URL + "img/64x64/faqq.png"})
		const file = "F0CANVFAQ09"
		sp := func(t string) []richtext.Span { return []richtext.Span{{Kind: richtext.Text, Text: t}} }
		d := canvas.Doc{Blocks: []canvas.Block{
			{ID: "temp:C:Q1", Kind: canvas.Heading1, Spans: sp("FAQ")},
			{ID: "temp:C:Q2", Kind: canvas.Heading2, Spans: sp("Getting started")},
			{ID: "temp:C:Q3", Kind: canvas.Paragraph, Spans: []richtext.Span{{Kind: richtext.Text, Text: "Read the "},
				{Kind: richtext.Link, Text: "setup guide", URL: "https://example.invalid/setup"}, {Kind: richtext.Text, Text: " and run "},
				{Kind: richtext.Text, Text: "make setup", Style: richtext.Code}, {Kind: richtext.Text, Text: " once."}}},
			{ID: "temp:C:Q4", Kind: canvas.Heading2, Spans: sp("Asking for help")},
			{ID: "temp:C:Q5", Kind: canvas.Paragraph, Spans: sp("Say what you tried and what you expected to happen.")},
			{ID: "temp:C:Q6", Kind: canvas.Bullet, Spans: sp("Include the error text")},
			{ID: "temp:C:Q7", Kind: canvas.Bullet, Spans: sp("Say which version you run")},
			{ID: "temp:C:Q8", Kind: canvas.Heading2, Spans: sp("Common problems")},
			{ID: "temp:C:Q9", Kind: canvas.Paragraph, Spans: sp("If the build fails, clear the cache and try again.")},
		}}
		s.PutCanvasPreview(file, core.CanvasPreviewBlocks(d))
		day := time.Now()
		ts := fmt.Sprintf("%d.350659", time.Date(day.Year(), day.Month(), day.Day(), 10, 5, 0, 0, time.Local).Unix())
		sec := strings.Split(ts, ".")[0]
		link := "https://fakeclub.enterprise.slack.com/docs/T0FAKE/" + file
		raw := `{"type":"message","ts":"` + ts + `","user":"UDEPLOYAPP","bot_id":"B0DEPLOY1","upload":false,"display_as_bot":false,
			"text":"Check <` + link + `|the FAQ> first.","edited":{"user":"B0DEPLOY1","ts":"` + sec + `.000000"},
			"blocks":[{"type":"section","block_id":"a","text":{"type":"mrkdwn","text":"Thanks for asking! Check <` + link + `|the FAQ> first.","verbatim":false}},
			{"type":"actions","block_id":"b","elements":[
			{"type":"button","action_id":"mark_resolved","text":{"type":"plain_text","text":"Mark resolved","emoji":true},"style":"primary","value":"r"},
			{"type":"button","action_id":"ask_ai","text":{"type":"plain_text","text":"Ask the bot","emoji":true},"value":"a"}]}],
			"files":[{"id":"` + file + `","created":1762500000,"timestamp":1762500000,"mimetype":"application/vnd.slack-docs","filetype":"quip",
			"pretty_type":"Canvas","user":"UANA00001","editable":true,"size":3782,"mode":"quip","is_external":false,"external_type":"",
			"is_public":true,"display_as_bot":false,"username":"","name":"_faq_q__FAQ","title":":faq-q: FAQ",
			"url_private":"` + fs.URL + `files-pri/T0FAKE-` + file + `/canvas","permalink":"` + link + `","has_rich_preview":false,
			"file_access":"visible","canvas_creator_id":"UANA00001",
			"title_blocks":[{"type":"rich_text","block_id":"t","elements":[{"type":"rich_text_section","elements":[{"type":"emoji","name":"faq-q"},{"type":"text","text":" FAQ"}]}]}]}]}`
		var m slack.Message
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		var msgs []slack.Message
		if tl := s.Timeline(tShots); tl != nil {
			msgs = append(msgs, tl.Msgs...)
		}
		s.SetLatest(tShots, append(msgs, m), false)
		th := theme.New(dark)
		host := identityHost{coreHost: coreHost{app: app, th: th}, imgs: imgcache.New(fs.Fetch, "", nil)}
		const w, h = 1100, 1500
		v := NewChannelView(host, tShots)
		v.Preview = true
		ops := new(op.Ops)
		f := func() { shotFrame(v, th, w, h, ops) }
		loadList(v, f)
		until(t, 5*time.Second, "the app's canvas message", f, func() bool { return v.s.rows[ts] != nil && itemHeight(v.s, ts) > 0 })
		for i := 0; i < 60; i++ {
			f()
			time.Sleep(10 * time.Millisecond)
		}
		f()
		r := v.s.rows[ts]
		if !r.m.Edited || r.rvOpts().Edited {
			t.Errorf("%s: edited %v, (edited) shown %v; want an edited message with no label (bot_id)", mode, r.m.Edited, r.rvOpts().Edited)
		}
		var cf *richtext.File
		for _, b := range r.m.Doc.Blocks {
			if b.File != nil && richtext.IsCanvasFile(b.File) {
				cf = b.File
			}
		}
		if cf == nil || cf.Uploader != "ana" || richtext.PlainSpans(cf.TitleSpans) != ":faq-q: FAQ" || len(cf.CanvasPreview) == 0 {
			t.Fatalf("%s: the canvas file %+v", mode, cf)
		}
		if dir == "" {
			continue
		}
		img := pixeltest.Render(t, w, h, ops)
		y0, y1 := -1, 0
		for i, it := range v.s.items {
			if it.kind == itMsg && it.key == ts && i < len(v.s.pos) && i < len(v.s.heights) {
				y0, y1 = v.s.pos[i], v.s.pos[i]+v.s.heights[i]
			}
		}
		if y0 < 0 {
			t.Fatalf("%s: no row", mode)
		}
		o := v.s.origin.Y
		savePNG(t, filepath.Join(dir, "canvas-file-"+mode+".png"), crop(img, max(y0+o-8, 0), min(y1+o+8, h)))
	}
}
