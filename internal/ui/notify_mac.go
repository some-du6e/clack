package ui

import (
	"log"
	"sync"
	"time"

	"github.com/some-du6e/clack/internal/desktop"
	"github.com/some-du6e/clack/internal/i18n"
)

// The macOS notification, as Slack desktop's mac shell shows the webapp's
// (main.bundle.cjs 4.53.276 darwin). h6 keeps the payload: the title
// capped at 1500/1700, the body at 1500/2000 and unescaped, hasReply as
// given (else whether it names a channel), mac.closeButtonOverride when it
// has actionButtons, the first one's content as closeButtonText ("Close"
// without one); with notificationPlayback "web" (the setting's default)
// silent stays as given and the sound is "none". Class h0's
// getElectronNotificationOptions hands Electron's Notification the title
// (the workspace's name when there is none: a huddle invite), subtitle,
// body, closeButtonText, the actionButtons after the first as its buttons,
// hasReply unless the close button stands in for the first button, sound,
// silent, id and groupId. Electron draws that with UNUserNotificationCenter
// (internal/desktop/usernotify.go). The icon is the app's (getIcon returns
// nothing); urgency and groupTitle mean nothing on macOS.
//
// h0 turns Electron's events into Slack's: a click; a reply; button i,
// which is actionButtons[i+1] under the close button override; and a close,
// which under the override is the first button (action 0). Only the first
// event counts (the listeners go once one ran). The shell routes them (h5):
//
//   - click: notificationClicked, the conversation (a reply's thread);
//   - reply: replyToNotification, the text sent there;
//   - action: the button's launchUri as a deep link when it's a slack: one,
//     else the webapp's handleNotificationAction (Decline declines an
//     invite; Dismiss, Join and View do nothing there);
//   - close: nothing.
//
// Clack routes them as it routes the Windows toast's (toastActivated).
//
// h0.show ends at the first answer within 5 s, either way; none counts as
// shown ("never reported show or failed"), and a failure is logged. After
// either, at most every 5 minutes, macNotificationDeliveryCheck reads the
// permission: once it was authorized or provisional, a change away from
// that is logged ("macOS accepted a notification it will not render"), and
// while it is undetermined Clack asks again (l9's "delivery": at most 3
// times a session, 10 minutes apart).

const (
	macNoteShowWait = 5 * time.Second  // h0.show's showTimeout
	macNoteKeep     = 10000            // h8: notifications waiting for a response
	macCheckEvery   = 5 * time.Minute  // the delivery check's hK
	macAskEvery     = 10 * time.Minute // l9's interval
	macAskMax       = 3                // l9's session_limit
)

// macOptions is h6 and getElectronNotificationOptions for n.
func macOptions(n *desktopNote) desktop.UserNotification {
	title := n.toastTitle()
	if title == "" {
		title = n.Workspace // win32.workspaceName: the team's name
	}
	u := desktop.UserNotification{ID: n.ID, Title: title, Subtitle: n.Subtitle, Body: n.toastBody(), GroupID: n.GroupID,
		HasReply: n.HasReply, Silent: n.Silent, Sound: "none", Actions: []string{}}
	if len(n.Buttons) > 0 { // mac.closeButtonOverride
		u.CloseText = n.Buttons[0].Content
		if u.CloseText == "" {
			u.CloseText = i18n.T("desktop_general", "Close")
		}
		u.HasReply = false
		for _, b := range n.Buttons[1:] {
			u.Actions = append(u.Actions, b.Content)
		}
	}
	return u
}

// macActivation is h0's event for what the user did with n, and the
// toast activation Clack routes it as; false: nothing to route (a close
// without the override, a button n doesn't have).
func macActivation(n *desktopNote, ev desktop.UserNotificationEvent) (toastActivation, bool) {
	override := len(n.Buttons) > 0
	kind, index := ev.Kind, ev.Index
	switch kind {
	case "close":
		if !override {
			return toastActivation{}, false
		}
		kind, index = "action", 0
	case "action":
		if override {
			index++
		}
	}
	act := toastActivation{Team: n.Team, Channel: n.Channel, MessageID: n.Msg, NotificationID: n.ID, TraceID: n.TraceID,
		ThreadTS: n.ThreadTS, Index: -1}
	switch kind {
	case "click":
		act.Type = "notification"
	case "reply":
		act.Type, act.Reply = "reply", ev.Reply
	case "action":
		if index < 0 || index >= len(n.Buttons) {
			return toastActivation{}, false
		}
		act = toastActivation{Type: "action", ID: n.ID, Index: index}
		if uri := n.Buttons[index].LaunchURI; isSlackURI(uri) {
			act.LaunchURI = uri // the deep link; without one, handleNotificationAction (the button's Do)
		}
	default:
		return toastActivation{}, false
	}
	return act, true
}

// macNoteSys is Notification Center: desktop's UNUserNotificationCenter,
// a fake in tests.
type macNoteSys interface {
	SetHandler(on func(desktop.UserNotificationEvent))
	Request(done func(granted bool, err error))
	Status(done func(desktop.UserNotificationAuth, error))
	Show(n desktop.UserNotification, done func(error))
}

type desktopUserNotifications struct{}

func (desktopUserNotifications) SetHandler(on func(desktop.UserNotificationEvent)) {
	desktop.SetUserNotificationHandler(on)
}
func (desktopUserNotifications) Request(done func(bool, error)) {
	desktop.RequestUserNotificationAuthorization(done)
}
func (desktopUserNotifications) Status(done func(desktop.UserNotificationAuth, error)) {
	desktop.UserNotificationAuthorization(done)
}
func (desktopUserNotifications) Show(n desktop.UserNotification, done func(error)) {
	desktop.ShowUserNotification(n, done)
}

