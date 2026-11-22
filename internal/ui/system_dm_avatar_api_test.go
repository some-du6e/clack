package ui

import "github.com/some-du6e/clack/internal/ui/slackc"

// sbPresencePropsFor is the BasePresence props a DM row with user as its
// partner gives (TestSidebarSystemUserPresence).
func sbPresencePropsFor(user string) slackc.Props {
	return sbPresence{system: sbSystemUser(user), key: "D1"}.props()
}
