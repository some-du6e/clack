package imgcache

// Huffman decoding for the VP8L validator (webpvp8l.go), mirroring
// golang.org/x/image/vp8l (huffman.go and the tree readers in decode.go)
// so the validator consumes exactly the bits x/image does. Every malformed
// stream maps to errBadVP8L; x/image would reject it too (after it has
// already allocated), so an early rejection here is safe.

import "io"

// reverseBits reverses the bits in a byte.
var vlReverseBits = [256]uint8{
	0x00, 0x80, 0x40, 0xc0, 0x20, 0xa0, 0x60, 0xe0, 0x10, 0x90, 0x50, 0xd0, 0x30, 0xb0, 0x70, 0xf0,
	0x08, 0x88, 0x48, 0xc8, 0x28, 0xa8, 0x68, 0xe8, 0x18, 0x98, 0x58, 0xd8, 0x38, 0xb8, 0x78, 0xf8,
	0x04, 0x84, 0x44, 0xc4, 0x24, 0xa4, 0x64, 0xe4, 0x14, 0x94, 0x54, 0xd4, 0x34, 0xb4, 0x74, 0xf4,
	0x0c, 0x8c, 0x4c, 0xcc, 0x2c, 0xac, 0x6c, 0xec, 0x1c, 0x9c, 0x5c, 0xdc, 0x3c, 0xbc, 0x7c, 0xfc,
	0x02, 0x82, 0x42, 0xc2, 0x22, 0xa2, 0x62, 0xe2, 0x12, 0x92, 0x52, 0xd2, 0x32, 0xb2, 0x72, 0xf2,
	0x0a, 0x8a, 0x4a, 0xca, 0x2a, 0xaa, 0x6a, 0xea, 0x1a, 0x9a, 0x5a, 0xda, 0x3a, 0xba, 0x7a, 0xfa,
	0x06, 0x86, 0x46, 0xc6, 0x26, 0xa6, 0x66, 0xe6, 0x16, 0x96, 0x56, 0xd6, 0x36, 0xb6, 0x76, 0xf6,
	0x0e, 0x8e, 0x4e, 0xce, 0x2e, 0xae, 0x6e, 0xee, 0x1e, 0x9e, 0x5e, 0xde, 0x3e, 0xbe, 0x7e, 0xfe,
	0x01, 0x81, 0x41, 0xc1, 0x21, 0xa1, 0x61, 0xe1, 0x11, 0x91, 0x51, 0xd1, 0x31, 0xb1, 0x71, 0xf1,
	0x09, 0x89, 0x49, 0xc9, 0x29, 0xa9, 0x69, 0xe9, 0x19, 0x99, 0x59, 0xd9, 0x39, 0xb9, 0x79, 0xf9,
	0x05, 0x85, 0x45, 0xc5, 0x25, 0xa5, 0x65, 0xe5, 0x15, 0x95, 0x55, 0xd5, 0x35, 0xb5, 0x75, 0xf5,
	0x0d, 0x8d, 0x4d, 0xcd, 0x2d, 0xad, 0x6d, 0xed, 0x1d, 0x9d, 0x5d, 0xdd, 0x3d, 0xbd, 0x7d, 0xfd,
	0x03, 0x83, 0x43, 0xc3, 0x23, 0xa3, 0x63, 0xe3, 0x13, 0x93, 0x53, 0xd3, 0x33, 0xb3, 0x73, 0xf3,
	0x0b, 0x8b, 0x4b, 0xcb, 0x2b, 0xab, 0x6b, 0xeb, 0x1b, 0x9b, 0x5b, 0xdb, 0x3b, 0xbb, 0x7b, 0xfb,
	0x07, 0x87, 0x47, 0xc7, 0x27, 0xa7, 0x67, 0xe7, 0x17, 0x97, 0x57, 0xd7, 0x37, 0xb7, 0x77, 0xf7,
	0x0f, 0x8f, 0x4f, 0xcf, 0x2f, 0xaf, 0x6f, 0xef, 0x1f, 0x9f, 0x5f, 0xdf, 0x3f, 0xbf, 0x7f, 0xff,
}

// vlHNode is a node in a Huffman tree: children, if positive, is the index
// of the first of this node's two children; 0 is uninitialized, -1 a leaf.
type vlHNode struct {
	symbol   uint32
	children int32
}

const vlLeafNode = -1

const vlLutSize, vlLutMask = 7, 1<<7 - 1

