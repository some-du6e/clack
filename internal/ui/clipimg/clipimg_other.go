//go:build !(linux && !android) && !(darwin && !ios && cgo) && !windows

package clipimg

import (
	"context"
	"image"
)

func write(context.Context, image.Image, func(int64) error) error { return ErrUnsupported }
