package ui

// handleChildSignals: Windows has no SIGPIPE (a write to the closed pipe
// fails with an error) and no parent-death signal; the deferred cleanup
// runs after the failed write.
func handleChildSignals(string) {}
