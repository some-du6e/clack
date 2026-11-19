package richview

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	rt "github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/slack"
)

// emailRes names the #email-inbox channel and one member, as the store
// would for an email's recipients.
type emailRes struct{ codeRes }

func (emailRes) ChannelName(id string) string {
	if id == "C0EMAIL0001" {
		return "#email-inbox"
	}
	return id
}

func (emailRes) UserName(id string) string { return "@" + id }

// emailMessage is an email file message in the shape Slack sends (read
// from Hack Club's on 2026-11-18: the Email app's upload, no text, a file
// of mode "email" with from, to, cc, subject, the HTML preview and its
// plain text, headers, email_security and attachments), posted ts.
func emailMessage(t *testing.T, ts string, file map[string]any) rt.Doc {
	t.Helper()
	f := map[string]any{"id": "F0EMAIL0001", "name": "Lunch on Friday?", "title": "Lunch on Friday?", "mimetype": "text/html",
		"filetype": "email", "pretty_type": "Email", "mode": "email", "user": "USLACKBOT", "size": 120, "display_as_bot": true,
		"url_private": "https://files.slack.test/files-pri/T0-F0EMAIL0001/email", "permalink": "https://x.slack.test/files/USLACKBOT/F0EMAIL0001/email",
		"to":      []map[string]any{{"address": "", "name": "C0EMAIL0001", "original": "email-inbox"}},
		"from":    []map[string]any{{"address": "alex@mail.example", "name": "Alex Rivera", "original": "Alex Rivera <alex@mail.example>"}},
		"cc":      []any{},
		"subject": "Lunch on Friday?", "plain_text": "Hi team,\nAnyone up for lunch on Friday at noon?\nAlex",
		"preview":            "<div>Hi team,</div><div>Anyone up for lunch on Friday at noon?</div><div>Alex</div>",
		"preview_plain_text": "Hi team,\nAnyone up for lunch on Friday at noon?\nAlex",
		"headers":            map[string]any{"date": "Tue, 11 Nov 2025 10:00:00 +0000", "in_reply_to": nil, "reply_to": nil, "message_id": "<x@mail.example>"},
		"email_security":     map[string]any{"has_valid_dmarc": true}, "attachments": []any{}, "original_attachment_count": 0,
		"inline_attachment_count": 0, "has_more": false, "sent_to_self": false, "bot_id": "B0FAKEMAIL", "app_id": "A0FAKEMAIL",
		"timestamp": 1762800000}
	for k, v := range file {
		f[k] = v
	}
	raw, err := json.Marshal(map[string]any{"type": "message", "ts": ts, "user": "USLACKBOT", "bot_id": "B0FAKEMAIL",
		"display_as_bot": true, "upload": true, "text": "", "files": []any{f}})
	if err != nil {
		t.Fatal(err)
	}
	var m slack.Message
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return rt.FromMessage(emailRes{}, &m)
}

func docFile(t *testing.T, d rt.Doc) *rt.File {
	t.Helper()
	for _, b := range d.Blocks {
		if b.Kind == rt.FileBlock && b.File != nil {
			return b.File
		}
	}
	t.Fatalf("no file block in %+v", d.Blocks)
	return nil
}

func hasIcon(d *display, name string) bool {
	for _, o := range d.overlayIcons {
		if o.name == name {
			return true
		}
	}
	return false
}

func plainHas(v *View, s string) bool { return strings.Contains(string(v.d.plain), s) }

// drawnText is the drawn words in order, a space between them (a word
// the flow broke after a hyphen joined again).
func drawnText(d *display) string {
	return strings.ReplaceAll(strings.Join(fragStrings(d), " "), "- ", "-")
}

// drawnHas reports whether s is drawn, word by word (plainHas reads the
// copy text, which keeps what the layout cut).
func drawnHas(v *View, s string) bool { return strings.Contains(drawnText(&v.d), s) }