// vlHTree is a Huffman tree with a look-up table for short codes.
type vlHTree struct {
	nodes []vlHNode
	lut   [1 << vlLutSize]uint32
}

func (h *vlHTree) insert(symbol uint32, code uint32, codeLength uint32) error {
	if symbol > 0xffff || codeLength > 0xfe {
		return errBadVP8L
	}
	baseCode := uint32(0)
	if codeLength > vlLutSize {
		baseCode = uint32(vlReverseBits[(code>>(codeLength-vlLutSize))&0xff]) >> (8 - vlLutSize)
	} else {
		baseCode = uint32(vlReverseBits[code&0xff]) >> (8 - codeLength)
		for i := 0; i < 1<<(vlLutSize-codeLength); i++ {
			h.lut[baseCode|uint32(i)<<codeLength] = symbol<<8 | (codeLength + 1)
		}
	}

	n := uint32(0)
	for jump := vlLutSize; codeLength > 0; {
		codeLength--
		if int(n) > len(h.nodes) {
			return errBadVP8L
		}
		switch h.nodes[n].children {
		case vlLeafNode:
			return errBadVP8L
		case 0:
			if len(h.nodes) == cap(h.nodes) {
				return errBadVP8L
			}
			h.nodes[n].children = int32(len(h.nodes))
			h.nodes = h.nodes[:len(h.nodes)+2]
		}
		n = uint32(h.nodes[n].children) + 1&(code>>codeLength)
		jump--
		if jump == 0 && h.lut[baseCode] == 0 {
			h.lut[baseCode] = n << 8
		}
	}

	switch h.nodes[n].children {
	case vlLeafNode:
		// no-op
	case 0:
		h.nodes[n].children = vlLeafNode
	default:
		return errBadVP8L
	}
	h.nodes[n].symbol = symbol
	return nil
}

func vlCodeLengthsToCodes(codeLengths []uint32) ([]uint32, error) {
	maxCodeLength := uint32(0)
	for _, cl := range codeLengths {
		if maxCodeLength < cl {
			maxCodeLength = cl
		}
	}
	const maxAllowedCodeLength = 15
	if len(codeLengths) == 0 || maxCodeLength > maxAllowedCodeLength {
		return nil, errBadVP8L
	}
	histogram := [maxAllowedCodeLength + 1]uint32{}
	for _, cl := range codeLengths {
		histogram[cl]++
	}
	currCode, nextCodes := uint32(0), [maxAllowedCodeLength + 1]uint32{}
	for cl := 1; cl < len(nextCodes); cl++ {
		currCode = (currCode + histogram[cl-1]) << 1
		nextCodes[cl] = currCode
	}
	codes := make([]uint32, len(codeLengths))
	for symbol, cl := range codeLengths {
		if cl > 0 {
			codes[symbol] = nextCodes[cl]
			nextCodes[cl]++
		}
	}
	return codes, nil
}

