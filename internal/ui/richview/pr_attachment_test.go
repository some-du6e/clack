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
