package ui

// backend adapts the in-process core service (internal/core) to the shell's
// model. Until core lands on the ui branch this is an empty stand-in.
type backend struct {
	m model
}

// Attach connects a backend (call before Run, or from Post after login).
func (a *App) attach(b *backend) { a.data = b }

func (b *backend) start(a *App)                   {}
func (b *backend) stop()                          {}
func (b *backend) opened(ch string)               {}
func (b *backend) tabs(ch string) []chTab         { return nil }
func (b *backend) avatar(id string) string        { return "" }
func (b *backend) togglePresence()                {}
func (b *backend) setMuted(ch string, muted bool) {}
func (b *backend) leave(ch string)                {}
func (b *backend) openBookmark(ch, id string)     {}
func (b *backend) toggleStar(ch string)           {}
func (b *backend) markRead(ch string)             {}
func (b *backend) markAllRead()                   {}
func (b *backend) permalink(ch, ts string) string { return "" }
func (b *backend) signOut()                       {}
