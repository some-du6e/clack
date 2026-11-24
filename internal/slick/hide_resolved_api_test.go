package slick

// HideResolved's hooks into Taut (hide_resolved_test.go).

// historyHidden is what plugins' patches took out of channelHistory[ch]
// (Taut.HistoryHidden): whether a timestamp is one of them, how many, and
// whether Taut's answer is as of the latest change.
func historyHidden(h *harness, ch string) (has func(string) bool, n int, ok bool) {
	hid, ok := h.tt.HistoryHidden(ch)
	return hid.Has, hid.Len(), ok
}

// setViewHighlight is a navigation to ch's view highlighting ts ("" for
// none): viewUiState's highlightTs (Taut.SetViewHighlight).
func setViewHighlight(h *harness, ch, ts string) { h.tt.SetViewHighlight(ch, ts) }
