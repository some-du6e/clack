package composer

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/store"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// The edit composer loads a message the way Slack's does
// (getDeltaForMessageObject → convertBlocksToDelta, docForMessage), so an
// unchanged edit saves the blocks it started from. Karim, 2026-11-12: a
// message_mention (a message permalink, with its unfurl) came up as the
// plain label "clod in #clack" glued to the next word, and saving would
// have dropped the link and its unfurl. Synthetic workspace only.

const (
	erPermalink = "https://hackclub.slack.com/archives/C0GENERAL/p1700000050000100"
	erLinkedTS  = "1700000050.000100"
)

// erSlugEl is a message_mention as Slack's composer writes it
// (createSlugChunkFromOp: text is the url).
const erSlugEl = `{"type":"message_mention","message_ts":"` + erLinkedTS + `","channel_id":"C0GENERAL","text":"` + erPermalink +
	`","url":"` + erPermalink + `","author_id":"UALICE"}`

// erUnfurl is the message unfurl that rides along with a permalink.
const erUnfurl = `[{"id":1,"is_msg_unfurl":true,"from_url":"` + erPermalink + `","original_url":"` + erPermalink +
	`","author_id":"UALICE","author_name":"alice","channel_id":"C0GENERAL","ts":"` + erLinkedTS + `","text":"a synthetic linked message","fallback":"[a synthetic linked message]"}]`

func erSeed(s *store.Store) {
	s.Groups = append(s.Groups, slack.Usergroup{ID: "S0SYNTH", Handle: "devs", Name: "Devs"})
}

func erMessage(blocks string) core.Message {
	return core.Message{Channel: tChGen, TS: "1700000100.000100", User: tSelf, Text: "synthetic",
		Blocks: json.RawMessage(blocks), Attachments: json.RawMessage(erUnfurl)}
}

// erCanon decodes blocks for comparison, without block_id.
func erCanon(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var v []map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("blocks %s: %v", raw, err)
	}
	for _, b := range v {
		delete(b, "block_id")
	}
	return v
}

