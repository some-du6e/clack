package jsrt

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// A blob: URL's bytes for the UI (BlobURL): a blob past the caller's max
// gives its size and no bytes, never copied out of the JS heap; and a
// caller's deadline bounds the wait while the JS thread is busy, so busy
// plugins don't hold up an image lane's worker.
func TestBlobURLCapAndDeadline(t *testing.T) {
	r := newTest(t)
	var raw json.RawMessage
	if err := r.Do(func(js *Ctx) error {
		v, err := js.Eval("blob.js", `JSON.stringify(URL.createObjectURL(new Blob([new Uint8Array(1000)], { type: 'image/png' })))`)
		if err != nil {
			return err
		}
		raw = json.RawMessage(v.String())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var u string
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if data, typ, size, ok := blobURL(r, ctx, u, 0); !ok || len(data) != 1000 || size != 1000 || typ != "image/png" {
		t.Errorf("no max: %d bytes, size %d, %q, %v; want the 1000 bytes", len(data), size, typ, ok)
	}
	if data, _, size, ok := blobURL(r, ctx, u, 999); !ok || data != nil || size != 1000 {
		t.Errorf("max 999: %d bytes, size %d, %v; want no bytes and its size, 1000", len(data), size, ok)
	}
	// the JS thread busy for 2 s: a 200 ms deadline gives up at 200 ms
	busy := make(chan struct{})
	r.Post(func(js *Ctx) { close(busy); time.Sleep(2 * time.Second) })
	<-busy
	dctx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	_, _, _, ok := blobURL(r, dctx, u, 0)
	if d := time.Since(t0); ok || d > time.Second {
		t.Errorf("with the JS thread busy: ok %v after %v; want a give-up at the 200 ms deadline", ok, d.Round(time.Millisecond))
	}
}
