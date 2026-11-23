package core

// The channel_retention.go API the tests reach (a red run stubs this
// file alone).

import (
	"context"
	"testing"

	"github.com/some-du6e/clack/internal/store"
)

func retHistoryShown(a *App, inf ChannelDetails) bool { return a.MessageHistoryShown(inf) }
func retExportShown(a *App, inf ChannelDetails) bool  { return a.ExportHistoryShown(inf) }
func retShown(a *App, inf ChannelDetails, own Retention) Retention {
	return a.ShownRetention(inf, own)
}
func retGet(a *App, ch string) (Retention, error) {
	return a.ChannelRetention(context.Background(), ch)
}
func retSet(a *App, ch string, typ, days int) error {
	return a.SetChannelRetention(context.Background(), ch, typ, days)
}
func retExport(a *App, ch string) error { return a.ExportChannel(context.Background(), ch) }

func retEntCached(a *App) []byte { return a.S.CachedEnterprisePrefs() }

// retEntRestore writes saved to a disk cache, reads it back as the next
// start does and installs it.
func retEntRestore(t *testing.T, a *App, saved []byte) {
	t.Helper()
	dir := t.TempDir()
	s := store.New(dir)
	s.SetEnterprisePrefs(saved)
	if err := s.SaveCache(); err != nil {
		t.Fatal(err)
	}
	s2 := store.New(dir)
	s2.LoadCache()
	a.S.SetEnterprisePrefs(s2.CachedEnterprisePrefs())
	a.installCachedEnterprisePrefs()
}
