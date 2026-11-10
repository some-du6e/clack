package richview

import (
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/uipref"
)

// inlineImgLimit is isImageTooLarge's 0x1400000 bytes (20MB).
const inlineImgLimit = 0x1400000

// ImageTooLarge is Slack's isImageTooLarge (boot-shared uvrc): a linked
// image over 20MB doesn't expand inline while obey_inline_img_limit is on
// (Messages & media > "Even if they’re larger than 20MB" off).
func ImageTooLarge(img *richtext.Image) bool {
	return img != nil && img.Bytes > inlineImgLimit && !uipref.Get().LargeImgs
}
