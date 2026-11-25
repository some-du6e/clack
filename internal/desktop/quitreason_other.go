//go:build !darwin || !cgo

package desktop

// QuitEndsSession is macOS's: no quit Apple Event elsewhere.
func QuitEndsSession() bool { return false }