// Karim (2026-11-17, #clack 1794883661.462709): an email the Email app
// posted drew as a plain file card whose type tile read "EMAI". Slack's
// (new_email_rendering, on for Hack Club): MessageFile's email header, at
// most 594 wide: the email file icon (a #1D9BD1 tile, a white envelope),
// the subject in bold and the message's time under it ("Today at …");
// no type tile, no pretty_type line.
func TestEmailFileCard(t *testing.T) {
	st := newTestStyle()
	ts := itoa(time.Now().Unix()) + ".000200" // today
	d := emailMessage(t, ts, nil)
	v := layoutAt(st, d, Options{CompactFiles: true, Channel: "C0EMAIL0001", TS: ts}, 900)
	cards := boxesOf(&v.d, bBorder)
	if len(cards) != 1 || cards[0].r.Dx() != 594 || cards[0].r.Dy() != 62 {
		t.Fatalf("email card %+v, want one 594x62 pillow (.p-message_file--email)", cards)
	}
	for _, f := range textFrags(&v.d) {
		if f.sh.str == "EMAI" || f.sh.str == "Email" {
			t.Errorf("the generic file card's %q is drawn", f.sh.str)
		}
	}
	if !hasIcon(&v.d, "file-email") {
		t.Errorf("no email file icon (c-icon--file-email-svgicon); icons %+v", v.d.overlayIcons)
	}
	if !drawnHas(v, "Lunch on Friday?") {
		t.Errorf("no subject drawn; words %q", drawnText(&v.d))
	}
	if !strings.Contains(strings.Join(fragStrings(&v.d), " "), "Today at ") {
		t.Errorf("no \"Today at …\" detail (toHumanReadableDateAndTimePure); frags %q", fragStrings(&v.d))
	}
	// collapsed, nothing of the email shows
	if plainHas(v, "Anyone up for lunch") || plainHas(v, "alex@mail.example") {
		t.Errorf("a collapsed email draws its unfurl: %q", string(v.d.plain))
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// Expanded (FileActions' Expand, fetchAndExpand), FileEmailV2 under the
// header: From (the name and the address chip with the email glyph), To
// (the channel the email went to, by name), the body (the email's HTML,
// laid out: a 150px box for a plain text of 150 characters and 3 lines),
// no attachments.
func TestEmailFileExpanded(t *testing.T) {
	st := newTestStyle()
	ts := itoa(time.Now().Add(-48*time.Hour).Unix()) + ".000200"
	d := emailMessage(t, ts, nil)
	docFile(t, d).Expanded = true
	v := layoutAt(st, d, Options{CompactFiles: true, Channel: "C0EMAIL0001", TS: ts}, 900)
	for _, s := range []string{"From", "To", "Alex Rivera", "alex@mail.example", "#email-inbox", "Anyone up for lunch on Friday at noon?"} {
		if !drawnHas(v, s) {
			t.Errorf("expanded email draws no %q; words %q", s, drawnText(&v.d))
		}
	}
	if !hasIcon(&v.d, "email") {
		t.Error("the address chip has no email glyph")
	}
	cards := boxesOf(&v.d, bBorder)
	if len(cards) != 1 {
		t.Fatalf("%d pillows", len(cards))
	}
	// header 62 + the recipient rows (1+8+22+8, a rule, 8+22+8, 1) + the 150px body
	if h := cards[0].r.Dy(); h < 62+70+150 || h > 62+90+150 {
		t.Errorf("expanded email %dpx tall, want the header, two rows and the 150px body", h)
	}
	if hasFrag(&v.d, "Heads up — we couldn’t verify this email’s sender. Make sure you know them before you open any links or attachments.") {
		t.Error("a verified sender gets the warning")
	}
}

// An HTML email whose sender failed DMARC: FileEmailV2's warning under
// From; the body's markup read (a heading, a link, a list, a table's
// cells), its style, title and hidden parts left out, cut at 400px (the
// plain text is past 400 characters).
func TestEmailFileHTML(t *testing.T) {
	st := newTestStyle()
	ts := itoa(time.Now().Add(-48*time.Hour).Unix()) + ".000200"
	html := "<html><head><style>p{color:red}</style><title>ignored title</title></head><body><h1>The Weekly Build</h1>" +
		"<p>Hello <b>makers</b>, read the <a href=\"https://example.com/notes\">full notes</a>.</p>" +
		"<ul><li>A faster sidebar</li><li>Email previews</li></ul><table><tr><td>Builds</td><td>42</td></tr></table>" +
		"<p style=\"display:none\">tracking pixel text</p>" + strings.Repeat("<p>More news.</p>", 40) + "<p>The very end</p></body></html>"
	d := emailMessage(t, ts, map[string]any{"preview": html, "plain_text": strings.Repeat("More news. ", 60),
		"email_security": map[string]any{"has_valid_dmarc": false},
		"cc":             []map[string]any{{"address": "team@build.example", "name": "", "original": "team@build.example"}}})
	docFile(t, d).Expanded = true
	v := layoutAt(st, d, Options{CompactFiles: true, Channel: "C0EMAIL0001", TS: ts}, 900)
	if !plainHas(v, "Heads up") {
		t.Error("no Heads up for a sender that failed DMARC")
	}
	for _, s := range []string{"Cc", "team@build.example", "The Weekly Build", "makers", "full notes", "A faster sidebar", "Builds"} {
		if !drawnHas(v, s) {
			t.Errorf("the expanded HTML email draws no %q; words %q", s, drawnText(&v.d))
		}
	}
	for _, s := range []string{"p{color:red}", "ignored title", "tracking pixel text", "The very end"} {
		if drawnHas(v, s) {
			t.Errorf("%q is drawn", s)
		}
	}
	link := false
	for _, tg := range v.d.targets {
		if tg.kind == tLink && tg.url == "https://example.com/notes" {
			link = true
		}
	}
	if !link {
		t.Error("the body's link isn't a link")
	}
}

// FileActions on an email (new_email_rendering): Expand (expand-diagonal)
// while collapsed, Collapse (reduce-diagonal) once expanded, then Share,
// View file details, More actions; no Download, and no Open in new window
// (renderExternal returns null for an email with the flag on).
func TestEmailFileActions(t *testing.T) {
	ts := itoa(time.Now().Unix()) + ".000200"
	f := docFile(t, emailMessage(t, ts, nil))
	var acts []FileAction
	var icons []string
	for _, b := range fileActions(f, false, false) {
		acts = append(acts, b.act)
		icons = append(icons, b.icon)
	}
	want := []FileAction{FileShowMore, FileShare, FileDetails, FileMore}
	if len(acts) != len(want) {
		t.Fatalf("collapsed email's FileActions %v (%v), want %v", acts, icons, want)
	}
	for i := range want {
		if acts[i] != want[i] {
			t.Fatalf("collapsed email's FileActions %v, want %v", acts, want)
		}
	}
	if icons[0] != "expand" {
		t.Errorf("Expand's icon %q, want expand-diagonal", icons[0])
	}
	f.Expanded = true
	bs := fileActions(f, false, false)
	if len(bs) == 0 || bs[0].act != FileCollapse || bs[0].icon != "collapse" {
		t.Errorf("expanded email's first action %+v, want Collapse with reduce-diagonal", bs)
	}
}

// getEmailContentHeight (boot-shared wwJm) measures the plain text's
// e.length, UTF-16 units: 80 emoji are 160 of them, so the body gets the
// medium 300px box, not the small 150px one (review of karim-1118).
func TestEmailBodyHeightUTF16(t *testing.T) {
	if h := emailBodyH(strings.Repeat("\U0001F600", 80)); h != 300 {
		t.Errorf("80 emoji: %dpx, want 300 (160 UTF-16 units, past 150)", h)
	}
	if h := emailBodyH(strings.Repeat("a", 150)); h != 150 {
		t.Errorf("150 letters: %dpx, want 150", h)
	}
}
