package taut

// What TestPluginUpsertsEmojiWithObjectURL reads through APIs the
// upsertEmoji port adds (Taut.BlobURL, emoji.Custom.ServerSnapshot).

func blobOf(h *harness, u string) ([]byte, bool) { return h.tt.BlobURL(u) }

func serverEmoji(h *harness) map[string]string { return h.app.S.CustomEmoji.ServerSnapshot() }
