package gif

// Clack: what the decoder charges its budget for (../CLACK_PATCHES.md).
// This file is Clack's, not upstream's.

// The fixed sizes the decoder charges, at least what Go allocates for each
// (budget_test.go measures them).
const (
	// decoderBytes: the decoder (its 1 KB tmp) and, for DecodeAll, the GIF
	decoderBytes = 2 << 10
	// bufioBytes: the bufio.Reader decode wraps a reader without ReadByte in
	bufioBytes = 4096 + 128
	// colorBytes: one colour-table entry, the color.Color in the Palette and
	// the color.RGBA boxed behind it
	colorBytes = 16 + 8
	// lzwBytes: a frame's lzw.Reader (its 4K suffix, 8K prefix and 8K
	// output tables) and its blockReader
	lzwBytes = 24 << 10
	// imageBytes: a frame's image.Paletted header
	imageBytes = 128
	// keptBytes: what DecodeAll's three per-frame slices grow by for one
	// frame, amortized (a pointer, an int and a byte, twice over)
	keptBytes = 64
	// configBytes: what DecodeConfig's header read may charge (a reader and
	// a 256-colour global table)
	configBytes = bufioBytes + 256*colorBytes
)
