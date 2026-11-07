//go:build !unix

package desktop

// privateDir: only used for the shared-/tmp fallback on Unix.
func privateDir(dir string) error { return nil }
