`blue-purple-pink.lossy.webp` is a still lossy (VP8) WebP copied unchanged
from golang.org/x/image v0.33.0's `testdata/` (Copyright 2009 The Go
Authors, BSD-3-Clause, the same licence as the module itself). The
animated WebP tests in `webpanim_test.go` wrap its VP8 chunk as an ANMF
frame, with an ALPH chunk, because a lossy picture can't be written by
hand.

`lossless-alpha.webp` (48×40 RGBA gradient), `lossless-indexed.webp`
(32×32, four colours), `lossless-cc.webp` (64×64, a 10-bit colour cache so
green's alphabet and Huffman tree are large) and `lossless-multigroup.webp`
(128×128, 38 Huffman groups across a meta prefix-code image, subtract-green
transform) are lossless (VP8L) stills encoded by libwebp (via Pillow,
`lossless=True`), so between them they use the predictor, subtract-green and
colour-indexing transforms, a colour cache, meta prefix codes and
multi-symbol prefix codes that `webptest.VP8LSolid` does not.
`webpbudget_test.go` decodes them through the vendored budgeted decoder and
asserts the pixels match x/image's own decode, so the decoder is proven not
to reject or alter real libwebp output.

Nothing in this directory comes from Slack.
