package taut

// The tests' ways to what round 4 of slick-473 added to the UI hooks,
// through these alone, so the tests build against code from before it.

// setDeepLinkPrimary sets UIHooks.DeepLinkPrimary.
func setDeepLinkPrimary(h *UIHooks, f func(uri string)) { h.DeepLinkPrimary = f }

// onlyOwnPatch is Taut.OnlyOwnPatch.
func onlyOwnPatch(t *Taut, name string) bool { return t.OnlyOwnPatch(name) }
