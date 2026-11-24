package popups

import (
	"log"
	"strconv"

	"gioui.org/layout"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// EditMessageError is what saveMessageEdit does when chat.update fails
// (boot-shared, build 83aff18a): message_not_found drops the message
// (Clack logs it; the message's own event removes it), and every other
// failure opens Slack's EditMessageErrorDialog (the message I18n) by the
// error's code, a failure with no code being unknown_error, the default.
// The composer puts the edit back first, as saveMessageEdit does.
func EditMessageError(h Host, err error) {
	code := APICode(err)
	if code == "message_not_found" {
		log.Printf("popups: chat.update: %v", err)
		return
	}
	title, body := nsMessage.T("Upload failed"), ""
	more := ""
	switch code {
	case "edit_window_closed":
		title = nsMessage.T("Message editing failed")
		mins := -1
		if a := h.Core(); a != nil {
			if n, ok := a.TeamPrefInt("msg_edit_window_mins"); ok {
				mins = n
			}
			if s := a.Self(); s.IsAdmin || s.IsOwner {
				more = nsMessage.T("(To adjust the message editing window, visit Workspace Settings)")
			}
		}
		body = nsMessage.T("Sorry, but messages can only be edited for {duration} after posting.", "duration", editWindow(mins))
	case "msg_too_long":
		title = nsMessage.T("Message editing failed")
		body = nsMessage.T("Sorry, your message is too long. Please shorten it and try again.")
	case "blocked_file_type":
		body = nsMessage.T("According to your organization’s policy, you can’t share this type of file.")
	case "slack_connect_file_upload_sharing_blocked", "slack_connect_file_link_sharing_blocked":
		body = nsMessage.T("File sharing with external people has been disabled by your admin.")
	case "slack_connect_blocked_file_type":
		body = nsMessage.T("According to your organization’s policy, you can’t share this file with external people.")
	case "slack_connect_canvas_contain_file_sharing_blocked":
		body = nsMessage.T("This canvas contains at least one file, and file sharing with external people has been disabled by your admin.")
	case "slack_connect_list_contain_file_sharing_blocked":
		body = nsMessage.T("This list contains at least one file, and file sharing with external people has been disabled by your admin.")
	case "file_deleted", "file_is_deleted":
		body = nsMessage.T("Looks like this file was moved or deleted before you hit send.")
	case "file_attach_after_send_disabled":
		title = nsMessage.T("Message attachment editing is disabled")
		body = nsMessage.T("At the request of your administrator, message editing is limited to text only.")
	default:
		title = nsMessage.T("Message editing failed")
		body = nsMessage.T("Something went wrong with editing your message. Try again in a moment.")
	}
	th := h.Theme()
	m := NewModal(h, ModalOpts{Title: title, TitleWrap: true, EnterSubmits: true, NoClose: code != "edit_window_closed",
		Body: func(gtx layout.Context) layout.Dimensions {
			fg := colorsOf(th).Fg
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions { return para(gtx, th, 15, 22, theme.Regular, fg, body, 0) }),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if more == "" {
						return layout.Dimensions{}
					}
					return para(gtx, th, 15, 22, theme.Regular, fg, more, 0)
				}),
			)
		},
		Buttons: []*ModalButton{{Label: nsMessage.T("OK"), Kind: BtnPrimary, Do: func() bool { return true }}}})
	h.ShowOverlay(m)
}

// editWindow is the edit window (msg_edit_window_mins) as the dialog's
// {duration}: whole days, hours or minutes.
func editWindow(mins int) string {
	unit := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.Itoa(n) + " " + many
	}
	switch {
	case mins <= 0:
		return unit(0, "minute", "minutes")
	case mins%1440 == 0:
		return unit(mins/1440, "day", "days")
	case mins%60 == 0:
		return unit(mins/60, "hour", "hours")
	}
	return unit(mins, "minute", "minutes")
}
