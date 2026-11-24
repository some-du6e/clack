package taut

import (
	"bytes"
	"encoding/base64"
	"slices"
	"testing"
	"time"

	"github.com/some-du6e/clack/internal/emoji"
)

const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

// HMojis (izie's Taut plugin) registers each hidden emoji with Slack's
// upsertEmoji, its image an object URL its runtime made from the image it
// fetched. Clack defines the thunk: the name resolves to the URL in the
// workspace's emoji (an emoji_changed names it, so the messages drawing
// it convert again), the URL's bytes load from the runtime until revoked,
// and the emoji stays through a list reload but never reaches what the
// disk cache keeps. A value of both emoji styles, an alias and a
// non-existent name read as Slack's thunk reads them.
func TestPluginUpsertsEmojiWithObjectURL(t *testing.T) {
	h := newHarness(t)
	evs, stop := h.app.Subscribe()
	defer stop()
	var got struct {
		URL string `json:"url"`
		Err string `json:"err"`
	}
	h.jsInto(`
		const png = Uint8Array.from(atob('`+onePixelPNG+`'), (c) => c.charCodeAt(0))
		const url = URL.createObjectURL(new Blob([png], { type: 'image/png' }))
		try {
			await api.redux.dispatchThunk('upsertEmoji', [{ name: 'hm_secret', value: url, updated: 1764000000 }])
		} catch (e) {
			return { url, err: String((e && e.message) || e) }
		}
		return { url }`, &got)
	if got.Err != "" {
		t.Fatalf("dispatchThunk('upsertEmoji') rejected: %s", got.Err)
	}
	if u, _, ok := h.app.S.Emoji("hm_secret"); !ok || u != got.URL {
		t.Fatalf("hm_secret resolves to %q (%v), want its object URL %q", u, ok, got.URL)
	}
	deadline := time.After(5 * time.Second)
	for named := false; !named; {
		select {
		case ev := <-evs:
			named = ev.Type == "emoji_changed" && slices.Contains(ev.Users, "hm_secret")
		case <-deadline:
			t.Fatal("no emoji_changed named hm_secret: the messages drawing it wouldn't convert again")
		}
	}
	want, _ := base64.StdEncoding.DecodeString(onePixelPNG)
	if b, ok := blobOf(h, got.URL); !ok || !bytes.Equal(b, want) {
		t.Errorf("the object URL's bytes from the runtime: %d bytes (%v), want the PNG's %d", len(b), ok, len(want))
	}
	// a list reload keeps it; the disk cache's part never has it
	h.app.S.SetEmojiList(map[string]string{"party": "https://emoji.slack-edge.com/T0266FRGM/party/1.png"})
	if u, _, ok := h.app.S.Emoji("hm_secret"); !ok || u != got.URL {
		t.Errorf("after a list reload hm_secret resolves to %q (%v)", u, ok)
	}
	if v, ok := serverEmoji(h)["hm_secret"]; ok {
		t.Errorf("the server's emoji (the disk cache's) hold hm_secret: %q", v)
	}
	h.js(`URL.revokeObjectURL('` + got.URL + `'); return 1`)
	if _, ok := blobOf(h, got.URL); ok {
		t.Error("a revoked object URL still loads")
	}
	// Slack's reading of the other shapes
	h.js(`await api.redux.dispatchThunk('upsertEmoji', [
		{ name: 'hm_pair', value: { apple: 'https://e/apple.png', google: 'https://e/google.png' }, updated: 1 },
		{ name: 'hm_alias', value: 'alias:party', alias: 'party', is_alias: 1, updated: 1 },
		{ name: 'hm_none', isNonExistent: true, updated: 1 },
	]); return 1`)
	wantPair := map[string]string{"apple": "https://e/apple.png", "google": "https://e/google.png"}[emoji.Style()]
	if u, _, _ := h.app.S.Emoji("hm_pair"); u != wantPair {
		t.Errorf("a value with both styles resolves to %q, want the %s one", u, emoji.Style())
	}
	if u, _, _ := h.app.S.Emoji("hm_alias"); u != "https://emoji.slack-edge.com/T0266FRGM/party/1.png" {
		t.Errorf("the alias resolves to %q, want its target's", u)
	}
	if _, _, ok := h.app.S.Emoji("hm_none"); ok {
		t.Error("a non-existent name became an emoji")
	}
}
