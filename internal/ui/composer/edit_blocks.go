package composer

import (
	"bytes"
	"encoding/json"
)

// withKeptBlocks is what an edit of a message saves: its new rich text,
// then the message's other blocks. Slack's saveMessageEdit (boot-shared
// IHC0) sends [...convertDeltaToBlocks(delta), ...message.blocks.filter(
// shouldPreserveBlockForEdit)], and shouldPreserveBlockForEdit (qIlP S)
// keeps every block that isn't rich_text when the edit has no opaque
// block embeds, as Clack's edit document never does: a header, divider,
// section, context or image block stays where the edit composer can't
// show it. (In plain_text_mode Slack keeps only the blocks it can't edit
// as text, editing headers, dividers and plain sections in the box;
// Clack's edit document holds none of those, so it keeps them all there
// too, rather than lose them.) orig is the message's blocks as Slack
// sent them; rich the edit's rich_text blocks.
func withKeptBlocks(rich, orig json.RawMessage) json.RawMessage {
	var olds []json.RawMessage
	if len(bytes.TrimSpace(orig)) == 0 || json.Unmarshal(orig, &olds) != nil {
		return rich
	}
	var kept []json.RawMessage
	for _, b := range olds {
		var t struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(b, &t) == nil && t.Type != "rich_text" {
			kept = append(kept, b)
		}
	}
	if len(kept) == 0 {
		return rich
	}
	var news []json.RawMessage
	if r := bytes.TrimSpace(rich); len(r) > 0 && string(r) != "null" {
		if json.Unmarshal(r, &news) != nil {
			return rich
		}
	}
	out, err := json.Marshal(append(news, kept...))
	if err != nil {
		return rich
	}
	return out
}
