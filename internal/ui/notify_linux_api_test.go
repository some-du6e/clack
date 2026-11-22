//go:build linux

package ui

// linuxTestNotifier is the D-Bus notifier with send in place of the bus's
// Notify call (red branches stub it).
func linuxTestNotifier(send func(title, body string, actions []string) (uint32, error)) notifier {
	return &dbusNotifier{send: send}
}
