package taut

// The API TestHistoryHiddenFollowsPatches uses, on the red tree (over the
// code before HideResolved's port): nothing is ever hidden, a jump's
// highlight goes nowhere, and JS watches no keys.

type Hidden struct{ ts map[string]bool }

func (h *Hidden) Has(ts string) bool { return h != nil && h.ts[ts] }

func (h *Hidden) Len() int {
	if h == nil {
		return 0
	}
	return len(h.ts)
}

func (t *Taut) HistoryHidden(key string) (*Hidden, bool) { return nil, true }

func (t *Taut) SetViewHighlight(ch, ts string) {}

func (t *Taut) historyKeys() []string { return nil }
