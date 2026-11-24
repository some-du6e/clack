package core

// Karim's report on α.165 (2026-11-24): a canvas whose title has an emoji,
// a mention, a link or a line break couldn't be edited at all, because
// every save sends the title's metadata and Clack refused to build it
// (CanvasTitleSaveable). Each canvas here has such a title; a body edit
// saves through edit-document with the title metadata Slack's
// extractTitleMetadata gives for it (its values from the quip-save-title
// snapshot, Slack's modules run in node), and the strict fake accepts it.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/quip"
	"github.com/some-du6e/clack/internal/richtext"
)

// slackTitleCase is a quip-save-title case's Slack side.
type slackTitleCase struct {
	Title     string   `json:"title"`
	RichText  string   `json:"richText"`
	EmojiInfo []string `json:"emojiInfo"`
}

func slackTitle(t *testing.T, name string) slackTitleCase {
	t.Helper()
	b, err := os.ReadFile("../quip/testdata/parity/quip-save-title.json")
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Cases []struct {
			Name  string         `json:"name"`
			Slack slackTitleCase `json:"slack"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		t.Fatal(err)
	}
	for _, c := range snap.Cases {
		if c.Name == name {
			return c.Slack
		}
	}
	t.Fatalf("no quip-save-title case %q", name)
	return slackTitleCase{}
}

// richTitleSave gives the shared canvas the title, edits a body paragraph
// and returns the edit-document call.
func richTitleSave(t *testing.T, a *App, fs *fakeslack.Server, title string, spans []richtext.Span, info []quip.EmojiInfo) fakeslack.QuipRequest {
	t.Helper()
	fs.SetCanvasRichTitle(fakeslack.CanvasShared, title, spans, info)
	loadModel(t, a, fakeslack.CanvasShared)
	if ok, why := a.CanvasTitleSaveable(fakeslack.CanvasShared); !ok {
		t.Errorf("the canvas can't be edited: %s", why)
	}
	p := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	if err := a.SaveCanvas(context.Background(), fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "Weekly sync, edited"}); err != nil {
		t.Fatalf("the body edit: %v", err)
	}
	rs := saves(fs)
	if len(rs) != 1 || rs[0].Status != http.StatusOK {
		t.Fatalf("edit-document calls %+v", rs)
	}
	fc, _ := fs.Canvas(fakeslack.CanvasShared)
	if b := fc.Doc.Find(*p.ID); b == nil || b.Text() != "Weekly sync, edited" {
		t.Errorf("the export after the save %+v", b)
	}
	if fc.TitleSpans == nil {
		t.Error("the save changed the title section")
	}
	return rs[0]
}

func TestCanvasRichTitleSaves(t *testing.T) {
	txt := func(s string) richtext.Span { return richtext.Span{Kind: richtext.Text, Text: s} }
	for _, c := range []struct {
		name, snapshot string
		spans          []richtext.Span
		info           []quip.EmojiInfo
	}{
		{"emoji", "emoji, loaded", []richtext.Span{txt("Launch "), {Kind: richtext.Emoji, ID: "tada", Text: ":tada:"}},
			[]quip.EmojiInfo{{TeamID: quip.Ptr("T0FAKE0001"), DisplayName: quip.Ptr("tada"), Unicode: quip.Ptr("1f389"),
				DisplayURL: quip.Ptr("https://example.com/tada.png")}}},
		{"mention", "user", []richtext.Span{txt("Notes for "), {Kind: richtext.User, ID: "U0FAKE00001", Text: "@pat"}}, nil},
		{"link", "link with a path", []richtext.Span{txt("Read "), {Kind: richtext.Link, Text: "the docs", URL: "https://example.com/a"}, txt(" first")}, nil},
		{"line break", "line break", []richtext.Span{txt("Line one"), {Kind: richtext.Break}, txt("Line two")}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			want := slackTitle(t, c.snapshot)
			a, fs := canvasApp(t)
			r := richTitleSave(t, a, fs, want.Title, c.spans, c.info)
			if got := r.Form.Get("title"); got != want.Title {
				t.Errorf("title %q, Slack's %q", got, want.Title)
			}
			if got := hex.EncodeToString(quip.Marshal(r.Save.TitleRichText)); got != want.RichText {
				t.Errorf("title_rich_text_api\nclack: %s\nslack: %s", got, want.RichText)
			}
			var info []string
			for i := range r.Save.TitleSlackEmojiInfo {
				info = append(info, hex.EncodeToString(quip.Marshal(&r.Save.TitleSlackEmojiInfo[i])))
			}
			if strings.Join(info, ",") != strings.Join(want.EmojiInfo, ",") {
				t.Errorf("title emoji info %v, Slack's %v", info, want.EmojiInfo)
			}
		})
	}
}

// A channel mention's label is the channel's privacy as the client knows
// it ("#channel", "#private channel").
func TestCanvasRichTitleChannel(t *testing.T) {
	a, fs := canvasApp(t)
	ch, private := "", false
	for _, id := range fs.ChannelIDs() {
		if c, ok := a.S.Conv(id); ok && !c.IsIM && !c.IsMPIM {
			ch, private = id, c.IsPrivate
			break
		}
	}
	if ch == "" {
		t.Fatal("the client knows no channel")
	}
	label := "#channel"
	if private {
		label = "#private channel"
	}
	r := richTitleSave(t, a, fs, "Plans in "+label, []richtext.Span{{Kind: richtext.Text, Text: "Plans in "}, {Kind: richtext.Channel, ID: ch, Text: "#x"}}, nil)
	if got := r.Form.Get("title"); got != "Plans in "+label {
		t.Errorf("title %q", got)
	}
	rt := r.Save.TitleRichText
	if rt == nil || len(rt.Elements) != 1 || len(rt.Elements[0].Elements) != 2 {
		t.Fatalf("title_rich_text_api %+v", rt)
	}
	if e := rt.Elements[0].Elements[1]; e.Type == nil || *e.Type != "channel" || e.ChannelID == nil || *e.ChannelID != ch {
		t.Errorf("the channel's element %+v", e)
	}
}

// A date in the title is still refused (its title text is dJkV.lC's
// date, which the oracle can't run), so the canvas says why and no save
// goes out.
func TestCanvasRichTitleDateRefused(t *testing.T) {
	a, fs := canvasApp(t)
	fs.SetCanvasRichTitle(fakeslack.CanvasShared, "Due Nov 7", []richtext.Span{{Kind: richtext.Text, Text: "Due "}, {Kind: richtext.Date, ID: "1762473600", Text: "{date_short}"}}, nil)
	loadModel(t, a, fakeslack.CanvasShared)
	if ok, why := a.CanvasTitleSaveable(fakeslack.CanvasShared); ok || !strings.Contains(why, "date") {
		t.Errorf("a title with a date: %v (%q)", ok, why)
	}
	p := modelSection(t, a, fakeslack.CanvasShared, "Weekly sync")
	if err := a.SaveCanvas(context.Background(), fakeslack.CanvasShared, quip.Edit{Kind: quip.EditText, SectionID: *p.ID, RTML: "Weekly sync, edited"}); !errors.Is(err, quip.ErrUnsupported) {
		t.Errorf("the body edit under a dated title: %v", err)
	}
	if n := len(saves(fs)); n != 0 {
		t.Errorf("%d edit-document calls", n)
	}
}
