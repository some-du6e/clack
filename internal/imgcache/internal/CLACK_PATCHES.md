# Vendored image decoders and scaler

Every image decoder `imgcache` runs is vendored here so each charges the
memory it allocates to a `budget.Budget` the caller passes, before it
allocates, and fails before any allocation that would exceed it:

- `jpeg/`, `png/`, `gif/` are Go **1.24.9**'s `image/jpeg`, `image/png` and
  `image/gif` decoders (the readers, not the encoders);
- `bmp/` is `golang.org/x/image/bmp` at **v0.33.0** (the version in the
  repo's `go.mod`);
- `webp/` and `vp8l/` are `golang.org/x/image`'s `webp` and `vp8l` at
  **v0.33.0**; the `vp8` and `riff` packages they depend on are used
  unchanged from the module;
- `catmullrom/` is the kernel scaler of `golang.org/x/image/draw` at
  **v0.33.0** (`scale.go` and the `impl.go` paths `CatmullRom.Scale` runs
  for the source types imgcache decodes into an `*image.RGBA`).

`budget/` is Clack's own (not vendored): the `Budget` and the process-wide
`Pool` every decode's budget draws from (see `../decmem.go`).

All BSD-3-Clause; each directory keeps upstream's `LICENSE`. The first
commit of each vendored file is upstream verbatim, so the Clack changes
read as a diff against it. Every changed line is marked `Clack:`.

## Why

`imgcache` decodes avatars, custom emoji and thumbnails whose bytes come
from any workspace member. x/image's `vp8l` decoder sizes several
allocations from fields in the bitstream before it reads the data those
fields describe, so a few bytes can make it allocate gigabytes:

- **Huffman groups.** It built `make([]hGroup, maxGroupIndex+1)` — one index
  read from the meta prefix-code image — and decoded all five Huffman trees
  of every group, used or not. A meta image naming group 65535 made it build
  65536 groups before the first pixel. A group's trees, sized by the
  alphabets the stream declares (green up to 280 + a 2048-entry colour
  cache), are tens of KB, not the ~2.7 KB a group's fixed look-up tables
  suggest.
- **Pixels, colour caches, transform buffers, alpha planes, the lossy
  frame** are all sized from header or transform fields the same way.

The previous approach (a separate validator that walked the stream and
rejected a group count past the tile count) bounded only the count, not what
each group's trees cost, and duplicated x/image's parser beside it. This
enforces the bound where the memory is allocated.

## Changes

### `budget` (Clack's own package)

- `Budget.Charge(n)` takes `n` bytes before they're allocated, or fails,
  taking nothing, with `ErrBudget` (past the budget's own limit: the same
  bytes always need the same memory) or `ErrBusy` (the process-wide pool
  can't cover it now: other decodes hold the memory — try again shortly).
- A root `Budget` draws from a `Pool` with a non-blocking try-acquire, so
  nothing ever waits for memory: no queue, no head-of-line blocking, no
  deadlock. A `Pool`'s last reserve is kept for small budgets (emoji,
  avatars), so big decodes can't make every small one fail. `Sub` makes a
  child with a tighter limit of its own (a WebP picture's) that still draws
  from the pool.
- Nothing is returned when a decoder frees memory mid-decode, so what a
  decode charges is at least everything it allocates over its life, not
  just its peak: a pool's limit bounds what all the decodes in flight hold.

### `jpeg`, `png`, `gif`, `bmp` (`budget.go` is new in each; the readers patched)

Each reader takes a `*budget.Budget` and charges, before allocating, every
buffer the file sizes:

- **jpeg** — the image (`image.NewGray`/`NewYCbCr`), the 4th component's
  plane (`blackPix`), the progressive coefficients (`progCoeffs`), and the
  `convertToRGB`/`applyBlack` output. `DecodeConfig` is the same parser, so
  a decoy marker can't make the charge disagree with what the decode
  allocates. (`image/internal/imageutil` is std-internal, so the YCbCr→RGBA
  draw goes through `image/draw`.)
- **png** — the decoder, the zlib inflater, the palette, and each pass
  image (grayscale and truecolor with `tRNS` as the `NRGBA`/`NRGBA64` they
  decode to, every Adam7 pass, the row buffers).
- **gif** — the decoder and its reader, each colour table (and a frame's
  copy of the global one), each frame's pixels, its LZW reader, its
  de-interlace copy, and what `DecodeAll` keeps per frame.
- **bmp** — the header/palette, the image, and the row buffer.

No `init` registers these with package `image` (the registry can't pass a
budget); `imgcache` dispatches to them itself by signature (`decmem.go`),
and decodes only PNG, JPEG, GIF, WebP and BMP — TIFF and other registered
formats are refused as `image.ErrFormat`.

### `catmullrom` (`scale.go`, one `Scale` entry Clack's)

`Scale` draws exactly what `CatmullRom.Scale(dst, dst.Bounds(), src, sr,
draw.Src, nil)` draws, every pixel bit for bit, but a band of destination
rows at a time: the temporary buffer holds only the source rows a band
reads (a few MB) instead of `dw×sh` entries of 32 bytes for the whole image
(a 24 MP photo to 2048 px: 262 MB), and the buffer and the kernel weights
charge the caller's budget. `scale_test.go` checks the output equals
x/image's over a range of sizes, source types and sub-rectangles.

### `vp8l` (`budget.go` is new; `decode.go`, `huffman.go` patched)

- `Budget` (budget.go) is now an alias of `budget.Budget`, so a WebP
  picture's budget draws from the same process-wide pool as every other
  decode. `Charge(n)` fails with `ErrBudget` when fewer than `n` bytes are
  left and takes nothing. Because nothing is ever returned, a decode's whole
  lifetime, not just its peak, stays within the budget.
- `Decode`/`decodeHeader` take a `*Budget`; `DecodeConfig` reads the header
  under a tiny internal one.
- Every allocation charges first, before it's made: the decoder and its
  `bufio.Reader`, the colour caches, the pixel buffers, the colour-indexing
  transform's unpacked output, the group array, and each Huffman tree's node
  array and code-length / code scratch.
- `decodeHuffmanGroups` builds only the groups the meta image actually names
  (as libwebp's `ReadHuffmanCodes` does), numbered densely in first-use
  order, and rewrites the meta image to those numbers. Every group's codes
  are still read and checked in bitstream order — a group no tile uses is
  read into a reused scratch tree and dropped — so exactly the streams
  x/image accepts are accepted, and those it rejects are rejected, at the
  same point. Only the kept groups are retained.
- The code-length tree, the "dropped" group's trees, the code-length array
  and the canonical-codes array are reused across all the trees in a decode,
  so reading a code allocates nothing beyond the one tree a used group keeps.
  `build`, `buildSimple` and `codeLengthsToCodes` became decoder methods to
  reach that scratch and the budget.

### `webp` (`decode.go` patched)

- `Decode` takes a `*Budget`, threaded into the vendored `vp8l`; the lossy
  (`vp8`) decoder's frame and decoder allocations and the alpha plane charge
  it too, by the header size, before `vp8.DecodeFrame` runs.
- Imports the vendored `vp8l`, not the module's.
- No `init` registering the format with package `image`: the registry can't
  pass a budget, so `imgcache` registers the format itself with a budgeted
  decode (`imgcache.go`).

## Known limits

One decode holds at most `MaxDecode` (224 MiB, `../decmem.go`) at its
peak. What a pixel costs at that peak: a baseline JPEG 1.5 bytes (4:2:0)
to 3 (4:4:4); a progressive one 4.5 (4:2:0), 6 (4:2:2) or 9 (4:4:4), its
int16 coefficients given back once the image is built; a progressive
CMYK (Adobe) JPEG 12; an 8-bit PNG 1 to 4, a 16-bit one 8, an interlaced
one half as much again for its largest pass; a GIF's canvas 4, 8 when it
disposes to previous. So these normal-looking images, which decoded (with
no memory bound) under `imgcache`'s old 40 MP gate, are past it:

- a progressive 4:4:4 JPEG above about 26 MP;
- a progressive CMYK JPEG above about 19 MP;
- a 16-bit PNG above about 27 MP (less interlaced).

A Slack file's largest thumbnail stands in for such an original: the
lightbox shows it (`lbShown`), Copy image copies it and says so
("Copied a smaller version of the image"). An image with no Slack
thumbnail (an unfurl's or attachment's image, an image block) fails as
`ErrOverCap`, and its placeholder shows. Decoding those at a reduced
resolution (a JPEG's DCT scaling, as libjpeg's `scale_denom`; PNG rows
reduced as they're read) would lift the limit for the lightbox, but needs
new decode paths in the vendored readers, not a change to their charges.

## Updating

Re-copy the upstream files for the new version (Go's `image/jpeg`,
`image/png`, `image/gif`; x/image's `bmp`, `webp`, `vp8l`, and `draw`'s
scaler paths), commit them verbatim, then re-apply the `Clack:`-marked
hunks. `vp8l/budget_test.go` cross-checks the charged node/tree/group sizes
against `unsafe.Sizeof`; `catmullrom/scale_test.go` proves the banded scale
still matches x/image pixel for pixel; and `imgcache`'s `decmem_test.go`
and `webpbudget_test.go` measure `TotalAlloc` across the real decode
entries and prove the vendored decodes still match x/image's output.
