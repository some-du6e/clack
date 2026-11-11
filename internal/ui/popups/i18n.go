package popups

import "github.com/some-du6e/clack/internal/i18n"

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