// Every rich_text element type a person's message can carry, loaded into
// the edit composer and saved unchanged, comes out as it went in.
func TestEditRoundTripEveryElement(t *testing.T) {
	app, _ := syntheticApp(t, erSeed)
	r := AtomResolver(app.S)
	for _, tc := range []struct{ name, blocks string }{
		{"permalink with unfurl", `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` + erSlugEl +
			`,{"type":"text","text":"the short things are nice"}]}]}]`},
		{"inline elements", `[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` +
			`{"type":"text","text":"see "},{"type":"link","url":"https://example.com/plain"},` +
			`{"type":"text","text":" and "},{"type":"link","url":"https://example.com/labelled","text":"a label"},` +
			`{"type":"text","text":" "},{"type":"link","url":"https://example.com/bold","text":"bold link","style":{"bold":true}},` +
			`{"type":"text","text":" hi "},{"type":"user","user_id":"UALICE"},` +
			`{"type":"text","text":" in "},{"type":"channel","channel_id":"C0GENERAL"},` +
			`{"type":"text","text":" cc "},{"type":"usergroup","usergroup_id":"S0SYNTH"},` +
			`{"type":"text","text":" "},{"type":"broadcast","range":"here"},` +
			`{"type":"text","text":" "},{"type":"emoji","name":"wave","unicode":"1f44b"},` +
			`{"type":"emoji","name":"partyparrot"},` +
			`{"type":"text","text":" "},{"type":"text","text":"bold","style":{"bold":true}},` +
			`{"type":"text","text":" "},{"type":"text","text":"italic","style":{"italic":true}},` +
			`{"type":"text","text":" "},{"type":"text","text":"strike","style":{"strike":true}},` +
			`{"type":"text","text":" "},{"type":"text","text":"code","style":{"code":true}},` +
			`{"type":"text","text":" then "},` + erSlugEl + `]}]}]`},
		{"bullet list", `[{"type":"rich_text","elements":[{"type":"rich_text_list","elements":[` +
			`{"type":"rich_text_section","elements":[{"type":"text","text":"one "},{"type":"user","user_id":"UALICE"}]},` +
			`{"type":"rich_text_section","elements":[{"type":"link","url":"https://example.com/two","text":"two"}]}],` +
			`"style":"bullet","indent":0,"border":0}]}]`},
		{"quote", `[{"type":"rich_text","elements":[{"type":"rich_text_quote","elements":[` +
			`{"type":"text","text":"quoted "},` + erSlugEl + `]}]}]`},
		{"code block", `[{"type":"rich_text","elements":[{"type":"rich_text_preformatted","elements":[` +
			`{"type":"text","text":"go vet ./...\nok"}],"border":0}]}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := erMessage(tc.blocks)
			d := docForMessage(m, r)
			got := d.RichTextBlocks()
			if want := erCanon(t, m.Blocks); !reflect.DeepEqual(erCanon(t, got), want) {
				t.Errorf("an unchanged edit saves different blocks\n got: %s\nwant: %s", got, m.Blocks)
			}
			text := d.Mrkdwn()
			if strings.Contains(tc.blocks, "message_mention") && !strings.Contains(text, "<"+erPermalink+">") {
				t.Errorf("text %q lost the permalink", text)
			}
			if tc.name == "inline elements" {
				for _, tok := range []string{"<@UALICE>", "<#C0GENERAL>", "<!subteam^S0SYNTH>", "<!here>", "https://example.com/plain", "a label"} {
					if !strings.Contains(text, tok) {
						t.Errorf("text %q has no %s", text, tok)
					}
				}
			}
		})
	}
}

// What Slack's convertTextChunks makes of the elements it doesn't keep:
// a date is its fallback text, a color its value.
func TestEditDateAndColorLikeSlack(t *testing.T) {
	m := erMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` +
		`{"type":"text","text":"on "},{"type":"date","timestamp":1700000000,"format":"{date_short}","fallback":"Nov 14"},` +
		`{"type":"text","text":" in "},{"type":"color","value":"#1264A3"}]}]}]`)
	d := docForMessage(m, nil)
	if got := d.Text(); got != "on Nov 14 in #1264A3" {
		t.Errorf("text %q, want Slack's fallback and value", got)
	}
}

// Karim's message in the edit composer: the message slug (a chip, with
// its url, author and channel) and then the text, as Slack's edit
// composer shows it; Save without a change sends the slug back.
func TestEditComposerMessageSlug(t *testing.T) {
	app, _ := syntheticApp(t, erSeed)
	stubTyping(t)
	h := &keysHost{slickHost: &slickHost{app: app, th: theme.New(false)}}
	m := erMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` + erSlugEl +
		`,{"type":"text","text":"the short things are nice"}]}]}]`)
	// the rendered message, as the list drew it: the slug's label only
	m.Doc.Blocks = []richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{
		{Kind: richtext.MessageMention, Text: "alice in #general", ID: erLinkedTS, URL: erPermalink},
		{Kind: richtext.Text, Text: "the short things are nice"}}}}
	saved := make(chan [2]string, 1)
	c := NewEdit(h, m, func() {})
	t.Cleanup(c.Close)
	c.SaveEdit = func(_ context.Context, _, _, text string, blocks json.RawMessage) error {
		saved <- [2]string{text, string(blocks)}
		return nil
	}
	d := c.ed.Doc()
	if d.Len() == 0 || d.Attrs[0].Atom == nil {
		t.Fatalf("the edit starts with %q, not the message slug", d.Text())
	}
	a := d.Attrs[0].Atom
	if a.Kind != AtomMessage || a.ID != erLinkedTS || a.Channel != tChGen || a.Author != "UALICE" || a.URL != erPermalink {
		t.Errorf("slug %+v", *a)
	}
	// getMessageSlugAttributes' label on the chip, the text after it
	if a.Text != "alice in #general" {
		t.Errorf("slug label %q, want %q", a.Text, "alice in #general")
	}
	if got := d.rtPlain(0, d.Len()); got != "alice in #generalthe short things are nice" {
		t.Errorf("visible text %q", got)
	}
	if got := string(d.Runes[1:]); got != "the short things are nice" {
		t.Errorf("text after the slug %q", got)
	}
	c.submit()
	select {
	case s := <-saved:
		if want := erCanon(t, m.Blocks); !reflect.DeepEqual(erCanon(t, json.RawMessage(s[1])), want) {
			t.Errorf("saved blocks %s, want %s", s[1], m.Blocks)
		}
		if !strings.HasPrefix(s[0], "<"+erPermalink+">") {
			t.Errorf("saved text %q doesn't start with the permalink", s[0])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Save didn't save")
	}
}

// A rendered message_mention span (drafts, a message without blocks)
// comes back as the slug from its permalink, never as its label alone.
func TestDocFromRichtextMessageMention(t *testing.T) {
	d := DocFromRichtext([]richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{
		{Kind: richtext.MessageMention, Text: "alice in #general", ID: erLinkedTS, URL: erPermalink},
		{Kind: richtext.Text, Text: "after"}}}})
	if a := d.Attrs[0].Atom; a == nil || a.Kind != AtomMessage || a.ID != erLinkedTS || a.Channel != tChGen || a.URL != erPermalink {
		t.Fatalf("doc %q starts with %+v", d.Text(), d.Attrs[0])
	}
	// the private one Slack can't show has no url: its label stays text
	d = DocFromRichtext([]richtext.Block{{Kind: richtext.Paragraph, Spans: []richtext.Span{
		{Kind: richtext.MessageMention, Text: "Private message", ID: erLinkedTS, Off: true}}}})
	if d.Text() != "Private message" {
		t.Errorf("off slug %q", d.Text())
	}
}

// TestEditComposerSlugShots draws the edit composer on Karim's message
// shape, light and dark at 2x, into COMPOSER_AUDIT_SHOTS_DIR (the
// composer-audit-shots workflow). Synthetic data only.
func TestEditComposerSlugShots(t *testing.T) {
	dir := os.Getenv("COMPOSER_AUDIT_SHOTS_DIR")
	if dir == "" {
		t.Skip("COMPOSER_AUDIT_SHOTS_DIR not set")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	app, _ := syntheticApp(t, erSeed)
	stubTyping(t)
	const scale, w, hgt = 2, 760, 200
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			th := theme.New(dark)
			h := &keysHost{slickHost: &slickHost{app: app, th: th}}
			m := erMessage(`[{"type":"rich_text","elements":[{"type":"rich_text_section","elements":[` + erSlugEl +
				`,{"type":"text","text":" the short things are nice, "},{"type":"user","user_id":"UALICE"},` +
				`{"type":"text","text":" "},{"type":"link","url":"https://example.com/labelled","text":"a label"},` +
				`{"type":"text","text":" "},{"type":"emoji","name":"wave","unicode":"1f44b"}]}]}]`)
			c := NewEdit(h, m, func() {})
			t.Cleanup(c.Close)
			var ops op.Ops
			var r input.Router
			for i := 0; i < 5; i++ {
				time.Sleep(40 * time.Millisecond)
				ops.Reset()
				paint.FillShape(&ops, th.C.Bg, clip.Rect{Max: image.Pt(w*scale, hgt*scale)}.Op())
				gtx := layout.Context{Ops: &ops, Metric: unit.Metric{PxPerDp: scale, PxPerSp: scale},
					Constraints: layout.Constraints{Max: image.Pt(w*scale, hgt*scale)}, Now: time.Now(), Source: r.Source()}
				slackc.Frame(gtx)
				off := op.Offset(image.Pt(0, 16*scale)).Push(gtx.Ops)
				c.Layout(gtx)
				off.Pop()
				r.Frame(&ops)
			}
			img := pixeltest.Render(t, w*scale, hgt*scale, &ops)
			f, err := os.Create(filepath.Join(dir, "edit-message-slug-"+mode+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
		})
	}
}
