package ui

// The history entries' primary-thread bit, through these alone (so the
// tests that read it build against code from before it).

func routerSetPrimary(h *router, on bool) { h.SetPrimary(on) }
func routerPrimary(h *router) bool        { return h.Primary() }
