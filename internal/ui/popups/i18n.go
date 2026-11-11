package popups

import (
	"strings"
	"unicode"

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
	// the bell menu: n9iP's option labels, MoreOptionsSubmenu (laNq) and
	// enqueueMuteChannelToast (vA1n) are channel_sidebar's;
	// ChannelNotificationsPrefsModal (RemI) is the prefs I18n's
	nsSidebar = i18n.NS("channel_sidebar")
	nsPrefs   = i18n.NS("prefs")
	nsShare   = i18n.NS("share-dialog") // the share/forward dialogs (jdUv, Zp0h, IEmj, e9lq)
	// the emoji picker (Zu56, Pz3S), its categories (bEcp) and the GIF
	// picker (DMNm)
	nsEmojiPicker = i18n.NS("emoji_picker")
	nsEmoji       = i18n.NS("emoji")
	nsGifPicker   = i18n.NS("gif-picker")
	nsStatus      = i18n.NS("current_status") // the status modal (D72a), its expiration (yU++)
	nsSearch      = i18n.NS("search")         // the search switcher (rs63, SidebarSearchCmdKResults), the results page and its filters
	nsAttachments = i18n.NS("attachments")    // an unfurl's ⋮ menu
	nsFileUpload  = i18n.NS("file-upload")    // the upload failure dialog
	// the section modals: BaseChannelSectionModalInput and the create
	// modal's first step are current_status's, the conversation picker
	// plug_and_play_sections', the move speed bump create_section_modal's;
	// the delete confirmation is channel_browser_coachmark's
	nsSection    = i18n.NewChain("current_status", "plug_and_play_sections", "create_section_modal", "channel_sidebar")
	nsDelSection = i18n.NewChain("channel_browser_coachmark", "generic_dialog")
	// CreateChannelModal (create_channel_modal, its name field
	// channel_modal's) and the invite step (channel_invite)
	nsCreateCh = i18n.NewChain("create_channel_modal", "channel_modal", "channel_invite")
	// the Add emoji dialog (custom_emoji_list), Loading… from the files'
	nsEmojiList = i18n.NewChain("custom_emoji_list", "files")
	nsProfile   = i18n.NS("member_profile") // the profile's ⋮ menu (CpMO)
	// the media viewer's toolbar: MediaViewerActions (file, the thread
	// toggle stories_av's) and the zoom controls (image_viewer)
	nsViewer = i18n.NewChain("file", "stories_av", "image_viewer")
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

// cjkChunks cuts a word where a line may break inside it: between CJK
// characters (each its own chunk), not before closing punctuation or a
// small kana and not after opening punctuation (kinsoku). Other text
// stays whole. Hand-flowed rich text (cnRuns) wraps Japanese with it.
func cjkChunks(word string) []string {
	var out []string
	cur := ""
	glue := false // the chunk so far ends in opening punctuation
	prevCJK := false
	for _, r := range word {
		cjk := isCJKRune(r)
		switch {
		case cur == "":
			cur = string(r)
		case glue || strings.ContainsRune(cjkNoBreakBefore, r) || !cjk && !prevCJK:
			cur += string(r)
		default:
			out = append(out, cur)
			cur = string(r)
		}
		glue = strings.ContainsRune(cjkNoBreakAfter, r)
		prevCJK = cjk
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

const (
	cjkNoBreakBefore = "、。，．・：；？！ー）」』】〕〉》］｝ぁぃぅぇぉっゃゅょゎァィゥェォッャュョヮヵヶ々"
	cjkNoBreakAfter  = "（「『【〔〈《［｛"
)

// isCJKRune: ideographs, kana, hangul and CJK punctuation.
func isCJKRune(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) ||
		r >= 0x3000 && r <= 0x303f || r >= 0xff00 && r <= 0xffef || r == 0x30fc
}
