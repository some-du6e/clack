package taut

import "context"

// What TestPluginUpsertsEmojiWithObjectURL reads through APIs the
// upsertEmoji port adds (Taut.BlobURL, emoji.Custom.ServerSnapshot).

func blobOf(h *harness, u string) ([]byte, bool) {
	b, _, ok := h.tt.BlobURL(context.Background(), u, 0)
	return b, ok
}

func serverEmoji(h *harness) map[string]string { return h.app.S.CustomEmoji.ServerSnapshot() }
