// Package avperm asks the OS for microphone and camera access before Clack
// captures them. On macOS that is TCC through AVCaptureDevice (the prompt
// that needs NSMicrophoneUsageDescription/NSCameraUsageDescription in
// Info.plist); without it Core Audio capture silently delivers zeros. Other
// systems have no prompt to ask for, so Check reports Authorized and the
// capture itself reports failures (see internal/huddle's MicStatus).
//
// Nothing here blocks on the main thread or the UI goroutine: Request
// returns at once and the answer arrives on a channel, from whatever queue
// the OS calls back on.
package avperm

// Media is what to capture.
type Media int

const (
	Mic Media = iota
	Camera
)

func (m Media) String() string {
	if m == Camera {
		return "camera"
	}
	return "microphone"
}

// Status is the OS's answer (AVAuthorizationStatus on macOS).
type Status int

const (
	Authorized    Status = iota
	NotDetermined        // never asked: Request shows the prompt
	Denied               // the user said no (System Settings › Privacy)
	Restricted           // parental controls or MDM: the user can't change it
	// NoUsageString: the app bundle has no usage description for m, so
	// asking would make macOS kill the process. Only a broken build does.
	NoUsageString
)

func (s Status) String() string {
	return [...]string{"authorized", "not determined", "denied", "restricted", "no usage description"}[s]
}

// Check returns the current status without prompting. It is cheap and safe
// from any goroutine.
func Check(m Media) Status { return check(m) }

// Request asks for access (macOS shows its prompt the first time) and
// returns at once; the final status arrives on the channel, which is
// buffered and sent exactly once. If the status is already decided it is
// sent without asking.
func Request(m Media) <-chan Status {
	ch := make(chan Status, 1)
	if st := check(m); st != NotDetermined {
		ch <- st
		return ch
	}
	request(m, ch)
	return ch
}

// SettingsURL is the system settings page where the user grants m to
// Clack ("" where there is none, e.g. Linux).
func SettingsURL(m Media) string { return settingsURL(m) }
