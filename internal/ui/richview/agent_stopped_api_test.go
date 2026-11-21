package richview

// stoppedOpts are Options with StoppedLabel's label (by: the stopper's
// name), and editedStopped both labels' flags. The red-evidence commit
// replaces this file with a stub returning plain Options.
func stoppedOpts(by string) Options { return Options{Stopped: true, StoppedBy: by} }

func editedStoppedOpts() Options { return Options{Edited: true, Stopped: true} }
