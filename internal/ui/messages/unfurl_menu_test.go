package messages

import (
	"slices"
	"testing"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/richview"
)

// A right-click on a v3 link preview opens Slack's attachment menu
// (renderContextMenu): Remove preview and a separator for the sender's
// own message, then Copy link; anywhere else on the row the message menu.
func TestAttachmentContextMenu(t *testing.T) {
	const site = "https://news.example.com/2026/11/native"
	att := func() []slack.Attachment {
		return []slack.Attachment{{ID: 1, FromURL: site, OriginalURL: site, TitleLink: site, Title: "Native clients",
			ServiceName: "Example News", Text: "A look at GPU-drawn widgets."}}
	}
	const tsOwn, tsOther = "1700000020.000100", "1700000021.000100"
	e := newCtxEnvSetup(t, tGen, []slack.Message{
		{Type: "message", TS: tsOwn, User: tSelf, Text: "<" + site + ">", Attachments: att()},
		{Type: "message", TS: tsOther, User: "UALICE", Text: "<" + site + ">", Attachments: att()},
	}, func(app *core.App) {
		if u, ok := app.S.User(tSelf); ok {
			app.S.Self = *u
		} else {
			app.S.Self = slack.User{ID: tSelf}
		}
	})
	for _, tc := range []struct {
		ts   string
		want []string
	}{{tsOwn, []string{"Remove preview", "-", "Copy link"}}, {tsOther, []string{"Copy link"}}} {
		p := e.find(tc.ts, "the preview card", func(h richview.Hit) bool { return h.Kind == richview.HitCard })
		n := len(e.h.menus())
		e.rightClick(p)
		m := e.menu(n, tc.ts)
		if got := labels(m); !slices.Equal(got, tc.want) {
			t.Errorf("%s: menu %q, want %q", tc.ts, got, tc.want)
		}
		m.Dismiss()
	}
}