// macNotifier shows Clack's notifications in macOS's Notification Center.
type macNotifier struct {
	sys      macNoteSys
	once     sync.Once
	now      func() time.Time
	showWait time.Duration

	mu    sync.Mutex
	notes map[string]*desktopNote // h8: shown, no response yet, by id
	order []string                // their ids, oldest first (may hold answered ones)

	// macNotificationDeliveryCheck, and l9's "delivery" requests
	checkAt, askAt   time.Time
	checking, asking bool
	asked            int
	wasAuthorized    bool
	dropped          desktop.UserNotificationAuth // the state last logged as hiding notifications; -1 none
}

func newMacNotifier(sys macNoteSys) *macNotifier {
	return &macNotifier{sys: sys, now: time.Now, showWait: macNoteShowWait, notes: map[string]*desktopNote{}, dropped: -1}
}

// start is NotificationPresenterMac's constructor, before the first
// notification: the center's delegate, and authorization for alerts,
// sounds and badges (macOS asks the user the first time).
func (m *macNotifier) start() {
	m.once.Do(func() {
		m.sys.SetHandler(m.respond)
		m.sys.Request(func(granted bool, err error) {
			if err != nil {
				log.Printf("notify: macOS notification authorization: %v", err)
				return
			}
			log.Printf("notify: macOS notification authorization: granted %t", granted)
		})
	})
}

func (m *macNotifier) Notify(title, body string) error {
	return m.NotifyNote(&desktopNote{Title: title, Body: body})
}

// NotifyActions is a notification with buttons and no conversation (as
// notify_windows.go's): actions' key/label pairs are its actionButtons,
// and on gets the key of the one pressed.
func (m *macNotifier) NotifyActions(title, body string, actions []string, on func(string)) error {
	n := &desktopNote{Title: title, Body: body}
	for i := 0; i+1 < len(actions); i += 2 {
		key := actions[i]
		n.Buttons = append(n.Buttons, noteButton{Content: actions[i+1], LaunchURI: "slack://noop", Do: func() {
			if on != nil {
				on(key)
			}
		}})
	}
	n.Activated = func(act toastActivation) {
		if act.Type == "action" && act.Index >= 0 && act.Index < len(n.Buttons) {
			n.Buttons[act.Index].Do()
		}
	}
	return m.NotifyNote(n)
}

// NotifyNote shows n (h0's constructor and show). It returns at once; the
// center answers later.
func (m *macNotifier) NotifyNote(n *desktopNote) error {
	m.start()
	if n.ID == "" {
		n.ID = shellNoteID(n.Team, n.Msg)
	}
	opts := macOptions(n)
	m.keep(n)
	var once sync.Once
	end := func(err error, timedOut bool) {
		once.Do(func() {
			switch {
			case timedOut:
				log.Printf("notify: macOS never reported showing a notification or failing to")
			case err != nil:
				log.Printf("notify: macOS didn't show a notification: %v", err)
			}
			m.deliveryCheck()
		})
	}
	t := time.AfterFunc(m.showWait, func() { end(nil, true) })
	m.sys.Show(opts, func(err error) {
		t.Stop()
		end(err, false)
	})
	return nil
}

// keep is h8: n waits for the user's response, the oldest of more than
// macNoteKeep forgotten.
func (m *macNotifier) keep(n *desktopNote) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.notes[n.ID]; !ok {
		m.order = append(m.order, n.ID)
	}
	m.notes[n.ID] = n
	for len(m.order) > macNoteKeep {
		delete(m.notes, m.order[0])
		m.order = m.order[1:]
	}
}

// respond is a response from the center's delegate (its queue): the
// notification's first event, routed as h5 routes it. One Clack didn't
// show in this run (from before a restart) finds nothing, as in Electron
// ("Could not find notification").
func (m *macNotifier) respond(ev desktop.UserNotificationEvent) {
	m.mu.Lock()
	n := m.notes[ev.ID]
	delete(m.notes, ev.ID)
	m.mu.Unlock()
	if n == nil {
		return
	}
	if act, ok := macActivation(n, ev); ok && n.Activated != nil {
		n.Activated(act)
	}
}

// deliveryCheck is macNotificationDeliveryCheck, after a notification
// showed or failed to.
func (m *macNotifier) deliveryCheck() {
	m.mu.Lock()
	now := m.now()
	if m.checking || (!m.checkAt.IsZero() && now.Sub(m.checkAt) < macCheckEvery) {
		m.mu.Unlock()
		return
	}
	m.checking, m.checkAt = true, now
	m.mu.Unlock()
	m.sys.Status(func(st desktop.UserNotificationAuth, err error) {
		m.mu.Lock()
		m.checking = false
		if err != nil {
			m.mu.Unlock()
			return
		}
		if st == desktop.AuthAuthorized || st == desktop.AuthProvisional {
			m.wasAuthorized, m.dropped = true, -1
			m.mu.Unlock()
			return
		}
		if m.wasAuthorized && st != m.dropped {
			m.dropped = st
			log.Printf("notify: macOS accepted a notification it will not show (permission %s)", st)
		}
		ask := st == desktop.AuthNotDetermined && !m.asking && m.asked < macAskMax &&
			(m.askAt.IsZero() || m.now().Sub(m.askAt) >= macAskEvery)
		if ask {
			m.asking, m.asked, m.askAt = true, m.asked+1, m.now()
		}
		m.mu.Unlock()
		if !ask {
			return
		}
		m.sys.Request(func(granted bool, err error) {
			m.mu.Lock()
			m.asking = false
			m.mu.Unlock()
			if err != nil {
				log.Printf("notify: asking macOS again for notifications: %v", err)
				return
			}
			log.Printf("notify: asked macOS again for notifications: granted %t", granted)
		})
	})
}
