package composer

import (
	"context"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/popups"
)

// The fix's API the slash tests call (parity-slash-fixes-2), in one place
// for a red tree on the old code to stand in for (red-evidence-branch).

// srServeCommands answers the slash command store's load with the server
// commands cmds, in their order (client.appCommands' list).
func srServeCommands(fs *fakeSlack, cmds []any) {
	fs.answer("client.appCommands", map[string]any{"ok": true, "commands": cmds, "app_actions": []any{}, "cache_ts": "1700000000"})
}

// srWarm is what the channel's views and the member autocomplete asked for
// before anything is typed: ch's and the team's permissions
// (permissions/info), and the membership of members in ch.
func srWarm(t *testing.T, app *core.App, ch string, members []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, c := range []string{ch, ""} {
		if err := app.LoadChannelGrants(ctx, c); err != nil {
			t.Fatalf("permissions/info for %q: %v", c, err)
		}
	}
	if len(members) > 0 {
		if got := app.ChannelMembershipInfo(ctx, ch, members); len(got) != len(members) {
			t.Fatalf("channels/membership: %v", got)
		}
	}
}

// srModalName is a dialog's name as Slack's getModalName gives it
// (popups' ModalName), "" for an overlay without one.
func srModalName(o popups.Overlay) string {
	if m, ok := o.(interface{ ModalName() string }); ok {
		return m.ModalName()
	}
	return ""
}

// srPopupsIdle reports that popups' async work is over.
func srPopupsIdle() bool { return popups.Idle() }

// srMark makes f hear the client command each send dispatches (nil
// stops it).
func srMark(f *func(name string)) { clientCommandRan.Store(f) }
