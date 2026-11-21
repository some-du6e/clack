package media

// SetPaceWake makes f run each time Pace waits (paceWake), until the undo
// it returns.
func SetPaceWake(f func()) func() {
	prev := paceWake
	paceWake = f
	return func() { paceWake = prev }
}
