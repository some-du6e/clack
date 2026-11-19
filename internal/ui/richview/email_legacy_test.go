package richview

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// new_email_rendering off (review of karim-1118: Clack never read it):
// Slack draws FileEmail (boot-shared 2bi5 `ee`), not FileEmailV2's card.
// Collapsed it's the small density view: the subject in bold with the
// date on the right, " From Alex Rivera " and the start of the text, no
// email icon header, no Expand or Collapse button; a click expands it.
// Expanded it's the header table (From, To, Subject, Date) over the
// email. The meta text is "Email from {appName}", "Unknown email app" for
// an app with no name; with the flag on and no name it's "Email".
func TestEmailLegacyFileEmail(t *testing.T) {
	st := newTestStyle()
	setEmailRendering(st, false)
	ts := itoa(time.Now().Unix()) + ".000200"
	d := emailMessage(t, ts, nil)
	opts := Options{CompactFiles: true, Channel: "C0EMAIL0001", TS: ts}
	v := layoutAt(st, d, opts, 900)
	dd := &v.d
	if hasIcon(dd, "file-email") {
		t.Error("FileEmailV2's header (the email icon) is drawn with the flag off")
	}
	drawn := drawnText(dd)
	for _, s := range []string{"Lunch on Friday?", "From Alex Rivera", "Hi team, Anyone up for lunch"} {
		if !strings.Contains(drawn, s) {
			t.Errorf("collapsed FileEmail lacks %q; frags %q", s, drawn)
		}
	}
	expand := false
	for _, h := range dd.hits {
		if h.t >= 0 && int(h.t) < len(dd.targets) && dd.targets[h.t].kind == tSnippet && dd.targets[h.t].act == FileShowMore {
			expand = true
		}
	}
	if !expand {
		t.Error("a click on the collapsed FileEmail doesn't expand it (expandEmail)")
	}
	f := docFile(t, d)
	ext := false
	for _, b := range emailActions(f, true) {
		if b.act == FileShowMore || b.act == FileCollapse {
			t.Errorf("an email's %q button with the flag off (renderEmailExpand and renderCollapse draw nothing)", b.tip)
		}
		ext = ext || b.act == FileExternal
	}
	if !ext {
		t.Error("no Open in new window on an email with the flag off (renderExternal: isEmail)")
	}
	for _, b := range emailActions(f, false) {
		if b.act == FileExternal {
			t.Error("Open in new window on an email with the flag on (renderExternal returns null)")
		}
	}
	f.Expanded = true
	v = layoutAt(st, d, opts, 900)
	drawn = drawnText(&v.d)
	for _, s := range []string{"From", "Alex Rivera", "<alex@mail.example>", "To", "Subject", "Date", "Anyone up for lunch on Friday at noon?"} {
		if !strings.Contains(drawn, s) {
			t.Errorf("expanded FileEmail lacks %q; frags %q", s, drawn)
		}
	}
	for _, b := range emailActions(f, true) {
		if b.act == FileCollapse {
			t.Error("an expanded email's Collapse with the flag off")
		}
	}
	if got := emailMeta(f, true); got != "Email from Unknown email app" {
		t.Errorf("meta text, flag off, no app name: %q", got)
	}
	f.Email.AppName = "Mailer"
	if got := emailMeta(f, true); got != "Email from Mailer" {
		t.Errorf("meta text, flag off: %q", got)
	}
	f.Email.AppName = ""
	if got := emailMeta(f, false); got != "Email" {
		t.Errorf("meta text, flag on, no app name: %q", got)
	}
}

// FileEmailV2's recipient rows (SenderRecipientRow, boot-shared 2bi5):
// past three wrapped rows a row is cut at the third's bottom with "Show
// more" under it; open, every recipient shows and "Show less". Review of
// karim-1118: a long To list filled as many rows as it needed.
func TestEmailRecipientsShowMore(t *testing.T) {
	st := newTestStyle()
	var to []map[string]any
	for i := 0; i < 30; i++ {
		to = append(to, map[string]any{"address": fmt.Sprintf("person%02d@mail.example", i), "name": "", "original": ""})
	}
	ts := itoa(time.Now().Unix()) + ".000200"
	d := emailMessage(t, ts, map[string]any{"to": to})
	f := docFile(t, d)
	f.Expanded = true
	t.Cleanup(func() { openRecipients(f, "To", false) })
	opts := Options{CompactFiles: true, Channel: "C0EMAIL0001", TS: ts}
	v := layoutAt(st, d, opts, 900)
	drawn := drawnText(&v.d)
	if !strings.Contains(drawn, "Show more") {
		t.Fatalf("no Show more under 30 recipients; frags %q", drawn)
	}
	if strings.Contains(drawn, "person29@mail.example") {
		t.Error("the last recipient is drawn past the third row")
	}
	openRecipients(f, "To", true)
	v = layoutAt(st, d, opts, 900)
	drawn = drawnText(&v.d)
	if !strings.Contains(drawn, "Show less") || !strings.Contains(drawn, "person29@mail.example") {
		t.Errorf("open: frags %q, want every recipient and Show less", drawn)
	}
}
