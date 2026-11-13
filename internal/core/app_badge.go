package core

// The app badge: one count for the window title's "N new items", the
// macOS Dock and the Windows taskbar overlay, as Slack has it (boot-render
// build fa938a086569):
//
//   - WorkspaceDocumentTitle (m935) passes appBadgeCount (3x6A ER
//     getAppBadgeCount) to DocumentTitle (OrkB), whose "{n} new item(s)"
//     shows appBadgeCount ?? totalUnreadHighlights.
//   - getDesktopUnreads (eoP) hands the desktop shell unreadHighlights =
//     getAppBadgeCount, showBullet = mac_ssb_bullet and hasUnreads: under
//     activity_inbox count > 0 || (showBullet && anything unread), else
//     anything unread. The Dock's badge and the Windows overlay read
//     unreadHighlights.
//   - getAppBadgeCount under activity_inbox (5Ns/ Lh): the Activity badge
//     (getActivityBadgeCount), plus the overdue to-dos (YeRq BK
//     getSavedOverdueCount) when reminders aren't in Activity (5Ns/ ah)
//     and Later is in the rail (5ydT b, default true; or swdb G$
//     slack6_todo_badging), plus activity_v2's dm when DMs aren't in
//     Activity (5Ns/ Vn). Without the inbox: the total unread highlights
//     (ZVw+ P, the caller's highlights) plus the overdue to-dos (+Knw ay).
//     (E0VC g_, quip's sidebar badge count, adds 0: Clack has none.)

// AppBadgeCount is getAppBadgeCount; highlights is the total unread
// highlights (getTotalUnreadHighlights: the channels' mentions and the
// DMs'), which the inbox-off count is built from.
func (a *App) AppBadgeCount(highlights int) int {
	overdue := a.SavedOverdueCount()
	if !a.ActivityInbox() {
		return highlights + overdue
	}
	n := a.ActivityBadgeCount()
	if !a.GlobalNotifyBool("global_reminders_in_activity", true) {
		if later, ok := a.RailCustomization()["later"]; !ok || later || a.experiment("slack6_todo_badging") == "on" {
			n += overdue
		}
	}
	if a.PrefBool("activity_dms_opt_out", false) { // 5Ns/ Vn false: DMs aren't in Activity
		a.mu.Lock()
		n += a.actBadges["dm"]
		a.mu.Unlock()
	}
	return n
}

// DesktopUnreads is getDesktopUnreads for the shell's badge: the count
// and whether to show the "•" without one.
type DesktopUnreads struct {
	Count      int  // unreadHighlights (AppBadgeCount)
	HasUnreads bool // hasUnreads
	Bullet     bool // showBullet: mac_ssb_bullet
}

// DesktopUnreads is eoP: unread is whether anything is unread (o6 Z6 > 0),
// bullet the mac_ssb_bullet pref.
func (a *App) DesktopUnreads(highlights int, unread, bullet bool) DesktopUnreads {
	n := a.AppBadgeCount(highlights)
	has := unread
	if a.ActivityInbox() {
		has = n > 0 || bullet && unread
	}
	return DesktopUnreads{Count: n, HasUnreads: has, Bullet: bullet}
}

// SavedOverdueCount is saved.list's counts.uncompleted_overdue_count, the
// last one an answer carried (0 before any).
func (a *App) SavedOverdueCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.savedOverdue
}

// The experiment the app badge reads (TestExperimentReadsListed): swdb G$
// todo badging, which puts the overdue to-dos in the count.
func init() { experimentReads["slack6_todo_badging"] = on }
