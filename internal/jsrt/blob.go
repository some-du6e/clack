package jsrt

import (
	"context"
	"strings"
)

// BlobURL is the bytes and MIME type of a blob: URL this runtime made
// (URL.createObjectURL) and hasn't revoked: what Chromium loads when an
// <img> or a plugin's custom emoji names one. size is the blob's; past max
// bytes (max > 0) data is nil, the bytes never copied out of the JS heap.
// ok is false for any other URL, once the runtime has closed, or when ctx
// ends before the JS thread takes the task, so busy plugins don't hold up
// the caller (an image lane's worker). Not from a JS thread caller that
// holds it: it waits for the JS thread.
func (r *Runtime) BlobURL(ctx context.Context, u string, max int) (data []byte, typ string, size int, ok bool) {
	if r == nil || !strings.HasPrefix(u, "blob:") {
		return nil, "", 0, false
	}
	err := r.doCtx(ctx, task{kind: "Do", what: "BlobURL", pc: callerPC(1)}, func(js *Ctx) error {
		f := js.Global().Get("__jr").Get("blobOf")
		if !f.IsFunction() {
			return nil
		}
		v, err := f.Call(u, max)
		if err != nil || v.IsNullish() {
			return err
		}
		size, typ, ok = v.Index(2).Int(), v.Index(1).String(), true
		if d := v.Index(0); !d.IsNullish() {
			data = d.Bytes()
		}
		return nil
	})
	if err != nil {
		return nil, "", 0, false
	}
	return data, typ, size, ok
}
