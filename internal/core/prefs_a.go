package core

import "github.com/some-du6e/yoloslackgoclient/internal/slack"

// connectSectionOn is useCalculateSidebarSections' showConnectSection:
// isSlackConnectSectionVisible (yl3L: !!should_show_connect_section) and
// the viewer has a slack_connect section.
func (a *App) connectSectionOn(ordered []slack.Section) bool {
	return a.PrefBool("should_show_connect_section", false) && hasSectionType(ordered, "slack_connect")
}

// extShared is 5DYq ju: the conversation is shared with another
// organization (is_ext_shared).
func (a *App) extShared(id string) bool {
	c, ok := a.S.Conv(id)
	return ok && c.IsExtShared
}
