package richview

import (
	"testing"
	"time"
)

// TestEmailBodyLinksOpenNoURLCard (karim-1118's review): an email body's
// links are the email's (Slack draws the body in EmailIframe, where a link
// has no UrlHoverCard and an http(s) one asks first, "Open Link"), so with
// truncated_url_hover_card on they open no hover card, whose "Open in new
// tab" would skip that question. A message's own links still do.
func TestEmailBodyLinksOpenNoURLCard(t *testing.T) {
	prev := URLHoverCard
	URLHoverCard = func(string) bool { return true }
	t.Cleanup(func() { URLHoverCard = prev })
	st := newTestStyle()
	ts := itoa(time.Now().Add(-48*time.Hour).Unix()) + ".000300"
	html := `<p>Read the <a href="https://example.com/notes">full notes</a> today.</p>`
	d := emailMessage(t, ts, map[string]any{"preview": html, "plain_text": "Read the full notes today."})
	docFile(t, d).Expanded = true
	v := layoutAt(st, d, Options{CompactFiles: true, Channel: "C0EMAIL0001", TS: ts}, 900)
	found := false
	for i, tg := range v.d.targets {
		if tg.kind != tLink || tg.url != "https://example.com/notes" {
			continue
		}
		found = true
		if v.isURLCard(int16(i)) {
			t.Errorf("an email body's link (target %d) opens the URL hover card", i)
		}
	}
	if !found {
		t.Fatal("the email body's link isn't a link target")
	}
}
