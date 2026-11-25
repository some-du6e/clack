package slick

import (
	"context"
	"encoding/json"

	"github.com/some-du6e/clack/internal/taut"
)

// Taut's stand-in Quill for the native composers (taut/ui.go), for the
// ClearURLs check.

func composerFocus(tt *taut.Taut, kind string, on bool) { tt.ComposerFocus(kind, on) }

func pasteClipboardOn(tt *taut.Taut) bool { return tt.HasPasteClipboard() }

func preparePaste(ctx context.Context, tt *taut.Taut, kind string, ops json.RawMessage, formats map[string]any) (json.RawMessage, bool) {
	return tt.PreparePastedDelta(ctx, kind, ops, formats)
}
