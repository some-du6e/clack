package core

// The channel_retention.go API the tests reach (a red run stubs this
// file alone).

import "context"

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
