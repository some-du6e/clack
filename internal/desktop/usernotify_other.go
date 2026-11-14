//go:build !(darwin && cgo)

package desktop

// UserNotificationsAvailable: UNUserNotificationCenter is macOS's, and
// reaching it takes cgo.
func UserNotificationsAvailable() bool { return false }

func SetUserNotificationHandler(func(UserNotificationEvent)) {}

func RequestUserNotificationAuthorization(done func(granted bool, err error)) {
	if done != nil {
		done(false, errNoUserNotifications)
	}
}

func UserNotificationAuthorization(done func(UserNotificationAuth, error)) {
	if done != nil {
		done(AuthNotDetermined, errNoUserNotifications)
	}
}

func ShowUserNotification(_ UserNotification, done func(error)) {
	if done != nil {
		done(errNoUserNotifications)
	}
}
