package core

// DropCache deletes this account's disk cache (sign-out: meta.json and the
// image cache, store/cachedir.go): saves stop at once, and the files are
// removed in the background. Any goroutine.
func (a *App) DropCache() <-chan struct{} {
	a.S.StopSaving()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.S.DropDisk()
	}()
	return done
}
