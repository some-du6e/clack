package popups

import (
	"testing"
	"time"
)

// Nicknames' patched member reaches the popups' user lookups (profile
// card and pane, quick switcher, mentions list), as Slack's components
// read the member from the store.
func TestSlickViewNicknamesInPopups(t *testing.T) {
	e := slNewEnv(t)
	e.load(t, "Nicknames", map[string]any{"names": map[string]any{slAlice: "Ally Nick"}})
	slUntil(t, 10*time.Second, "Alice looked up by her nickname", nil, func() bool {
		u, ok := lookupUser(e.h, slAlice)
		return ok && u.Name == "Ally Nick"
	})
}
