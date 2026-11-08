//go:build !(linux && !android) && !windows && !(darwin && !ios && cgo)

package composer

import (
	"context"
	"errors"
)

// Other platforms (Android, iOS, BSDs, macOS without cgo): Gio's plain
// text only; pasted lists are still detected in it.

func clipHTMLSupported() bool { return false }

func clipReadHTML(context.Context) ([]byte, error) {
	return nil, errors.New("no rich clipboard on this platform")
}
