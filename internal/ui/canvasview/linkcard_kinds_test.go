package canvasview

import (
	"encoding/json"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"gioui.org/io/pointer"
	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// TestCardURLSafe: isUrlSafeFromXSS (I5QS) decides EmptyUnfurl's "Double-
// check this link". A plain web link is safe and so is text that isn't a
// URL; a script, a ; anywhere, a ' outside the query (an http(s) query's
// is escaped), credentials, another scheme or a decoded <script aren't.
func TestCardURLSafe(t *testing.T) {
	for s, want := range map[string]bool{
		"https://example.com/repo":           true,
		"":                                   true,
		"not a url at all":                   true,
		"https://example.com/?q=it's":        true,
		"data:text/plain;base64,aGVsbG8=":    true,
		"https://example.com/%E0%A4%A":       true, // decodeURIComponent throws: the patterns alone
		"javascript:alert(1)":                false,
		"https://example.com/a;b":            false,
		"https://example.com/it's":           false,
		"https://example.com/#it's":          false,
		"https://user:pw@example.com/":       false,
		"mailto:someone@example.com":         false,
		"https://example.com/%3Cscript%3E":   false,
		"data:text/html;base64,PHNjcmlwdD4=": false, // <script>
	} {
		if got := urlSafeOf(s); got != want {
			t.Errorf("urlSafe(%q) = %v, want %v", s, got, want)
		}
	}
}

// TestCardPermalinks: parsePermalink (ui7s) as a card routes by it: a
// workspace host's archives (with a ts: a message; without: a channel),
// files, team, docs (not on app.slack.com), lists; anything else isn't one.
func TestCardPermalinks(t *testing.T) {
	type want struct {
		typ, ts string
		ok      bool
	}
	for u, w := range map[string]want{
		"https://fake.slack.com/archives/C0FAKE00000/p1762500000000100":  {"message", "1762500000.000100", true},
		"https://fake.slack.com/archives/C0FAKE00000":                    {"message", "", true},
		"https://fake.slack.com/files/U0/F0CANVFILE1/report.pdf":         {"file", "", true},
		"https://fake.slack.com/team/U0FAKE00001":                        {"member", "", true},
		"https://fake.slack.com/docs/T0FAKE0001/F0CANVAS002":             {"quip", "", true},
		"https://fake.enterprise.slack.com/lists/T0FAKE0001/F0LIST00001": {"list", "", true},
		"https://app.slack.com/docs/T0FAKE0001/F0CANVAS002":              {"", "", false},
		"https://app.slack.com/client/T0FAKE0001/C0FAKE00000":            {"", "", false},
		"https://a.b.slack.com/archives/C0FAKE00000":                     {"", "", false},
		"https://example.com/archives/C0FAKE00000":                       {"", "", false},
	} {
		typ, ts, ok := permalinkOf(u)
		if ok != w.ok || ok && (typ != w.typ || ts != w.ts) {
			t.Errorf("%s: %q %q %v, want %q %q %v", u, typ, ts, ok, w.typ, w.ts, w.ok)
		}
	}
}

// TestCardPublicLinkShape: shouldRenderPublicLinkCard: a web link's own
// preview (a title or text, not an app's, no video or audio, no blocks
// that aren't a fallback, not Slack's own callbacks); a tweet always.
func TestCardPublicLinkShape(t *testing.T) {
	web := slack.Attachment{Title: "Example repo", TitleLink: cardURL, Text: "A repository", ServiceName: "example.com", OriginalURL: cardURL}
	app := web
	app.IsAppUnfurl = true
	bare := slack.Attachment{OriginalURL: cardURL}
	tweet := slack.Attachment{ServiceName: "twitter", AuthorName: "someone"}
	blocks := web
	blocks.Blocks = json.RawMessage(`[{"type":"section"}]`)
	video := web
	video.VideoHTML = "<iframe></iframe>"
	var slackbot slack.Attachment // callback_id through JSON, as Slack sends it
	if err := json.Unmarshal([]byte(`{"title":"Example repo","text":"A repository","callback_id":"slackbotresponse_button"}`), &slackbot); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		a    slack.Attachment
		want bool
	}{"web": {web, true}, "app": {app, false}, "no title or text": {bare, false}, "tweet": {tweet, true},
		"blocks": {blocks, false}, "video": {video, false}, "slackbot": {slackbot, false}} {
		if got := publicShape(&c.a); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// softWait frames for up to d while cond is false: what's drawn then is
// what the checks after it read (on code without the card, they fail
// there, not in a timeout).
func softWait(s *shooter, v *View, d time.Duration, cond func() bool) {
	for end := time.Now().Add(d); !cond() && time.Now().Before(end); {
		s.frame(func(gtx layout.Context) { v.Layout(gtx) })
		time.Sleep(10 * time.Millisecond)
	}
}

// webUnfurl is a container holding a web link's own preview (no app): what
// Slack draws as EmbeddedPublicLinkUnfurl.
func webUnfurl(ts string) *slack.Message {
	return unfurledAs(&slack.Message{Type: "message", TS: ts, Attachments: []slack.Attachment{{ID: 1,
		Title: "Example repo", TitleLink: cardURL, Text: "A synthetic repository description", ServiceName: "example.com",
		ServiceURL: "https://example.com", FromURL: cardURL, OriginalURL: cardURL, Fallback: "Example repo"}}}, true)
}

// TestRenderLinkCardPublic: a web link's own preview draws Slack's public
// link card: the title, the text, and "example.com | Added on Nov 7th,
// 2025" (when the link was added to the canvas: the container's ts).
func TestRenderLinkCardPublic(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, m := range []struct {
			mode Mode
			sz   image.Point
		}{{Full, image.Pt(1100, 900)}, {Tab, image.Pt(900, 700)}} {
			_, _, open := cardEnv(t, dark, webUnfurl(cardTS), cardTS, nil)
			v, s := open(m.mode, m.sz)
			softWait(s, v, 8*time.Second, func() bool { k, _, _ := cardState(v); return k == "public" })
			if k, _, _ := cardState(v); k != "public" {
				t.Errorf("mode %d dark %v: the card is %q, want public", m.mode, dark, k)
			}
			txt := embedText(v)
			for _, want := range []string{"Example repo", "A synthetic repository description", "example.com | Added on Nov 7th, 2025"} {
				if !strings.Contains(txt, want) {
					t.Errorf("mode %d dark %v: the card %q has no %q", m.mode, dark, txt, want)
				}
			}
			noLabel(t, v)
		}
	}
}

// TestRenderLinkCardRoutes: useEmbeddedUnfurlType's routing. A channel's
// archives link has no card in Card (EmptyUnfurl); app.slack.com/docs
// isn't a permalink, so its web preview draws; a work object with layouts
// draws its card, one without draws EmptyUnfurl.
func TestRenderLinkCardRoutes(t *testing.T) {
	const wo = `{"type":"message","ts":"1762500000.000100","document_unfurl":{"is_unfurled":true},"attachments":[
		{"id":1,"from_url":"https://tracker.example.com/T-1","original_url":"https://tracker.example.com/T-1","is_app_unfurl":true,
		 "title":"must not draw","service_name":"must not draw",
		 "work_object_entity":{"work_object_type":"external","entity_id":"W:A1:T-1","app_id":"A1","display_type":"Task",
		  "layouts":{"expanded":{"title":{"text":"Fix it"}}}}}]}`
	woMsg := func(raw string) *slack.Message {
		var m slack.Message
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		return &m
	}
	noLayouts := strings.Replace(wo, `"layouts":{"expanded":{"title":{"text":"Fix it"}}}`, `"title":"no layouts"`, 1)
	for _, c := range []struct {
		name, link string
		m          *slack.Message
		kind, has  string
	}{
		{"channel", "https://fake.slack.com/archives/C0FAKE00000", webUnfurl(cardTS), "empty", "Unable to create preview."},
		{"app docs", "https://app.slack.com/docs/T0FAKE0001/F0CANVAS002", webUnfurl(cardTS), "public", "Example repo"},
		{"work object", "https://tracker.example.com/T-1", woMsg(wo), "work-object", "Fix it"},
		{"work object without layouts", "https://tracker.example.com/T-1", woMsg(noLayouts), "empty", "Unable to create preview."},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.m.Attachments[0].OriginalURL == cardURL {
				c.m.Attachments[0].OriginalURL = c.link
			}
			_, _, open := cardEnvURL(t, false, c.link, c.m, cardTS, nil)
			v, s := open(Full, image.Pt(1100, 900))
			softWait(s, v, 8*time.Second, func() bool {
				k, _, _ := cardState(v)
				return k == c.kind && strings.Contains(embedText(v), c.has)
			})
			if k, _, _ := cardState(v); k != c.kind || !strings.Contains(embedText(v), c.has) {
				t.Errorf("kind %q text %q, want %q with %q", k, embedText(v), c.kind, c.has)
			}
			noLabel(t, v)
		})
	}
}

