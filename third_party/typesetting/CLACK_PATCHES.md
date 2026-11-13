# Clack's patches to go-text/typesetting v0.3.5

This directory is `github.com/go-text/typesetting@v0.3.5` from the Go
module cache, its non-test Go files, `LICENSE` files, `README.md` files,
`go.mod` and `go.sum` byte for byte (no tests, test data or `testutils`:
Clack doesn't run go-text's tests), plus the changes below. The root
`go.mod` points at it with `replace github.com/go-text/typesetting =>
./third_party/typesetting`. When an upstream release has both changes,
drop the replace and delete this directory.

The changed files are `font/clack_glyf.go` (new), `font/font.go`,
`font/glyphs.go`, `font/metrics.go`, `font/renderer.go`,
`font/variations.go`, `font/opentype/clack_bytes.go` (new),
`font/opentype/reader.go`, `fontscan/clack_mmap.go`,
`fontscan/clack_mmap_unix.go`, `fontscan/clack_mmap_windows.go`,
`fontscan/clack_mmap_other.go` (new), `fontscan/fontmap.go` and this
file. `supply-chain.yml`'s typesetting
job checks every other file against the module proxy's v0.3.5.

Why: the memory workflow (perf.yml's scale jobs) found the fonts holding
85 MB of a Windows client's heap at the Hack Club account's scale: the
embedded Lato faces 14 MB, and 71 MB of system fonts the shaper fell back
to for text in other scripts (CJK, Hangul, Devanagari, Thai, symbols).
go-text parsed every glyph outline of every face when the face loaded,
and copied every table out of fonts already in memory.

## 1. font: the glyf table parsed glyph by glyph

`font/clack_glyf.go` and the `glyf` uses in `font/font.go`,
`font/glyphs.go`, `font/metrics.go`, `font/renderer.go` and
`font/variations.go`.

`Font.glyf` was `tables.Glyf`, every glyph parsed by `tables.ParseGlyf`
in `NewFont`. It is now a `*glyfTable` holding the raw table and its loca
offsets and parsing a glyph (`tables.ParseGlyph`) the first time
something asks for it, under a mutex with an atomic fast path (a `Font`
is shared by every shaper in the process). A glyph costs 40 bytes until
it is used. Outlines, extents and variations come out the same; variable
fonts still parse every glyph when they load (`newGvar` counts every
glyph's points). One difference: a malformed glyph is empty on its own,
where `ParseGlyf` dropped the whole table, and a table whose offsets don't
fit is dropped as before.

## 2. opentype: tables of a font in memory, without copies

`font/opentype/clack_bytes.go` (`BytesResource`, `NewBytesResource`) and
`font/opentype/reader.go` (`findTableBuffer`).

`Loader.RawTable` read each table into a new buffer, even from a font
already in memory. A `Resource` that also has `Bytes() []byte` now hands
out slices of those bytes for uncompressed tables (WOFF's zlib tables are
still inflated into new buffers) and never writes into `dst`.
`gioui.org/font/opentype.Parse` and `ParseCollection` (Gio patch 16)
parse through `NewBytesResource`, so the embedded Lato's tables stay in
the binary's read-only data.

Tests: `internal/ui/theme` `TestFontTablesNotCopied` and
`TestLazyGlyphsMatchEager`.

## 3. fontscan: system fonts mapped, not read

`fontscan/clack_mmap*.go` and `FontMap.loadFont` in `fontscan/fontmap.go`.

A face the shaper falls back to (a system font, for a script the
collection lacks) was read from its file into the heap, table by table.
It is now memory-mapped read-only (`syscall.Mmap` on Linux, macOS and the
BSDs; `CreateFileMapping` and `MapViewOfFile` on Windows) and parsed in
place through patch 2's `BytesResource`. The pages are the file's: shared
with every other process using the font, reclaimable, and read only where
a table or glyph is used. The mapping stays for the process's life, like
the face in the FontMap's cache. Where mapping isn't available or fails,
the font is read as before (`loadFromDisk`, which `assertValid` still
uses for its one-off check). After patch 1, the copied tables of the
fallback faces a Windows client loaded were 32 MB of its heap.
