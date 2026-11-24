package ui

import (
	"os"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
)

// TestChannelDetailsLongTitleShot saves channel details for a channel
// whose name is too long for the header's line, light and dark
// (PREVIEW_SHOTS_DIR: ch-details-shots): Slack wraps the title
// (.p-about_modal__title .p-view_header__channel_title{white-space:
// break-spaces; word-break: break-word}), and the header, its buttons and
// the tabs move down by its lines. Synthetic: the fake's channel renamed
// before the client boots.
func TestChannelDetailsLongTitleShot(t *testing.T) {
	if os.Getenv("PREVIEW_SHOTS_DIR") == "" {
		t.Skip("PREVIEW_SHOTS_DIR unset (ch-details-shots sets it)")
	}
	const long = "release-coordination-and-planning-for-the-whole-company-across-every-team"
	for _, mode := range []string{"light", "dark"} {
		t.Run(mode, func(t *testing.T) {
			ch := ""
			e, _ := previewEnvWith(t, func(fs *fakeslack.Server) {
				for _, id := range fs.ChannelIDs() {
					if c, ok := fs.Conv(id); ok && c.IsMember && !c.IsPrivate && fs.SetConvName(id, long) {
						ch = id
						return
					}
				}
			})
			if ch == "" {
				t.Skip("no public member channel to rename")
			}
			e.setDark(mode == "dark")
			e.openChannel(ch)
			e.settle(200 * time.Millisecond)
			e.a.OpenDetails(ch)
			e.settle(600 * time.Millisecond)
			topDetails(t, e)
			e.shot("channel-details-long-title-" + mode)
		})
	}
}
