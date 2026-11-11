package core

// MpdmCustomName is a group DM's custom name, when Slack shows one
// (client-boot tpcx `JM`): the conversation has
// properties.has_custom_mpdm_name, mpdm_naming is on, and for a Slack
// Connect group DM (is_ext_shared, 5DYq `ju`) sc_mpdm_naming is on too.
// Slack's setMpimName keeps the server's name for such a group DM instead
// of listing its members, so every place that names the conversation
// (sidebar, header, composer placeholder, switcher) shows it.
func (a *App) MpdmCustomName(id string) (string, bool) {
	c, ok := a.S.Conv(id)
	if !ok || !c.IsMPIM || c.Name == "" || !c.HasCustomMpdmName() {
		return "", false
	}
	if a.experiment("mpdm_naming") != "on" || (c.IsExtShared && a.experiment("sc_mpdm_naming") != "on") {
		return "", false
	}
	return c.Name, true
}
