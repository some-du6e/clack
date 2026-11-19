package richview

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// Karim (2026-11-18, C06089401GT 1795044006.277809): a code host's "Pull
// request opened" message (no blocks, one legacy attachment, edited, with
// a reply) was drawn wrong. Slack's MessageAttachmentGeneric
// (client-boot-render 1NOh, build 031dbe6a7400):
//
//   - the title is AttachmentTitle (boot-shared yvlf): Mrkdwn with
//     noSpecials, so a title that is one <url|label> token draws its label
//     as a link (Clack drew the token, "<https://…|#42 …>"); the title is
//     bold in the text's colour (.c-message_attachment__title), only a
//     title_link or its own links in the link colour;
//   - its rows are the author, the title, the text, the fields, the
//     footer, the media, then the actions (Ea, c-message_attachment__row
//     --actions, 6px under the row before): the legacy button goes under
//     the footer, not above it.
func TestLegacyAttachmentPR(t *testing.T) {
	st := newTestStyle()
	acts, _ := json.Marshal([]map[string]any{{"id": "1", "name": "comment", "text": "Comment", "type": "button", "value": `{"pr":42}`, "style": ""}})
	d := attDoc("", slack.Attachment{ID: 1, Color: "6f42c1", Fallback: "[example/dns] Pull request opened",
		Pretext: "Pull request opened by <https://code.example.com/someone|someone>",
		Title:   "<https://code.example.com/example/dns/pull/42|#42 Update record for demo.example.com>",
		Text:    "*DNS Editor submission*\n\nOpened by someone.\n\n*Edit*\n\nReplacing `CNAME a.example.com.` under `demo.example.com`.",
		Fields:  []slack.AttachmentField{{Title: "Reviewers", Value: "<https://code.example.com/t/infra|@example/infra>", Short: true}},
		Footer:  "<https://code.example.com/example/dns|example/dns>", FooterIcon: "https://code.example.com/icon.png",
		TS: json.RawMessage(`1762700000`), MrkdwnIn: []string{"text"}, Actions: acts},
		slack.Attachment{ID: 2, Color: "6f42c1", Fallback: "plain", Title: "Plain title", Text: "body"})
	v := layoutAt(st, d, Options{}, 700)
	dd := &v.d
	words := fragStrings(dd)
	drawn := strings.Join(words, " ")
	for _, w := range words {
		if strings.Contains(w, "<") || strings.Contains(w, "code.example.com/example/dns/pull") {
			t.Errorf("the title's token is drawn: %q (frags %q)", w, drawn)
		}
	}
	if !strings.Contains(drawn, "#42 Update record for demo.example.com") {
		t.Errorf("no title label; frags %q", drawn)
	}
	p := &st.Palette
	var label, plain, footer, comment *frag
	for _, f := range textFrags(dd) {
		switch {
		case f.sh.str == "Update" && label == nil:
			label = f
		case f.sh.str == "Plain":
			plain = f
		case f.sh.str == "example/dns":
			footer = f
		case f.sh.str == "Comment":
			comment = f
		}
	}
	if label == nil || label.col != p.Link {
		t.Errorf("the title's link label %+v, want the link colour", label)
	}
	if plain == nil || plain.col != p.Text {
		t.Errorf("a title with no link %+v, want the text colour %v, not the link's", plain, p.Text)
	}
	if footer == nil || comment == nil {
		t.Fatalf("footer %v, button %v; frags %q", footer != nil, comment != nil, drawn)
	}
	if comment.top < footer.bot {
		t.Errorf("the legacy button (line %d-%d) is above the footer (%d-%d): Slack's actions row comes after it", comment.top, comment.bot, footer.top, footer.bot)
	}
	var btn *box
	for i := range dd.boxes {
		if b := &dd.boxes[i]; b.target >= 0 && int(b.target) < len(dd.targets) && dd.targets[b.target].kind == tButton {
			btn = b
		}
	}
	if btn == nil {
		t.Fatal("no button box")
	}
	if gap := btn.r.Min.Y - footer.bot; gap < 4 || gap > 12 {
		t.Errorf("the button is %dpx under the footer's line, want the actions row's 6px and the button's 2px margin", gap)
	}
}

// A bot's attachment with an image, a footer and a button: Slack's
// MessageAttachmentGeneric v1 (client-boot-render 1NOh) draws its rows as
// author, title, text, fields, footer (YNNH), media (jtXh), then the
// actions (Ea), so an alert's chart sits under "Monitoring | Nov 9th"
// and above its button. Clack drew the image above the footer.
func TestLegacyAttachmentMediaAfterFooter(t *testing.T) {
	st := newTestStyle()
	acts, _ := json.Marshal([]map[string]any{{"id": "1", "name": "ack", "text": "Acknowledge", "type": "button", "value": "a1"}})
	d := attDoc("", slack.Attachment{ID: 1, Color: "danger", Fallback: "CPU high on web-3", Title: "CPU above 90% on web-3",
		Text: "for ten minutes", ImageURL: "https://img.test/chart.png", ImageWidth: 600, ImageHeight: 300,
		Footer: "Monitoring", TS: json.RawMessage(`1762700000`), Actions: acts})
	v := layoutAt(st, d, Options{}, 700)
	dd := &v.d
	var footer, btn *frag
	for _, f := range textFrags(dd) {
		switch f.sh.str {
		case "Monitoring":
			footer = f
		case "Acknowledge":
			btn = f
		}
	}
	var img *imgPrim
	for i := range dd.imgs {
		if dd.imgs[i].url == "https://img.test/chart.png" {
			img = &dd.imgs[i]
		}
	}
	if footer == nil || btn == nil || img == nil {
		t.Fatalf("footer %v, button %v, image %v; frags %q", footer != nil, btn != nil, img != nil, fragStrings(dd))
	}
	if img.r.Min.Y < footer.bot {
		t.Errorf("the image (%v) is above the footer's line (%d-%d): Slack draws the media after the footer", img.r, footer.top, footer.bot)
	}
	if btn.top < img.r.Max.Y {
		t.Errorf("the button (line %d-%d) is above the image's bottom (%d): Slack's actions come after the media", btn.top, btn.bot, img.r.Max.Y)
	}
	if img.r.Dx() != 360 || img.r.Dy() != 180 {
		t.Errorf("image %v, want AttachmentMedia's 360x180 for a 600x300 image", img.r)
	}
}
