//go:build !(linux && !android) && !(darwin && !ios && cgo) && !windows

package clipimg

import (
	"context"
	"image"
)

func write(context.Context, image.Image, *Tag, func(int64) error) error { return ErrUnsupported }
