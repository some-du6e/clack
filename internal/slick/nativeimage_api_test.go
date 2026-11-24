package slick

// On the red tree (the code before the size limits): what
// TestNativeImageSizeLimits names that the code had none of. An app icon
// went to the OS as the main half gave it.

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

const maxAppIcon = 1024

func appIconPNG(b []byte) ([]byte, error) {
	if _, err := decodeImageBytes(b); err != nil {
		return nil, err
	}
	return b, nil
}