func (h *vlHTree) build(codeLengths []uint32) error {
	var nSymbols, lastSymbol uint32
	for symbol, cl := range codeLengths {
		if cl != 0 {
			nSymbols++
			lastSymbol = uint32(symbol)
		}
	}
	if nSymbols == 0 {
		return errBadVP8L
	}
	h.nodes = make([]vlHNode, 1, 2*nSymbols-1)
	if nSymbols == 1 {
		if len(codeLengths) <= int(lastSymbol) {
			return errBadVP8L
		}
		return h.insert(lastSymbol, 0, 0)
	}
	codes, err := vlCodeLengthsToCodes(codeLengths)
	if err != nil {
		return err
	}
	for symbol, cl := range codeLengths {
		if cl > 0 {
			if err := h.insert(uint32(symbol), codes[symbol], cl); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *vlHTree) buildSimple(nSymbols uint32, symbols [2]uint32, alphabetSize uint32) error {
	h.nodes = make([]vlHNode, 1, 2*nSymbols-1)
	for i := uint32(0); i < nSymbols; i++ {
		if symbols[i] >= alphabetSize {
			return errBadVP8L
		}
		if err := h.insert(symbols[i], i, nSymbols-1); err != nil {
			return err
		}
	}
	return nil
}

// next returns the next Huffman-encoded symbol from the bit-stream.
func (h *vlHTree) next(d *vlDecoder) (uint32, error) {
	var n uint32
	if d.nBits < vlLutSize {
		c, err := d.readByte()
		if err != nil {
			if err == io.EOF {
				goto slowPath
			}
			return 0, err
		}
		d.bits |= uint32(c) << d.nBits
		d.nBits += 8
	}
	n = h.lut[d.bits&vlLutMask]
	if b := n & 0xff; b != 0 {
		b--
		d.bits >>= b
		d.nBits -= b
		return n >> 8, nil
	}
	n >>= 8
	d.bits >>= vlLutSize
	d.nBits -= vlLutSize

slowPath:
	for h.nodes[n].children != vlLeafNode {
		if d.nBits == 0 {
			c, err := d.readByte()
			if err != nil {
				if err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return 0, err
			}
			d.bits = uint32(c)
			d.nBits = 8
		}
		n = uint32(h.nodes[n].children) + 1&d.bits
		d.bits >>= 1
		d.nBits--
	}
	return h.nodes[n].symbol, nil
}

const vlRepeatsCodeLength = 16

var (
	vlCodeLengthCodeOrder = [19]uint8{
		17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15,
	}
	vlRepeatBits    = [3]uint8{2, 3, 7}
	vlRepeatOffsets = [3]uint8{3, 3, 11}
)

// decodeCodeLengths decodes a tree's code lengths, themselves Huffman-coded.
func (d *vlDecoder) decodeCodeLengths(dst []uint32, codeLengthCodeLengths []uint32) error {
	h := vlHTree{}
	if err := h.build(codeLengthCodeLengths); err != nil {
		return err
	}

	maxSymbol := len(dst)
	useLength, err := d.read(1)
	if err != nil {
		return err
	}
	if useLength != 0 {
		n, err := d.read(3)
		if err != nil {
			return err
		}
		n = 2 + 2*n
		ms, err := d.read(n)
		if err != nil {
			return err
		}
		maxSymbol = int(ms) + 2
		if maxSymbol > len(dst) {
			return errBadVP8L
		}
	}

	prevCodeLength := uint32(8)
	for symbol := 0; symbol < len(dst); {
		if maxSymbol == 0 {
			break
		}
		maxSymbol--
		codeLength, err := h.next(d)
		if err != nil {
			return err
		}
		if codeLength < vlRepeatsCodeLength {
			dst[symbol] = codeLength
			symbol++
			if codeLength != 0 {
				prevCodeLength = codeLength
			}
			continue
		}

		repeat, err := d.read(uint32(vlRepeatBits[codeLength-vlRepeatsCodeLength]))
		if err != nil {
			return err
		}
		repeat += uint32(vlRepeatOffsets[codeLength-vlRepeatsCodeLength])
		if symbol+int(repeat) > len(dst) {
			return errBadVP8L
		}
		cl := uint32(0)
		if codeLength == 16 {
			cl = prevCodeLength
		}
		for ; repeat > 0; repeat-- {
			dst[symbol] = cl
			symbol++
		}
	}
	return nil
}

// decodeHuffmanTree decodes a Huffman tree into h.
func (d *vlDecoder) decodeHuffmanTree(h *vlHTree, alphabetSize uint32) error {
	useSimple, err := d.read(1)
	if err != nil {
		return err
	}
	if useSimple != 0 {
		nSymbols, err := d.read(1)
		if err != nil {
			return err
		}
		nSymbols++
		firstSymbolLengthCode, err := d.read(1)
		if err != nil {
			return err
		}
		firstSymbolLengthCode = 7*firstSymbolLengthCode + 1
		var symbols [2]uint32
		symbols[0], err = d.read(firstSymbolLengthCode)
		if err != nil {
			return err
		}
		if nSymbols == 2 {
			symbols[1], err = d.read(8)
			if err != nil {
				return err
			}
		}
		return h.buildSimple(nSymbols, symbols, alphabetSize)
	}

	nCodes, err := d.read(4)
	if err != nil {
		return err
	}
	nCodes += 4
	if int(nCodes) > len(vlCodeLengthCodeOrder) {
		return errBadVP8L
	}
	codeLengthCodeLengths := [len(vlCodeLengthCodeOrder)]uint32{}
	for i := uint32(0); i < nCodes; i++ {
		codeLengthCodeLengths[vlCodeLengthCodeOrder[i]], err = d.read(3)
		if err != nil {
			return err
		}
	}
	codeLengths := make([]uint32, alphabetSize)
	if err = d.decodeCodeLengths(codeLengths, codeLengthCodeLengths[:]); err != nil {
		return err
	}
	return h.build(codeLengths)
}
