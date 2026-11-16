`blue-purple-pink.lossy.webp` is a still lossy (VP8) WebP copied unchanged
from golang.org/x/image v0.33.0's `testdata/` (Copyright 2009 The Go
Authors, BSD-3-Clause, the same licence as the module itself). The
animated WebP tests in `webpanim_test.go` wrap its VP8 chunk as an ANMF
frame, with an ALPH chunk, because a lossy picture can't be written by
hand. Nothing in this directory comes from Slack.
