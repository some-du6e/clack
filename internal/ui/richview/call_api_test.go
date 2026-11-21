package richview

// setDefaultAvatar sets DefaultAvatar until the returned func runs (red
// branches stub it: a no-op).
func setDefaultAvatar(u string) func() {
	old := DefaultAvatar
	DefaultAvatar = u
	return func() { DefaultAvatar = old }
}
