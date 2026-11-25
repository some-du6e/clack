package imgcache

import "context"

// setBlobSource sets the blob source with the signature the round-2 fix
// gives it (a red tree on the code before maps it onto the old one).
func setBlobSource(f func(ctx context.Context, u string, max int) ([]byte, int, bool)) {
	SetBlobSource(f)
}
