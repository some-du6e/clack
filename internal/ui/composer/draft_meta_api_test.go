package composer

import "time"

// The draft file tests' reads of what the change adds (a red run replaces
// this file with stubs).

// attChipState is what an attachment card draws.
type attChipState struct {
	name, title, label string
	loading, gone      bool
}

func chipState(c *attChip) attChipState {
	return attChipState{name: c.name, title: c.title, label: c.kind.Label, loading: c.loading, gone: c.gone}
}

// hashedFileID is the id the log names a file by.
func hashedFileID(id string) string { return attLogID(id) }

// shortMetaTimeout makes each files.info attempt time out after d.
func shortMetaTimeout(d time.Duration) func() {
	old, oldB := attMetaTimeout, attMetaBackoff
	attMetaTimeout, attMetaBackoff = d, 10*time.Millisecond
	return func() { attMetaTimeout, attMetaBackoff = old, oldB }
}
