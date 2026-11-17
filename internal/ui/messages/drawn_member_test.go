package messages

// The hub redraws the lists for a member's change only when a list drew
// the member in its latest frame (drawsMember: the store's last access
// against the frame's stamp). A frame stamps itself before its rows read
// their senders, so a check that ran while the latest frame laid out its
// rows found the sender's read in the frame before, and a rename then
// never redrew the row. Synthetic data.

import (
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/theme"
)

func TestDrawsMemberReadInTheFrameBefore(t *testing.T) {
	app, _, _ := shotsApp(t)
	const cara = "UCARA0005"
	app.S.PutUsers([]slack.User{{ID: cara, Name: "cara", Profile: slack.Profile{DisplayName: "cara"}}})
	hb := hubFor(coreHost{app: app, th: theme.New(false)})
	m := hb.channel("C0DRAWNMB1")
	hb.markDrawn(m, time.Now()) // a frame begins
	app.S.User(cara)            // and a row of it reads its sender
	time.Sleep(3 * time.Millisecond)
	hb.markDrawn(m, time.Now()) // the next frame begins, its rows not laid out yet
	if !hb.drawsMember(core.Event{Type: "user", UserID: cara}) {
		t.Error("a member the frame before read, checked before the latest frame's rows read her again, counts as undrawn: her rename redraws nothing")
	}
	time.Sleep(3 * time.Millisecond)
	hb.markDrawn(m, time.Now())
	time.Sleep(3 * time.Millisecond)
	hb.markDrawn(m, time.Now())
	if hb.drawsMember(core.Event{Type: "user", UserID: cara}) {
		t.Error("a member no frame read for two frames counts as drawn")
	}
}
