package jsrt

import "strings"

// BlobURL is the bytes and MIME type of a blob: URL this runtime made
// (URL.createObjectURL) and hasn't revoked: what Chromium loads when an
// <img> or a plugin's custom emoji names one. ok is false for any other
// URL, or once the runtime has closed. Any goroutine but the JS thread's
// callers that hold it: it waits for the JS thread.
func (r *Runtime) BlobURL(u string) (data []byte, typ string, ok bool) {
	if r == nil || !strings.HasPrefix(u, "blob:") {
		return nil, "", false
	}
	err := r.Do(func(js *Ctx) error {
		f := js.Global().Get("__jr").Get("blobOf")
		if !f.IsFunction() {
			return nil
		}
		v, err := f.Call(u)
		if err != nil || v.IsNullish() {
			return err
		}
		data, typ, ok = v.Index(0).Bytes(), v.Index(1).String(), true
		return nil
	})
	if err != nil {
		return nil, "", false
	}
	return data, typ, ok
}
