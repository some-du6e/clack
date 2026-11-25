package taut

import (
	"context"
	"encoding/json"
)

// The stand-in Quill's Go side (ui.go), for quill_test.go.

func composerFocus(tt *Taut, kind string, on bool) { tt.ComposerFocus(kind, on) }

func pasteClipboardOn(tt *Taut) bool { return tt.HasPasteClipboard() }

func preparePaste(ctx context.Context, tt *Taut, kind string, ops json.RawMessage, formats map[string]any) (json.RawMessage, bool) {
	return tt.PreparePastedDelta(ctx, kind, ops, formats)
}
