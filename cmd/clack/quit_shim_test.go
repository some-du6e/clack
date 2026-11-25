package main

// quitBlocked is quitInstallBlocked for the tests: the exit reason and
// whether the session started ending (sessionEnding).
func quitBlocked(why string, ending bool) error { return quitInstallBlocked(why, ending) }