// TestRenderLinkCardBanner: a card whose URL isUrlSafeFromXSS refuses
// draws EmptyUnfurl's "Double-check this link" instead of its line.
func TestRenderLinkCardBanner(t *testing.T) {
	const link = "https://example.com/a;b"
	_, _, open := cardEnvURL(t, false, link, nil, cardTS, nil)
	v, s := open(Full, image.Pt(1100, 900))
	softWait(s, v, 8*time.Second, func() bool { return strings.Contains(embedText(v), "Double-check this link") })
	txt := embedText(v)
	if !strings.Contains(txt, link) || !strings.Contains(txt, "Double-check this link") || strings.Contains(txt, "Unable to create preview.") {
		t.Errorf("text %q, want the URL and the banner, no line", txt)
	}
	noLabel(t, v)
}

// TestRenderLinkCardTitleHover: EmptyUnfurl's title link is the text's
// colour at rest (primary foreground) and the base a:hover's while the
// pointer is on it (content-hgl-1: #1264a3, dark #2ba5ce).
func TestRenderLinkCardTitleHover(t *testing.T) {
	for _, dark := range []bool{false, true} {
		h, _, _ := cardEnv(t, dark, nil, cardTS, nil)
		v := New(h, cardCanvasID, Full, Actions{FullView: func() {}, SideView: func() {}, Close: func() {}})
		r := &pointerRig{t: t, v: v, sz: image.Pt(1100, 900)}
		deadline := time.Now().Add(20 * time.Second)
		for {
			r.frame()
			if b := embedBlk(v); b != nil && b.card.title != nil && strings.Contains(embedText(v), "Unable") || labelled(v) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("no empty card")
			}
			time.Sleep(10 * time.Millisecond)
		}
		rest, hot := titleLinkColours(v)
		wantRest, wantHot := color.NRGBA{29, 28, 29, 255}, color.NRGBA{0x12, 0x64, 0xa3, 255}
		if dark {
			wantRest, wantHot = color.NRGBA{209, 210, 211, 255}, color.NRGBA{0x2b, 0xa5, 0xce, 255}
		}
		if rest != wantRest || hot != wantHot {
			t.Errorf("dark %v: title link %v at rest, %v hovered; want %v, %v", dark, rest, hot, wantRest, wantHot)
		}
		b := embedBlk(v)
		if b == nil || b.card.title == nil {
			t.Fatal("no title to hover")
		}
		full, _, ok := r.router.AreaBounds(b.card.title)
		if !ok {
			t.Fatal("the title has no input area")
		}
		var at image.Point
		for _, lr := range b.card.title.LinkRects() {
			if lr.Kind == richview.HitLink {
				at = full.Min.Add(lr.R.Min.Add(lr.R.Max).Div(2))
			}
		}
		if titleHot(v) {
			t.Error("the title is hot before the pointer reached it")
		}
		r.queue(mouse(pointer.Move, float32(at.X), float32(at.Y)))
		r.frame()
		if !titleHot(v) {
			t.Errorf("dark %v: the hovered title isn't drawn in its hover colour", dark)
		}
		r.queue(mouse(pointer.Move, 5, 5))
		r.frame()
		if titleHot(v) {
			t.Errorf("dark %v: the title stays hot after the pointer left", dark)
		}
	}
}
