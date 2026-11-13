package popups

import (
	"bytes"
	_ "embed"
	"image"
	"image/png"
	"sync"

	"gioui.org/layout"
	"gioui.org/op/paint"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's default app icon: getAppIcon (client-boot i+lO) falls back to
// module OYli, default_new_app_icon-936d613.png from the web client's own
// bundle (1024×1024 on a.slack-edge.com/bv1-13-br/), when the app's icons
// have no image of the size asked for. Slack serves it from the bundle the
// client already holds, so it draws at once; Clack carries a 256px copy
// (the card's 64px icon and the flexpane's 36px avatar at up to 4x) and
// never fetches it.

//go:embed assets/default_new_app_icon-936d613@256.png
var defaultAppIconPNG []byte

var (
	defaultAppIconOnce sync.Once
	defaultAppIconImg  paint.ImageOp
	defaultAppIconOK   bool
)

func defaultAppIcon() (paint.ImageOp, bool) {
	defaultAppIconOnce.Do(func() {
		img, err := png.Decode(bytes.NewReader(defaultAppIconPNG))
		if err != nil {
			return
		}
		defaultAppIconImg, defaultAppIconOK = paint.NewImageOp(img), true
	})
	return defaultAppIconImg, defaultAppIconOK
}

// appIcon draws c's icon in a size px square with radius rad px: the
// app's own image, Slack's default when it has none. It reports whether
// a picture was drawn (false while the app's own image loads).
func appIcon(gtx layout.Context, h Host, c core.AppCard, size, rad int) bool {
	sz := image.Pt(size, size)
	if c.IconDefault || c.Icon == "" {
		if img, ok := defaultAppIcon(); ok {
			theme.Image(gtx, img, sz, rad)
			return true
		}
		return false
	}
	if img, ok := h.Images().Get(c.Icon, size); ok {
		theme.Image(gtx, img, sz, rad)
		return true
	}
	return false
}
