package views

import "github.com/some-du6e/clack/internal/core"

// memberNamesFor is the Members row's names for u under the
// display_real_names pref (details_members_live_test.go).
func memberNamesFor(u core.User, realNames bool) (primary, secondary string) {
	return cdMemberNamesPref(u, "", realNames)
}
