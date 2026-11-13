package ui

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/fakeslack"
	"github.com/some-du6e/clack/internal/ui/theme/pixeltest"
)

// Slack Connect in the sidebar (sidebar_connect.go): the externally
// shared channel's name tooltip ("Partner Co is in this channel" under
// Partner Co's icon) after BaseChannelName's delay, and the external DM's
// row with the partner's org after their name, light and dark
// (THREAD_HEADER_SHOTS_DIR: slack-connect-*.png).
func TestSidebarSlackConnectMarkers(t *testing.T) {
	for _, dark := range []bool{false, true} {
		mode := map[bool]string{false: "light", true: "dark"}[dark]
		t.Run(mode, func(t *testing.T) {
			sz := fakeslack.Small
			sz.ThreadHeader, sz.SlackConnectDM = true, true
			fs, err := fakeslack.Start(sz)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { fs.Close() })
			e := startEnv(t, fs, t.TempDir())
			if os.Getenv("THREAD_HEADER_SHOTS_DIR") != "" {
				if e.gpu != nil {
					e.gpu.Release()
				}
				w, err := pixeltest.NewWindow(e.size.X, e.size.Y)
				if err != nil {
					t.Fatalf("headless window: %v", err)
				}
				e.gpu = w
			}
			e.waitReady()
			if dark {
				e.a.setDark(true)
				e.frame()
			}
			ch, dm := fakeslack.ThreadSharedID, fakeslack.SlackConnectDMID
			e.until("the shared channel's and the external DM's rows", 15*time.Second, func() bool {
				c, d := e.a.model().conv(ch), e.a.model().conv(dm)
				return c != nil && d != nil && e.a.sidebar.sbPos(ch) >= 0 && e.a.sidebar.sbPos(dm) >= 0
			})
			if c := e.a.model().conv(ch); !c.Shared || !slices.Equal(c.Teams, []string{fakeslack.ExternalTeamID}) {
				t.Fatalf("shared channel: Shared %v Teams %v, want Partner Co's %s", c.Shared, c.Teams, fakeslack.ExternalTeamID)
			}
			if d := e.a.model().conv(dm); d.PeerTeam != fakeslack.ExternalTeamID {
				t.Fatalf("external DM: PeerTeam %q, want %s", d.PeerTeam, fakeslack.ExternalTeamID)
			}
			// the DM's row asks for Partner Co (team.info) as it draws
			e.sbRowPoint(t, dm)
			e.until("Partner Co's name", 10*time.Second, func() bool {
				name, _, ok := e.a.Core().Team(fakeslack.ExternalTeamID, 40)
				return ok && name == "Partner Co"
			})
			e.frame()
			// hover the shared channel's name: the tooltip after 600 ms
			p := e.sbRowPoint(t, ch)
			e.movePtr(p) // over the name
			e.until("the shared channel's tooltip", 5*time.Second, func() bool {
				st := e.a.sidebar.sharedTips[ch]
				return st != nil && st.tip.Showing(time.Now())
			})
			e.settle(200 * time.Millisecond)
			e.threadHeaderShot("slack-connect-sidebar-" + mode)
		})
	}
}
