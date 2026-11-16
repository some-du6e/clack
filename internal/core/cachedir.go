package core

// DropCache deletes this account's disk cache (sign-out: meta.json, the
// warm boot's files and the image cache, store/cachedir.go). At once, on
// the calling goroutine: saves stop, the warm boot's small files go, and
// the signed-out mark is left (store.SignOutNow), so a quit or a crash
// right after still leaves nothing for the next start to finish but the
// rest (store.SweepAccounts). The rest is removed in the background,
// after any metadata save still writing.
func (a *App) DropCache() <-chan struct{} {
	a.S.SignOutNow()
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.S.DropDisk()
	}()
	return done
}
