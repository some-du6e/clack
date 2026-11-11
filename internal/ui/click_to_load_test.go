package ui

// Taut's ClickToLoad through the whole window (ui-taut): a Spotify
// unfurl's media is Slack's AttachmentMedia on its own (the card's right
// box), with audioHtml and audioHtmlHeight from the attachment, so the
// plugin's patch replaces the player with its "Load Spotify" placeholder
// (Slack's click-to-play markup, drawn with Slack's rules and the legacy
// icon font's play glyph) while the card's title, text and footer stay.
// A screenshot goes to TAUT_SHOTS_DIR. Synthetic data only.

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestTautViewClickToLoad(t *testing.T) {
	dir := os.Getenv("TAUT_PLUGINS_DIR")
	if dir == "" {
		t.Skip("TAUT_PLUGINS_DIR not set: the ui-taut workflow builds Taut's plugins and runs this")
	}
	const track = "https://open.spotify.com/track/0SyntheticTrack00000"
	e := newSV(t, func(home string, a *App, ca *core.App) {
		ca.S.SetLatest(ttGen, []slack.Message{{Type: "message", TS: "1700000000.000100", User: svBob, Text: "<" + track + ">",
			Attachments: []slack.Attachment{{ID: 1, ServiceName: "Spotify", ServiceIcon: "https://avatars.invalid/spotify.png",
				Title: "A Synthetic Song", TitleLink: track, FromURL: track, OriginalURL: track, Text: "Song · Synthetic Band · 2025",
				ThumbURL: "https://thumbs.invalid/synthetic-cover.jpg", ThumbWidth: 300, ThumbHeight: 300,
				AudioHTML:  `<iframe width="400" height="152" frameborder="0" allowtransparency="true" src="https://open.spotify.com/embed/track/0SyntheticTrack00000?utm_source=oembed"></iframe>`,
				AudioHTMLH: 152}}}}, false)
	})
	tt := e.a.Taut()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	code, err := os.ReadFile(filepath.Join(dir, "ClickToLoad.js"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := tt.Import(ctx, code, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tt.SetEnabled(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	e.until(10*time.Second, "AttachmentMedia patched", func() bool { return slackc.Patched("AttachmentMedia") })
	e.until(15*time.Second, "the Load Spotify placeholder in the card's media", func() bool {
		return e.tree("taut-click-to-load", "Load Spotify") != ""
	})
	// the placeholder stands for the player: it read the provider from
	// audioHtml's iframe ("Load Spotify") and took audioHtmlHeight as its
	// height; it covers the media alone, not the card's title
	tr := e.tree("taut-click-to-load", "Load Spotify")
	if !strings.Contains(tr, `"height":152`) {
		t.Errorf("the placeholder's height isn't audio_html_height's 152: %.600s", tr)
	}
	if strings.Contains(tr, "A Synthetic Song") {
		t.Errorf("the media's tree holds the card's title: the patch should cover the media alone")
	}
	sh := newSVShooter(t, e)
	sh.dir = os.Getenv("TAUT_SHOTS_DIR")
	for i := 0; i < 5; i++ {
		e.frame()
	}
	img := sh.shot()
	sh.save("click-to-load", img)
	if sh.dir != "" {
		x := int(theme.RailW) + e.a.sidebar.width
		sh.save("click-to-load-pane", img.SubImage(image.Rect(x, int(theme.TopNavH), e.sz.X, e.sz.Y)).(*image.RGBA))
	}
}
