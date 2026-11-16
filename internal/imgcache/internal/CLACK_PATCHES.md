# Vendored WebP/VP8L decoders

`webp/` and `vp8l/` are `golang.org/x/image`'s `webp` and `vp8l` packages at
**v0.33.0** (the version in the repo's `go.mod`), copied here so every
allocation a decode makes charges a byte budget the caller passes, and the
decoder fails before any allocation that would exceed it. BSD-3-Clause; each
directory keeps upstream's `LICENSE`. The `vp8` and `riff` packages they
depend on are used unchanged from the module.

The first commit of these files is upstream verbatim, so the budget changes
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

### `vp8l` (`budget.go` is new; `decode.go`, `huffman.go` patched)

- `Budget` (budget.go): a byte allowance; `Charge(n)` fails with `ErrBudget`
  when fewer than `n` bytes are left and takes nothing. Because nothing is
  ever returned, a decode's whole lifetime, not just its peak, stays within
  the budget.
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

## Updating

Re-copy the four upstream files for the new version, commit them verbatim,
then re-apply the `Clack:`-marked hunks. `vp8l/budget_test.go` cross-checks
the charged node/tree/group sizes against `unsafe.Sizeof`, and `imgcache`'s
`webpbudget_test.go` measures `TotalAlloc` across the real decode entries
and proves the vendored decode still matches x/image's pixel for pixel.
