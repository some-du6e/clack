package core

// stoppedOf is the message's StoppedLabel values (Stopped, StoppedBy).
// The red-evidence commit replaces this file with a stub that reports
// none.
func stoppedOf(m Message) (bool, string) { return m.Stopped, m.StoppedBy }
