package blockkit

import "testing"

// captureTaskIcons records the task icons drawn (agentIconHook) until the
// test ends. The red-evidence commit replaces this file with a stub that
// records nothing.
func captureTaskIcons(t *testing.T) *[]string {
	got := &[]string{}
	agentIconHook = func(name string) { *got = append(*got, name) }
	t.Cleanup(func() { agentIconHook = nil })
	return got
}
