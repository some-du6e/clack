package inspect

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/some-du6e/clack/internal/slack"
)

// The /remind confirmation as Slack sends it (captured 11-24, in a
// self-DM): one Slackbot ephemeral, one rich_text block of an emoji and a
// text element, no attachments. Its shape says so, and nothing it holds.
func TestMessageShapeOfRemindConfirmation(t *testing.T) {
	r := NewRedactor()
	m := slack.Message{Type: "message", Subtype: "bot_message", User: "USLACKBOT", BotID: "B01", IsEphemeral: true, TS: "1795558164.745689",
		Text: "I will remind you “stretch your legs” at 3PM tomorrow.",
		Blocks: json.RawMessage(`[{"type":"rich_text","block_id":"aB1","elements":[{"type":"rich_text_section","elements":[` +
			`{"type":"emoji","name":"thumbsup","unicode":"1f44d"},{"type":"text","text":" I will remind you “stretch your legs” at 3PM tomorrow."}]}]}]`)}
	got := r.MessageShape(&m)
	want := []string{
		"msg: subtype=bot_message · user=USLACKBOT · bot=B01 · ephemeral",
		"text: set (plain)",
		"blocks: rich_text(rich_text_section(emoji, text))",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("shape:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, leak := range []string{"stretch", "remind", "thumbsup", "1f44d", "aB1", "1795558164"} {
		if strings.Contains(strings.Join(got, " "), leak) {
			t.Errorf("the shape keeps %q: %v", leak, got)
		}
	}
}

// A variant with a labelled mention whose label is a lone U+200B (a real
// name like Karim's), a channel link, a link, and a legacy attachment with
// its text and two buttons: the mention kinds with their ids' first
// letters, the label's invisible character as a code point, the
// attachment's field names, colour and button styles. No names, labels,
// URLs or ids.
func TestMessageShapeKeepsStructureOnly(t *testing.T) {
	r := NewRedactor()
	atts := json.RawMessage(`[{"id":1,"callback_id":"secret_callback","fallback":"Delete it","text":"I will remind <@U082DRXPMF1|\u200b> to sell","actions":[` +
		`{"id":"1","name":"delete","text":"Delete","type":"button","style":"danger","value":"Rm0SECRET"},{"id":"2","name":"list","text":"View Reminders","type":"button"}]}]`)
	m := slack.Message{Subtype: "bot_message", User: "USLACKBOT", BotID: "B01", IsEphemeral: true,
		Text:           "I will remind <@U082DRXPMF1|\u200b> “sell your soul” in <#C0B4DEFKFCJ|clack> <https://example.com/x|here>",
		RawAttachments: atts}
	got := strings.Join(r.MessageShape(&m), "\n")
	for _, want := range []string{
		"text: set · tokens=[@U:", "|label(U+200B)", ", #C:", "|label, link:https|label]",
		"attachment 1: fields=actions,callback_id,fallback,id,text · color=none · actions(button:danger, button)",
		"text: set · tokens=[@U:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in\n%s", want, got)
		}
	}
	for _, leak := range []string{"U082DRXPMF1", "C0B4DEFKFCJ", "clack", "example.com", "sell", "soul", "Delete", "View Reminders", "secret", "Rm0SECRET", "here"} {
		if strings.Contains(got, leak) {
			t.Errorf("the shape keeps %q:\n%s", leak, got)
		}
	}
	// one id has one tag within a report
	if a, b := r.pref("U082DRXPMF1"), r.pref("U082DRXPMF1"); a != b || !strings.HasPrefix(a, "U:") {
		t.Errorf("pref %q / %q", a, b)
	}
}

// The report shows the shape as its "message" lines.
func TestReportLinesShowTheMessageShape(t *testing.T) {
	rep := Report{Message: []string{"msg: subtype=bot_message", "blocks: rich_text(rich_text_section(text))"}}
	var n int
	for _, l := range rep.Lines() {
		if l.Key == "message" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d message lines in %v", n, rep.Lines())
	}
}
