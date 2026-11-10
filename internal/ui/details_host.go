package ui

// Host methods channel details (views.ChannelDetails) reaches through
// interface checks, so its buttons run the same code as the rest of the
// app.

// CopyHuddleLink is Settings > Huddles' "Copy Huddle Link": Slack's
// copyHuddleLinkByChannelId, the same one the huddle menu runs.
func (a *App) CopyHuddleLink(ch string) { a.huddles().CopyLink(ch) }

// AddTabMenu is Tabs > "New Tab": Slack's AddTabMenuTrigger renders the
// tab bar's AddTabMenu (isManageTabsMenu), so it opens the "+" menu.
func (a *App) AddTabMenu(ch string) { a.addTabMenu(ch) }
