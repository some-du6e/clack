package messages

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"sync"

	"gioui.org/op/paint"
)

var (
	defaultBotOnce sync.Once
	defaultBotImg  paint.ImageOp
	defaultBotOK   bool
)

// isDefaultBotAvatar reports whether url is core's DefaultBotAvatar (any size).
func isDefaultBotAvatar(url string) bool {
	return strings.HasPrefix(url, "https://a.slack-edge.com/df10d/img/avatars/ava_0002-")
}

func defaultBotAvatarImage() (paint.ImageOp, bool) {
	defaultBotOnce.Do(func() {
		img, err := png.Decode(bytes.NewReader(defaultBotAvatarPNG))
		if err != nil {
			return
		}
		rgba := image.NewRGBA(img.Bounds())
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				rgba.Set(x, y, img.At(x, y))
			}
		}
		defaultBotImg, defaultBotOK = paint.NewImageOp(rgba), true
	})
	return defaultBotImg, defaultBotOK
}
