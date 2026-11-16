package usernotifyprobe

// For other packages' tests run from a test Clack.app (ci.yml's macos
// job: internal/ui's TestMacNotificationClickJumps).

// GrantProvisional asks for provisional authorization (alerts, sounds,
// badges), which macOS grants at once and which stands in for the user's
// Allow; it returns the answer as text.
func GrantProvisional() string { return request(OptAlert|OptSound|OptBadge|OptProvisional, 10) }

// Click hands Notification Center's delegate a click on the delivered
// notification id (the default action), the response macOS hands it when
// the user clicks the banner: "" once the delegate took it, else what went
// wrong.
func Click(id string) string {
	return respond(id, "com.apple.UNNotificationDefaultActionIdentifier", "", false, 10)
}
