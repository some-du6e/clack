`blue-purple-pink.lossy.webp` is a still lossy (VP8) WebP copied unchanged
from golang.org/x/image v0.33.0's `testdata/` (Copyright 2009 The Go
Authors, BSD-3-Clause, the same licence as the module itself). The
animated WebP tests in `webpanim_test.go` wrap its VP8 chunk as an ANMF
frame, with an ALPH chunk, because a lossy picture can't be written by
hand.

`lossless-alpha.webp` (48×40 RGBA gradient) and `lossless-indexed.webp`
(32×32, four colours) are lossless (VP8L) stills encoded by libwebp (via
Pillow, `lossless=True`, `method=6`), so they use the predictor,
subtract-green and colour-indexing transforms and multi-symbol prefix
codes that `webptest.VP8LSolid` does not. `webpbomb_test.go` decodes them
through the VP8L validator and asserts the pixels match x/image's own
decode, so the validator is proven not to reject real libwebp output.

Nothing in this directory comes from Slack.
