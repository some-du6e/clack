package popups

import (
	"strings"

	"github.com/some-du6e/clack/internal/i18n"
)

// Slack's I18n instances the menus and dialogs draw from (docs/I18N.md):
// each is the namespace of the `new I18n("…")` in the Slack module that
// renders the string, so a word Slack translates differently per
// component ("Copy link" is "Copiar el enlace" in a message's menu,
// "Copiar enlace" in a channel's) comes out as Slack has it.
var (
	nsMessage = i18n.NS("message")         // the message action modules (jFAp, ximo, ojBT, loUg, 5GOt, 1ixK, …)
	nsSaved   = i18n.NS("saved_for_later") // the save action (mcYr), useReminderTimeOptions (GufF), LaterTimePicker (FzLp)
	nsMsgEdit = i18n.NS("msg_edit")        // BaseDeleteMessageDialog (6Qou), BaseRemoveBroadcastDialog (L16H)
	nsInline  = i18n.NS("inline_attachments")
	nsFile    = i18n.NS("file")  // buildFileContextMenuItems (a3Wp), enqueueCopyLinkSuccessToast (Bwpd)
	nsFiles   = i18n.NS("files") // DeleteFileDialog
	// the channel context menu's templates (channel_context_menu_get_template:
	// getDetailsTemplate, getCopyTemplate, getChannelSectionsTemplate,
	// getNotificationsTemplate, getLeaveTemplate, …), the team and user menus
	nsMenu      = i18n.NS("menu")
	nsChDetails = i18n.NS("channel_details")
	// the mute rows: Slack's channel menu mutes a channel from its
	// notifications submenu and never says "Unmute channel", so that one
	// is the activity menu's (renderMenuPlaceholderItem)
	nsMenuMute = i18n.NewChain("menu", "activity")
	// the workspace menu (makeMenuItemIcon, useSwitchWorkspaceMenu) and
	// the user menu (PresenceToggle, StatusField, getNotificationsState),
	// with the pause submenu's do_not_disturb strings
	nsIa4        = i18n.NS("ia4")
	nsClassicNav = i18n.NS("classic_nav")
	nsNativeApps = i18n.NS("native_apps_entry_points")
	nsDND        = i18n.NS("do_not_disturb")
	// Clack's Tools submenu row "Workflows" (Slack's says "Workflow
	// Builder"): the client's
	nsTeamMenu = i18n.NewChain("ia4", "client")
	nsDialog   = i18n.NS("generic_dialog") // DeprecatedBaseDialog / DeprecatedAlertDialog: Cancel, OK
	nsPresence = i18n.NS("presence")       // PresencePromptDialog
	nsChannels = i18n.NS("channels")       // LeaveChannelError
	nsGroups   = i18n.NS("groups")         // LeaveChannel
)

// In is it as a row of Slack's own text in the I18n namespace ns: the
// label (or a header) is drawn in the active locale, and the template key
// stays the English label's, so plugins find the row by the same key in
// every locale (LabelKey).
func (it MenuItem) In(ns i18n.NS) MenuItem {
	switch {
	case it.Label != "":
		if it.Key == "" {
			it.Key = LabelKey(it.Label)
		}
		it.Label = ns.T(it.Label)
	case it.Header != "":
		if it.Key == "" {
			it.Key = LabelKey(it.Header)
		}
		it.Header = ns.T(it.Header)
	}
	return it
}

// inAll is In over items and their submenus: for a list that is all one
// Slack component's own strings (separators and custom rows pass
// through). Never for rows of data or rows already translated.
func inAll(ns i18n.NS, items []MenuItem) []MenuItem {
	for i := range items {
		if items[i].Widget != nil || items[i].LabelText != nil || items[i].Separator {
			continue
		}
		items[i] = items[i].In(ns)
		if len(items[i].Sub) > 0 {
			items[i].Sub = inAll(ns, items[i].Sub)
		}
	}
	return items
}

// linkSplit is an rt() string with one link tag as the text around the
// link and the link's text (Clack draws the link on its own line, as
// pass 1's ptLink does for Preferences).
func linkSplit(parts []i18n.Part, tag string) (text, link string) {
	var b []string
	for _, p := range parts {
		if p.Tag == tag && link == "" {
			link = p.Text
			continue
		}
		b = append(b, p.Text)
	}
	return strings.TrimSpace(strings.Join(b, "")), link
}
