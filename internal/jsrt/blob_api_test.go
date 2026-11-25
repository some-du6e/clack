package jsrt

import "context"

// blobURL is Runtime.BlobURL with the signature the round-2 fix gives it
// (a red tree on the code before maps it onto the old one).
func blobURL(r *Runtime, ctx context.Context, u string, max int) ([]byte, string, int, bool) {
	return r.BlobURL(ctx, u, max)
}
